package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func adminLoginAttempt(gw *Gateway, mux *http.ServeMux, password, ip string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":"root","password":%q}`, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestLoginThrottle_LocksAfterFailures(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// 4 wrong attempts: still 401.
	for i := 0; i < 4; i++ {
		rec := adminLoginAttempt(gw, mux, "wrong", "1.2.3.4")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}
	// 5th failure triggers the lock (403 login_locked).
	rec := adminLoginAttempt(gw, mux, "wrong", "1.2.3.4")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "login_locked") {
		t.Fatalf("5th failure: status = %d, want 403 login_locked; body %s", rec.Code, rec.Body.String())
	}
	// While locked, even the CORRECT password is rejected.
	rec = adminLoginAttempt(gw, mux, "s3cret", "1.2.3.4")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "login_locked") {
		t.Errorf("locked: correct password should also get 403 login_locked, got %d", rec.Code)
	}

	// A different IP is unaffected.
	rec = adminLoginAttempt(gw, mux, "s3cret", "9.9.9.9")
	if rec.Code != http.StatusOK {
		t.Errorf("other IP: status = %d, want 200", rec.Code)
	}
}

func TestLoginThrottle_SuccessClearsWindow(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	for i := 0; i < 4; i++ {
		adminLoginAttempt(gw, mux, "wrong", "5.6.7.8")
	}
	if rec := adminLoginAttempt(gw, mux, "s3cret", "5.6.7.8"); rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200", rec.Code)
	}
	// Window cleared: 4 more failures should NOT lock.
	for i := 0; i < 4; i++ {
		rec := adminLoginAttempt(gw, mux, "wrong", "5.6.7.8")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("after success, attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}
}

func TestLoginThrottle_LockExpires(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	for i := 0; i < gw.loginThrottle.maxFailures; i++ {
		adminLoginAttempt(gw, mux, "wrong", "2.2.2.2")
	}
	if rec := adminLoginAttempt(gw, mux, "s3cret", "2.2.2.2"); rec.Code != http.StatusForbidden {
		t.Fatalf("should be locked, got %d", rec.Code)
	}

	// Simulate lock expiry by rewinding lockedUntil.
	key := "2.2.2.2|root"
	gw.loginThrottle.mu.Lock()
	gw.loginThrottle.fails[key].lockedUntil = time.Now().Add(-time.Second)
	gw.loginThrottle.mu.Unlock()

	if rec := adminLoginAttempt(gw, mux, "s3cret", "2.2.2.2"); rec.Code != http.StatusOK {
		t.Errorf("after lock expiry: status = %d, want 200", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := clientIP(r); got != "1.1.1.1" {
		t.Errorf("clientIP XFF = %q", got)
	}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("X-Real-IP", "3.3.3.3")
	if got := clientIP(r2); got != "3.3.3.3" {
		t.Errorf("clientIP X-Real-IP = %q", got)
	}
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.RemoteAddr = "4.4.4.4:5678"
	if got := clientIP(r3); got != "4.4.4.4" {
		t.Errorf("clientIP RemoteAddr = %q", got)
	}
}
