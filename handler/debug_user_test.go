package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"dify2api/auth"
	"dify2api/db"
)

// newUserSession creates a regular (non-admin) user and returns a session
// cookie, the gateway, store, and user.
func newUserSession(t *testing.T) (*Gateway, *db.Store, *db.User, *http.Cookie) {
	t.Helper()
	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("42", "tester", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}
	return gw, store, u, cookie
}

func doDebugRequest(t *testing.T, gw *Gateway, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ---- Start / Stop / Status ----

func TestDebugStartStopStatus(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	// Initially not active.
	rec := doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: code = %d", rec.Code)
	}
	var st map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&st)
	if st["active"] != false {
		t.Fatalf("status: active should be false, got %v", st["active"])
	}

	// Start debug (with dry_run=true).

	// Start debug (with dry_run=true).
	rec = doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Should be active now.
	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	json.NewDecoder(rec.Body).Decode(&st)
	if st["active"] != true {
		t.Errorf("status after start: active should be true, got %v", st["active"])
	}
	if st["dry_run"] != true {
		t.Errorf("status after start: dry_run should be true, got %v", st["dry_run"])
	}

	// Stop debug.
	rec = doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/stop", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Should be inactive now.
	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	json.NewDecoder(rec.Body).Decode(&st)
	if st["active"] != false {
		t.Fatalf("status after stop: active should be false, got %v", st["active"])
	}
}

func TestDebugStart_DefaultsToDryRun(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	rec := doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	var stDef map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&stDef)
	if stDef["dry_run"] != true {
		t.Fatalf("default dry_run should be true, got %v", stDef["dry_run"])
	}
}

func TestDebugStart_ExplicitDryRunFalse(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	rec := doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":false}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	var stExpl map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&stExpl)
	if stExpl["dry_run"] != false {
		t.Fatalf("dry_run should be false, got %v", stExpl["dry_run"])
	}
}

func TestDebugRequiresAuth(t *testing.T) {
	gw, _, _, _ := newUserSession(t)

	paths := []string{
		"/api/me/debug/start",
		"/api/me/debug/stop",
		"/api/me/debug/status",
		"/api/me/debug/stream",
		"/api/me/debug/dry-run",
	}
	for _, p := range paths {
		method := http.MethodGet
		if strings.Contains(p, "start") || strings.Contains(p, "stop") || strings.Contains(p, "dry-run") {
			method = http.MethodPost
		}
		rec := doDebugRequest(t, gw, method, p, "", nil) // no cookie
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without auth: code = %d, want 401", p, rec.Code)
		}
	}
}

// ---- Dry-Run Toggle ----

func TestDebugDryRunToggle(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	// Start debug with dry_run=true.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	// Toggle to false.
	rec := doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/dry-run", `{"dry_run":false}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle dry_run: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	var st map[string]interface{}
	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	json.NewDecoder(rec.Body).Decode(&st)
	if st["dry_run"] != false {
		t.Fatalf("dry_run should be false after toggle, got %v", st["dry_run"])
	}

	// Toggle back to true.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/dry-run", `{"dry_run":true}`, cookie)
	rec = doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/status", "", cookie)
	json.NewDecoder(rec.Body).Decode(&st)
	if st["dry_run"] != true {
		t.Fatalf("dry_run should be true, got %v", st["dry_run"])
	}
}

func TestDebugDryRun_RequiresActiveSession(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	// Toggle without starting debug first.
	rec := doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/dry-run", `{"dry_run":false}`, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("toggle without active debug: code = %d, want 400", rec.Code)
	}
}

// ---- SSE Stream ----

func TestDebugStream_RequiresActiveDebug(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	rec := doDebugRequest(t, gw, http.MethodGet, "/api/me/debug/stream", "", cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stream without active debug: code = %d, want 400", rec.Code)
	}
}

