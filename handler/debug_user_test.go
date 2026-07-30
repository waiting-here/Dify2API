package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
