package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/config"
	"dify2api/db"
	"golang.org/x/crypto/bcrypt"
)

func setupAuthGateway(t *testing.T, adminPassword string) (*Gateway, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	return setupAuthGatewayAt(t, adminPassword, filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
}

func setupAuthGatewayAt(t *testing.T, adminPassword, dbPath, keyPath string) (*Gateway, *db.Store) {
	t.Helper()
	store, err := db.Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{
		ListenAddr:                   "localhost:10086",
		DifyHTTPTimeoutMs:            600000,
		DifyMaxResponseMB:            32,
		DifyProbeInFlight:            8,
		RemoteContentOriginAllowlist: []string{"https://example.com", "http://a.b"},
		MaxChatInFlight:              64,
		MaxRequestBodyMB:             4,
		MaxWebRequestBodyKB:          256,
		TrustedProxyCIDRs:            []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		SSEBufferMB:                  1,
		LoginMaxFailures:             5,
		LoginWindowMin:               10,
		LoginLockMin:                 60,
		LoginMinLatencyMs:            0, // keep the shared fixture fast; throttle tests adjust their own
		RPMWindowSec:                 60,
		IPThrottleWindowSec:          60,
		LogDetailMaxChars:            500,
		Admin: config.AdminConfig{
			Username:            "root",
			Password:            adminPassword,
			DiscordClientID:     "cid",
			DiscordClientSecret: "csecret",
			SiteBaseURL:         "http://localhost:10086",
			SiteHost:            "localhost",
			SiteURLHost:         "localhost:10086",
			AdminHost:           "admin.localhost",
			SourceURL:           gwSourceURL,
		},
	}
	gw := NewGateway(cfg, store)
	cleanupGatewayForTest(t, gw)
	disableAntiAbuseForTest(t, gw)
	return gw, store
}

func execTestSQLite(t *testing.T, dbPath, statement string) {
	t.Helper()
	testDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test sqlite connection: %v", err)
	}
	defer testDB.Close()
	if _, err := testDB.Exec(statement); err != nil {
		t.Fatalf("execute test sqlite statement: %v", err)
	}
}

func assertNoSessionCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.SessionCookieName {
			t.Fatalf("unexpected session cookie: %+v", cookie)
		}
	}
}

