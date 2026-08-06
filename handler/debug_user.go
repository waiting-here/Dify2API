package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dify2api/openai"
)

const (
	debugAbuseWindow    = 10 * time.Minute // detection window
	debugAbuseThreshold = 5                // trigger alert when session count exceeds this
	debugGracePeriod    = 30 * time.Second // reconnect window after SSE disconnect

	// Capture truncation limits — applied before writing to channel.
	debugMaxReqBody  = 64 * 1024  // 64 KiB
	debugMaxInputs   = 64 * 1024  // 64 KiB total serialised inputs
	debugMaxRespBody = 256 * 1024 // 256 KiB

	// Channel sizing.
	debugChanBuf     = 10              // was 100
	debugChanByteCap = 4 * 1024 * 1024 // 4 MiB cumulative per-session

	// Session lifecycle timers.
	debugNoAttachTimeout = 10 * time.Second // auto-close if no SSE within this window
	debugIdleTimeout     = 5 * time.Minute  // close after idle
	debugMaxLifetime     = 1 * time.Hour    // hard lifetime cap
)

// debugHeaderAllowlist lists the only headers that may appear in a debug
// event.  Credentials and proxy headers are never included.
var debugHeaderAllowlist = map[string]bool{
	"content-type":    true,
	"user-agent":      true,
	"accept":          true,
	"accept-encoding": true,
	"x-request-id":    true,
}

// ---- types ----

// debugEvent is pushed to the user's SSE stream for each intercepted request.
type debugEvent struct {
	Event           string             `json:"event"` // "request", "replaced", "dropped", "idle_timeout", "session_expired", "no_attach_timeout"
	Timestamp       int64              `json:"timestamp"`
	Request         debugReqData       `json:"request"`
	Inputs          map[string]any     `json:"dify_inputs"`
	InputsTruncated bool               `json:"inputs_truncated,omitempty"`
	Response        *debugRespData     `json:"response"`
	Error           string             `json:"error,omitempty"`
	MessageLayout   []debugMessageSlot `json:"message_layout,omitempty"`
}

// debugMessageSlot describes one message position as parsed by the translator.
type debugMessageSlot struct {
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Content string `json:"content"` // truncated to 200 chars
}

type debugReqData struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body"`
	Truncated bool              `json:"truncated,omitempty"`
}

