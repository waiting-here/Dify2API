package handler

import (
	"encoding/json"
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
	state, _ := gw.newOAuthState(time.Now())
	rec = callbackRequest(gw, mux, "code", state, state)
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

func TestSettings_CheckinParametersRoundtrip(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// Get default settings.
	req, _ := http.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: status %d", rec.Code)
	}
	var data map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &data)
	// Verify defaults are present.
	if v, ok := data["checkin_min"].(float64); !ok || int(v) != db.DefaultCheckinMin {
		t.Errorf("checkin_min = %v, want %d", data["checkin_min"], db.DefaultCheckinMin)
	}
	if v, ok := data["checkin_max"].(float64); !ok || int(v) != db.DefaultCheckinMax {
		t.Errorf("checkin_max = %v, want %d", data["checkin_max"], db.DefaultCheckinMax)
	}
	if v, ok := data["credits_cap"].(float64); !ok || int(v) != db.DefaultCreditsCap {
		t.Errorf("credits_cap = %v, want %d", data["credits_cap"], db.DefaultCreditsCap)
	}

	// Set custom check-in parameters.
	body := `{"checkin_min":8,"checkin_max":25,"credits_cap":60}`
	req, _ = http.NewRequest(http.MethodPut, "/api/admin/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put settings: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Read back and verify.
	req, _ = http.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	json.Unmarshal(rec.Body.Bytes(), &data)
	if v, _ := data["checkin_min"].(float64); int(v) != 8 {
		t.Errorf("checkin_min = %v, want 8", v)
	}
	if v, _ := data["checkin_max"].(float64); int(v) != 25 {
		t.Errorf("checkin_max = %v, want 25", v)
	}
	if v, _ := data["credits_cap"].(float64); int(v) != 60 {
		t.Errorf("credits_cap = %v, want 60", v)
	}
}

func TestBatchCredits_SetAddSub(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Create two regular users.
	u1, _ := store.CreateUser("1", "u1", "")
	u2, _ := store.CreateUser("2", "u2", "")
	store.SetUserCredits(u1.ID, 10)
	store.SetUserCredits(u2.ID, 20)

	// Set both to 100.
	rec := batchCreditsPost(gw, adminCookie, []int64{u1.ID, u2.ID}, "set", 100)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if v, _ := resp["updated"].(float64); int(v) != 2 {
		t.Errorf("updated = %v, want 2", v)
	}
	got1, _ := store.GetUserByID(u1.ID)
	got2, _ := store.GetUserByID(u2.ID)
	if got1.Credits != 100 || got2.Credits != 100 {
		t.Errorf("credits = %d/%d, want 100/100", got1.Credits, got2.Credits)
	}

	// Add 50 to both.
	rec = batchCreditsPost(gw, adminCookie, []int64{u1.ID, u2.ID}, "add", 50)
	if rec.Code != http.StatusOK {
		t.Fatalf("add: status %d", rec.Code)
	}
	got1, _ = store.GetUserByID(u1.ID)
	got2, _ = store.GetUserByID(u2.ID)
	if got1.Credits != 150 || got2.Credits != 150 {
		t.Errorf("credits after add = %d/%d, want 150/150", got1.Credits, got2.Credits)
	}

	// Sub 30 from both.
	rec = batchCreditsPost(gw, adminCookie, []int64{u1.ID, u2.ID}, "sub", 30)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub: status %d", rec.Code)
	}
	got1, _ = store.GetUserByID(u1.ID)
	got2, _ = store.GetUserByID(u2.ID)
	if got1.Credits != 120 || got2.Credits != 120 {
		t.Errorf("credits after sub = %d/%d, want 120/120", got1.Credits, got2.Credits)
	}
}

func TestBatchCredits_SkipsAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u1, _ := store.CreateUser("1", "u1", "")
	store.SetUserCredits(u1.ID, 10)
	admin, _ := store.GetUserByDiscordID(db.AdminDiscordID)

	// Try to set credits for both a regular user and admin.
	rec := batchCreditsPost(gw, adminCookie, []int64{u1.ID, admin.ID}, "set", 50)
	if rec.Code != http.StatusOK {
		t.Fatalf("set with admin: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if v, _ := resp["updated"].(float64); int(v) != 1 {
		t.Errorf("updated = %v, want 1 (admin skipped)", v)
	}
	got1, _ := store.GetUserByID(u1.ID)
	if got1.Credits != 50 {
		t.Errorf("u1 credits = %d, want 50", got1.Credits)
	}
}

func TestBatchDonationCredit_SetAddSub(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u1, _ := store.CreateUser("1", "u1", "")
	u2, _ := store.CreateUser("2", "u2", "")
	store.SetUserDonationCredit(u1.ID, 5)
	store.SetUserDonationCredit(u2.ID, 3)

	// Set both to 10.
	rec := batchDonationCreditPost(gw, adminCookie, []int64{u1.ID, u2.ID}, "set", 10)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if v, _ := resp["updated"].(float64); int(v) != 2 {
		t.Errorf("updated = %v, want 2", v)
	}

	// Add 5.
	rec = batchDonationCreditPost(gw, adminCookie, []int64{u1.ID}, "add", 5)
	if rec.Code != http.StatusOK {
		t.Fatalf("add: status %d", rec.Code)
	}
	got1, _ := store.GetUserByID(u1.ID)
	if got1.DonationCredit != 15 {
		t.Errorf("donation_credit = %d, want 15", got1.DonationCredit)
	}

	// Sub 8.
	rec = batchDonationCreditPost(gw, adminCookie, []int64{u1.ID}, "sub", 8)
	if rec.Code != http.StatusOK {
		t.Fatalf("sub: status %d", rec.Code)
	}
	got1, _ = store.GetUserByID(u1.ID)
	if got1.DonationCredit != 7 {
		t.Errorf("donation_credit after sub = %d, want 7", got1.DonationCredit)
	}
}

func TestBatchCredits_Validation(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Negative amount.
	rec := batchCreditsPost(gw, adminCookie, []int64{1}, "set", -1)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("negative amount: status = %d, want 400", rec.Code)
	}

	// Empty user_ids.
	rec = batchCreditsPost(gw, adminCookie, nil, "set", 10)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty user_ids: status = %d, want 400", rec.Code)
	}

	// Invalid action.
	rec = batchCreditsPost(gw, adminCookie, []int64{1}, "multiply", 10)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid action: status = %d, want 400", rec.Code)
	}

	// Non-admin forbidden.
	rec = batchCreditsPost(gw, nil, []int64{1}, "set", 10)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403", rec.Code)
	}
}

// batchCreditsPost is a helper for POST /api/admin/users/credits.
func batchCreditsPost(gw *Gateway, cookie *http.Cookie, userIDs []int64, action string, amount int) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]interface{}{
		"user_ids": userIDs,
		"action":   action,
		"amount":   amount,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/credits", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// batchDonationCreditPost is a helper for POST /api/admin/users/donation_credit.
func batchDonationCreditPost(gw *Gateway, cookie *http.Cookie, userIDs []int64, action string, amount int) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]interface{}{
		"user_ids": userIDs,
		"action":   action,
		"amount":   amount,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/donation_credit", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
