package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/db"
)

// --- rateLimiter unit tests -------------------------------------------------

func TestRateLimiter_ThreeIndependentWindows(t *testing.T) {
	l := newRateLimiter(60)
	now := time.Now()
	limits := [rpmClassCount]int{2, 3, 4}

	// Fill class A to its cap; B and C stay empty.
	l.record(rpmClassA, 1, now)
	l.record(rpmClassA, 1, now)
	ok, violated := l.check(1, now, limits)
	if ok || violated != rpmClassA {
		t.Fatalf("check = %v/%v, want blocked by class A", ok, violated)
	}

	// A different user is unaffected.
	if ok, _ := l.check(2, now, limits); !ok {
		t.Error("user 2 should not be blocked by user 1's hits")
	}

	// Entries older than 60s expire.
	past := now.Add(-61 * time.Second)
	l2 := newRateLimiter(60)
	l2.record(rpmClassA, 1, past)
	l2.record(rpmClassA, 1, past)
	if ok, _ := l2.check(1, now, limits); !ok {
		t.Error("expired hits should not block")
	}
}

func TestRateLimiter_ClassBAndCBlockIndependently(t *testing.T) {
	l := newRateLimiter(60)
	now := time.Now()
	limits := [rpmClassCount]int{10, 1, 10}

	l.record(rpmClassB, 7, now)
	ok, violated := l.check(7, now, limits)
	if ok || violated != rpmClassB {
		t.Fatalf("check = %v/%v, want blocked by class B", ok, violated)
	}

	limits = [rpmClassCount]int{10, 10, 1}
	l3 := newRateLimiter(60)
	l3.record(rpmClassC, 7, now)
	ok, violated = l3.check(7, now, limits)
	if ok || violated != rpmClassC {
		t.Fatalf("check = %v/%v, want blocked by class C", ok, violated)
	}
}

// --- ipThrottle unit tests --------------------------------------------------

func TestIPThrottle_BlocksAndRecovers(t *testing.T) {
	th := newIPThrottle(3, 2, 60) // 3/min, 2s penalty
	now := time.Now()

	for i := 0; i < 3; i++ {
		if !th.allow("1.2.3.4", now) {
			t.Fatalf("hit %d should be allowed", i+1)
		}
	}
	if th.allow("1.2.3.4", now) {
		t.Fatal("4th hit should be blocked")
	}
	// Still blocked during the penalty even without new hits.
	if th.allow("1.2.3.4", now.Add(1*time.Second)) {
		t.Fatal("should stay blocked during penalty")
	}
	// Another IP is unaffected.
	if !th.allow("5.6.7.8", now) {
		t.Fatal("other IP should be allowed")
	}
	// After the penalty the window is fresh.
	if !th.allow("1.2.3.4", now.Add(3*time.Second)) {
		t.Fatal("should be allowed after penalty expires")
	}
}

func TestIPThrottle_DisabledAlwaysAllows(t *testing.T) {
	th := newIPThrottle(0, 60, 60)
	now := time.Now()
	for i := 0; i < 100; i++ {
		if !th.allow("9.9.9.9", now) {
			t.Fatal("disabled throttle must always allow")
		}
	}
}

func TestIPThrottle_RetryAfter(t *testing.T) {
	th := newIPThrottle(1, 30, 60)
	now := time.Now()
	th.allow("1.1.1.1", now)
	th.allow("1.1.1.1", now) // crosses the cap -> 30s penalty
	got := th.retryAfterSec("1.1.1.1", now)
	if got < 29 || got > 30 {
		t.Errorf("retryAfterSec = %d, want ~30", got)
	}
}

// --- three-class RPM integration (chat endpoint) ----------------------------

// setRPMSettings stores the three global caps in the settings table.
func setRPMSettings(t *testing.T, gw *Gateway, a, b, c int) {
	t.Helper()
	for k, v := range map[string]int{
		db.SettingRPMLimitA: a,
		db.SettingRPMLimitB: b,
		db.SettingRPMLimitC: c,
	} {
		if err := gw.Store.SetSetting(k, fmt.Sprintf("%d", v)); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	gw.invalidateRPMCache(0)
}

func TestRPM_ClassCBlocksAtGate(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]m")
	// C=2: the 3rd received request must be rejected even though
	// A/B caps are high.
	setRPMSettings(t, gw, 100, 100, 2)

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 2; i++ {
		rec := chatRequest(gw, key, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, body %s", i+1, rec.Code, rec.Body.String())
		}
	}
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("3rd request: status %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rpm_exceeded") {
		t.Errorf("expected rpm_exceeded, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "C（请求接收）") {
		t.Errorf("expected class C label in message, got %s", rec.Body.String())
	}
	// Violation logged.
	n, _ := gw.Store.CountRecentErrors(uid, "rpm_exceeded", time.Now().Add(-time.Hour))
	if n != 1 {
		t.Errorf("violations logged = %d, want 1", n)
	}
}

func TestRPM_ClassABlocksAfterCompletedTransfers(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]m")
	// A=1: after one completed (blocking) transfer the next gate check
	// must reject on class A.
	setRPMSettings(t, gw, 1, 100, 100)

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: status %d", rec.Code)
	}
	rec = chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second request: status %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "A（传输完成）") {
		t.Errorf("expected class A label, got %s", rec.Body.String())
	}
}

