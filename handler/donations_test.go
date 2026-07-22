package handler

import (
	"database/sql"
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

// adminCookie logs into the admin account and returns the session cookie.
func adminCookie(t *testing.T, gw *Gateway) *http.Cookie {
	t.Helper()
	return loginCookie(t, gw, "root", "x")
}

func donationRequest(gw *Gateway, cookie *http.Cookie, method, path string, body interface{}) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	var reqBody string
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = string(b)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCreateDonation_WithSourceUser creates a donation with a source user
// and verifies the snapshot fields are populated.
func TestCreateDonation_WithSourceUser(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	// Register a source user (non-admin).
	u, err := store.CreateUser("100", "source_user", "avatar1")
	if err != nil {
		t.Fatalf("create source user: %v", err)
	}

	deadline := time.Now().Add(24 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":        "general",
		"model":          "claude-opus-4-6",
		"dify_base_url":  "https://dify.example.com/v1",
		"dify_api_key":   "app-test-key",
		"source_user_id": u.ID,
		"deadline":       deadline,
		"total_count":    10,
		"note":           "test donation",
	}
	rec := donationRequest(gw, admin, "POST", "/api/admin/donations", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK       bool                   `json:"ok"`
		Donation map[string]interface{} `json:"donation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
	d := resp.Donation
	if d["service"] != "general" {
		t.Errorf("service = %v", d["service"])
	}
	if d["model"] != "claude-opus-4-6" {
		t.Errorf("model = %v", d["model"])
	}
	if d["source_user_id"] != float64(u.ID) {
		t.Errorf("source_user_id = %v (want %d)", d["source_user_id"], u.ID)
	}
	if d["source_discord_id"] != "100" {
		t.Errorf("source_discord_id = %v", d["source_discord_id"])
	}
	if d["source_username"] != "source_user" {
		t.Errorf("source_username = %v", d["source_username"])
	}
	// API key should be returned in creation response
	if d["dify_api_key"] != "app-test-key" {
		t.Errorf("dify_api_key = %v (want app-test-key)", d["dify_api_key"])
	}
	// has_key should be true
	if d["has_key"] != true {
		t.Errorf("has_key = %v", d["has_key"])
	}
	if d["remaining_count"] != float64(10) {
		t.Errorf("remaining_count = %v", d["remaining_count"])
	}
}

// TestCreateDonation_WithSourceText creates a donation with free-text source.
func TestCreateDonation_WithSourceText(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	deadline := time.Now().Add(24 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "claude-opus-4-6",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"source_text":   "第三方捐赠（2026-07）",
		"deadline":      deadline,
		"total_count":   5,
		"note":          "管理员录入的捐赠",
	}
	rec := donationRequest(gw, admin, "POST", "/api/admin/donations", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateDonation_AdminSourceRequiresNote checks that admin users
// CAN be selected as source but REQUIRE a note (ref §8.4#2).
func TestCreateDonation_AdminSourceRequiresNote(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminCookie := adminCookie(t, gw)

	// Get the admin user row.
	adminUser, err := store.GetUserByDiscordID(db.AdminDiscordID)
	if err != nil || adminUser == nil {
		t.Fatal("admin user not found")
	}

	deadline := time.Now().Add(24 * time.Hour).Unix()

	// Test 1: admin as source without note → rejected.
	body := map[string]interface{}{
		"service":        "general",
		"model":          "claude-opus-4-6",
		"dify_base_url":  "https://dify.example.com/v1",
		"dify_api_key":   "app-test-key",
		"source_user_id": adminUser.ID,
		"deadline":       deadline,
		"total_count":    5,
		// note intentionally omitted
	}
	rec := donationRequest(gw, adminCookie, "POST", "/api/admin/donations", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin without note: status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "备注为必填") {
		t.Errorf("want note-required message, got: %s", rec.Body.String())
	}

	// Test 2: admin as source WITH note → accepted.
	bodyWithNote := map[string]interface{}{
		"service":        "general",
		"model":          "claude-opus-4-7",
		"dify_base_url":  "https://dify.example.com/v1",
		"dify_api_key":   "app-test-key2",
		"source_user_id": adminUser.ID,
		"deadline":       deadline,
		"total_count":    5,
		"note":           "管理员自捐",
	}
	rec2 := donationRequest(gw, adminCookie, "POST", "/api/admin/donations", bodyWithNote)
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin with note: status %d, want 200; body: %s", rec2.Code, rec2.Body.String())
	}
}

// TestCreateDonation_ModelWithBrackets rejects a donation with brackets in model.
func TestCreateDonation_ModelWithBrackets(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	deadline := time.Now().Add(24 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "claude[opus]",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"source_text":   "test",
		"deadline":      deadline,
		"total_count":   5,
		"note":          "test",
	}
	rec := donationRequest(gw, admin, "POST", "/api/admin/donations", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不得包含方括号") {
		t.Errorf("want bracket error, got: %s", rec.Body.String())
	}
}

// TestListDonations returns the list and entries have has_key but not the key itself.
func TestListDonations(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	// Create one donation via direct DB to avoid going through the handler again.
	u, _ := store.CreateUser("101", "donor", "")
	deadline := time.Now().Add(24 * time.Hour).Unix()
	d := &db.Donation{
		Service:         "general",
		Model:           "claude-opus-4-6",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        deadline,
		TotalCount:      10,
		Status:          db.DonationActive,
		Note:            "test",
	}
	if _, err := store.CreateDonation(d, "app-secret"); err != nil {
		t.Fatalf("create donation: %v", err)
	}

	rec := donationRequest(gw, admin, "GET", "/api/admin/donations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Donations []map[string]interface{} `json:"donations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(resp.Donations) == 0 {
		t.Fatal("expected at least one donation")
	}
	d2 := resp.Donations[0]
	// has_key should be true but dify_api_key should NOT be present.
	if d2["has_key"] != true {
		t.Errorf("has_key = %v", d2["has_key"])
	}
	if _, ok := d2["dify_api_key"]; ok {
		t.Error("dify_api_key should not be returned in list")
	}
}

// TestDonationStatusToggle tests active↔inactive switching.
func TestDonationStatusToggle(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	u, _ := store.CreateUser("102", "donor2", "")
	deadline := time.Now().Add(24 * time.Hour).Unix()
	d := &db.Donation{
		Service:         "general",
		Model:           "gpt4",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        deadline,
		TotalCount:      10,
		Status:          db.DonationActive,
		Note:            "test",
	}
	created, err := store.CreateDonation(d, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	// Toggle to inactive
	rec := donationRequest(gw, admin, "POST", fmt.Sprintf("/api/admin/donations/%d/status", created.ID),
		map[string]string{"status": "inactive"})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle inactive status = %d, body: %s", rec.Code, rec.Body.String())
	}
	updated, _ := store.GetDonation(created.ID)
	if updated.Status != db.DonationInactive {
		t.Errorf("status = %s, want inactive", updated.Status)
	}

	// Toggle back to active
	rec2 := donationRequest(gw, admin, "POST", fmt.Sprintf("/api/admin/donations/%d/status", created.ID),
		map[string]string{"status": "active"})
	if rec2.Code != http.StatusOK {
		t.Fatalf("toggle active status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	updated2, _ := store.GetDonation(created.ID)
	if updated2.Status != db.DonationActive {
		t.Errorf("status = %s, want active", updated2.Status)
	}
	// Re-activation should reset consecutive_failures
	if updated2.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d, want 0 after reactivation", updated2.ConsecutiveFailures)
	}
}

// TestDonationExpiredRejectsStatusChange verifies that expired donations cannot
// have their status changed.
func TestDonationExpiredRejectsStatusChange(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	u, _ := store.CreateUser("103", "donor3", "")
	d := &db.Donation{
		Service:         "general",
		Model:           "gpt4",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        time.Now().Unix() + 3600,
		TotalCount:      0, // zero remaining → expired
		Status:          db.DonationExpired,
		Note:            "test",
	}
	// Direct DB insert since CreateDonation validates total_count>0.
	store.RawExec(`INSERT INTO donations (service, model, dify_base_url, dify_api_key_enc,
		source_user_id, source_discord_id, source_username, source_text,
		deadline, total_count, remaining_count, status, note, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.Service, d.Model, d.DifyBaseURL, "",
		d.SourceUserID, d.SourceDiscordID, d.SourceUsername, "",
		d.Deadline, 0, 0, d.Status, d.Note, time.Now().Unix(), time.Now().Unix())

	// Should not be able to change expired status.
	rec := donationRequest(gw, admin, "POST", fmt.Sprintf("/api/admin/donations/%d/status", 1),
		map[string]string{"status": "active"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "已失效的捐赠条目不可更改状态") {
		t.Errorf("want expired message, got: %s", rec.Body.String())
	}
}

// TestDeleteDonation verifies deletion works (orphan retention is schema-level).
func TestDeleteDonation(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	u, _ := store.CreateUser("104", "donor4", "")
	deadline := time.Now().Add(24 * time.Hour).Unix()
	d := &db.Donation{
		Service:         "general",
		Model:           "gpt4",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        deadline,
		TotalCount:      10,
		Status:          db.DonationActive,
		Note:            "test",
	}
	created, err := store.CreateDonation(d, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	rec := donationRequest(gw, admin, "DELETE", fmt.Sprintf("/api/admin/donations/%d", created.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	// Verify it's gone
	gone, _ := store.GetDonation(created.ID)
	if gone != nil {
		t.Error("donation should be deleted")
	}
}

// TestDonationAdminAccess verifies that non-admin users cannot access donation endpoints.
func TestDonationAdminAccess(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	// Create a normal user session
	u, err := store.CreateUser("200", "normal", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}
	urls := []string{
		"POST /api/admin/donations",
		"GET /api/admin/donations",
		"POST /api/admin/donations/1/status",
		"DELETE /api/admin/donations/1",
	}
	for _, path := range urls {
		parts := strings.SplitN(path, " ", 2)
		method, urlPath := parts[0], parts[1]
		rec := donationRequest(gw, cookie, method, urlPath, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403; body: %s", method, urlPath, rec.Code, rec.Body.String())
		}
	}
}

// TestUserCharityAPI tests the GET/PUT /api/me/charity endpoints.
func TestUserCharityAPI(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	u, err := store.CreateUser("300", "charity_user", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// GET initial state (should be false)
	req := httptest.NewRequest("GET", "/api/me/charity", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var getResp0 struct {
		CharityEnabled bool `json:"charity_enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp0); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if getResp0.CharityEnabled {
		t.Error("expected charity_enabled=false initially")
	}

	// PUT to enable WITHOUT confirmed → should fail
	body := `{"enabled":true}`
	req2 := httptest.NewRequest("PUT", "/api/me/charity", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec2.Code, rec2.Body.String())
	}

	// PUT to enable WITH confirmed → should succeed
	body3 := `{"enabled":true,"confirmed":true}`
	req3 := httptest.NewRequest("PUT", "/api/me/charity", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec3.Code, rec3.Body.String())
	}

	// Verify enabled: check store directly first
	updatedUser, _ := store.GetUserByID(u.ID)
	if !updatedUser.CharityEnabled {
		t.Errorf("store: charity_enabled is still false after PUT")
	}
	// Verify via API
	req4 := httptest.NewRequest("GET", "/api/me/charity", nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	mux.ServeHTTP(rec4, req4)
	var getResp2 struct {
		CharityEnabled bool `json:"charity_enabled"`
	}
	json.Unmarshal(rec4.Body.Bytes(), &getResp2)
	if !getResp2.CharityEnabled {
		t.Error("expected charity_enabled=true after toggle")
	}

	// PUT to disable (no confirm needed)
	body5 := `{"enabled":false}`
	req5 := httptest.NewRequest("PUT", "/api/me/charity", strings.NewReader(body5))
	req5.Header.Set("Content-Type", "application/json")
	req5.AddCookie(cookie)
	rec5 := httptest.NewRecorder()
	mux.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec5.Code, rec5.Body.String())
	}
}

