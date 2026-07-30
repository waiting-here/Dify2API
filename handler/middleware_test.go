package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dify2api/db"
)

func TestForceHTTPS_Redirect(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	gw.Config.ForceHTTPS = true
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/v1/models" {
		t.Errorf("Location = %q", loc)
	}

	// X-Forwarded-Proto: https passes through (proxy-terminated TLS).
	req = httptest.NewRequest(http.MethodGet, "http://example.com/health", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("https forwarded: status = %d, want 200", rec.Code)
	}
}

func TestForceHTTPS_OffByDefault(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("default (http allowed): status = %d, want 200", rec.Code)
	}
}

func TestHostSeparation(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	// setupAuthGateway uses SiteBaseURL http://localhost:10086
	// -> siteHost localhost, adminHost admin.localhost
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	// Admin host: admin login works.
	body := `{"username":"root","password":"s3cret"}`
	req := httptest.NewRequest(http.MethodPost, "http://admin.localhost/api/auth/admin/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin host admin login: status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	// Admin host: /v1/models blocked, discord login blocked.
	for _, p := range []string{"/v1/models", "/auth/discord/login", "/api/configs"} {
		req = httptest.NewRequest(http.MethodGet, "http://admin.localhost"+p, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("admin host %s: status = %d, want 404", p, rec.Code)
		}
	}

	// User host: admin login + admin api blocked.
	req = httptest.NewRequest(http.MethodPost, "http://localhost/api/auth/admin/login", strings.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("user host admin login: status = %d, want 404", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "http://localhost/api/admin/users/1/ban", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("user host admin api: status = %d, want 404", rec.Code)
	}

	// User host: discord login redirect works, /health works.
	req = httptest.NewRequest(http.MethodGet, "http://localhost/auth/discord/login", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("user host discord login: status = %d, want 302", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "http://localhost/health", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("user host health: status = %d, want 200", rec.Code)
	}
}

func TestCheckAppBinding_Verdicts(t *testing.T) {
	// Mock Dify App with: required user_0, optional system_prompt, optional extra_var.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user_input_form":[`+
			`{"paragraph":{"variable":"user_0","required":true}},`+
			`{"paragraph":{"variable":"system_prompt","required":false}},`+
			`{"paragraph":{"variable":"extra_var","required":false}}]}`)
	}))
	defer srv.Close()

	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	_ = store

	// general requires only user_0: extra optional system_prompt/extra_var allowed.
	req := httptest.NewRequest(http.MethodPost, "/api/configs",
		strings.NewReader(fmt.Sprintf(`{"model":"[general]x","dify_base_url":%q,"dify_api_key":"app-k"}`, srv.URL)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		AppCheck struct {
			Compatible      bool     `json:"compatible"`
			ExtraOptional   []string `json:"extra_app_optional"`
			Error           string   `json:"error"`
		} `json:"app_check"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.AppCheck.Compatible {
		t.Errorf("extra optional vars should be allowed: %+v", resp.AppCheck)
	}
	// general has no optional vars: both system_prompt and extra_var are
	// reported as unused-but-allowed extras.
	if len(resp.AppCheck.ExtraOptional) != 2 {
		t.Errorf("extra optional = %v, want [system_prompt extra_var]", resp.AppCheck.ExtraOptional)
	}
}

func TestCheckAppBinding_Incompatible(t *testing.T) {
	// App REQUIRES a variable the general contract never sends.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user_input_form":[`+
			`{"paragraph":{"variable":"user_0","required":true}},`+
			`{"paragraph":{"variable":"must_have","required":true}}]}`)
	}))
	defer srv.Close()

	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/api/configs",
		strings.NewReader(fmt.Sprintf(`{"model":"[general]x","dify_base_url":%q,"dify_api_key":"app-k"}`, srv.URL)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	var resp struct {
		AppCheck struct {
			Compatible           bool     `json:"compatible"`
			UncoveredAppRequired []string `json:"uncovered_app_required"`
		} `json:"app_check"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.AppCheck.Compatible {
		t.Error("app-required uncovered var should be incompatible")
	}
	if len(resp.AppCheck.UncoveredAppRequired) != 1 || resp.AppCheck.UncoveredAppRequired[0] != "must_have" {
		t.Errorf("uncovered = %v", resp.AppCheck.UncoveredAppRequired)
	}

	// Missing contract var: App lacks user_0 entirely.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"user_input_form":[{"paragraph":{"variable":"other","required":false}}]}`)
	}))
	defer srv2.Close()
	check := gw.checkAppBinding("[general]x", srv2.URL, "k")
	if check["compatible"] != false {
		t.Errorf("missing user_0 should be incompatible: %v", check)
	}
	if mv, ok := check["missing_contract_vars"].([]string); !ok || len(mv) != 1 || mv[0] != "user_0" {
		t.Errorf("missing = %v", check["missing_contract_vars"])
	}
}

