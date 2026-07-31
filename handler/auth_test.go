package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
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
	store, err := db.Open(dir+"/test.db", dir+"/test.key")
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
		},
	}
	gw := NewGateway(cfg, store)
	disableAntiAbuseForTest(t, gw)
	return gw, store
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
