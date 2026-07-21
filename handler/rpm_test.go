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

func TestRPMLimit_EnforcementAndAutoBan(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")

	// Per-user override: 2 RPM.
	limit := int64(2)
	if err := gw.Store.SetUserRPMLimit(uid, &limit); err != nil {
		t.Fatalf("SetUserRPMLimit: %v", err)
	}

	body := `{"model":"[general]claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`
	want := []struct {
		status int
		note   string
	}{
		{http.StatusOK, "1st within limit"},
		{http.StatusOK, "2nd within limit"},
		{http.StatusForbidden, "3rd over limit"},
		{http.StatusForbidden, "4th over limit"},
		{http.StatusForbidden, "5th over limit"},
		{http.StatusForbidden, "6th over limit"},
		{http.StatusForbidden, "7th: 5th violation -> auto-ban"},
	}
	var lastBody string
	for i, w := range want {
		rec := chatRequest(gw, key, body)
		if rec.Code != w.status {
			t.Fatalf("req %d (%s): status = %d, want %d; body %s", i+1, w.note, rec.Code, w.status, rec.Body.String())
		}
		lastBody = rec.Body.String()
	}

	// Auto-ban applied: user banned (timed), sessions cleared.
	u, _ := gw.Store.GetUserByID(uid)
	if !db.IsBanned(u) || u.BannedUntil == 0 {
		t.Errorf("user should be auto-banned after 5 violations: %+v", u)
	}
	if !strings.Contains(lastBody, "自动封禁 24 小时") {
		t.Errorf("ban message should mention auto-ban: %s", lastBody)
	}
	if !strings.Contains(lastBody, "rpm_exceeded") {
		t.Errorf("error should carry rpm_exceeded code: %s", lastBody)
	}

	// Violations recorded as logs.
	n, _ := gw.Store.CountRecentErrors(uid, "rpm_exceeded", time.Now().Add(-24*time.Hour))
	if n != 5 {
		t.Errorf("rpm_exceeded logs = %d, want 5", n)
	}
}

func TestRPMLimit_GlobalDefaultAndOverride(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	// Global default is 3 when unset.
	if got := store.GetGlobalRPM(); got != db.DefaultGlobalRPM {
		t.Errorf("default global RPM = %d, want %d", got, db.DefaultGlobalRPM)
	}
	// Admin changes global to 6.
	if err := store.SetSetting(db.SettingRPMLimit, "6"); err != nil {
		t.Fatal(err)
	}
	if got := store.GetGlobalRPM(); got != 6 {
		t.Errorf("global RPM = %d, want 6", got)
	}

	// Per-user override wins; clearing reverts to global.
	u, _ := store.CreateUser("1", "u1", "")
	limit := int64(1)
	store.SetUserRPMLimit(u.ID, &limit)
	if got := gw.effectiveRPM(u.ID); got != 1 {
		t.Errorf("effectiveRPM with override = %d, want 1", got)
	}
	store.SetUserRPMLimit(u.ID, nil)
	if got := gw.effectiveRPM(u.ID); got != 6 {
		t.Errorf("effectiveRPM after clearing = %d, want 6 (global)", got)
	}
}

func TestConfigNote_Roundtrip(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// Create with note.
	req := httptest.NewRequest(http.MethodPost, "/api/configs",
		strings.NewReader(`{"model":"[general]x","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k","note":"主用 Claude"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Config struct {
			ID   int64  `json:"id"`
			Note string `json:"note"`
		} `json:"config"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Config.Note != "主用 Claude" {
		t.Errorf("note = %q", resp.Config.Note)
	}

	// Update note.
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/configs/%d", resp.Config.ID),
		strings.NewReader(`{"model":"[general]x","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k","note":"备用"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	c, _ := store.GetAppConfig(resp.Config.ID)
	if c.Note != "备用" {
		t.Errorf("note after update = %q", c.Note)
	}
}

func TestAdminSetUserRPM(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	u, _ := store.CreateUser("42", "tester", "")

	// Set override.
	rec := adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{"limit":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set rpm: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUserByID(u.ID)
	if !got.RPMLimit.Valid || got.RPMLimit.Int64 != 9 {
		t.Errorf("rpm override = %+v", got.RPMLimit)
	}

	// Reset to default.
	rec = adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), `{"default":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset rpm: %d", rec.Code)
	}
	got, _ = store.GetUserByID(u.ID)
	if got.RPMLimit.Valid {
		t.Errorf("rpm should be back to default (NULL), got %+v", got.RPMLimit)
	}

	// Non-admin forbidden.
	token, _, _ := store.CreateSession(u.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/rpm", u.ID), strings.NewReader(`{"limit":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
	rec2 := httptest.NewRecorder()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	mux.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("non-admin set rpm: status = %d, want 403", rec2.Code)
	}
}