func TestDebugStream_SendsConnectedEvent(t *testing.T) {
	gw, _, _, cookie := newUserSession(t)

	// Start debug.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	// Connect to SSE stream in a goroutine, then stop debug to trigger "done" event.
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/me/debug/stream", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler time to write headers and the connected event.
	time.Sleep(100 * time.Millisecond)

	// Stop debug — this closes the channel and triggers "done".
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/stop", "", cookie)

	// Wait for the handler to finish.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream handler did not return after stop")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Fatalf("SSE stream should contain 'event: connected', got: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("SSE stream should contain 'event: done', got: %s", body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("SSE stream: code = %d", rec.Code)
	}
}

// ---- Debug Interception: Dry-Run (mock response) ----

func TestDebugDryRun_InterceptsChat(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store, u, cookie := newUserSession(t)

	// Set up app config pointing at mock Dify.
	_, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	model := "[general]test-model"
	_, err = store.CreateAppConfig(u.ID, model, srv.URL, "app-secret", "")
	if err != nil {
		t.Fatalf("create app config: %v", err)
	}

	// Start debug in dry-run mode.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	// Get caller key.
	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	// Make a chat request using caller key (not session cookie).
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	chatBody := `{"model":"[general]test-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Dry-run: should return 200 + mock response, NOT forward to Dify.
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run chat: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	var chatResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&chatResp)

	// Should be a mock response (id = "debug-dry-run").
	if chatResp["id"] != "debug-dry-run" {
		t.Fatalf("dry-run response should have id='debug-dry-run', got: %v", chatResp)
	}

	// Dify should NOT have been called.
	if len(captured) > 0 {
		t.Fatalf("Dify should not have been called in dry-run mode, but captured: %v", captured)
	}

	// The debug event should have been pushed (check via the channel).
	// We verify indirectly by checking that the hub is still active and
	// an event was queued.
	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should still be active after dry-run interception")
	}
	// Drain the channel to verify an event was pushed.
	select {
	case evt := <-sess.ch:
		if evt.Event != "request" {
			t.Fatalf("expected event='request', got %q", evt.Event)
		}
		if evt.Request.Method != "POST" {
			t.Fatalf("expected POST, got %s", evt.Request.Method)
		}
		if evt.Response != nil {
			t.Fatal("dry-run event should have nil Response")
		}
	default:
		t.Fatal("expected a debug event in the channel")
	}
}

// ---- Debug Interception: Non-Dry-Run (capture response) ----

func TestDebugNonDryRun_CapturesResponse(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store, u, cookie := newUserSession(t)
	allowDifyTestOrigin(t, gw, srv.URL)

	// Set up app config pointing at mock Dify.
	_, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	model := "[general]test-model"
	_, err = store.CreateAppConfig(u.ID, model, srv.URL, "app-secret", "")
	if err != nil {
		t.Fatalf("create app config: %v", err)
	}

	// Start debug with dry_run=false.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":false}`, cookie)

	// Get caller key.
	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	// Make a chat request (non-streaming).
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	chatBody := `{"model":"[general]test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Non-dry-run: should forward to Dify and get real response.
	if rec.Code != http.StatusOK {
		t.Fatalf("chat: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Dify should have been called.
	if len(captured) == 0 {
		t.Fatal("Dify should have been called in non-dry-run mode")
	}

	// Debug event should be pushed with response data.
	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should still be active")
	}

	select {
	case evt := <-sess.ch:
		if evt.Event != "request" {
			t.Fatalf("expected event='request', got %q", evt.Event)
		}
		if evt.Response == nil {
			t.Fatal("non-dry-run event should have Response")
		}
		if evt.Response.Status != http.StatusOK {
			t.Fatalf("expected response status 200, got %d", evt.Response.Status)
		}
		if evt.Response.Body == "" {
			t.Fatal("response body should not be empty")
		}
		if !strings.Contains(evt.Response.Body, "MOCK_REPLY") {
			t.Fatalf("response body should contain MOCK_REPLY, got: %s", evt.Response.Body)
		}
	default:
		t.Fatal("expected a debug event in the channel")
	}
}

func TestDebugResponseCapture_BoundedBuffer(t *testing.T) {
	orig := httptest.NewRecorder()
	cap := &debugResponseCapture{orig: orig}

	// Two writes that together far exceed the capture cap.
	big1 := make([]byte, 200*1024)
	big2 := make([]byte, 200*1024)
	for i := range big1 {
		big1[i] = 'a'
	}
	for i := range big2 {
		big2[i] = 'b'
	}
	if _, err := cap.Write(big1); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := cap.Write(big2); err != nil {
		t.Fatalf("second write: %v", err)
	}

	if cap.buf.Len() > debugMaxRespBody {
		t.Fatalf("buffered preview exceeds cap: %d > %d", cap.buf.Len(), debugMaxRespBody)
	}
	if !cap.overflowed {
		t.Fatal("overflowed flag should be set when the body exceeds the cap")
	}
	// The client-facing stream must be unaffected.
	if orig.Body.Len() != len(big1)+len(big2) {
		t.Fatalf("client stream truncated: got %d, want %d", orig.Body.Len(), len(big1)+len(big2))
	}
	// The preview contains the leading bytes, not a suffix.
	body := cap.body()
	if !strings.HasPrefix(body, "aaaa") {
		t.Fatal("buffered preview should contain the leading bytes")
	}
}

func TestDebugNonDryRun_LargeResponseTruncated(t *testing.T) {
	// Mock Dify returning a ~300 KiB response body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(404)
			return
		}
		bigText := strings.Repeat("x", 300*1024)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"task_id":"t","workflow_run_id":"w","data":{"id":"x","status":"succeeded","outputs":{"text":%q}}}`, bigText)
	}))
	defer srv.Close()

	gw, store, u, cookie := newUserSession(t)
	allowDifyTestOrigin(t, gw, srv.URL)
	if _, err := store.SetCallerKey(u.ID); err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	if _, err := store.CreateAppConfig(u.ID, "[general]test-model", srv.URL, "app-secret", ""); err != nil {
		t.Fatalf("create app config: %v", err)
	}
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":false}`, cookie)

	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	chatBody := `{"model":"[general]test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("chat: code = %d, body = %s", rec.Code, rec.Body.String())
	}
	// Client must receive the full response even though the preview is capped.
	if rec.Body.Len() <= debugMaxRespBody {
		t.Fatalf("client response should be larger than the capture cap, got %d", rec.Body.Len())
	}

	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should still be active")
	}

	select {
	case evt := <-sess.ch:
		if evt.Event != "request" {
			t.Fatalf("expected event='request', got %q", evt.Event)
		}
		if evt.Response == nil {
			t.Fatal("expected Response in event")
		}
		if !evt.Response.Truncated {
			t.Fatal("response should be marked truncated")
		}
		if len(evt.Response.Body) > debugMaxRespBody {
			t.Fatalf("response preview exceeds cap: %d > %d", len(evt.Response.Body), debugMaxRespBody)
		}
	default:
		t.Fatal("expected a debug event in the channel")
	}
}

// ---- Debug Interception: Translation Error ----

func TestDebugError_InvalidMessages(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store, u, cookie := newUserSession(t)

	_, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	model := "[general]test-model"
	_, err = store.CreateAppConfig(u.ID, model, srv.URL, "app-secret", "")
	if err != nil {
		t.Fatalf("create app config: %v", err)
	}

	// Start debug with dry_run=false.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":false}`, cookie)

	// Get caller key.
	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	// Make a chat request with an invalid message sequence (only assistant message).
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	chatBody := `{"model":"[general]test-model","messages":[{"role":"assistant","content":"I am AI"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Should return error.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid messages: code = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Debug event should be pushed with error.
	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should still be active")
	}

	select {
	case evt := <-sess.ch:
		if evt.Error == "" {
			t.Fatal("expected error in debug event for invalid messages")
		}
		if evt.Response == nil {
			t.Fatal("expected Response in debug event")
		}
		if evt.Response.Status != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", evt.Response.Status)
		}
	default:
		t.Fatal("expected a debug event in the channel")
	}
}

// ---- Debug Interception: Replaces Old Session ----

func TestDebugStart_ReplacesOldSession(t *testing.T) {
	gw, _, u, cookie := newUserSession(t)

	// Start first debug session.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	gw.userDebug.mu.RLock()
	first := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()

	// Start second — should close the first channel and create new one.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":false}`, cookie)

	gw.userDebug.mu.RLock()
	second := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()

	if first == second {
		t.Fatal("second start should create a new session")
	}

	// First channel should have a "replaced" event, then be closed.
	select {
	case evt, ok := <-first.ch:
		if !ok {
			t.Fatal("first channel should still be open for the replaced event")
		}
		if evt.Event != "replaced" {
			t.Fatalf("expected replaced event, got %q", evt.Event)
		}
	default:
		t.Fatal("expected a replaced event in the first channel")
	}
	// After the replaced event, channel should be closed.
	_, ok := <-first.ch
	if ok {
		t.Fatal("first channel should be closed after replaced event")
	}
}