// TestCharityRouting_Success tests a full charity routing path with a real
// donation entry, verifying the donation is consumed and credits are deducted.
func TestCharityRouting_Success(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("500", "charity_success", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	store.SetUserCharityEnabled(u.ID, true)
	store.SetUserCredits(u.ID, 20)
	store.SetSetting(db.SettingCharityGlobalEnabled, "true")

	// Create a source user (donor) and a donation entry.
	donor, err := store.CreateUser("501", "donor", "")
	if err != nil {
		t.Fatalf("create donor: %v", err)
	}
	donation := &db.Donation{
		Service:         "general",
		Model:           "x",
		DifyBaseURL:     srv.URL,
		SourceUserID:    sql.NullInt64{Int64: donor.ID, Valid: true},
		SourceDiscordID: donor.DiscordID,
		SourceUsername:  donor.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
		Status:          db.DonationActive,
		Note:            "test donation",
	}
	created, err := store.CreateDonation(donation, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	model := "[公益][general]x"
	rec := chatRequest(gw, key, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Verify: donation was consumed
	updated, _ := store.GetDonation(created.ID)
	if updated.RemainingCount != 9 {
		t.Errorf("remaining_count = %d, want 9", updated.RemainingCount)
	}
	if updated.SuccessCount != 1 {
		t.Errorf("success_count = %d, want 1", updated.SuccessCount)
	}

	// Verify: donor's donation_credit increased
	donorUpdated, _ := store.GetUserByID(donor.ID)
	if donorUpdated.DonationCredit != 1 {
		t.Errorf("donor donation_credit = %d, want 1", donorUpdated.DonationCredit)
	}

	// Verify: caller's credits decreased
	callerUpdated, _ := store.GetUserByID(u.ID)
	if callerUpdated.Credits != 10 {
		t.Errorf("caller credits = %d, want 10", callerUpdated.Credits)
	}
}

// TestCharityRouting_GlobalGate checks that charity models return charity_disabled
// when the global switch is off.
func TestCharityRouting_GlobalGate(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("400", "charity_tester", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	// Enable charity for this user
	store.SetUserCharityEnabled(u.ID, true)
	// Give some credits
	store.SetUserCredits(u.ID, 10)

	// Global switch is off by default — request should get charity_disabled.
	model := "[公益][general]x"
	rec := chatRequest(gw, key, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "charity_disabled") {
		t.Errorf("want charity_disabled, got: %s", rec.Body.String())
	}
}

// TestCharityRouting_InsufficientCredits tests that credits=0 returns 403.
func TestCharityRouting_InsufficientCredits(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("401", "creditless", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	store.SetUserCharityEnabled(u.ID, true)
	store.SetUserCredits(u.ID, 0) // No credits

	// Enable global charity
	store.SetSetting(db.SettingCharityGlobalEnabled, "true")

	model := "[公益][general]x"
	rec := chatRequest(gw, key, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "insufficient_credits") {
		t.Errorf("want insufficient_credits, got: %s", rec.Body.String())
	}
}

// TestCharityRouting_UserSwitchOff tests that a user with charity_enabled=false
// receives model_not_found (not revealing the switch's existence) when calling
// a charity model.
func TestCharityRouting_UserSwitchOff(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("403", "no_charity", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	// charity_enabled is false by default — do NOT enable it.
	store.SetUserCredits(u.ID, 10)
	store.SetSetting(db.SettingCharityGlobalEnabled, "true")

	model := "[公益][general]x"
	rec := chatRequest(gw, key, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model_not_found") {
		t.Errorf("want model_not_found, got: %s", rec.Body.String())
	}
}

// TestCharityRouting_NoDonationsAvailable tests that no routable donations
// returns 503.
func TestCharityRouting_NoDonationsAvailable(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("402", "nodonation", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	store.SetUserCharityEnabled(u.ID, true)
	store.SetUserCredits(u.ID, 10)
	store.SetSetting(db.SettingCharityGlobalEnabled, "true")

	// No donations exist, so no routable ones.
	model := "[公益][general]x"
	rec := chatRequest(gw, key, fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
}

// TestPickWeightedDonation verifies that the weighted selection prefers
// shorter-deadline donations.
func TestPickWeightedDonation(t *testing.T) {
	now := time.Now().Unix()
	// Two donations: one with deadline soon (higher weight), one far.
	donations := []*db.Donation{
		{ID: 1, Deadline: now + 120},   // 2 min away → weight ~1/120
		{ID: 2, Deadline: now + 86400}, // 24h away → weight ~1/86400
	}
	// Run many trials and count selections.
	const trials = 500
	count1 := 0
	for i := 0; i < trials; i++ {
		picked := pickWeightedDonation(donations)
		if picked == nil {
			t.Fatal("pick returned nil")
		}
		if picked.ID == 1 {
			count1++
		}
	}
	// Donation 1 (shorter deadline) should win significantly more often.
	if count1 <= trials/2 {
		t.Errorf("donation 1 (short deadline) picked %d/%d times, expected >50%%", count1, trials)
	}
}

// TestLoginRefreshUsername verifies that Discord OAuth login updates
// username/avatar when they differ (simulated via the store directly).
func TestLoginRefreshUsername(t *testing.T) {
	_, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("600", "oldname", "oldavatar")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Update profile
	if err := store.UpdateUserProfile(u.ID, "newname", "newavatar"); err != nil {
		t.Fatalf("update profile: %v", err)
	}

	updated, _ := store.GetUserByID(u.ID)
	if updated.Username != "newname" {
		t.Errorf("username = %q, want newname", updated.Username)
	}
	if updated.Avatar != "newavatar" {
		t.Errorf("avatar = %q, want newavatar", updated.Avatar)
	}
}

// TestConfigValidate_BackendBrackets checks that model names with brackets
// in the backend part are rejected.
func TestConfigValidate_BackendBrackets(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("700", "cfguser", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	// Try creating a config with brackets in backend
	body := map[string]interface{}{
		"model":         "[general]x[sillytavern]y",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test",
		"note":          "",
	}
	rec := donationRequest(gw, cookie, "POST", "/api/configs", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不得包含方括号") {
		t.Errorf("want bracket message, got: %s", rec.Body.String())
	}
}

// TestConfigValidate_CharityPrefix checks that [公益] is rejected.
func TestConfigValidate_CharityPrefix(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("701", "cfguser2", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	// Try creating a config with [公益] prefix — should be rejected.
	body := map[string]interface{}{
		"model":         "[公益][general]x",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test",
		"note":          "",
	}
	rec := donationRequest(gw, cookie, "POST", "/api/configs", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "方括号") {
		t.Errorf("want unified bracket/reserved message, got: %s", rec.Body.String())
	}
}

// TestDonationAdminLogs verifies that charity request logs have the donation_id
// and use the charity model name format.
func TestDonationAdminLogs(t *testing.T) {
	// This test verifies the logRequestDonation function stores donation_id.
	gw, store := setupAuthGateway(t, "x")
	now := time.Now()
	// logRequestDonation should not panic and should store the donation ID.
	gw.logRequestDonation(1, "[公益][general]x", "general", now, "success", "", 200, "", 42)

	logs, _, err := store.ListAllRequestLogs(db.LogFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	found := false
	for _, l := range logs {
		if l.Model == "[公益][general]x" && l.DonationID != nil && *l.DonationID == 42 {
			found = true
			break
		}
	}
	if !found {
		t.Error("charity log entry with donation_id=42 not found")
	}
}
