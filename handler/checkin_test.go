package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func TestCheckin_Success(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8 // UTC+8

	// Create a non-admin user and log them in.
	u, _ := store.CreateUser("42", "tester", "")
	u.Credits = 5
	store.SetUserCredits(u.ID, 5)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: status %d, body %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK      bool `json:"ok"`
		Bonus   int  `json:"bonus"`
		Credits int  `json:"credits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK {
		t.Error("ok should be true")
	}
	if resp.Bonus < db.DefaultCheckinMin || resp.Bonus > db.DefaultCheckinMax {
		t.Errorf("bonus %d not in [%d,%d]", resp.Bonus, db.DefaultCheckinMin, db.DefaultCheckinMax)
	}
	// credits = 5 + bonus
	if resp.Credits != 5+resp.Bonus {
		t.Errorf("credits %d != %d", resp.Credits, 5+resp.Bonus)
	}
}

func TestCheckin_AlreadyCheckedIn(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, 5)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	// First checkin succeeds.
	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("first checkin: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Second checkin: already checked in today.
	rec = checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second checkin: status %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "今日已签到") {
		t.Errorf("expected '今日已签到', got %s", rec.Body.String())
	}
}

func TestCheckin_CreditsCapped(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, db.DefaultCreditsCap) // exactly at cap
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("capped checkin: status %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "超过上限") {
		t.Errorf("expected '超过上限', got %s", rec.Body.String())
	}
}

func TestCheckin_CreditsAboveCap(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, db.DefaultCreditsCap+10) // above cap
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("above cap: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestCheckin_AdminRejected(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("admin checkin: status = %d, want 400", rec.Code)
	}
}

func TestCheckin_UnauthenticatedRejected(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	rec := checkinPost(gw, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want 401", rec.Code)
	}
}

func TestCheckin_DayBoundary(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8 // UTC+8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, 0)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	// Simulate yesterday's check-in by directly setting last_checkin_day.
	today := time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
	yesterday := time.Now().UTC().Add(8 * time.Hour).Add(-24 * time.Hour).Format("2006-01-02")

	// Manually set last_checkin_day to yesterday (bypassing the atomic check).
	if _, err := store.RawExec(`UPDATE users SET last_checkin_day=?, credits=0 WHERE id=?`, yesterday, u.ID); err != nil {
		t.Fatalf("set yesterday: %v", err)
	}

	// Checkin should succeed because today != yesterday.
	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("day boundary checkin: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Verify last_checkin_day was updated.
	got, _ := store.GetUserByID(u.ID)
	if got.LastCheckinDay != today {
		t.Errorf("last_checkin_day = %s, want %s", got.LastCheckinDay, today)
	}
}

func TestCheckin_RandomRange(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	// Set narrow custom settings for reproducibility.
	store.SetSetting(db.SettingCheckinMin, "15")
	store.SetSetting(db.SettingCheckinMax, "18")

	u, _ := store.CreateUser("42", "tester", "")
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	for i := 0; i < 20; i++ {
		store.SetUserCredits(u.ID, 0)
		// Reset checkin day so each iteration can check in.
		store.RawExec(`UPDATE users SET last_checkin_day='' WHERE id=?`, u.ID)

		rec := checkinPost(gw, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d: status %d", i, rec.Code)
		}
		var resp struct {
			Bonus int `json:"bonus"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Bonus < 15 || resp.Bonus > 18 {
			t.Errorf("bonus %d not in [15,18]", resp.Bonus)
		}
	}
}

func TestCheckin_SettingsCustomRange(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 0

	// Set custom min/max/cap via settings.
	store.SetSetting(db.SettingCheckinMin, "5")
	store.SetSetting(db.SettingCheckinMax, "5") // deterministic
	store.SetSetting(db.SettingCreditsCap, "100")

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, 90)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("custom range: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Bonus   int `json:"bonus"`
		Credits int `json:"credits"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Bonus != 5 {
		t.Errorf("bonus = %d, want 5", resp.Bonus)
	}
	if resp.Credits != 95 {
		t.Errorf("credits = %d, want 95", resp.Credits)
	}
}

// checkinStatusGet is a helper for GET /api/me/checkin/status.
func checkinStatusGet(gw *Gateway, cookie *http.Cookie) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/me/checkin/status", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCheckin_Disabled(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	store.SetSetting(db.SettingCreditsCap, "0")

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, 0)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled checkin: status %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "checkin_disabled") {
		t.Errorf("expected 'checkin_disabled' in body, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "签到系统未开放") {
		t.Errorf("expected '签到系统未开放' in body, got %s", rec.Body.String())
	}
}

func TestCheckinStatus_NotCheckedIn(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinStatusGet(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: code %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CheckedInToday bool  `json:"checked_in_today"`
		NextCheckinAt  int64 `json:"next_checkin_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.CheckedInToday {
		t.Error("expected checked_in_today=false for fresh user")
	}
	if resp.NextCheckinAt <= time.Now().Unix() {
		t.Errorf("next_checkin_at should be in the future, got %d", resp.NextCheckinAt)
	}
}

func TestCheckinStatus_Capped(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, db.DefaultCreditsCap) // at the cap
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	// Status must report capped=true so the client can disable the button.
	rec := checkinStatusGet(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: code %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CheckedInToday bool `json:"checked_in_today"`
		Capped         bool `json:"capped"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.CheckedInToday {
		t.Error("expected checked_in_today=false for fresh user")
	}
	if !resp.Capped {
		t.Error("expected capped=true when credits >= cap")
	}

	// The POST must still refuse while at the cap (unchanged behavior).
	rec = checkinPost(gw, cookie)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "credits_capped") {
		t.Fatalf("checkin at cap: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestCheckinStatus_CheckedIn(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	gw.Config.CheckinTZOffset = 8

	u, _ := store.CreateUser("42", "tester", "")
	store.SetUserCredits(u.ID, 5)
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	// First, check in.
	rec := checkinPost(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("checkin: status %d, body %s", rec.Code, rec.Body.String())
	}

	// Now check status — should show checked_in_today=true.
	rec = checkinStatusGet(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after checkin: code %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CheckedInToday bool  `json:"checked_in_today"`
		NextCheckinAt  int64 `json:"next_checkin_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.CheckedInToday {
		t.Error("expected checked_in_today=true after check-in")
	}
	if resp.NextCheckinAt <= time.Now().Unix() {
		t.Errorf("next_checkin_at should be in the future, got %d", resp.NextCheckinAt)
	}
}

func TestCheckinStatus_Disabled(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	store.SetSetting(db.SettingCreditsCap, "0")

	u, _ := store.CreateUser("42", "tester", "")
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	rec := checkinStatusGet(gw, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("disabled status: code %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CheckedInToday bool  `json:"checked_in_today"`
		NextCheckinAt  int64 `json:"next_checkin_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.CheckedInToday {
		t.Error("expected checked_in_today=true when credits_cap=0")
	}
	if resp.NextCheckinAt != 9999999999 {
		t.Errorf("expected next_checkin_at=9999999999, got %d", resp.NextCheckinAt)
	}
}

func TestCheckinStatus_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	rec := checkinStatusGet(gw, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want 401", rec.Code)
	}
}

// checkinPost is a helper for POST /api/me/checkin.
func checkinPost(gw *Gateway, cookie *http.Cookie) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/me/checkin", nil)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