// drainDebugChan drains a debug event channel in a goroutine to prevent
// blocking when start() replaces the previous session.
func drainDebugChan(ch chan debugEvent) {
	for range ch {
	}
}

func TestUserDebugHubShutdownClosesSessionsAndTimers(t *testing.T) {
	hub := newUserDebugHub()
	ch, _ := hub.start(42, true)
	s, epoch, ok := hub.attachStream(42)
	if !ok {
		t.Fatal("attachStream failed")
	}
	hub.closeAfter(42, s, epoch, time.Hour)
	hub.shutdown()
	if hub.isActive(42) {
		t.Fatal("session remained active after shutdown")
	}
	if _, ok := <-ch; ok {
		t.Fatal("session channel remained open after shutdown")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.noAttachTimer != nil || s.idleTimer != nil || s.maxLifeTimer != nil || s.graceTimer != nil {
		t.Fatal("session timers remained armed after shutdown")
	}
}

// ---- Debug Abuse Detection ----

func TestDebugAbuse_TriggersAlert(t *testing.T) {
	gw, _, u, _ := newUserSession(t)
	hub := gw.userDebug

	// Call start() 6 times rapidly; first 5 should not trigger, 6th should.
	for i := 0; i < 5; i++ {
		ch, isAbuse := hub.start(u.ID, true)
		if isAbuse {
			t.Fatalf("start #%d: expected isAbuse=false, got true", i+1)
		}
		// Drain the old channel so it doesn't block (each start() replaces
		// the previous session and closes the old channel after "replaced").
		go drainDebugChan(ch)
	}
	// 6th call should exceed the threshold of 5.
	ch, isAbuse := hub.start(u.ID, true)
	if !isAbuse {
		t.Fatal("6th start should trigger abuse alert")
	}
	go drainDebugChan(ch)

	// After abuse trigger, startTimes should be cleared.
	// A 7th call should NOT trigger abuse (counter reset).
	ch2, isAbuse2 := hub.start(u.ID, true)
	if isAbuse2 {
		t.Fatal("after abuse counter reset, next start should not trigger abuse")
	}
	go drainDebugChan(ch2)
}

func TestDebugAbuse_WindowExpiry(t *testing.T) {
	gw, _, u, _ := newUserSession(t)
	hub := gw.userDebug

	// Directly insert an old timestamp outside the detection window.
	hub.mu.Lock()
	hub.startTimes[u.ID] = []time.Time{time.Now().Add(-20 * time.Minute)}
	hub.mu.Unlock()

	// Call start() once — the old timestamp (20 min ago) should be cleaned
	// because it's outside the 10-min window, leaving only 1 recent timestamp.
	ch, isAbuse := hub.start(u.ID, true)
	if isAbuse {
		t.Fatal("old timestamp outside window should be cleaned, not trigger abuse")
	}
	go drainDebugChan(ch)
}

func TestUserDebugHub_ConcurrentPushStopAndReplace(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(99)
	hub.start(userID, true)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				hub.push(userID, debugEvent{Event: "request"})
				_ = hub.isActive(userID)
				_ = hub.isDryRun(userID)
				hub.setDryRun(userID, i%2 == 0)
			}
		}()
	}
	for i := 0; i < 100; i++ {
		hub.stop(userID)
		hub.start(userID, i%2 == 0)
	}
	wg.Wait()
	hub.stop(userID)
}

