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

	"dify2api/openai"
)

const (
	debugAbuseWindow    = 10 * time.Minute // detection window
	debugAbuseThreshold = 5                 // trigger alert when session count exceeds this
)

// ---- types ----

// debugEvent is pushed to the user's SSE stream for each intercepted request.
type debugEvent struct {
	Event         string               `json:"event"` // always "request"
	Timestamp     int64                `json:"timestamp"`
	Request       debugReqData         `json:"request"`
	Inputs        map[string]any       `json:"dify_inputs"`
	Response      *debugRespData       `json:"response"`
	Error         string               `json:"error,omitempty"`
	MessageLayout []debugMessageSlot   `json:"message_layout,omitempty"`
}

// debugMessageSlot describes one message position as parsed by the translator.
type debugMessageSlot struct {
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Content string `json:"content"` // truncated to 200 chars
}

type debugReqData struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

type debugRespData struct {
	Status int    `json:"status"`
	Body   string `json:"body,omitempty"`
}

// userDebugSession represents one user's active debug session.
type userDebugSession struct {
	ch        chan debugEvent
	dryRun    bool
	active    bool
	createdAt time.Time
	mu        sync.Mutex
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
	// Filter out old timestamps outside the detection window.
	kept := make([]time.Time, 0, len(times)+1)
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	isAbuse := len(kept) > debugAbuseThreshold
	if isAbuse {
		h.startTimes[userID] = nil // reset counter to prevent repeated alerts
	} else {
		h.startTimes[userID] = kept
	}

	if old, ok := h.sessions[userID]; ok {
		// Push a "replaced" event so the old SSE consumer can close gracefully.
		select {
		case old.ch <- debugEvent{Event: "replaced", Timestamp: time.Now().Unix()}:
		default:
		}
		old.mu.Lock()
		old.active = false
		close(old.ch)
		old.mu.Unlock()
	}

	ch := make(chan debugEvent, 100)
	h.sessions[userID] = &userDebugSession{
		ch:        ch,
		dryRun:    dryRun,
		active:    true,
		createdAt: time.Now(),
	}
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
		s.mu.Unlock()
		delete(h.sessions, userID)
	}
}

// isActive reports whether the user has an active debug session.
func (h *userDebugHub) isActive(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[userID]
	return ok && s.active
}