func loginCookie(t *testing.T, gw *Gateway, username, password string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin login: status %d, body %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestAdminLogin_PlaintextAndBcrypt(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	loginCookie(t, gw, "root", "s3cret")

	hash, _ := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	gw2, _ := setupAuthGateway(t, string(hash))
	loginCookie(t, gw2, "root", "s3cret")
}

func TestAdminLogin_WrongPassword(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	body := `{"username":"root","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAdminLogin_SessionFailureReturnsInternalError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	gw, _ := setupAuthGatewayAt(t, "s3cret", dbPath, filepath.Join(dir, "test.key"))
	execTestSQLite(t, dbPath, `CREATE TRIGGER fail_session_insert
		BEFORE INSERT ON sessions BEGIN
			SELECT RAISE(FAIL, 'session insert sentinel');
		END`)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(`{"username":"root","password":"s3cret"}`))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	assertNoSessionCookie(t, rec)
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "internal" || body.Error.Message != "[Dify2API] internal error" {
		t.Errorf("error = %+v, want safe internal envelope", body.Error)
	}
	if strings.Contains(rec.Body.String(), "session insert sentinel") || strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("response leaked failure details or reported success: %s", rec.Body.String())
	}
}

func TestMe_And_Logout(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d", rec.Code)
	}
	var me map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&me)
	if me["is_admin"] != true || me["username"] != "root" {
		t.Errorf("me = %v", me)
	}
	if _, ok := me["credits"]; !ok {
		t.Errorf("me missing 'credits' field: %v", me)
	}
	if _, ok := me["donation_credit"]; !ok {
		t.Errorf("me missing 'donation_credit' field: %v", me)
	}

	// Logout invalidates the session.
	req = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("after logout: status = %d, want 401", rec.Code)
	}
}

// discordStub fakes the Discord API for OAuth callback tests.
func discordStub(t *testing.T, roles []string, guildStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			json.NewEncoder(w).Encode(map[string]string{"access_token": "tok-123"})
		case r.URL.Path == "/users/@me":
			json.NewEncoder(w).Encode(map[string]string{"id": "42", "username": "tester", "avatar": "a1"})
		case strings.HasPrefix(r.URL.Path, "/users/@me/guilds/"):
			if guildStatus != 200 {
				w.WriteHeader(guildStatus)
				return
			}
			json.NewEncoder(w).Encode(map[string][]string{"roles": roles})
		default:
			w.WriteHeader(404)
		}
	}))
}

func withDiscordStub(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := auth.APIBase
	auth.APIBase = srv.URL
	t.Cleanup(func() { auth.APIBase = old })
}

func callbackRequest(gw *Gateway, mux *http.ServeMux, code, state, stateCookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/discord/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), nil)
	if stateCookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.OAuthStateCookieName, Value: stateCookie})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestOAuthState_IsAuthenticatedAndExpires(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	now := time.Unix(1_800_000_000, 0)
	state, err := gw.newOAuthState(now)
	if err != nil {
		t.Fatal(err)
	}
	if !gw.validOAuthState(state, now.Add(9*time.Minute)) {
		t.Fatal("fresh state should validate")
	}
	if gw.validOAuthState(state, now.Add(11*time.Minute)) {
		t.Fatal("expired state should be rejected")
	}
	tamperedBytes := []byte(state)
	if tamperedBytes[10] == 'A' {
		tamperedBytes[10] = 'B'
	} else {
		tamperedBytes[10] = 'A'
	}
	if gw.validOAuthState(string(tamperedBytes), now) {
		t.Fatal("tampered state should be rejected")
	}
	gw.Config.Admin.DiscordClientSecret = "rotated"
	if gw.validOAuthState(state, now) {
		t.Fatal("state signed with another secret should be rejected")
	}
}

func TestDiscordCallback_BadState(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	rec := callbackRequest(gw, mux, "code", "bad-state", "bad-state")
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
}

func TestDiscordCallback_RegisterWithRole(t *testing.T) {
	stub := discordStub(t, []string{"role-1"}, 200)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	// User created + session cookie set.
	u, _ := store.GetUserByDiscordID("42")
	if u == nil || u.Username != "tester" {
		t.Fatalf("user not registered: %+v", u)
	}
	// Caller key auto-provisioned on registration.
	if ok, _ := store.CallerKeyExists(u.ID); !ok {
		t.Error("caller key should be auto-provisioned on registration")
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("no session cookie on successful login")
	}
}

func TestDiscordCallback_SessionFailureUsesSafeFailureRedirect(t *testing.T) {
	stub := discordStub(t, nil, 200)
	withDiscordStub(t, stub)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	gw, store := setupAuthGatewayAt(t, "x", dbPath, filepath.Join(dir, "test.key"))
	if _, err := store.CreateUser("42", "tester", "a1"); err != nil {
		t.Fatalf("create existing Discord user: %v", err)
	}
	execTestSQLite(t, dbPath, `CREATE TRIGGER fail_session_insert
		BEFORE INSERT ON sessions BEGIN
			SELECT RAISE(FAIL, 'session insert sentinel');
		END`)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, err := gw.newOAuthState(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	assertNoSessionCookie(t, rec)
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse failure redirect: %v", err)
	}
	if location.Path != "/403" {
		t.Fatalf("redirect path = %q, want /403", location.Path)
	}
	reason := location.Query().Get("reason")
	if reason != "服务器内部错误，请稍后重试。" {
		t.Errorf("reason = %q, want safe generic failure", reason)
	}
	if strings.Contains(rec.Header().Get("Location"), "session insert sentinel") {
		t.Errorf("redirect leaked database error: %s", rec.Header().Get("Location"))
	}
}

func TestDiscordCallback_RegisterDenied(t *testing.T) {
	stub := discordStub(t, []string{"other-role"}, 200)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
	if u, _ := store.GetUserByDiscordID("42"); u != nil {
		t.Error("user should not be registered")
	}
}

func TestDiscordCallback_NotGuildMember(t *testing.T) {
	stub := discordStub(t, nil, 404)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
}

func TestDiscordCallback_DisabledUser(t *testing.T) {
	stub := discordStub(t, nil, 200)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserDisabled(u.ID, true, "test")

	state, _ := gw.newOAuthState(time.Now())
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 for disabled user", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
}

func TestDiscordCallback_NoGuildConfigured(t *testing.T) {
	stub := discordStub(t, []string{"role-1"}, 200)
	withDiscordStub(t, stub)
	gw, _ := setupAuthGateway(t, "x")
	// No guild/role settings -> registration closed.
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	rec := callbackRequest(gw, mux, "code", state, state)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302 (registration closed)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
}

func TestDiscordCallback_NoStateCookie(t *testing.T) {
	stub := discordStub(t, []string{"role-1"}, 200)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	// No cookie — login-CSRF check must fail.
	rec := callbackRequest(gw, mux, "code", state, "")
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
	// User must NOT be registered.
	if u, _ := store.GetUserByDiscordID("42"); u != nil {
		t.Error("user should not be registered without valid state cookie")
	}
}

func TestDiscordCallback_StateCookieMismatch(t *testing.T) {
	stub := discordStub(t, []string{"role-1"}, 200)
	withDiscordStub(t, stub)
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	state, _ := gw.newOAuthState(time.Now())
	// Cookie has a different value than query state — login-CSRF check must fail.
	rec := callbackRequest(gw, mux, "code", state, "attackers-state")
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/403?") {
		t.Errorf("Location = %q, want prefix /403?", loc)
	}
	// User must NOT be registered.
	if u, _ := store.GetUserByDiscordID("42"); u != nil {
		t.Error("user should not be registered with mismatched state cookie")
	}
}

func TestAdminLogin_HugeUsernameBoundedInThrottleKey(t *testing.T) {
	// Regression: the login-throttle map key is ip|username; an unbounded
	// username could inflate the in-memory failure map by near-body-sized
	// keys per attempt (memory DoS). Usernames are truncated to
	// maxLoginUsernameLen before entering the key, so names that differ only
	// beyond byte 128 share one throttle entry.
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	post := func(username string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"username":%q,"password":"wrong"}`, username)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Four failures with a name that differs from the second batch only
	// after byte 128: the fifth attempt (with a colliding truncated name)
	// must hit the lock on the shared key.
	nameA := strings.Repeat("A", maxLoginUsernameLen) + "first"
	for i := 0; i < gw.loginThrottle.maxFailures-1; i++ {
		if rec := post(nameA); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// A name that collides after truncation must hit the lock; without the
	// fix it would be a fresh 401 path.
	if rec := post(strings.Repeat("A", maxLoginUsernameLen) + "second"); rec.Code != http.StatusForbidden {
		t.Fatalf("colliding truncated name: status = %d, want 403 login_locked", rec.Code)
	}

	// Every key in the throttle map must be bounded: ip + '|' + 128 max.
	gw.loginThrottle.mu.Lock()
	defer gw.loginThrottle.mu.Unlock()
	prefix := gw.clientIP(httptest.NewRequest(http.MethodPost, "/", nil)) + "|"
	for key := range gw.loginThrottle.fails {
		if len(key) > len(prefix)+maxLoginUsernameLen {
			t.Fatalf("throttle key %d bytes exceeds bound %d", len(key), len(prefix)+maxLoginUsernameLen)
		}
	}
}

func TestAdminLogin_LockLogQuotesControlCharacters(t *testing.T) {
	for _, username := range []string{
		"anonymous\rforged",
		"anonymous\n[AUTH] forged-success",
		"anonymous\r\nforged",
		"anonymous\tforged",
	} {
		t.Run(fmt.Sprintf("%q", username), func(t *testing.T) {
			gw, _ := setupAuthGateway(t, "s3cret")
			mux := http.NewServeMux()
			gw.RegisterRoutes(mux)

			oldWriter := log.Writer()
			var logs bytes.Buffer
			log.SetOutput(&logs)
			defer log.SetOutput(oldWriter)

			post := func() *httptest.ResponseRecorder {
				body := fmt.Sprintf(`{"username":%q,"password":"wrong"}`, username)
				req := httptest.NewRequest(http.MethodPost, "/api/auth/admin/login", strings.NewReader(body))
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				return rec
			}
			for i := 0; i < gw.loginThrottle.maxFailures; i++ {
				if rec := post(); rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
					t.Fatalf("attempt %d: status = %d, want auth failure", i+1, rec.Code)
				}
			}

			got := logs.String()
			if strings.ContainsAny(got, "\r\t") {
				t.Fatalf("lock log contains raw CR/TAB: %q", got)
			}
			if lines := strings.Split(got, "\n"); len(lines) != 2 {
				t.Fatalf("lock log contains unexpected raw LF/control line: %q", got)
			}
			if strings.Contains(got, "\n[AUTH] forged-success") {
				t.Fatalf("lock log contains forged standalone line: %q", got)
			}
			quoted := fmt.Sprintf("%q", username)
			quoted = quoted[1 : len(quoted)-1]
			if !strings.Contains(got, quoted) {
				t.Fatalf("lock log does not contain quoted username %q: %q", quoted, got)
			}
		})
	}
}
