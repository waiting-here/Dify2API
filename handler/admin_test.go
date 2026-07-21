package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func adminPost(gw *Gateway, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestBanAndUnban_Timed(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "tester", "")
	userToken, _, _ := store.CreateSession(u.ID)

	// Timed ban (1h) -> sessions invalidated, user treated as banned.
	until := time.Now().Add(time.Hour).Unix()
	rec := adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/ban", u.ID), fmt.Sprintf(`{"until":%d}`, until))
	if rec.Code != http.StatusOK {
		t.Fatalf("ban: status %d, body %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUserByID(u.ID)
	if !db.IsBanned(got) {
		t.Fatal("user should be banned after timed ban")
	}
	if sessUser, _ := store.GetSessionUser(userToken); sessUser != nil {
		t.Error("sessions should be invalidated on ban")
	}

	// Unban -> both flags cleared.
	rec = adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/unban", u.ID), `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unban: status %d", rec.Code)
	}
	got, _ = store.GetUserByID(u.ID)
	if db.IsBanned(got) {
		t.Error("user should be unbanned")
	}
}

func TestBan_PermanentAndLapse(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	u, _ := store.CreateUser("42", "tester", "")

	// Permanent ban via disabled flag.
	rec := adminPost(gw, adminCookie, fmt.Sprintf("/api/admin/users/%d/ban", u.ID), `{"permanent":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("permanent ban: status %d", rec.Code)
	}
	got, _ := store.GetUserByID(u.ID)
	if !db.IsBanned(got) {
		t.Fatal("user should be banned (permanent)")
	}

	// A past banned_until means the ban has lapsed (auto-removal).
	store.BanUser(u.ID, time.Now().Add(-time.Hour), "")
	store.SetUserDisabled(u.ID, false, "")
	got, _ = store.GetUserByID(u.ID)
	if db.IsBanned(got) {
		t.Error("lapsed timed ban should not count as banned")
	}
}

func TestDeleteUser_RecordsClearedAndReregisterAllowed(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	u, _ := store.CreateUser("42", "tester", "")
	store.SetCallerKey(u.ID)
	store.CreateAppConfig(u.ID, "[general]x", "http://x", "k", "")

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", u.ID), nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Records wiped.
	if got, _ := store.GetUserByID(u.ID); got != nil {
		t.Error("user row should be gone")
	}
	if ok, _ := store.CallerKeyExists(u.ID); ok {
		t.Error("caller key should be gone")
	}
	if cfgs, _ := store.ListAppConfigs(u.ID); len(cfgs) != 0 {
		t.Error("app configs should be gone")
	}

	// Re-registration via Discord succeeds (guild role satisfied).
	stub := discordStub(t, []string{"role-1"}, 200)
	withDiscordStub(t, stub)
	store.SetSetting(db.SettingGuildID, "g1")
	store.SetSetting(db.SettingRoleID, "role-1")
	state, _ := newOAuthState()
	rec = callbackRequest(gw, mux, "code", state)
	if rec.Code != http.StatusFound {
		t.Fatalf("re-register: status = %d, want 302; body: %s", rec.Code, rec.Body.String())
	}
	if got, _ := store.GetUserByDiscordID("42"); got == nil {
		t.Error("user should be re-registered after deletion")
	}
}

func TestDeleteUser_AdminAndUnknownGuards(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	admin, _ := store.GetUserByDiscordID(db.AdminDiscordID)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", admin.ID), nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete admin: status = %d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/users/999", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown: status = %d, want 404", rec.Code)
	}

	// Non-admin forbidden.
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", u.ID), nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin delete: status = %d, want 403", rec.Code)
	}
}

func TestBan_ForbiddenForNonAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	// Normal user session (not admin).
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	rec := adminPost(gw, cookie, fmt.Sprintf("/api/admin/users/%d/ban", u.ID), `{"permanent":true}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin ban: status = %d, want 403", rec.Code)
	}

	// Cannot ban the admin itself / unknown user.
	rec = adminPost(gw, loginCookie(t, gw, "root", "s3cret"), "/api/admin/users/999/ban", `{"permanent":true}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d, want 404", rec.Code)
	}
}