func TestConfigs_RejectUnsupportedService(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	mk := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/configs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Unsupported service prefix -> 400 listing supported services.
	rec := mk(`{"model":"[nope]x","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported service: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "不支持的服务") {
		t.Errorf("body should explain supported services: %s", rec.Body.String())
	}

	// Missing bracket prefix entirely -> 400.
	rec = mk(`{"model":"plain-name","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no prefix: status = %d, want 400", rec.Code)
	}

	// Registered service -> accepted.
	rec = mk(`{"model":"[website-summary]claude-opus-4-6","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("supported service: status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
}

func TestListServices(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	names := map[string]bool{}
	for _, s := range resp.Services {
		names[s.Name] = true
	}
	for _, want := range []string{"general", "custom", "website-summary", "sillytavern-main-trimmed", "sillytavern-SP·数据库-填表"} {
		if !names[want] {
			t.Errorf("services should include %q: %v", want, resp.Services)
		}
	}
}

func TestConfigsCRUD_AndGuards(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// Unauthorized without session.
	req := httptest.NewRequest(http.MethodGet, "/api/configs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: status = %d, want 401", rec.Code)
	}

	// Create against an unreachable App: saved anyway, app_check carries error.
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	userCookie := &http.Cookie{Name: "dify2api_session", Value: token}
	_ = adminCookie

	req = httptest.NewRequest(http.MethodPost, "/api/configs",
		strings.NewReader(`{"model":"[general]x","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create (unreachable app): status %d, want 200 (check is informational); body %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Config struct {
			ID     int64  `json:"id"`
			HasKey bool   `json:"has_key"`
		} `json:"config"`
		AppCheck struct {
			Error string `json:"error"`
		} `json:"app_check"`
	}
	json.NewDecoder(rec.Body).Decode(&createResp)
	if createResp.Config.ID == 0 || !createResp.Config.HasKey {
		t.Errorf("config payload wrong: %+v", createResp.Config)
	}
	if createResp.AppCheck.Error == "" {
		t.Error("unreachable app should carry app_check.error")
	}
	id := createResp.Config.ID

	// Duplicate model -> 409.
	req = httptest.NewRequest(http.MethodPost, "/api/configs",
		strings.NewReader(`{"model":"[general]x","dify_base_url":"http://127.0.0.1:1","dify_api_key":"k"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status = %d, want 409", rec.Code)
	}

	// Toggle off, list shows disabled.
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/configs/%d/toggle", id), strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle: status %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/configs", nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var listResp struct {
		Configs []struct {
			ID      int64 `json:"id"`
			Enabled bool  `json:"enabled"`
		} `json:"configs"`
	}
	json.NewDecoder(rec.Body).Decode(&listResp)
	if len(listResp.Configs) != 1 || listResp.Configs[0].Enabled {
		t.Errorf("after toggle: %+v", listResp.Configs)
	}

	// Delete, then 404 on second delete.
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/configs/%d", id), nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/configs/%d", id), nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete twice: status = %d, want 404", rec.Code)
	}
}

// TestMaintenanceMode covers the full maintenance-switch behaviour (B4).
func TestMaintenanceMode(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	enable := func()  { store.SetSetting(db.SettingMaintenanceMode, "true") }
	disable := func() { store.SetSetting(db.SettingMaintenanceMode, "false") }

	// --- Disabled by default: everything works normally. ---
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("off: / status = %d, want 200", rec.Code)
	}

	// --- Enable maintenance mode. ---
	enable()

	// User host: home page → 503 + maintenance.html.
	req = httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("on: / status = %d, want 503", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("on: / Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "站点维护中") {
		t.Errorf("on: / body should contain 站点维护中: %s", rec.Body.String())
	}
	// Placeholder should be replaced.
	if strings.Contains(rec.Body.String(), "__SITE_NAME__") {
		t.Error("on: / placeholder __SITE_NAME__ should be replaced")
	}

	// User host: arbitrary page path → 503 + maintenance.html.
	req = httptest.NewRequest(http.MethodGet, "http://localhost/dashboard", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("on: /dashboard status = %d, want 503", rec.Code)
	}

	// User host: API endpoint → 503 JSON error.
	for _, p := range []string{"/api/me", "/api/configs", "/v1/chat/completions", "/v1/models"} {
		req = httptest.NewRequest(http.MethodGet, "http://localhost"+p, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("on: %s status = %d, want 503", p, rec.Code)
		}
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp.Error.Code != "maintenance" {
			t.Errorf("on: %s error.code = %q, want maintenance", p, errResp.Error.Code)
		}
		if !strings.Contains(errResp.Error.Message, "站点维护中") {
			t.Errorf("on: %s error.message should contain 站点维护中: %q", p, errResp.Error.Message)
		}
	}

	// Static resources: pass through.
	for _, p := range []string{"/static/pico.min.css", "/favicon.ico", "/credits-logo", "/privacy", "/terms", "/health"} {
		req = httptest.NewRequest(http.MethodGet, "http://localhost"+p, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			t.Errorf("on: %s status = %d, should NOT be blocked by maintenance", p, rec.Code)
		}
	}

	// Discord OAuth paths: pass through.
	for _, p := range []string{"/auth/discord/login", "/auth/discord/callback"} {
		req = httptest.NewRequest(http.MethodGet, "http://localhost"+p, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// /auth/discord/login returns 302, /auth/discord/callback may return
		// an error (no state param) but NOT 503 maintenance.
		if rec.Code == http.StatusServiceUnavailable {
			t.Errorf("on: %s status = %d, should NOT be blocked by maintenance", p, rec.Code)
		}
	}

	// Admin host: NOT affected by maintenance.
	for _, p := range []string{"/", "/api/me", "/api/admin/settings", "/api/site-info", "/privacy", "/terms"} {
		req = httptest.NewRequest(http.MethodGet, "http://admin.localhost"+p, nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			t.Errorf("on admin host: %s status = %d, should NOT be affected by maintenance", p, rec.Code)
		}
	}
	// Admin host API endpoints should also work.
	req = httptest.NewRequest(http.MethodGet, "http://admin.localhost/api/admin/users", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusServiceUnavailable {
		t.Error("on admin host: /api/admin/users should NOT be affected by maintenance")
	}

	// --- Disable maintenance mode: everything back to normal. ---
	disable()
	req = httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("off after on: / status = %d, want 200", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "http://localhost/api/me", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// /api/me without auth → 401, not 503.
	if rec.Code == http.StatusServiceUnavailable {
		t.Error("off after on: /api/me should not be blocked by maintenance")
	}
}

func TestMaintenancePage_LangRouting(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	store.SetSetting(db.SettingMaintenanceMode, "true")
	defer store.SetSetting(db.SettingMaintenanceMode, "false")

	// 1. No lang → Chinese maintenance page.
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance / : status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "站点维护中") {
		t.Error("maintenance / (no lang): should contain 站点维护中")
	}
	if strings.Contains(rec.Body.String(), "__SITE_NAME__") {
		t.Error("maintenance /: placeholder __SITE_NAME__ should be replaced")
	}

	// 2. ?lang=en → English maintenance page.
	req = httptest.NewRequest(http.MethodGet, "http://localhost/?lang=en", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance /?lang=en: status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Site Under Maintenance") {
		t.Error("maintenance /?lang=en: should contain Site Under Maintenance")
	}
	if strings.Contains(rec.Body.String(), "__SITE_NAME__") {
		t.Error("maintenance /?lang=en: placeholder __SITE_NAME__ should be replaced")
	}

	// 3. ?lang=en on a sub-path → still English.
	req = httptest.NewRequest(http.MethodGet, "http://localhost/dashboard?lang=en", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Site Under Maintenance") {
		t.Error("maintenance /dashboard?lang=en: should contain Site Under Maintenance")
	}

	// 4. Placeholder substitution works for English page too (SiteName is empty in test,
	// so __SITE_NAME__ becomes "").
	if strings.Contains(rec.Body.String(), "__SITE_NAME__") {
		t.Error("maintenance /dashboard?lang=en: placeholder __SITE_NAME__ should be replaced")
	}
}