// isDryRun reports whether active session is in dry-run mode.
func (h *userDebugHub) isDryRun(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.sessions[userID]
	return ok && s.dryRun
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

// push sends an event to the user's debug channel (non-blocking; drops if full).
func (h *userDebugHub) push(userID int64, evt debugEvent) {
	h.mu.RLock()
	s, ok := h.sessions[userID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if !active {
		return
	}
	select {
	case s.ch <- evt:
	default:
		// channel full — drop the event silently (the user can reduce
		// request rate or read faster).
	}
}

// closeAfter schedules an auto-cleanup after grace if no new SSE consumer
// reconnects (used when the SSE client disconnects).
func (h *userDebugHub) closeAfter(userID int64, grace time.Duration) {
	time.AfterFunc(grace, func() {
		h.mu.RLock()
		s, ok := h.sessions[userID]
		h.mu.RUnlock()
		if !ok {
			return
		}
		s.mu.Lock()
		if s.active && time.Since(s.createdAt) > grace {
			s.active = false
			close(s.ch)
		}
		s.mu.Unlock()
		h.mu.Lock()
		if s, ok := h.sessions[userID]; ok {
			s.mu.Lock()
			if !s.active {
				delete(h.sessions, userID)
			}
			s.mu.Unlock()
		}
		h.mu.Unlock()
	})
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
}

func (c *debugResponseCapture) Header() http.Header { return c.orig.Header() }

func (c *debugResponseCapture) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	c.buf.Write(p)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	// Grab the current channel (or nil if debug is not active).
	g.userDebug.mu.RLock()
	s, ok := g.userDebug.sessions[u.ID]
	g.userDebug.mu.RUnlock()
	if !ok || !s.active {
		g.writeError(w, http.StatusBadRequest, "debug_not_active", "调试模式未开启，请先开启调试")
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

	// Send a connected event so the client knows the stream is alive.
	fmt.Fprintf(w, "event: connected\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client disconnected — schedule graceful cleanup.
			g.userDebug.closeAfter(u.ID, 30*time.Second)
			return
		case evt, ok := <-s.ch:
			if !ok {
				// Channel closed (debug stopped).
				fmt.Fprintf(w, "event: done\ndata: {\"ok\":true}\n\n")
				flusher.Flush()
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// POST /api/me/debug/dry-run
func (g *Gateway) handleDebugDryRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		g.writeError(w, http.StatusBadRequest, "debug_not_active", "调试模式未开启")
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

	// Build the request portion of the event.
	hdrs := make(map[string]string, 4)
	for k := range r.Header {
		if len(hdrs) >= 10 {
			break
		}
		hdrs[strings.ToLower(k)] = r.Header.Get(k)
	}
	reqData := debugReqData{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: hdrs,
		Body:    json.RawMessage(rawBody),
	}

	inputMap := make(map[string]any, len(inputs))
	for k, v := range inputs {
		inputMap[k] = v
	}

	evt := debugEvent{
		Event:         "request",
		Timestamp:     time.Now().Unix(),
		Request:       reqData,
		Inputs:        inputMap,
		MessageLayout: buildMessageLayout(messages),
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
		evt.Response = &debugRespData{
			Status: status,
			Body:   body,
		}
		// If the response indicates an error, surface it in the event.
		if status >= 400 {
			evt.Error = fmt.Sprintf("HTTP %d", status)
			// Try to extract a JSON error message from the body.
			if em := extractErrorMessage(body); em != "" {
				evt.Error = em
			}
		}
		g.userDebug.push(userID, evt)
	}
	return cap, finalize
}

// debugWrapError pushes a debug event for a request that failed before
// reaching Dify (translation error, etc.).  It should be called when debug
// is active and the request is being rejected with an error.
// messages may be nil when the request couldn't even be parsed to messages.
func (g *Gateway) debugWrapError(r *http.Request, userID int64, rawBody []byte, inputs map[string]string, messages []openai.Message, errMsg string, httpStatus int) {
	if !g.userDebug.isActive(userID) {
		return
	}

	hdrs := make(map[string]string, 4)
	for k := range r.Header {
		if len(hdrs) >= 10 {
			break
		}
		hdrs[strings.ToLower(k)] = r.Header.Get(k)
	}
	inputMap := make(map[string]any, len(inputs))
	for k, v := range inputs {
		inputMap[k] = v
	}

	errBody := fmt.Sprintf(`{"error":{"message":"%s","type":"invalid_request_error","code":"invalid_message_sequence"}}`, errMsg)
	evt := debugEvent{
		Event:     "request",
		Timestamp: time.Now().Unix(),
		Request: debugReqData{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: hdrs,
			Body:    json.RawMessage(rawBody),
		},
		Inputs:        inputMap,
		MessageLayout: buildMessageLayout(messages),
		Response: &debugRespData{
			Status: httpStatus,
			Body:   errBody,
		},
		Error: errMsg,
	}
	g.userDebug.push(userID, evt)
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

// debugGraceSec is the window during which a reconnecting SSE client can
// resume a debug session after disconnection.
const debugGraceSec = 30

// extractErrorMessage tries to pull a human-readable error message from a
// JSON error response body (Dify2API or OpenAI error format).  Returns ""
// when the body is not a recognised error envelope.
func extractErrorMessage(body string) string {
	if body == "" {
		return ""
	}
	// Try standard Dify2API error envelope: {"error":{"message":"..."}}
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

// ---- admin host allow-list: /api/me/debug/* endpoints are user endpoints ----
// The hostSeparation middleware already allows /api/me on the user host.
// Admin host does NOT expose /api/me/debug/* (they would 404 via hostSeparation).
// No additional middleware config needed.

// ---- Gateway field addition ----
// userDebug is set in NewGateway (see handler.go).

// buildMessageLayout builds a compact positional summary of parsed messages
// showing each message's index, role, and truncated content.  Returns nil
// when messages is nil (caller couldn't parse the request at all).
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
