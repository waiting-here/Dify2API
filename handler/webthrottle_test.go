package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dify2api/db"
)

// setupThrottledGateway builds a gateway with the web/auth-fail throttles
// enabled at small limits, routed through the full middleware chain
// (gw.Wrap) as in production.
func setupThrottledGateway(t *testing.T, webRPM, authFailRPM int) (*Gateway, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "t.db"), filepath.Join(dir, "t.key"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := testConfig()
	cfg.WebRPMPerIP = webRPM
	cfg.WebThrottleSec = 60
	cfg.AuthFailRPMPerIP = authFailRPM
	gw := NewGateway(cfg, store)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	return gw, gw.Wrap(mux)
}

func ipRequest(h http.Handler, method, path, ip, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("X-Forwarded-For", ip)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWebThrottle_APIBlockedStaticExempt(t *testing.T) {
	_, h := setupThrottledGateway(t, 3, 0)

	// 3 hits allowed, 4th throttled.
	for i := 0; i < 3; i++ {
		rec := ipRequest(h, http.MethodGet, "/api/me", "10.0.0.1", "")
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("hit %d: unexpectedly throttled", i+1)
		}
	}
	rec := ipRequest(h, http.MethodGet, "/api/me", "10.0.0.1", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th hit: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
	if !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Errorf("expected rate_limited code, got %s", rec.Body.String())
	}

	// Static/page requests from the same (throttled) IP are exempt.
	rec = ipRequest(h, http.MethodGet, "/health", "10.0.0.1", "")
	if rec.Code != http.StatusOK {
		t.Errorf("/health while throttled: status %d, want 200", rec.Code)
	}

	// /v1/* is exempt from the web throttle (401 without key, never 429).
	rec = ipRequest(h, http.MethodGet, "/v1/models", "10.0.0.1", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("/v1/models while web-throttled: status %d, want 401", rec.Code)
	}

	// Another IP is unaffected.
	rec = ipRequest(h, http.MethodGet, "/api/me", "10.0.0.2", "")
	if rec.Code == http.StatusTooManyRequests {
		t.Error("other IP should not be throttled")
	}
}

func TestWebThrottle_DisabledByZero(t *testing.T) {
	_, h := setupThrottledGateway(t, 0, 0)
	for i := 0; i < 50; i++ {
		rec := ipRequest(h, http.MethodGet, "/api/me", "10.0.0.3", "")
		if rec.Code == http.StatusTooManyRequests {
			t.Fatal("throttle disabled (0) must never 429")
		}
	}
}

func TestAuthFailThrottle_InvalidKeysThrottled(t *testing.T) {
	_, h := setupThrottledGateway(t, 0, 2)
	body := `{"model":"[general]m","messages":[{"role":"user","content":"x"}]}`

	// Invalid-key requests: first 2 rejected with 401, 3rd with 429.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer d2a_bogus")
		req.Header.Set("X-Forwarded-For", "10.9.9.9")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid key %d: status %d, want 401", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer d2a_bogus")
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd invalid key: status %d, want 429; body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
}

func TestAuthFailThrottle_ValidKeyNeverCounted(t *testing.T) {
	// A valid key must never trip the auth-fail throttle even at cap 1.
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]m")
	gw.authFailThrottle = newIPThrottle(1, 60)
	setRPMSettings(t, gw, 100, 100, 100)
	h := gw.Wrap(func() http.Handler {
		mux := http.NewServeMux()
		gw.RegisterRoutes(mux)
		return mux
	}())

	body := `{"model":"[general]m","messages":[{"role":"user","content":"x"}]}`
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-Forwarded-For", "10.8.8.8")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("valid key request %d: status %d, body %s", i+1, rec.Code, rec.Body.String())
		}
	}
}