func TestRPM_FailedRequestDoesNotCountClassAB(t *testing.T) {
	// Upstream always fails with 500 -> class A and B must stay at 0,
	// so with A=1,B=1 the user can keep retrying (class C permitting).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"code":"internal_error","message":"boom"}`)
	}))
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]m")
	setRPMSettings(t, gw, 1, 1, 100)

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 3; i++ {
		rec := chatRequest(gw, key, body)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("request %d: unexpectedly blocked by RPM (%s)", i+1, rec.Body.String())
		}
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("request %d: status %d, want 502", i+1, rec.Code)
		}
	}
}

func TestRPM_Blocking200FailedCountsClassB(t *testing.T) {
	// Dify returns HTTP 200 with workflow status "failed": per the §1.2
	// definition this is a "success" for class B (though not class A).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"task_id":"t","workflow_run_id":"w","data":{"id":"x","status":"failed","error":"model exploded","outputs":{}}}`)
	}))
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]m")
	setRPMSettings(t, gw, 100, 1, 100) // B=1

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`
	rec := chatRequest(gw, key, body)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("first request should not be RPM-blocked: %s", rec.Body.String())
	}
	// Second gate check must reject on class B.
	rec = chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second request: status %d, want 403 (class B)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "B（请求成功）") {
		t.Errorf("expected class B label, got %s", rec.Body.String())
	}
}

func TestRPM_PerUserOverrideWinsOverGlobal(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]m")
	setRPMSettings(t, gw, 100, 100, 1) // global C=1

	// Per-user override C=3 lets this user run 3 requests.
	c := 3
	if err := gw.Store.SetUserRPMLimits(uid, nil, nil, &c); err != nil {
		t.Fatalf("SetUserRPMLimits: %v", err)
	}
	gw.invalidateRPMCache(uid)

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 3; i++ {
		rec := chatRequest(gw, key, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200 (override C=3)", i+1, rec.Code)
		}
	}
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("4th request: status %d, want 403", rec.Code)
	}
}

func TestRPM_AutoBanAfterConfiguredViolations(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]m")
	setRPMSettings(t, gw, 100, 100, 1) // C=1: every 2nd call violates
	// Lower the violation threshold to 2 and ban duration to 1h.
	gw.Store.SetSetting(db.SettingRPMViolationLimit, "2")
	gw.Store.SetSetting(db.SettingRPMBanHours, "1")

	body := `{"model":"[general]m","messages":[{"role":"user","content":"hi"}]}`

	// 1st ok; 2nd violation #1; (window blocks) 3rd violation #2 -> ban.
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("first: %d", rec.Code)
	}
	rec = chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second: %d, want 403", rec.Code)
	}
	rec = chatRequest(gw, key, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("third: %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "自动封禁 1 小时") {
		t.Errorf("expected configured ban-hours in message, got %s", rec.Body.String())
	}
	u, _ := gw.Store.GetUserByID(uid)
	if !db.IsBanned(u) {
		t.Error("user should be auto-banned after 2 violations")
	}
	if !u.AutoBanned {
		t.Error("auto_banned flag should be set")
	}
	// Banned user's key no longer authenticates.
	rec = chatRequest(gw, key, body)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("banned user: status %d, want 401", rec.Code)
	}
}

func TestRPM_AutoBanMessageLocalizedValues(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{
			lang: "zh",
			want: "[Dify2API] 已超出类别 C（请求接收） 每分钟上限（1 次/分），且因 24 小时内累计 2 次超限，账号已被自动封禁 7 小时",
		},
		{
			lang: "en",
			want: "[Dify2API] Exceeded class C（请求接收） RPM limit (1/min); account auto-banned for 7 hours due to 2 violations in 24 hours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			var captured map[string]interface{}
			srv := mockDifyApp(t, &captured)
			defer srv.Close()
			gw, key, _ := setupRoutedUser(t, srv.URL, "[general]localized-ban")
			setRPMSettings(t, gw, 100, 100, 1)
			if err := gw.Store.SetSetting(db.SettingRPMViolationLimit, "2"); err != nil {
				t.Fatal(err)
			}
			if err := gw.Store.SetSetting(db.SettingRPMBanHours, "7"); err != nil {
				t.Fatal(err)
			}

			body := `{"model":"[general]localized-ban","messages":[{"role":"user","content":"long enough content"}]}`
			request := func() *httptest.ResponseRecorder {
				mux := http.NewServeMux()
				gw.RegisterRoutes(mux)
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?lang="+tt.lang, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+key)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				return rec
			}

			if rec := request(); rec.Code != http.StatusOK {
				t.Fatalf("first request status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			if rec := request(); rec.Code != http.StatusForbidden {
				t.Fatalf("first violation status = %d, want 403; body: %s", rec.Code, rec.Body.String())
			}
			rec := request()
			if rec.Code != http.StatusForbidden {
				t.Fatalf("auto-ban status = %d, want 403; body: %s", rec.Code, rec.Body.String())
			}
			var response struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode auto-ban response: %v", err)
			}
			if response.Error.Code != "rpm_exceeded" || response.Error.Message != tt.want {
				t.Errorf("auto-ban error = %+v, want message %q", response.Error, tt.want)
			}
		})
	}
}

// --- admin settings & per-user override API ----------------------------------

func TestAdminSettings_RPMTunablesRoundtrip(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	rec := adminPut(gw, adminCookie, "/api/admin/settings",
		`{"guild_id":"g","role_id":"r","rpm_limit_a":7,"rpm_limit_b":14,"rpm_limit_c":21,"rpm_violation_limit":3,"rpm_ban_hours":48,"probe_limit_per_user":4}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: %d, body %s", rec.Code, rec.Body.String())
	}

	rec = adminGet(gw, adminCookie, "/api/admin/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings: %d", rec.Code)
	}
	var got map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &got)
	for k, want := range map[string]float64{
		"rpm_limit_a": 7, "rpm_limit_b": 14, "rpm_limit_c": 21,
		"rpm_violation_limit": 3, "rpm_ban_hours": 48,
		"probe_limit_per_user": 4,
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}

	// Invalid: zero is rejected.
	rec = adminPut(gw, adminCookie, "/api/admin/settings", `{"guild_id":"g","role_id":"r","rpm_limit_a":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("rpm_limit_a=0: status %d, want 400", rec.Code)
	}
	rec = adminPut(gw, adminCookie, "/api/admin/settings", `{"probe_limit_per_user":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("probe_limit_per_user=0: status %d, want 400", rec.Code)
	}
}

