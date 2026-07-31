package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func adminLoginAttempt(gw *Gateway, mux *http.ServeMux, password, ip string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":"root","password":%q}`, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
	// Emulate the default loopback reverse proxy; direct untrusted peers must
	// not be able to influence the effective IP with this header.
	req.RemoteAddr = "127.0.0.1:12345"
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

func TestClientIP_TrustedProxyBoundary(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	gw.Config.TrustedProxyCIDRs = append(gw.Config.TrustedProxyCIDRs, netip.MustParsePrefix("10.0.0.0/8"))

	// Trusted loopback peer: strip the trusted 10/8 hop from the right and
	// return the first untrusted address. The spoofed far-left value is the
	// claimed client and is accepted only because every hop to its right is trusted.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 10.2.2.2")
	if got := gw.clientIP(r); got != "1.1.1.1" {
		t.Errorf("trusted proxy chain = %q, want 1.1.1.1", got)
	}

	// A direct, untrusted peer cannot spoof either forwarding header.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "4.4.4.4:5678"
	r2.Header.Set("X-Forwarded-For", "1.1.1.1")
	r2.Header.Set("X-Real-IP", "3.3.3.3")
	if got := gw.clientIP(r2); got != "4.4.4.4" {
		t.Errorf("direct spoof = %q, want peer", got)
	}

	// X-Real-IP remains a supported fallback for a trusted single proxy.
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.RemoteAddr = "127.0.0.1:1234"
	r3.Header.Set("X-Real-IP", "3.3.3.3")
	if got := gw.clientIP(r3); got != "3.3.3.3" {
		t.Errorf("trusted X-Real-IP = %q", got)
	}

	// Malformed or oversized chains fail closed to the immediate peer.
	r4 := httptest.NewRequest(http.MethodGet, "/", nil)
	r4.RemoteAddr = "127.0.0.1:1234"
	r4.Header.Set("X-Forwarded-For", "1.1.1.1, not-an-ip")
	if got := gw.clientIP(r4); got != "127.0.0.1" {
		t.Errorf("malformed XFF = %q, want peer", got)
	}
	r4.Header.Set("X-Forwarded-For", strings.Repeat("1", maxForwardedForBytes+1))
	if got := gw.clientIP(r4); got != "127.0.0.1" {
		t.Errorf("oversized XFF = %q, want peer", got)
	}
}
