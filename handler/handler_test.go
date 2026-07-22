package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dify2api/config"
	"dify2api/db"
)

func testConfig() *config.Config {
	return &config.Config{
		ListenAddr:          "localhost:10086",
		DifyHTTPTimeoutMs:   600000,
		MaxChatInFlight:     64,
		MaxRequestBodyMB:    4,
		SSEBufferMB:         1,
		LoginMaxFailures:    5,
		LoginWindowMin:      10,
		LoginLockMin:        60,
		LoginMinLatencyMs:   0,
		RPMWindowSec:        60,
		IPThrottleWindowSec: 60,
		LogDetailMaxChars:   500,
	}
}

func setupTestGateway(t *testing.T) *Gateway {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewGateway(testConfig(), store)
}

func setupTestGatewayDebug(t *testing.T, dir string) *Gateway {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "test.db"), filepath.Join(t.TempDir(), "test.key"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := testConfig()
	cfg.Debug = true
	cfg.DebugDir = dir
	return NewGateway(cfg, store)
}

func TestHandleHealth(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandleModels_MethodNotAllowed(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("models POST: status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleModels_RequiresCallerKey(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("models GET without key: status = %d, want 401", rec.Code)
	}
}

func TestHandleChatCompletions_EmptyMessages(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	body := `{"model":"[general]claude-opus-4-6","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty messages: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleChatCompletions_RequiresCallerKey(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	body := `{"model":"[sillytavern-main-trimmed]claude-opus-4-6","messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("valid request without key: status = %d, want 401; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletions_MethodNotAllowed(t *testing.T) {
	gw := setupTestGateway(t)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// The catch-all GET / handler serves the custom 404 page for GET
	// requests to POST-only endpoints — correct from a browser UX
	// perspective (the page doesn't exist as a GET-able resource).
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET chat: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func dumpFolders(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dump dir: %v", err)
	}
	return entries
}

func TestDebug_InterceptsAndDumps(t *testing.T) {
	dir := t.TempDir()
	gw := setupTestGatewayDebug(t, dir)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	body := `{"model":"[sillytavern-main-trimmed]claude-opus-4-6","messages":[` +
		`{"role":"system","content":"S"},` +
		`{"role":"user","content":"U0"},` +
		`{"role":"assistant","content":"A1"},` +
		`{"role":"user","content":"U1"}` +
		`],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Intercepted: 404 debug_intercept.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"]["code"] != "debug_intercept" {
		t.Errorf("error.code = %v, want debug_intercept", resp["error"]["code"])
	}

	// Exactly one timestamped folder with request.json + dify_inputs.json.
	entries := dumpFolders(t, dir)
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("expected 1 dump folder, got %v", entries)
	}
	folder := filepath.Join(dir, entries[0].Name())

	raw, err := os.ReadFile(filepath.Join(folder, "request.json"))
	if err != nil {
		t.Fatalf("request.json missing: %v", err)
	}
	var reqDump debugRequestDump
	if err := json.Unmarshal(raw, &reqDump); err != nil {
		t.Fatalf("parse request.json: %v", err)
	}
	if !strings.HasPrefix(reqDump.Note, "ok") || !strings.Contains(reqDump.RawBody, "\"U0\"") {
		t.Errorf("request.json wrong: note=%q", reqDump.Note)
	}

	raw, err = os.ReadFile(filepath.Join(folder, "dify_inputs.json"))
	if err != nil {
		t.Fatalf("dify_inputs.json missing: %v", err)
	}
	var inputs map[string]string
	if err := json.Unmarshal(raw, &inputs); err != nil {
		t.Fatalf("parse dify_inputs.json: %v", err)
	}
	want := map[string]string{
		"system_prompt": "S", "user_0": "U0", "assistant_1": "A1", "user_1": "U1",
		"assistant_2": "", "user_2": "", "assistant_3": "", "user_3": "",
	}
	for k, v := range want {
		if inputs[k] != v {
			t.Errorf("dify_inputs[%q] = %q, want %q", k, inputs[k], v)
		}
	}
}

func TestDebug_InvalidLayoutStillDumped(t *testing.T) {
	dir := t.TempDir()
	gw := setupTestGatewayDebug(t, dir)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	body := `{"messages":[{"role":"system","content":"S"},{"role":"user","content":"U0"},{"role":"user","content":"U1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (debug intercept)", rec.Code)
	}
	// Dumped with a rejection note, but WITHOUT dify_inputs.json.
	entries := dumpFolders(t, dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 dump folder, got %d", len(entries))
	}
	folder := filepath.Join(dir, entries[0].Name())
	raw, _ := os.ReadFile(filepath.Join(folder, "request.json"))
	if !strings.Contains(string(raw), "layout rejected") {
		t.Errorf("request.json should carry the rejection note, got: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(folder, "dify_inputs.json")); !os.IsNotExist(err) {
		t.Error("dify_inputs.json should not exist for a rejected layout")
	}
}