func TestUserDebugHub_ReconnectAndReplacementInvalidateCleanup(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(100)
	hub.start(userID, true)

	first, firstEpoch, ok := hub.attachStream(userID)
	if !ok {
		t.Fatal("first stream did not attach")
	}
	hub.closeAfter(userID, first, firstEpoch, 10*time.Millisecond)
	if _, _, ok := hub.attachStream(userID); !ok {
		t.Fatal("reconnecting stream did not attach")
	}
	time.Sleep(25 * time.Millisecond)
	if !hub.isActive(userID) {
		t.Fatal("old stream timer closed a reconnected session")
	}

	current, epoch, _ := hub.attachStream(userID)
	hub.closeAfter(userID, current, epoch, 10*time.Millisecond)
	hub.start(userID, false) // replacement must not be closed by old timer
	time.Sleep(25 * time.Millisecond)
	if !hub.isActive(userID) {
		t.Fatal("old stream timer closed a replacement session")
	}
	if hub.isDryRun(userID) {
		t.Fatal("replacement session state was not preserved")
	}
	hub.stop(userID)
}

// ---- Truncation helpers ----

func TestTruncateUTF8_NoTruncation(t *testing.T) {
	s := "hello"
	out, trunc := truncateUTF8(s, 100)
	if trunc {
		t.Fatal("should not truncate short string")
	}
	if out != s {
		t.Fatalf("expected %q, got %q", s, out)
	}
}