type debugRespData struct {
	Status    int    `json:"status"`
	Body      string `json:"body,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// userDebugSession represents one user's active debug session.
type userDebugSession struct {
	ch          chan debugEvent
	dryRun      bool
	active      bool
	streamEpoch uint64
	mu          sync.Mutex

	// Byte accounting — reset when channel drains to zero.
	totalBytes int64

	// Lifecycle.
	startedAt     time.Time
	noAttachTimer *time.Timer
	idleTimer     *time.Timer
	idleGen       uint64 // incremented on each idle timer reset, guards stale callbacks
	maxLifeTimer  *time.Timer
	graceTimer    *time.Timer
}

// userDebugHub manages all per-user debug sessions.
type userDebugHub struct {
	mu         sync.RWMutex
	sessions   map[int64]*userDebugSession
	startTimes map[int64][]time.Time // userID → recent session start timestamps
}

func newUserDebugHub() *userDebugHub {
	return &userDebugHub{
		sessions:   make(map[int64]*userDebugSession),
		startTimes: make(map[int64][]time.Time),
	}
}

// stopDebugTimersLocked stops every timer owned by a session. The caller must
// hold the session lock; the hub lock must be acquired first when both are
// needed, preserving the documented hub -> session lock order.
func stopDebugTimersLocked(s *userDebugSession) {
	for _, timer := range []*time.Timer{s.noAttachTimer, s.idleTimer, s.maxLifeTimer, s.graceTimer} {
		if timer != nil {
			timer.Stop()
		}
	}
	s.noAttachTimer = nil
	s.idleTimer = nil
	s.maxLifeTimer = nil
	s.graceTimer = nil
}

// ---- helpers: truncation & headers ----

// truncateUTF8 truncates s to at most maxBytes bytes, walking back to a valid
// UTF-8 rune boundary.  Reports whether any bytes were dropped.
func truncateUTF8(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	b := []byte(s)
	truncated := b[:maxBytes]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return string(truncated), true
}

// truncateRawJSON truncates raw to maxBytes.  When truncation occurs the
// result is stored as a JSON string so the outer debug event stays valid.
func truncateRawJSON(raw json.RawMessage, maxBytes int) (json.RawMessage, bool) {
	if len(raw) <= maxBytes {
		return raw, false
	}
	s := string(raw)
	truncated, _ := truncateUTF8(s, maxBytes)
	escaped, _ := json.Marshal(truncated)
	return json.RawMessage(escaped), true
}

// collectDebugHeaders returns a map containing only the allowlisted request
// headers (lower-cased).  Credentials and proxy headers are never included.
func collectDebugHeaders(r *http.Request) map[string]string {
	hdrs := make(map[string]string, len(debugHeaderAllowlist))
	for k := range r.Header {
		lower := strings.ToLower(k)
		if debugHeaderAllowlist[lower] {
			hdrs[lower] = r.Header.Get(k)
		}
	}
	return hdrs
}

// estimateEventSize returns the approximate JSON wire size of evt.
func estimateEventSize(evt debugEvent) int64 {
	data, err := json.Marshal(evt)
	if err != nil {
		return 1024 // conservative fallback
	}
	return int64(len(data))
}

// ---- timer-based session teardown ----

// closeSessionOnTimeout pushes a terminal event and then tears down the
// session.  Callers must capture the session pointer, stream epoch, and idle
// generation so stale timers are discarded.
func (h *userDebugHub) closeSessionOnTimeout(userID int64, expected *userDebugSession, streamEpoch uint64, idleGen uint64, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur, ok := h.sessions[userID]
	if !ok || cur != expected {
		return
	}
	cur.mu.Lock()
	defer cur.mu.Unlock()
	if !cur.active {
		return
	}
	// For timers that can be reset by reconnection / activity.
	switch reason {
	case "no_attach_timeout", "idle_timeout":
		if cur.streamEpoch != streamEpoch {
			return
		}
	}
	if reason == "idle_timeout" && cur.idleGen != idleGen {
		return
	}
	// Push terminal event (non-blocking — channel may be full).
	select {
	case cur.ch <- debugEvent{Event: reason, Timestamp: time.Now().Unix()}:
	default:
	}
	cur.active = false
	close(cur.ch)
	stopDebugTimersLocked(cur)
	delete(h.sessions, userID)
}

// resetIdleTimer stops the current idle timer and starts a new one.  Must be
// called while holding s.mu.
func (s *userDebugSession) resetIdleTimer(h *userDebugHub, userID int64) {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleGen++
	gen := s.idleGen
	epoch := s.streamEpoch
	s.idleTimer = time.AfterFunc(debugIdleTimeout, func() {
		h.closeSessionOnTimeout(userID, s, epoch, gen, "idle_timeout")
	})
}

// ---- hub methods ----

// start creates (or replaces) a debug session.  Returns the channel the SSE
// handler should read from and a boolean indicating whether the abuse threshold
// was exceeded.
func (h *userDebugHub) start(userID int64, dryRun bool) (chan debugEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Abuse tracking: record timestamp and check threshold.
	now := time.Now()
	times := h.startTimes[userID]
	cutoff := now.Add(-debugAbuseWindow)
	kept := make([]time.Time, 0, len(times)+1)
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	isAbuse := len(kept) > debugAbuseThreshold
	if isAbuse {
		h.startTimes[userID] = nil
	} else {
		h.startTimes[userID] = kept
	}

	if old, ok := h.sessions[userID]; ok {
		old.mu.Lock()
		if old.active {
			select {
			case old.ch <- debugEvent{Event: "replaced", Timestamp: time.Now().Unix()}:
			default:
			}
			old.active = false
			close(old.ch)
		}
		stopDebugTimersLocked(old)
		old.mu.Unlock()
	}

	ch := make(chan debugEvent, debugChanBuf)
	sess := &userDebugSession{
		ch:        ch,
		dryRun:    dryRun,
		active:    true,
		startedAt: now,
	}
	epoch := sess.streamEpoch // 0

	// No-attach timer: if SSE never connects, auto-close after the window.
	sess.noAttachTimer = time.AfterFunc(debugNoAttachTimeout, func() {
		h.closeSessionOnTimeout(userID, sess, epoch, 0, "no_attach_timeout")
	})
	// Idle timer: close if no events for the window.
	sess.idleGen++
	idleGen := sess.idleGen
	sess.idleTimer = time.AfterFunc(debugIdleTimeout, func() {
		h.closeSessionOnTimeout(userID, sess, epoch, idleGen, "idle_timeout")
	})
	// Max lifetime: hard cap from start.
	sess.maxLifeTimer = time.AfterFunc(debugMaxLifetime, func() {
		h.closeSessionOnTimeout(userID, sess, epoch, 0, "session_expired")
	})

	h.sessions[userID] = sess
	return ch, isAbuse
}

// stop closes and removes a session immediately.
func (h *userDebugHub) stop(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[userID]; ok {
		s.mu.Lock()
		if s.active {
			s.active = false
			close(s.ch)
		}
		stopDebugTimersLocked(s)
		s.mu.Unlock()
		delete(h.sessions, userID)
	}
}

// activeSession returns the current session only while it is active.
func (h *userDebugHub) activeSession(userID int64) (*userDebugSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[userID]
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return nil, false
	}
	return s, true
}

// attachStream returns the active session and advances its stream epoch.
// Cancels the no-attach timer since an SSE consumer is now connected.
// Resets the idle timer.
func (h *userDebugHub) attachStream(userID int64) (*userDebugSession, uint64, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[userID]
	if !ok {
		return nil, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return nil, 0, false
	}
	s.streamEpoch++
	if s.graceTimer != nil {
		s.graceTimer.Stop()
		s.graceTimer = nil
	}

	// Cancel no-attach timer — SSE is now attached.
	if s.noAttachTimer != nil {
		s.noAttachTimer.Stop()
		s.noAttachTimer = nil
	}
	// Reset idle timer for the new stream.
	s.resetIdleTimer(h, userID)

	return s, s.streamEpoch, true
}

// isActive reports whether the user has an active debug session.
func (h *userDebugHub) isActive(userID int64) bool {
	_, ok := h.activeSession(userID)
	return ok
}

// isDryRun reports whether active session is in dry-run mode.
func (h *userDebugHub) isDryRun(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[userID]
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && s.dryRun
}

// setDryRun toggles dry-run mode.  Returns false if no active session.
func (h *userDebugHub) setDryRun(userID int64, dryRun bool) bool {
	h.mu.RLock()
	s, ok := h.sessions[userID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return false
	}
	s.dryRun = dryRun
	return true
}

// push sends an event to the user's debug channel.  It is non-blocking with
// respect to the request path — when the channel is full or the cumulative
// byte cap is reached the oldest events are evicted.  Returns true when one or
// more events were dropped.
func (h *userDebugHub) push(userID int64, evt debugEvent) bool {
	h.mu.RLock()
	s, ok := h.sessions[userID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return false
	}

	// Reset idle timer on every push (activity heartbeat).
	s.resetIdleTimer(h, userID)

	dropped := false
	eventBytes := estimateEventSize(evt)
	dropBytes := estimateEventSize(debugEvent{Event: "dropped", Timestamp: time.Now().Unix()})

	// If the channel has drained, reset byte accounting.
	if len(s.ch) == 0 {
		s.totalBytes = 0
	}

	// Evict oldest events until cumulative byte cap is respected, leaving
	// room for the dropped-event notification in case we need it.
	for s.totalBytes+eventBytes+dropBytes > debugChanByteCap && len(s.ch) > 0 {
		old := <-s.ch
		s.totalBytes -= estimateEventSize(old)
		dropped = true
	}

	// Try non-blocking send.
	select {
	case s.ch <- evt:
		s.totalBytes += eventBytes
	default:
		// Channel buffer is full — evict events to make room for both the
		// new event and a potential dropped notification.
		select {
		case old := <-s.ch:
			s.totalBytes -= estimateEventSize(old)
			dropped = true
		default:
		}
		// Try again for the event.
		select {
		case s.ch <- evt:
			s.totalBytes += eventBytes
		default:
			dropped = true
		}
	}

	// Insert a dropped notification when events were lost.
	if dropped {
		// If channel is still full after inserting evt, evict one more.
		if len(s.ch) >= debugChanBuf {
			select {
			case old := <-s.ch:
				s.totalBytes -= estimateEventSize(old)
			default:
			}
		}
		dropEvt := debugEvent{Event: "dropped", Timestamp: time.Now().Unix()}
		select {
		case s.ch <- dropEvt:
			s.totalBytes += estimateEventSize(dropEvt)
		default:
		}
	}

	return dropped
}

// closeAfter schedules cleanup of the same session after the disconnect grace.
// Replacement sessions are protected by the expected pointer identity and epoch.
func (h *userDebugHub) closeAfter(userID int64, expected *userDebugSession, streamEpoch uint64, grace time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[userID]
	if !ok || s != expected {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.streamEpoch != streamEpoch {
		return
	}
	if s.graceTimer != nil {
		s.graceTimer.Stop()
	}
	s.graceTimer = time.AfterFunc(grace, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.sessions[userID]
		if !ok || s != expected {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.active || s.streamEpoch != streamEpoch {
			return
		}
		s.active = false
		close(s.ch)
		stopDebugTimersLocked(s)
		delete(h.sessions, userID)
	})
}

// shutdown closes every active debug stream and cancels all session timers.
// It is safe to call repeatedly after the HTTP server has stopped accepting
// work and drained its handlers.
func (h *userDebugHub) shutdown() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for userID, s := range h.sessions {
		s.mu.Lock()
		if s.active {
			s.active = false
			close(s.ch)
		}
		stopDebugTimersLocked(s)
		s.mu.Unlock()
		delete(h.sessions, userID)
	}
	h.startTimes = make(map[int64][]time.Time)
}

// ---- response capture (tee for debug interception) ----

// debugResponseCapture wraps an http.ResponseWriter to also capture
// everything written.  It implements http.Flusher so that SSE streaming
// continues to work.
type debugResponseCapture struct {
	orig        http.ResponseWriter
	buf         bytes.Buffer
	statusCode  int
	wroteHeader bool
	overflowed  bool // body exceeded debugMaxRespBody; further bytes not buffered
}

func (c *debugResponseCapture) Header() http.Header { return c.orig.Header() }

func (c *debugResponseCapture) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	// Bound the buffered preview: once debugMaxRespBody is reached, stop
	// accumulating (the response still streams to the client unchanged).
	if !c.overflowed {
		remaining := debugMaxRespBody - c.buf.Len()
		if remaining > 0 {
			if len(p) > remaining {
				c.buf.Write(p[:remaining])
				c.overflowed = true
			} else {
				c.buf.Write(p)
			}
		} else {
			c.overflowed = true
		}
	}
	return c.orig.Write(p)
}

func (c *debugResponseCapture) WriteHeader(status int) {
	if !c.wroteHeader {
		c.statusCode = status
		c.wroteHeader = true
	}
	c.orig.WriteHeader(status)
}

func (c *debugResponseCapture) Flush() {
	if f, ok := c.orig.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *debugResponseCapture) body() string {
	return c.buf.String()
}

// ---- HTTP handlers ----

// POST /api/me/debug/start
func (g *Gateway) handleDebugStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	var req struct {
		DryRun *bool `json:"dry_run"`
	}
	dryRun := true // default
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.DryRun != nil {
		dryRun = *req.DryRun
	}

	_, isAbuse := g.userDebug.start(u.ID, dryRun)
	if isAbuse && g.mailer != nil {
		g.mailer.DebugAbuse(u.Username, u.ID, debugAbuseThreshold, int(debugAbuseWindow.Minutes()))
	}
	log.Printf("[DEBUG_USER] user %d started debug (dry_run=%v)", u.ID, dryRun)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// POST /api/me/debug/stop
func (g *Gateway) handleDebugStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	g.userDebug.stop(u.ID)
	log.Printf("[DEBUG_USER] user %d stopped debug", u.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// GET /api/me/debug/status
func (g *Gateway) handleDebugStatus(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	active := g.userDebug.isActive(u.ID)
	dryRun := g.userDebug.isDryRun(u.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active":  active,
		"dry_run": dryRun,
	})
}

// GET /api/me/debug/stream — SSE endpoint.
func (g *Gateway) handleDebugStream(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	s, streamEpoch, ok := g.userDebug.attachStream(u.ID)
	if !ok {
		g.writeError(w, http.StatusBadRequest, "debug_not_active", t(g.resolveLang(r), "调试模式未开启，请先开启调试", "Debug mode is not active, please enable it first"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		g.writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			g.userDebug.closeAfter(u.ID, s, streamEpoch, debugGracePeriod)
			return
		case evt, ok := <-s.ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: {\"ok\":true}\n\n")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			// Route lifecycle events.  "done" uses its own SSE event type
			// (frontend has a dedicated listener); everything else is sent
			// as "request" so the frontend can inspect evt.Event.
			if evt.Event == "done" {
				fmt.Fprintf(w, "event: done\ndata: %s\n\n", data)
			} else {
				fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
			}
			flusher.Flush()
		}
	}
}

// POST /api/me/debug/dry-run
func (g *Gateway) handleDebugDryRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	var req struct {
		DryRun bool `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if !g.userDebug.setDryRun(u.ID, req.DryRun) {
		g.writeError(w, http.StatusBadRequest, "debug_not_active", t(g.resolveLang(r), "调试模式未开启", "Debug mode is not active"))
		return
	}
	log.Printf("[DEBUG_USER] user %d set dry_run=%v", u.ID, req.DryRun)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// ---- interception helpers (called from handleChatCompletions / charity path) ----

// debugWrap checks whether user debug is active and, if so, wraps the
// ResponseWriter so the full response body can be captured for the debug
// event.  It returns:
//
//   - (wrapped, finalize) — normal non-dry-run path; caller should defer
//     finalize() and use wrapped as the ResponseWriter.
//   - (nil, nil) — dry_run was active; a mock response has already been
//     written and the caller MUST return immediately.
//   - (w, nil) — debug is not active; proceed as normal.
func (g *Gateway) debugWrap(w http.ResponseWriter, r *http.Request, userID int64, modelName string, rawBody []byte, inputs map[string]string, messages []openai.Message) (http.ResponseWriter, func()) {
	if !g.userDebug.isActive(userID) {
		return w, nil
	}

	// Build request data with header whitelist and body truncation.
	reqBody, reqTrunc := truncateRawJSON(json.RawMessage(rawBody), debugMaxReqBody)
	reqData := debugReqData{
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   collectDebugHeaders(r),
		Body:      reqBody,
		Truncated: reqTrunc,
	}

	// Build inputs map and truncate if needed.
	inputMap, inputsTrunc := buildTruncatedInputs(inputs, debugMaxInputs)

	evt := debugEvent{
		Event:           "request",
		Timestamp:       time.Now().Unix(),
		Request:         reqData,
		Inputs:          inputMap,
		InputsTruncated: inputsTrunc,
		MessageLayout:   buildMessageLayout(messages),
	}

	// Dry-run: push event, write mock, tell caller to stop.
	if g.userDebug.isDryRun(userID) {
		g.userDebug.push(userID, evt)
		mock := mockChatCompletion(modelName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mock)
		return nil, nil
	}

	// Non-dry-run: wrap the writer so we can capture the response.
	cap := &debugResponseCapture{orig: w}
	finalize := func() {
		body := cap.body()
		status := cap.statusCode
		if status == 0 {
			status = http.StatusOK
		}
		respBody, respTrunc := truncateUTF8(body, debugMaxRespBody)
		evt.Response = &debugRespData{
			Status:    status,
			Body:      respBody,
			Truncated: cap.overflowed || respTrunc,
		}
		if status >= 400 {
			evt.Error = fmt.Sprintf("HTTP %d", status)
			if em := extractErrorMessage(body); em != "" {
				evt.Error = em
			}
		}
		g.userDebug.push(userID, evt)
	}
	return cap, finalize
}

// debugWrapError pushes a debug event for a request that failed before
// reaching Dify (translation error, etc.).
func (g *Gateway) debugWrapError(r *http.Request, userID int64, rawBody []byte, inputs map[string]string, messages []openai.Message, errMsg string, httpStatus int) {
	if !g.userDebug.isActive(userID) {
		return
	}

	reqBody, reqTrunc := truncateRawJSON(json.RawMessage(rawBody), debugMaxReqBody)
	inputMap, inputsTrunc := buildTruncatedInputs(inputs, debugMaxInputs)

	errBody := fmt.Sprintf(`{"error":{"message":"%s","type":"invalid_request_error","code":"invalid_message_sequence"}}`, errMsg)
	evt := debugEvent{
		Event:     "request",
		Timestamp: time.Now().Unix(),
		Request: debugReqData{
			Method:    r.Method,
			Path:      r.URL.Path,
			Headers:   collectDebugHeaders(r),
			Body:      reqBody,
			Truncated: reqTrunc,
		},
		Inputs:          inputMap,
		InputsTruncated: inputsTrunc,
		MessageLayout:   buildMessageLayout(messages),
		Response: &debugRespData{
			Status: httpStatus,
			Body:   errBody,
		},
		Error: errMsg,
	}
	g.userDebug.push(userID, evt)
}

// ---- helpers ----

// buildTruncatedInputs converts a string→string inputs map into the debug
// event format.  If the total JSON size exceeds maxBytes individual string
// values are truncated.  Returns the map and whether any truncation occurred.
func buildTruncatedInputs(inputs map[string]string, maxBytes int) (map[string]any, bool) {
	m := make(map[string]any, len(inputs))
	for k, v := range inputs {
		m[k] = v
	}
	data, err := json.Marshal(m)
	if err != nil || len(data) <= maxBytes {
		return m, false
	}
	// Truncate long values; try 1 KiB first, then 256 B.
	for _, cap := range []int{1024, 256} {
		truncated := false
		for k, v := range inputs {
			if len(v) > cap {
				tv, _ := truncateUTF8(v, cap)
				m[k] = tv
				truncated = true
			} else {
				m[k] = v
			}
		}
		data, _ = json.Marshal(m)
		if len(data) <= maxBytes {
			return m, truncated
		}
	}
	return m, true
}

// mockChatCompletion returns a dry-run placeholder response.
func mockChatCompletion(model string) map[string]interface{} {
	return map[string]interface{}{
		"id":      "debug-dry-run",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": "[Debug Dry-Run] 此响应为调试模式生成的模拟数据，未实际发送到 Dify。",
				},
				"finish_reason": "stop",
			},
		},
	}
}

// extractErrorMessage tries to pull a human-readable error message from a
// JSON error response body.
func extractErrorMessage(body string) string {
	if body == "" {
		return ""
	}
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &env) == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return ""
}

// buildMessageLayout builds a compact positional summary of parsed messages.
func buildMessageLayout(messages []openai.Message) []debugMessageSlot {
	if messages == nil {
		return nil
	}
	slots := make([]debugMessageSlot, len(messages))
	for i, m := range messages {
		content := string(m.Content)
		if len(content) > 200 {
			content = content[:200] + "…"
		}
		slots[i] = debugMessageSlot{
			Index:   i,
			Role:    m.Role,
			Content: content,
		}
	}
	return slots
}
