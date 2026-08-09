package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func userCookie(t *testing.T, gw *Gateway, store *db.Store) *http.Cookie {
	t.Helper()
	u, err := store.CreateUser("42", "tester", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token}
}

func assertCallerKeyNoStore(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", got)
	}
}

func TestCallerKey_GetAndReset(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	cookie := userCookie(t, gw, store)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// No key yet -> null.
	req := httptest.NewRequest(http.MethodGet, "/api/caller-key", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertCallerKeyNoStore(t, rec)
	var r1 struct {
		Key *string `json:"key"`
	}
	json.NewDecoder(rec.Body).Decode(&r1)
	if r1.Key != nil {
		t.Errorf("expected null key, got %v", *r1.Key)
	}

	// Reset -> new key returned and works for lookup.
	req = httptest.NewRequest(http.MethodPost, "/api/caller-key/reset", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertCallerKeyNoStore(t, rec)
	var r2 struct {
		Key string `json:"key"`
	}
	json.NewDecoder(rec.Body).Decode(&r2)
	if !strings.HasPrefix(r2.Key, db.CallerKeyPrefix) {
		t.Errorf("key = %q, want d2a_ prefix", r2.Key)
	}
	u, _ := store.GetUserByCallerKey(r2.Key)
	if u == nil {
		t.Error("new key should resolve to the user")
	}

	// Get returns the same key; second reset rotates it.
	req = httptest.NewRequest(http.MethodGet, "/api/caller-key", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertCallerKeyNoStore(t, rec)
	var r3 struct {
		Key string `json:"key"`
	}
	json.NewDecoder(rec.Body).Decode(&r3)
	if r3.Key != r2.Key {
		t.Errorf("get = %q, want %q", r3.Key, r2.Key)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/caller-key/reset", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertCallerKeyNoStore(t, rec)
	var r4 struct {
		Key string `json:"key"`
	}
	json.NewDecoder(rec.Body).Decode(&r4)
	if r4.Key == r2.Key {
		t.Error("reset should rotate the key")
	}
	if u, _ := store.GetUserByCallerKey(r2.Key); u != nil {
		t.Error("old key should stop working after reset")
	}
}

func TestCallerKey_NoStoreOnUnauthorizedAndInternalErrors(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	gw, store := setupAuthGatewayAt(t, "x", dbPath, filepath.Join(dir, "test.key"))
	cookie := userCookie(t, gw, store)
	user, err := store.GetUserByDiscordID("42")
	if err != nil || user == nil {
		t.Fatalf("get caller-key user: user=%v err=%v", user, err)
	}
	key, err := store.SetCallerKey(user.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	if !strings.HasPrefix(key, db.CallerKeyPrefix) {
		t.Fatalf("unexpected caller key: %q", key)
	}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	for _, endpoint := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/caller-key"},
		{http.MethodPost, "/api/caller-key/reset"},
	} {
		req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", endpoint.method, endpoint.path, rec.Code)
		}
		assertCallerKeyNoStore(t, rec)
	}

	execTestSQLite(t, dbPath, `UPDATE caller_keys SET key_enc='invalid ciphertext'`)
	req := httptest.NewRequest(http.MethodGet, "/api/caller-key", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET internal failure status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	assertCallerKeyNoStore(t, rec)

	execTestSQLite(t, dbPath, `CREATE TRIGGER fail_caller_key_write
		BEFORE INSERT ON caller_keys BEGIN
			SELECT RAISE(FAIL, 'caller key write sentinel');
		END`)
	req = httptest.NewRequest(http.MethodPost, "/api/caller-key/reset", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("reset internal failure status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	assertCallerKeyNoStore(t, rec)
}

func TestListLogs_SnakeCaseKeys(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	cookie := userCookie(t, gw, store)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	u, _ := store.GetUserByDiscordID("42")
	store.AddRequestLog(u.ID, "[general]x", "general", time.Now().Add(-time.Minute), time.Now(), "success", "")

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, key := range []string{"\"started_at\"", "\"ended_at\"", "\"model\"", "\"status\"", "\"error_code\"", "\"service\""} {
		if !strings.Contains(body, key) {
			t.Errorf("logs response missing snake_case key %s: %s", key, body)
		}
	}
	if strings.Contains(body, "StartedAt") {
		t.Error("response leaked Go field names (missing json tags)")
	}
}

func TestAdminListUsersAndSettings(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	store.CreateUser("1", "alice", "")
	u2, _ := store.CreateUser("2", "bob", "")
	store.SetUserDisabled(u2.ID, true, "test")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp struct {
		Users []struct {
			Username string `json:"username"`
			Banned   bool   `json:"banned"`
		} `json:"users"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Users) != 2 {
		t.Fatalf("users = %v, want 2", resp.Users)
	}
	if resp.Users[1].Username != "bob" || !resp.Users[1].Banned {
		t.Errorf("bob should be banned: %+v", resp.Users)
	}

	// Settings get/put roundtrip.
	req = httptest.NewRequest(http.MethodPut, "/api/admin/settings", strings.NewReader(`{"guild_id":"g9","role_id":"r9"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var s map[string]string
	json.NewDecoder(rec.Body).Decode(&s)
	if s["guild_id"] != "g9" || s["role_id"] != "r9" {
		t.Errorf("settings = %v", s)
	}
}

func TestSiteInfoAndStatic(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/site-info", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var info map[string]string
	json.NewDecoder(rec.Body).Decode(&info)
	if info["admin_host"] != "admin.localhost" || info["site_host"] != "localhost" {
		t.Errorf("site-info = %v", info)
	}

	// SPA shell served at /, assets under /static/.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "common.js") {
		t.Errorf("GET / should serve the SPA shell (status %d)", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/static/common.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "function route()") {
		t.Errorf("GET /static/common.js: status %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/static/pico.min.css", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /static/pico.min.css: status %d", rec.Code)
	}
}

// gwSourceURL is the SourceURL used by testConfig; legal pages must link it.
const gwSourceURL = "https://git.example.com/source/repo"

func TestLegalPages_LangRouting(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	getPage := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// 1. No lang param → Chinese (default).
	zh := getPage("/privacy")
	if !strings.Contains(zh, "隐私政策") {
		t.Error("/privacy (no lang): should contain 隐私政策")
	}

	// 2. ?lang=en → English.
	en := getPage("/privacy?lang=en")
	if !strings.Contains(en, "Privacy Policy") {
		t.Error("/privacy?lang=en: should contain Privacy Policy")
	}
	if !strings.Contains(en, "shall prevail") || !strings.Contains(en, "以中文版本为准") {
		t.Error("/privacy?lang=en: should contain bilingual governing-language clause")
	}

	// 3. ?lang=zh → Chinese (explicit).
	zh2 := getPage("/privacy?lang=zh")
	if !strings.Contains(zh2, "隐私政策") {
		t.Error("/privacy?lang=zh: should contain 隐私政策")
	}

	// 4. Terms page English with governing-language clause.
	enTerms := getPage("/terms?lang=en")
	if !strings.Contains(enTerms, "Terms of Service") {
		t.Error("/terms?lang=en: should contain Terms of Service")
	}
	// AGPL §13: both terms variants must link the Corresponding Source; the
	// __SOURCE_URL__ placeholder must be substituted server-side.
	for page, body := range map[string]string{"/terms": getPage("/terms"), "/terms?lang=en": enTerms} {
		if strings.Contains(body, "__SOURCE_URL__") {
			t.Errorf("%s: __SOURCE_URL__ placeholder not substituted", page)
		}
		if !strings.Contains(body, gwSourceURL) {
			t.Errorf("%s: should link the corresponding source repository", page)
		}
	}
	if !strings.Contains(enTerms, "shall prevail") || !strings.Contains(enTerms, "以中文版本为准") {
		t.Error("/terms?lang=en: should contain bilingual governing-language clause")
	}

	// 5. ?lang=en on 403 → no .en.html exists, falls back to Chinese 403 page.
	req403 := httptest.NewRequest(http.MethodGet, "/403?lang=en", nil)
	rec403 := httptest.NewRecorder()
	mux.ServeHTTP(rec403, req403)
	if rec403.Code != http.StatusForbidden {
		t.Errorf("/403?lang=en: status = %d, want 403", rec403.Code)
	}
	if strings.Contains(rec403.Body.String(), "Privacy Policy") {
		t.Error("/403?lang=en: should NOT serve English privacy page (path mismatch)")
	}

	// 6. User with lang=en preference → English without ?lang query param.
	u, err := store.CreateUser("99", "enguser", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	store.SetUserLang(u.ID, "en")
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/privacy (user lang=en): status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Privacy Policy") {
		t.Error("/privacy (user lang=en): should serve English version")
	}

	// 7. ?lang=en overrides user's zh preference.
	u2, err := store.CreateUser("100", "zhusr", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	store.SetUserLang(u2.ID, "zh")
	token2, _, _ := store.CreateSession(u2.ID)
	req = httptest.NewRequest(http.MethodGet, "/privacy?lang=en", nil)
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token2})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Privacy Policy") {
		t.Error("/privacy?lang=en (user lang=zh): query param should override user preference")
	}
}