func TestTruncateUTF8_Truncates(t *testing.T) {
	s := "hello world"
	out, trunc := truncateUTF8(s, 5)
	if !trunc {
		t.Fatal("should report truncation")
	}
	if out != "hello" {
		t.Fatalf("expected 'hello', got %q", out)
	}
}

func TestTruncateUTF8_MultiByteBoundary(t *testing.T) {
	// "こんにちは" is 15 bytes (5 runes × 3 bytes each).
	s := "こんにちは"
	// Truncate at 7 bytes — midway through the 3rd rune (に).
	out, trunc := truncateUTF8(s, 7)
	if !trunc {
		t.Fatal("should report truncation")
	}
	if !utf8.ValidString(out) {
		t.Fatal("output must be valid UTF-8")
	}
	// Should have backed off to 6 bytes (2 runes = "こん").
	if out != "こん" {
		t.Fatalf("expected first 2 runes 'こん', got %q (%d bytes)", out, len(out))
	}
}

func TestTruncateUTF8_ExactBoundary(t *testing.T) {
	// Truncate exactly on a rune boundary — still reports truncation
	// because maxBytes (6) < len(s) (15).
	s := "こんにちは"                     // 15 bytes, 5 runes
	out, trunc := truncateUTF8(s, 6) // exactly 2 runes (6 bytes)
	if !trunc {
		t.Fatal("should report truncation since bytes were dropped")
	}
	if out != "こん" {
		t.Fatalf("expected 'こん', got %q", out)
	}
	if !utf8.ValidString(out) {
		t.Fatal("output must be valid UTF-8")
	}
}

func TestTruncateRawJSON_NoTruncation(t *testing.T) {
	raw := json.RawMessage(`{"key":"value"}`)
	out, trunc := truncateRawJSON(raw, 100)
	if trunc {
		t.Fatal("should not truncate")
	}
	if string(out) != string(raw) {
		t.Fatalf("expected %s, got %s", raw, out)
	}
}

func TestTruncateRawJSON_TruncatedProducesValidJSON(t *testing.T) {
	raw := json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	out, trunc := truncateRawJSON(raw, 30)
	if !trunc {
		t.Fatal("should report truncation")
	}
	// Wrap in an outer struct to verify the truncated body serialises as valid JSON.
	outer := struct {
		Body json.RawMessage `json:"body"`
	}{Body: out}
	data, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("outer JSON must be valid after truncation: %v", err)
	}
	// Unmarshal back to verify.
	var back map[string]interface{}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("cannot unmarshal outer JSON: %v (data=%s)", err, data)
	}
	if _, ok := back["body"]; !ok {
		t.Fatal("body field missing")
	}
}

// ---- Header whitelist (integration via handler) ----