func TestAdminSettings_DefaultsWhenUnset(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	rec := adminGet(gw, adminCookie, "/api/admin/settings")
	var got map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &got)
	for k, want := range map[string]float64{
		"rpm_limit_a": 6, "rpm_limit_b": 12, "rpm_limit_c": 18,
		"rpm_violation_limit": 5, "rpm_ban_hours": 24,
		"probe_limit_per_user": 5,
	} {
		if got[k] != want {
			t.Errorf("default %s = %v, want %v", k, got[k], want)
		}
	}
}

func TestAdminSetUserRPM_Overrides(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	u, _ := store.CreateUser("42", "tester", "")

	// Set B only.
	rec := adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{"rpm_limit_b":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set override: %d, body %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUserByID(u.ID)
	if got.RPMLimitA.Valid || !got.RPMLimitB.Valid || got.RPMLimitB.Int64 != 9 || got.RPMLimitC.Valid {
		t.Errorf("overrides = %v/%v/%v, want null/9/null", got.RPMLimitA, got.RPMLimitB, got.RPMLimitC)
	}

	// Clear all (empty body fields -> null).
	rec = adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear override: %d", rec.Code)
	}
	got, _ = store.GetUserByID(u.ID)
	if got.RPMLimitA.Valid || got.RPMLimitB.Valid || got.RPMLimitC.Valid {
		t.Error("all overrides should be cleared")
	}

	// Invalid: < 1.
	rec = adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{"rpm_limit_a":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("zero override: %d, want 400", rec.Code)
	}

	// Admin target rejected.
	admin, _ := store.GetUserByDiscordID(db.AdminDiscordID)
	rec = adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", admin.ID), `{"rpm_limit_a":5}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("admin target: %d, want 404", rec.Code)
	}

	// Non-admin caller rejected.
	rec = adminPost(gw, nil, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{"rpm_limit_a":5}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous caller: %d, want 403", rec.Code)
	}
}

// adminPut mirrors adminPost/adminGet for PUT requests.
func adminPut(gw *Gateway, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