func TestDebugHeaderAllowlist_SafeHeadersPassThrough(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, _, u, cookie := newUserSession(t)

	_, err := gw.Store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	model := "[general]test-model"
	_, err = gw.Store.CreateAppConfig(u.ID, model, srv.URL, "app-secret", "")
	if err != nil {
		t.Fatalf("create app config: %v", err)
	}

	// Start debug in dry-run mode.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	// Content > 20 chars to pass anti-abuse MinChars check.
	chatBody := `{"model":"[general]test-model","messages":[{"role":"user","content":"This is a test message that is long enough to bypass anti-abuse checks."}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	req.Header.Set("Cookie", "session=abc123")
	req.Header.Set("X-Api-Key", "sk-secret")
	req.Header.Set("Proxy-Authorization", "Basic secret")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Diagnostic: if the chat request failed, surface the error now.
	if rec.Code != http.StatusOK {
		t.Fatalf("chat request failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should be active")
	}

	select {
	case evt := <-sess.ch:
		hdrs := evt.Request.Headers
		if _, ok := hdrs["content-type"]; !ok {
			t.Fatal("content-type should be present")
		}
		if _, ok := hdrs["user-agent"]; !ok {
			t.Fatal("user-agent should be present")
		}
		if _, ok := hdrs["accept"]; !ok {
			t.Fatal("accept should be present")
		}
		if _, ok := hdrs["x-request-id"]; !ok {
			t.Fatal("x-request-id should be present")
		}
		for _, forbidden := range []string{"authorization", "cookie", "proxy-authorization", "x-api-key", "x-forwarded-for", "x-custom-header"} {
			if _, ok := hdrs[forbidden]; ok {
				t.Fatalf("header %q should NOT be present in debug event", forbidden)
			}
		}
	default:
		t.Fatal("expected a debug event")
	}
}

// ---- Body / response truncation ----

func TestDebugBodyTruncation_LargeRequest(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, _, u, cookie := newUserSession(t)

	_, err := gw.Store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	model := "[general]test-model"
	_, err = gw.Store.CreateAppConfig(u.ID, model, srv.URL, "app-secret", "")
	if err != nil {
		t.Fatalf("create app config: %v", err)
	}

	// Start debug in dry-run mode.
	doDebugRequest(t, gw, http.MethodPost, "/api/me/debug/start", `{"dry_run":true}`, cookie)

	keyRec := doDebugRequest(t, gw, http.MethodGet, "/api/caller-key", "", cookie)
	var keyResp struct {
		Key string `json:"key"`
	}
	json.NewDecoder(keyRec.Body).Decode(&keyResp)

	// Build a request body that exceeds debugMaxReqBody (64 KiB).
	// Must also have enough content to pass anti-abuse MinChars (20).
	bigContent := strings.Repeat("x", 70000)
	chatBody := `{"model":"[general]test-model","messages":[{"role":"user","content":"` + bigContent + `"}]}`

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+keyResp.Key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Diagnostic.
	if rec.Code != http.StatusOK {
		t.Fatalf("chat request failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	gw.userDebug.mu.RLock()
	sess, ok := gw.userDebug.sessions[u.ID]
	gw.userDebug.mu.RUnlock()
	if !ok || !sess.active {
		t.Fatal("debug session should be active")
	}

	select {
	case evt := <-sess.ch:
		if !evt.Request.Truncated {
			t.Fatal("request body should be marked truncated")
		}
		if len(evt.Request.Body) >= len(chatBody) {
			t.Fatalf("body should be truncated: len=%d original=%d", len(evt.Request.Body), len(chatBody))
		}
		_, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("debug event with truncated body must be valid JSON: %v", err)
		}
	default:
		t.Fatal("expected a debug event")
	}
}

// ---- Channel eviction & byte cap ----

func TestDebugChannelEviction(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(200)

	hub.start(userID, true)
	hub.mu.RLock()
	s := hub.sessions[userID]
	hub.mu.RUnlock()

	// Drain any existing events.
drainLoop:
	for {
		select {
		case <-s.ch:
		default:
			break drainLoop
		}
	}

	// Push many medium-sized events to exceed the 4 MiB cumulative byte cap.
	bigStr := strings.Repeat("x", 200*1024) // 200 KiB
	droppedCount := 0
	for i := 0; i < 40; i++ {
		evt := debugEvent{
			Event:     "request",
			Timestamp: time.Now().Unix(),
			Request: debugReqData{
				Method: "POST",
				Path:   "/v1/chat/completions",
				Body:   json.RawMessage(`"` + bigStr + `"`),
			},
		}
		if hub.push(userID, evt) {
			droppedCount++
		}
	}

	// We should have seen at least one dropped event due to byte cap.
	if droppedCount == 0 {
		t.Fatal("expected events to be dropped due to byte cap")
	}

	// The channel should contain a "dropped" notification.
	foundDropped := false
drainLoop2:
	for {
		select {
		case evt := <-s.ch:
			if evt.Event == "dropped" {
				foundDropped = true
			}
		default:
			break drainLoop2
		}
	}
	if !foundDropped {
		t.Fatal("expected a 'dropped' event in channel after eviction")
	}

	hub.stop(userID)
}

// ---- No-attach auto-close ----

func TestDebugNoAttachTimeout_ClosesSession(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(300)

	hub.start(userID, true)
	if !hub.isActive(userID) {
		t.Fatal("session should be active immediately after start")
	}

	// Wait for the no-attach timeout (10s) plus a small buffer.
	time.Sleep(debugNoAttachTimeout + 500*time.Millisecond)

	if hub.isActive(userID) {
		t.Fatal("session should be auto-closed after no-attach timeout")
	}
}

func TestDebugNoAttachTimeout_AttachPreventsClose(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(301)

	hub.start(userID, true)

	// Attach SSE immediately — should cancel the no-attach timer.
	_, _, ok := hub.attachStream(userID)
	if !ok {
		t.Fatal("attachStream failed")
	}

	// Wait past the no-attach timeout.
	time.Sleep(debugNoAttachTimeout + 500*time.Millisecond)

	if !hub.isActive(userID) {
		t.Fatal("session should still be active because SSE was attached")
	}

	hub.stop(userID)
}

// ---- closeSessionOnTimeout logic ----

func TestDebugCloseSessionOnTimeout_IdleTimeout(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(400)

	hub.start(userID, true)
	if !hub.isActive(userID) {
		t.Fatal("session should be active")
	}

	hub.mu.RLock()
	s := hub.sessions[userID]
	hub.mu.RUnlock()

	// Simulate an idle-timeout firing with the correct epoch and gen.
	s.mu.Lock()
	epoch := s.streamEpoch
	gen := s.idleGen
	s.mu.Unlock()

	hub.closeSessionOnTimeout(userID, s, epoch, gen, "idle_timeout")

	if hub.isActive(userID) {
		t.Fatal("session should be closed after idle_timeout callback")
	}
}

func TestDebugCloseSessionOnTimeout_WrongGenIgnored(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(401)

	hub.start(userID, true)
	if !hub.isActive(userID) {
		t.Fatal("session should be active")
	}

	hub.mu.RLock()
	s := hub.sessions[userID]
	hub.mu.RUnlock()

	// Reset idle timer (increments gen).
	s.mu.Lock()
	s.resetIdleTimer(hub, userID)
	newGen := s.idleGen
	s.mu.Unlock()

	// Call with old gen — should be ignored.
	hub.closeSessionOnTimeout(userID, s, 0, 0, "idle_timeout")

	if !hub.isActive(userID) {
		t.Fatal("session should still be active — stale gen was ignored")
	}

	// Call with correct gen — should close.
	// After resetIdleTimer, epoch is still 0 (attachStream was not called).
	hub.closeSessionOnTimeout(userID, s, 0, newGen, "idle_timeout")
	if hub.isActive(userID) {
		t.Fatal("session should be closed with correct gen")
	}
}

func TestDebugCloseSessionOnTimeout_SessionExpired(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(402)

	hub.start(userID, true)

	hub.mu.RLock()
	s := hub.sessions[userID]
	hub.mu.RUnlock()

	// Attach SSE (increments epoch).
	hub.attachStream(userID)

	// session_expired should close even though epoch changed.
	hub.closeSessionOnTimeout(userID, s, 0, 0, "session_expired")

	if hub.isActive(userID) {
		t.Fatal("session_expired should close regardless of epoch changes")
	}
}

// ---- Push return value ----

func TestDebugPush_ReturnsDroppedWhenEvicting(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(500)

	hub.start(userID, true)

	// Push enough large events to fill the byte cap.
	bigStr := strings.Repeat("x", 500*1024) // 500 KiB
	dropped := false
	for i := 0; i < 20; i++ {
		evt := debugEvent{
			Event: "request",
			Request: debugReqData{
				Method: "POST",
				Path:   "/v1/chat/completions",
				Body:   json.RawMessage(`"` + bigStr + `"`),
			},
		}
		if hub.push(userID, evt) {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("push should return true when events are evicted")
	}

	hub.stop(userID)
}

// ---- Session lifecycle timers (integration via start/stop) ----

func TestDebugStart_StopsOldTimers(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(600)

	hub.start(userID, true)

	// Start a new session (replaces old).
	hub.start(userID, false)

	// Attach SSE to the new session to cancel its no-attach timer.
	_, _, ok := hub.attachStream(userID)
	if !ok {
		t.Fatal("attachStream failed on new session")
	}

	// Wait past no-attach timeout — old timers must not close new session.
	time.Sleep(debugNoAttachTimeout + 200*time.Millisecond)

	if !hub.isActive(userID) {
		t.Fatal("new session should still be active — old timers must not close it")
	}

	hub.stop(userID)
}

func TestDebugStop_CleansUpTimers(t *testing.T) {
	hub := newUserDebugHub()
	const userID = int64(601)

	hub.start(userID, true)

	hub.mu.RLock()
	s := hub.sessions[userID]
	hub.mu.RUnlock()

	hub.stop(userID)

	// After stop, session should be gone.
	hub.mu.RLock()
	_, ok := hub.sessions[userID]
	hub.mu.RUnlock()
	if ok {
		t.Fatal("session should be removed after stop")
	}

	// Wait past no-attach timeout — no session should be re-created.
	time.Sleep(debugNoAttachTimeout + 200*time.Millisecond)

	if hub.isActive(userID) {
		t.Fatal("session should not be active after stop + timeout")
	}

	// Closing already-stopped timers should not panic.
	s.mu.Lock()
	if s.noAttachTimer != nil {
		s.noAttachTimer.Stop()
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.maxLifeTimer != nil {
		s.maxLifeTimer.Stop()
	}
	s.mu.Unlock()
}

// ---- Multi-user concurrency ----

func TestDebugMultiUser_IndependentSessions(t *testing.T) {
	hub := newUserDebugHub()

	hub.start(1, true)
	hub.start(2, true)

	if !hub.isActive(1) {
		t.Fatal("user 1 should be active")
	}
	if !hub.isActive(2) {
		t.Fatal("user 2 should be active")
	}

	// Stop user 1 only.
	hub.stop(1)

	if hub.isActive(1) {
		t.Fatal("user 1 should be stopped")
	}
	if !hub.isActive(2) {
		t.Fatal("user 2 should still be active")
	}

	hub.stop(2)
}

// ---- accept-encoding header ----

func TestDebugHeaderAllowlist_AcceptEncoding(t *testing.T) {
	hdrs := collectDebugHeaders(&http.Request{
		Header: http.Header{
			"Accept-Encoding": {"gzip, deflate"},
			"Content-Type":    {"application/json"},
			"Authorization":   {"Bearer secret"},
		},
	})
	if _, ok := hdrs["accept-encoding"]; !ok {
		t.Fatal("accept-encoding should be present")
	}
	if _, ok := hdrs["authorization"]; ok {
		t.Fatal("authorization should NOT be present")
	}
}

func TestCollectDebugHeaders_EmptyWhenNoMatch(t *testing.T) {
	hdrs := collectDebugHeaders(&http.Request{
		Header: http.Header{
			"Authorization": {"Bearer x"},
			"Cookie":        {"s=1"},
		},
	})
	if len(hdrs) != 0 {
		t.Fatalf("headers should be empty when none match whitelist, got %v", hdrs)
	}
}
