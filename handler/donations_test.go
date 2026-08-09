package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestGetCharityPricingAvailability(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")

	u, _ := store.CreateUser("42", "tester", "")
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	now := time.Now().Unix()

	// Pricing row 1: active donation with remaining capacity -> available.
	if _, err := store.UpsertPricing("general", "gpt-5.6-sol", 100, nil); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	store.SetPricingEnabled("general", "gpt-5.6-sol", true)
	if _, err := store.CreateDonation(&db.Donation{
		Service: "general", Model: "gpt-5.6-sol", DifyBaseURL: "https://api.dify.ai/v1",
		Deadline: now + 86400, TotalCount: 100, RemainingCount: 50, Status: db.DonationActive,
	}, "k1"); err != nil {
		t.Fatalf("create donation: %v", err)
	}

	// Pricing row 2: only an expired donation -> must show unavailable even
	// though HasDonationsForPair (any status) is true.
	if _, err := store.UpsertPricing("image", "claude-sonnet-4-6", 200, nil); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	store.SetPricingEnabled("image", "claude-sonnet-4-6", true)
	if _, err := store.CreateDonation(&db.Donation{
		Service: "image", Model: "claude-sonnet-4-6", DifyBaseURL: "https://api.dify.ai/v1",
		Deadline: now - 3600, TotalCount: 100, RemainingCount: 50, Status: db.DonationActive,
	}, "k2"); err != nil {
		t.Fatalf("create donation: %v", err)
	}

	rec := donationRequest(gw, cookie, http.MethodGet, "/api/me/charity", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me/charity: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Pricing []struct {
			Service   string `json:"service"`
			Model     string `json:"model"`
			Available bool   `json:"available"`
		} `json:"pricing"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[string]bool{}
	for _, p := range resp.Pricing {
		got[p.Service+"/"+p.Model] = p.Available
	}
	if !got["general/gpt-5.6-sol"] {
		t.Errorf("general/gpt-5.6-sol: want available=true, got %v", got)
	}
	if got["image/claude-sonnet-4-6"] {
		t.Errorf("image/claude-sonnet-4-6: want available=false, got %v", got)
	}
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

// TestListDonations_MaterializesDeadlineExpiry verifies that the admin list
// does not expose an overdue entry as merely inactive/active when the periodic
// sweep has not run yet.
func TestListDonations_MaterializesDeadlineExpiry(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	d, err := store.CreateDonation(&db.Donation{
		Service:     "general",
		Model:       "overdue-list-test",
		DifyBaseURL: "https://dify.example.com/v1",
		Deadline:    time.Now().Add(-time.Hour).Unix(),
		TotalCount:  5,
		Status:      db.DonationInactive,
	}, "app-secret")
	if err != nil {
		t.Fatalf("create overdue donation: %v", err)
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
	var listed map[string]interface{}
	for _, item := range resp.Donations {
		if int64(item["id"].(float64)) == d.ID {
			listed = item
			break
		}
	}
	if listed == nil {
		t.Fatalf("overdue donation %d not found in list", d.ID)
	}
	if listed["status"] != db.DonationExpired {
		t.Errorf("listed status = %v, want expired", listed["status"])
	}
	stored, err := store.GetDonation(d.ID)
	if err != nil {
		t.Fatalf("get expired donation: %v", err)
	}
	if stored.Status != db.DonationExpired {
		t.Errorf("stored status = %q, want expired", stored.Status)
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

	// Create pricing for the (service, model) before re-activating.
	store.UpsertPricing("general", "gpt4", 10, ptr(5))

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

type charityOutcomeFixture struct {
	gw       *Gateway
	key      string
	consumer *db.User
	donor    *db.User
	donation *db.Donation
	service  string
	model    string
}

func setupCharityOutcomeFixture(t *testing.T, difyURL, service, model string) *charityOutcomeFixture {
	t.Helper()
	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, difyURL)
	consumer, err := store.CreateUser("charity-outcome-consumer-"+service+"-"+model, "charity_outcome_consumer", "")
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.SetCallerKey(consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserCharityEnabled(consumer.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserCredits(consumer.ID, 20); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(db.SettingCharityEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	donor, err := store.CreateUser("charity-outcome-donor-"+service+"-"+model, "charity_outcome_donor", "")
	if err != nil {
		t.Fatal(err)
	}
	donation, err := store.CreateDonation(&db.Donation{
		Service:      service,
		Model:        model,
		DifyBaseURL:  difyURL,
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(),
		TotalCount:   10,
		Status:       db.DonationActive,
	}, "app-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertPricing(service, model, 10, ptr(5)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPricingEnabled(service, model, true); err != nil {
		t.Fatal(err)
	}
	return &charityOutcomeFixture{
		gw: gw, key: key, consumer: consumer, donor: donor, donation: donation,
		service: service, model: model,
	}
}

func (f *charityOutcomeFixture) assertCommitted(t *testing.T) {
	t.Helper()
	donation, err := f.gw.Store.GetDonation(f.donation.ID)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := f.gw.Store.GetUserByID(f.consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	donor, err := f.gw.Store.GetUserByID(f.donor.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservations, err := f.gw.Store.ListUserCharityReservations(f.consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 1 || reservations[0].Status != db.ReservationCommitted {
		t.Fatalf("reservations = %+v, want one committed", reservations)
	}
	if donation.RemainingCount != 9 || donation.SuccessCount != 1 || donation.FailureCount != 0 || donation.ConsecutiveFailures != 0 {
		t.Fatalf("donation settlement = remaining %d success %d failure %d consecutive %d, want 9/1/0/0",
			donation.RemainingCount, donation.SuccessCount, donation.FailureCount, donation.ConsecutiveFailures)
	}
	if consumer.Credits != 10 {
		t.Fatalf("consumer credits = %d, want 10", consumer.Credits)
	}
	if donor.Credits != 5 || donor.DonationCredit != 1 {
		t.Fatalf("donor credits/contribution = %d/%d, want 5/1", donor.Credits, donor.DonationCredit)
	}
}

func (f *charityOutcomeFixture) requestBody(stream bool) string {
	return fmt.Sprintf(`{"model":"[公益][%s]%s","messages":[{"role":"user","content":"hello"}],"stream":%t}`,
		f.service, f.model, stream)
}

func TestCharityDispatchedUncertainOutcomesCommit(t *testing.T) {
	tests := []struct {
		name       string
		stream     bool
		handler    http.HandlerFunc
		wantStatus int
	}{
		{
			name: "blocking immediate upstream error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				fmt.Fprint(w, `{"code":"upstream_failed","message":"temporary failure"}`)
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			name: "blocking HTTP 200 workflow failed",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"data":{"status":"failed","error":"workflow failed","outputs":{}}}`)
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			name: "blocking truncated HTTP 200 response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				conn, rw, err := hijacker.Hijack()
				if err != nil {
					return
				}
				fmt.Fprint(rw, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 200\r\n\r\n{\"data\":")
				rw.Flush()
				conn.Close()
			},
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:   "streaming error before first event",
			stream: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"code":"server_overloaded","message":"try later"}`)
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			name:   "streaming truncated first frame",
			stream: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\n\n")
			},
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/workflows/run" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				tc.handler(w, r)
			}))
			defer srv.Close()

			fixture := setupCharityOutcomeFixture(t, srv.URL, "general", strings.ReplaceAll(tc.name, " ", "-"))
			rec := chatRequest(fixture.gw, fixture.key, fixture.requestBody(tc.stream))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			fixture.assertCommitted(t)
		})
	}
}

func TestCharityImageUploadErrorAfterDispatchCommits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/files/upload":
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"code":"upload_failed","message":"temporary failure"}`)
		case "/v1/workflows/run":
			t.Error("workflow request must not run after image upload failure")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	fixture := setupCharityOutcomeFixture(t, srv.URL, "image-processing", "image-upload-error")
	body := `{"model":"[公益][image-processing]image-upload-error","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}]}]}`
	rec := chatRequest(fixture.gw, fixture.key, body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s", rec.Code, rec.Body.String())
	}
	fixture.assertCommitted(t)
}

func TestCharityClientCancelAfterDispatchCommits(t *testing.T) {
	started := make(chan struct{})
	releaseUpstream := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-releaseUpstream:
		}
	}))
	defer srv.Close()
	defer close(releaseUpstream)
	fixture := setupCharityOutcomeFixture(t, srv.URL, "general", "client-cancel")

	mux := http.NewServeMux()
	fixture.gw.RegisterRoutes(mux)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fixture.requestBody(false))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+fixture.key)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("charity request did not stop after client cancellation")
	}
	fixture.assertCommitted(t)
}

// TestCharityRouting_Success tests a full charity routing path with a real
// donation entry, verifying the donation is consumed and credits are deducted.
func TestCharityRouting_Success(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, srv.URL)
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
	store.SetSetting(db.SettingCharityEnabled, "true")

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

	// beta.2: pricing must exist and be enabled for charity routing.
	store.UpsertPricing("general", "x", 10, ptr(5))
	store.SetPricingEnabled("general", "x", true)

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

// TestCharityStreaming_PostStartTransportErrorCountsAsSuccess verifies the
// contract boundary: once Dify has started the SSE stream, the donation use
// is successful even if the stream later fails to parse/transport completely.
func TestCharityStreaming_PostStartTransportErrorCountsAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test response writer does not support flushing")
		}
		fmt.Fprint(w, "data: {\"event\":\"text_chunk\",\"task_id\":\"t\",\"data\":{\"text\":\"partial\"}}\n\n")
		flusher.Flush()
		// The first event has started the stream. A malformed subsequent SSE
		// frame exercises the same post-start transport-error accounting path
		// as a connection reset, without relying on platform-specific TCP RST
		// behavior in the test.
		fmt.Fprint(w, "data: {\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, srv.URL)
	u, err := store.CreateUser("510", "charity_stream_truncated", "")
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	store.SetUserCharityEnabled(u.ID, true)
	store.SetUserCredits(u.ID, 20)
	store.SetSetting(db.SettingCharityEnabled, "true")

	donor, err := store.CreateUser("511", "charity_stream_donor", "")
	if err != nil {
		t.Fatalf("create donor: %v", err)
	}
	donation, err := store.CreateDonation(&db.Donation{
		Service:      "general",
		Model:        "stream-truncated",
		DifyBaseURL:  srv.URL,
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(),
		TotalCount:   10,
		Status:       db.DonationActive,
	}, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}
	if _, err := store.UpsertPricing("general", "stream-truncated", 10, ptr(5)); err != nil {
		t.Fatalf("create pricing: %v", err)
	}
	store.SetPricingEnabled("general", "stream-truncated", true)

	rec := chatRequest(gw, key, `{"model":"[公益][general]stream-truncated","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) || strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("post-start stream error should be an error frame without DONE: %s", rec.Body.String())
	}

	updated, err := store.GetDonation(donation.ID)
	if err != nil {
		t.Fatalf("get donation: %v", err)
	}
	if updated.SuccessCount != 1 || updated.RemainingCount != 9 {
		t.Errorf("donation success accounting = success %d remaining %d, want 1/9", updated.SuccessCount, updated.RemainingCount)
	}
	if updated.FailureCount != 0 || updated.ConsecutiveFailures != 0 {
		t.Errorf("post-start stream error counted as donation failure: failure %d consecutive %d", updated.FailureCount, updated.ConsecutiveFailures)
	}
	reservations, err := store.ListUserCharityReservations(u.ID)
	if err != nil || len(reservations) != 1 || reservations[0].Status != db.ReservationCommitted {
		t.Fatalf("post-start reservation = %+v, err=%v", reservations, err)
	}
	caller, _ := store.GetUserByID(u.ID)
	if caller.Credits != 10 {
		t.Errorf("caller credits = %d, want 10 after committed call", caller.Credits)
	}
	donorUpdated, _ := store.GetUserByID(donor.ID)
	if donorUpdated.DonationCredit != 1 || donorUpdated.Credits != 5 {
		t.Errorf("donor donation_credit/credits = %d/%d, want 1/5", donorUpdated.DonationCredit, donorUpdated.Credits)
	}

	gw.limiter.mu.Lock()
	bHits := len(gw.limiter.hits[rpmClassB][u.ID])
	gw.limiter.mu.Unlock()
	if bHits != 1 {
		t.Errorf("class-B hits = %d, want 1 after stream start", bHits)
	}
}

func TestCharityStreaming_BusinessErrorSanitizedButLoggedRaw(t *testing.T) {
	rawFailure := "failed at https://charity.secret.example 198.51.100.80 [2001:db8::80] app-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"event\":\"error\",\"data\":{\"error\":%q}}\n\n", rawFailure)
	}))
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, srv.URL)
	consumer, err := store.CreateUser("515", "charity_stream_error", "")
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.SetCallerKey(consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.SetUserCharityEnabled(consumer.ID, true)
	store.SetUserCredits(consumer.ID, 20)
	store.SetSetting(db.SettingCharityEnabled, "true")
	donor, err := store.CreateUser("516", "charity_stream_error_donor", "")
	if err != nil {
		t.Fatal(err)
	}
	donation, err := store.CreateDonation(&db.Donation{
		Service:      "general",
		Model:        "stream-error",
		DifyBaseURL:  srv.URL,
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(),
		TotalCount:   10,
		Status:       db.DonationActive,
	}, "app-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertPricing("general", "stream-error", 10, ptr(5)); err != nil {
		t.Fatal(err)
	}
	store.SetPricingEnabled("general", "stream-error", true)

	rec := chatRequest(gw, key, `{"model":"[公益][general]stream-error","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "上游 Dify 工作流执行失败") {
		t.Fatalf("charity SSE response = %d %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{rawFailure, "charity.secret.example", "198.51.100.80", "2001:db8::80", "app-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("charity SSE leaked %q: %s", secret, rec.Body.String())
		}
	}
	logs, err := store.ListRequestLogs(consumer.ID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("charity logs=%+v err=%v", logs, err)
	}
	if logs[0].ErrorDetail != rawFailure {
		t.Fatalf("raw charity SSE error not retained: %+v", logs[0])
	}
	updated, err := store.GetDonation(donation.ID)
	if err != nil || updated.SuccessCount != 1 || updated.RemainingCount != 9 || updated.FailureCount != 0 || updated.ConsecutiveFailures != 0 {
		t.Fatalf("charity business error accounting changed: donation=%+v err=%v", updated, err)
	}
	consumerUpdated, _ := store.GetUserByID(consumer.ID)
	donorUpdated, _ := store.GetUserByID(donor.ID)
	reservations, reservationErr := store.ListUserCharityReservations(consumer.ID)
	if consumerUpdated.Credits != 10 || donorUpdated.Credits != 5 || donorUpdated.DonationCredit != 1 ||
		reservationErr != nil || len(reservations) != 1 || reservations[0].Status != db.ReservationCommitted {
		t.Fatalf("charity business error balances/reservation: consumer=%+v donor=%+v reservations=%+v err=%v",
			consumerUpdated, donorUpdated, reservations, reservationErr)
	}
}

// TestCharityBlockingFailureLogIncludesDonationSource verifies that a failure
// after selecting a donation still links the request log to that donation, so
// the admin log table can resolve source_display.
func TestCharityBlockingFailureLogIncludesDonationSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"code":"upstream_failed","message":"temporary failure"}`)
	}))
	defer srv.Close()

	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, srv.URL)
	u, err := store.CreateUser("520", "charity_failure_caller", "")
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	store.SetUserCharityEnabled(u.ID, true)
	store.SetUserCredits(u.ID, 20)
	store.SetSetting(db.SettingCharityEnabled, "true")

	donor, err := store.CreateUser("521", "charity_failure_donor", "")
	if err != nil {
		t.Fatalf("create donor: %v", err)
	}
	donation, err := store.CreateDonation(&db.Donation{
		Service:      "general",
		Model:        "blocking-failure",
		DifyBaseURL:  srv.URL,
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(),
		TotalCount:   10,
		Status:       db.DonationActive,
	}, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}
	if _, err := store.UpsertPricing("general", "blocking-failure", 10, ptr(5)); err != nil {
		t.Fatalf("create pricing: %v", err)
	}
	store.SetPricingEnabled("general", "blocking-failure", true)

	rec := chatRequest(gw, key, `{"model":"[公益][general]blocking-failure","messages":[{"role":"user","content":"hello"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	adminRec := adminGet(gw, adminCookie(t, gw), "/api/admin/logs?status=error&limit=100")
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin logs status = %d, body: %s", adminRec.Code, adminRec.Body.String())
	}
	var resp struct {
		Logs []struct {
			DonationID    *int64 `json:"donation_id"`
			ErrorCode     string `json:"error_code"`
			SourceDisplay string `json:"source_display"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(adminRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode admin logs: %v", err)
	}
	found := false
	for _, entry := range resp.Logs {
		if entry.ErrorCode != "upstream_error" || entry.DonationID == nil || *entry.DonationID != donation.ID {
			continue
		}
		found = true
		if entry.SourceDisplay != donor.Username {
			t.Errorf("source_display = %q, want %q", entry.SourceDisplay, donor.Username)
		}
	}
	if !found {
		t.Fatalf("failed charity log for donation %d was not linked in admin response: %s", donation.ID, adminRec.Body.String())
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
	store.SetSetting(db.SettingCharityEnabled, "true")

	// beta.2: pricing must exist and be enabled.
	store.UpsertPricing("general", "x", 10, ptr(5))
	store.SetPricingEnabled("general", "x", true)

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
	store.SetSetting(db.SettingCharityEnabled, "true")

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
	store.SetSetting(db.SettingCharityEnabled, "true")

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
		{ID: 1, Deadline: now + 120, RpmLimit: 10000},   // 2 min away → weight ~1/120
		{ID: 2, Deadline: now + 86400, RpmLimit: 10000}, // 24h away → weight ~1/86400
	}
	limiter := newDonationRateLimiter()
	// Run many trials and count selections.
	const trials = 500
	count1 := 0
	for i := 0; i < trials; i++ {
		picked, release := pickWeightedDonation(donations, limiter)
		if picked == nil {
			t.Fatal("pick returned nil")
		}
		if picked.ID == 1 {
			count1++
		}
		release()
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
	gw.logRequestDonation(1, "[公益][general]x", "general", now, "success", "", 200, "", 42, 0, "")

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

// --- B1: User self-service donation applications ---

func appUserCookie(t *testing.T, gw *Gateway, store *db.Store) *http.Cookie {
	t.Helper()
	u, err := store.CreateUser("200", "testuser", "avatar")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Create a session directly.
	tok, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: tok}
}

// TestCreateDonationApplication_Submit submits a valid application and verifies it.
func TestCreateDonationApplication_Submit(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	// Enable donation system.
	store.SetSetting(db.SettingDonationEnabled, "true")
	cookie := appUserCookie(t, gw, store)

	deadline := time.Now().Add(48 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "claude-opus-4-6",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key-123",
		"deadline":      deadline,
		"total_count":   100,
		"note":          "测试捐赠申请",
	}
	rec := donationRequest(gw, cookie, "POST", "/api/me/donations", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK          bool                   `json:"ok"`
		Application map[string]interface{} `json:"application"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
	a := resp.Application
	if a["service"] != "general" {
		t.Errorf("service = %v", a["service"])
	}
	if a["model"] != "claude-opus-4-6" {
		t.Errorf("model = %v", a["model"])
	}
	if a["status"] != "pending" {
		t.Errorf("status = %v", a["status"])
	}
	if a["has_key"] != true {
		t.Errorf("has_key = %v", a["has_key"])
	}
	if a["total_count"] != float64(100) {
		t.Errorf("total_count = %v", a["total_count"])
	}
}

// TestCreateDonationApplication_ServiceGate rejects a service the admin has
// turned off for self-service donations, and verifies the services endpoint
// hides it behind ?donation=1.
func TestCreateDonationApplication_ServiceGate(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	cookie := appUserCookie(t, gw, store)

	// Disable "general" for self-service donations.
	if _, err := store.UpsertAntiAbuseConfig("general", 2, 20, 0, 0, 0); err != nil {
		t.Fatalf("disable service: %v", err)
	}

	// /api/services must still list it (config forms need all services) ...
	rec := donationRequest(gw, cookie, http.MethodGet, "/api/services", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"general"`) {
		t.Fatalf("/api/services: status %d body %s", rec.Code, rec.Body.String())
	}
	// ... but ?donation=1 must hide it.
	rec = donationRequest(gw, cookie, http.MethodGet, "/api/services?donation=1", nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"name":"general"`) {
		t.Fatalf("/api/services?donation=1: status %d body %s", rec.Code, rec.Body.String())
	}

	// The application endpoint must reject the disabled service (defense in depth).
	deadline := time.Now().Add(48 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "claude-opus-4-6",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key-123",
		"deadline":      deadline,
		"total_count":   100,
	}
	rec = donationRequest(gw, cookie, "POST", "/api/me/donations", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不接受自助捐赠申请") {
		t.Errorf("body should mention the service is not accepting self donations, got: %s", rec.Body.String())
	}
}

// TestCreateDonationApplication_DisabledGate rejects submission when donation_enabled is off.
func TestCreateDonationApplication_DisabledGate(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	// donation_enabled defaults to false.
	cookie := appUserCookie(t, gw, store)

	deadline := time.Now().Add(48 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "claude-opus-4-6",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"deadline":      deadline,
		"total_count":   100,
	}
	rec := donationRequest(gw, cookie, "POST", "/api/me/donations", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "donation_disabled") {
		t.Errorf("want donation_disabled, got: %s", rec.Body.String())
	}
}

// TestCreateDonationApplication_PendingLimit rejects when user already has N pending.
func TestCreateDonationApplication_PendingLimit(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	store.SetSetting(db.SettingDonationReviewLimit, "1") // cap at 1 pending
	cookie := appUserCookie(t, gw, store)

	deadline := time.Now().Add(48 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "m1",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key-1",
		"deadline":      deadline,
		"total_count":   100,
	}

	// First submission should succeed.
	rec := donationRequest(gw, cookie, "POST", "/api/me/donations", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("first submit: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Second submission should be rejected.
	body2 := map[string]interface{}{
		"service":       "general",
		"model":         "m2",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key-2",
		"deadline":      deadline,
		"total_count":   200,
	}
	rec2 := donationRequest(gw, cookie, "POST", "/api/me/donations", body2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("second submit: status = %d, want 400; body: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "too_many_pending") {
		t.Errorf("want too_many_pending, got: %s", rec2.Body.String())
	}
}

func TestCreateDonationApplication_ZeroReviewLimitPersistsAndStopsFirstSubmission(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	keyPath := filepath.Join(dir, "test.key")
	gw, store := setupAuthGatewayAt(t, "x", dbPath, keyPath)
	adminSession := loginCookie(t, gw, "root", "x")
	userSession := appUserCookie(t, gw, store)

	rec := adminPut(gw, adminSession, "/api/admin/settings", `{"donation_enabled":true,"donation_review_limit":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	assertZeroLimit := func(t *testing.T, current *Gateway) {
		t.Helper()
		adminRec := adminGet(current, adminSession, "/api/admin/settings")
		if adminRec.Code != http.StatusOK {
			t.Fatalf("GET admin settings: status = %d, body: %s", adminRec.Code, adminRec.Body.String())
		}
		var adminSettings map[string]interface{}
		if err := json.NewDecoder(adminRec.Body).Decode(&adminSettings); err != nil {
			t.Fatalf("decode admin settings: %v", err)
		}
		if got := adminSettings["donation_review_limit"]; got != float64(0) {
			t.Errorf("admin donation_review_limit = %v, want 0", got)
		}

		mux := http.NewServeMux()
		current.RegisterRoutes(mux)
		siteReq := httptest.NewRequest(http.MethodGet, "/api/site-info", nil)
		siteRec := httptest.NewRecorder()
		mux.ServeHTTP(siteRec, siteReq)
		if siteRec.Code != http.StatusOK {
			t.Fatalf("GET site-info: status = %d, body: %s", siteRec.Code, siteRec.Body.String())
		}
		var siteInfo map[string]interface{}
		if err := json.NewDecoder(siteRec.Body).Decode(&siteInfo); err != nil {
			t.Fatalf("decode site-info: %v", err)
		}
		if got := siteInfo["donation_review_limit"]; got != float64(0) {
			t.Errorf("site donation_review_limit = %v, want 0", got)
		}

		body := map[string]interface{}{
			"service":       "general",
			"model":         "first-while-paused",
			"dify_base_url": "https://dify.example.com/v1",
			"dify_api_key":  "app-test-key",
			"deadline":      time.Now().Add(48 * time.Hour).Unix(),
			"total_count":   100,
		}
		appRec := donationRequest(current, userSession, http.MethodPost, "/api/me/donations", body)
		if appRec.Code != http.StatusBadRequest || !strings.Contains(appRec.Body.String(), "too_many_pending") {
			t.Fatalf("first submission with zero limit: status = %d, body: %s", appRec.Code, appRec.Body.String())
		}
	}

	assertZeroLimit(t, gw)
	if err := store.Close(); err != nil {
		t.Fatalf("close database before reopen: %v", err)
	}
	reopened, err := db.Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	restarted := NewGateway(testConfig(), reopened)
	disableAntiAbuseForTest(t, restarted)
	assertZeroLimit(t, restarted)
}

// TestCreateDonationApplication_Validation tests various validation rejections.
func TestCreateDonationApplication_Validation(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	cookie := appUserCookie(t, gw, store)

	deadline := time.Now().Add(48 * time.Hour).Unix()

	tests := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{
			"brackets in model",
			map[string]interface{}{"service": "general", "model": "[bad]x", "dify_base_url": "https://dify.example.com/v1", "dify_api_key": "k", "deadline": deadline, "total_count": 100},
			http.StatusBadRequest,
		},
		{
			"invalid service",
			map[string]interface{}{"service": "nonexistent", "model": "x", "dify_base_url": "https://dify.example.com/v1", "dify_api_key": "k", "deadline": deadline, "total_count": 100},
			http.StatusBadRequest,
		},
		{
			"past deadline",
			map[string]interface{}{"service": "general", "model": "x", "dify_base_url": "https://dify.example.com/v1", "dify_api_key": "k", "deadline": 1, "total_count": 100},
			http.StatusBadRequest,
		},
		{
			"zero total_count",
			map[string]interface{}{"service": "general", "model": "x", "dify_base_url": "https://dify.example.com/v1", "dify_api_key": "k", "deadline": deadline, "total_count": 0},
			http.StatusBadRequest,
		},
		{
			"empty api key",
			map[string]interface{}{"service": "general", "model": "x", "dify_base_url": "https://dify.example.com/v1", "dify_api_key": "", "deadline": deadline, "total_count": 100},
			http.StatusBadRequest,
		},
		{
			"invalid base url",
			map[string]interface{}{"service": "general", "model": "x", "dify_base_url": "ftp://bad", "dify_api_key": "k", "deadline": deadline, "total_count": 100},
			http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := donationRequest(gw, cookie, "POST", "/api/me/donations", tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestListMyApplications returns the user's own applications.
func TestListMyApplications(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	cookie := appUserCookie(t, gw, store)

	// Submit two applications.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	for i := 0; i < 2; i++ {
		rec := donationRequest(gw, cookie, "POST", "/api/me/donations", map[string]interface{}{
			"service":       "general",
			"model":         fmt.Sprintf("m%d", i),
			"dify_base_url": "https://dify.example.com/v1",
			"dify_api_key":  fmt.Sprintf("key%d", i),
			"deadline":      deadline,
			"total_count":   100,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("submit %d: status = %d", i, rec.Code)
		}
	}

	// List.
	rec := donationRequest(gw, cookie, "GET", "/api/me/donations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}

	var resp struct {
		Applications []map[string]interface{} `json:"applications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(resp.Applications) != 2 {
		t.Errorf("want 2 applications, got %d", len(resp.Applications))
	}
	for _, a := range resp.Applications {
		if a["status"] != "pending" {
			t.Errorf("status = %v", a["status"])
		}
		// API key must NOT be exposed.
		if _, ok := a["dify_api_key"]; ok {
			t.Error("dify_api_key should not be exposed in list")
		}
	}
}

// TestAdminApproveRejectApplication tests the full review flow.
func TestAdminApproveRejectApplication(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	userC := appUserCookie(t, gw, store)
	adminC := adminCookie(t, gw)

	// User submits an application.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	rec := donationRequest(gw, userC, "POST", "/api/me/donations", map[string]interface{}{
		"service":       "general",
		"model":         "claude-opus-4-6",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"deadline":      deadline,
		"total_count":   100,
		"note":          "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("submit: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var submitResp struct {
		Application map[string]interface{} `json:"application"`
	}
	json.Unmarshal(rec.Body.Bytes(), &submitResp)
	appID := int64(submitResp.Application["id"].(float64))

	// Admin lists pending.
	rec2 := donationRequest(gw, adminC, "GET", "/api/admin/donations/pending", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list pending: status = %d", rec2.Code)
	}
	var pendingResp struct {
		Applications []map[string]interface{} `json:"applications"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &pendingResp)
	if len(pendingResp.Applications) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pendingResp.Applications))
	}

	// Admin approves with modified fields.
	rec3 := donationRequest(gw, adminC, "POST", fmt.Sprintf("/api/admin/donations/%d/approve", appID), map[string]interface{}{
		"dify_api_key": "app-modified-key",
		"review_note":  "密钥已更新，审核通过",
	})
	if rec3.Code != http.StatusOK {
		t.Fatalf("approve: status = %d, body: %s", rec3.Code, rec3.Body.String())
	}
	var apprResp struct {
		OK          bool                   `json:"ok"`
		Application map[string]interface{} `json:"application"`
		Donation    map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &apprResp)
	if !apprResp.OK {
		t.Fatal("expected ok=true")
	}
	if apprResp.Application["status"] != "approved" {
		t.Errorf("application status = %v", apprResp.Application["status"])
	}
	if apprResp.Donation["status"] != "inactive" {
		t.Errorf("donation status = %v (want inactive)", apprResp.Donation["status"])
	}
	if apprResp.Donation["source_user_id"] == nil {
		t.Error("donation should have source_user_id set to applicant")
	}

	// Pending list should be empty now.
	rec4 := donationRequest(gw, adminC, "GET", "/api/admin/donations/pending", nil)
	json.Unmarshal(rec4.Body.Bytes(), &pendingResp)
	if len(pendingResp.Applications) != 0 {
		t.Errorf("want 0 pending, got %d", len(pendingResp.Applications))
	}

	// Submit another application and reject it.
	rec5 := donationRequest(gw, userC, "POST", "/api/me/donations", map[string]interface{}{
		"service":       "general",
		"model":         "claude-haiku",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-key-2",
		"deadline":      deadline,
		"total_count":   50,
	})
	var sub2 struct {
		Application map[string]interface{} `json:"application"`
	}
	json.Unmarshal(rec5.Body.Bytes(), &sub2)
	appID2 := int64(sub2.Application["id"].(float64))

	rec6 := donationRequest(gw, adminC, "POST", fmt.Sprintf("/api/admin/donations/%d/reject", appID2), map[string]interface{}{
		"review_note": "模型名不符合规范",
	})
	if rec6.Code != http.StatusOK {
		t.Fatalf("reject: status = %d, body: %s", rec6.Code, rec6.Body.String())
	}
	var rejResp struct {
		Application map[string]interface{} `json:"application"`
	}
	json.Unmarshal(rec6.Body.Bytes(), &rejResp)
	if rejResp.Application["status"] != "rejected" {
		t.Errorf("status = %v", rejResp.Application["status"])
	}
	if rejResp.Application["review_note"] != "模型名不符合规范" {
		t.Errorf("review_note = %v", rejResp.Application["review_note"])
	}
}

// TestDonationAppAdminAccess verifies that user endpoints are admin-protected.
func TestDonationAppAdminAccess(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	cookie := appUserCookie(t, gw, store)

	// User cannot access admin pending list.
	rec := donationRequest(gw, cookie, "GET", "/api/admin/donations/pending", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("user access pending: status = %d, want 403", rec.Code)
	}

	// User cannot approve.
	rec2 := donationRequest(gw, cookie, "POST", "/api/admin/donations/1/approve", map[string]interface{}{})
	if rec2.Code != http.StatusForbidden {
		t.Errorf("user access approve: status = %d, want 403", rec2.Code)
	}

	// User cannot reject.
	rec3 := donationRequest(gw, cookie, "POST", "/api/admin/donations/1/reject", map[string]interface{}{})
	if rec3.Code != http.StatusForbidden {
		t.Errorf("user access reject: status = %d, want 403", rec3.Code)
	}

	// Unauthenticated cannot submit.
	rec4 := donationRequest(gw, nil, "POST", "/api/me/donations", map[string]interface{}{})
	if rec4.Code != http.StatusUnauthorized {
		t.Errorf("anonymous submit: status = %d, want 401", rec4.Code)
	}

	// Unauthenticated cannot list.
	rec5 := donationRequest(gw, nil, "GET", "/api/me/donations", nil)
	if rec5.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list: status = %d, want 401", rec5.Code)
	}
}

// TestPickWeightedDonation_RpmFilter verifies that candidates at RPM limit
// are excluded from selection.
func TestPickWeightedDonation_RpmFilter(t *testing.T) {
	now := time.Now().Unix()
	limiter := newDonationRateLimiter()

	// Fill up donation 1's RPM quota.
	for i := 0; i < 5; i++ {
		if _, ok := limiter.acquire(1, 5); !ok {
			t.Fatalf("pre-fill step %d unexpectedly blocked", i)
		}
	}

	donations := []*db.Donation{
		{ID: 1, Deadline: now + 120, RpmLimit: 5},
		{ID: 2, Deadline: now + 86400, RpmLimit: 10000},
		{ID: 3, Deadline: now + 600, RpmLimit: 10000},
	}

	// Run many trials: donation 1 should never be selected.
	const trials = 200
	for i := 0; i < trials; i++ {
		picked, release := pickWeightedDonation(donations, limiter)
		if picked == nil {
			t.Fatal("pick returned nil")
		}
		if picked.ID == 1 {
			t.Errorf("trial %d: donation 1 was selected despite being at RPM limit", i)
		}
		release()
	}
}

// TestPickWeightedDonation_AllOverloaded verifies nil return when all
// candidates are at RPM limit.
func TestPickWeightedDonation_AllOverloaded(t *testing.T) {
	now := time.Now().Unix()
	limiter := newDonationRateLimiter()

	// Fill up both donations' RPM quotas.
	for i := 0; i < 3; i++ {
		_, _ = limiter.acquire(1, 3)
		_, _ = limiter.acquire(2, 3)
	}

	donations := []*db.Donation{
		{ID: 1, Deadline: now + 120, RpmLimit: 3},
		{ID: 2, Deadline: now + 86400, RpmLimit: 3},
	}

	picked, _ := pickWeightedDonation(donations, limiter)
	if picked != nil {
		t.Errorf("expected nil when all overloaded, got donation %d", picked.ID)
	}
}

// TestPatchDonation verifies partial field updates via PATCH.
func TestPatchDonation(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create a donation.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	rec := donationRequest(gw, adminC, "POST", "/api/admin/donations", map[string]interface{}{
		"service":       "general",
		"model":         "patch-test",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"deadline":      deadline,
		"total_count":   100,
		"source_text":   "test source",
		"note":          "original note",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var cr struct {
		Donation map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec.Body.Bytes(), &cr)
	donID := int64(cr.Donation["id"].(float64))

	// Verify rpm_limit default is 10.
	if v, ok := cr.Donation["rpm_limit"]; !ok || int(v.(float64)) != 10 {
		t.Errorf("default rpm_limit: got %v, want 10", cr.Donation["rpm_limit"])
	}

	// Patch only rpm_limit.
	rec2 := donationRequest(gw, adminC, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{
		"rpm_limit": 20,
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("patch rpm_limit: status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	var pr struct {
		OK         bool                   `json:"ok"`
		Donation   map[string]interface{} `json:"donation"`
		Validation map[string]interface{} `json:"validation"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &pr)
	if !pr.OK {
		t.Fatal("expected ok=true")
	}
	if v := pr.Donation["rpm_limit"]; int(v.(float64)) != 20 {
		t.Errorf("rpm_limit after patch: got %v, want 20", v)
	}
	// Note should be unchanged.
	if v := pr.Donation["note"]; v != "original note" {
		t.Errorf("note after patch: got %v, want 'original note'", v)
	}
	// Validation: no URL/key change, so "no validation needed".
	if v := pr.Validation["compatible"]; v != true {
		t.Errorf("validation.compatible = %v", v)
	}

	// Patch multiple fields: note + status.
	rec3 := donationRequest(gw, adminC, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{
		"note":   "updated note",
		"status": "inactive",
	})
	if rec3.Code != http.StatusOK {
		t.Fatalf("patch note+status: status = %d, body: %s", rec3.Code, rec3.Body.String())
	}
	json.Unmarshal(rec3.Body.Bytes(), &pr)
	if pr.Donation["note"] != "updated note" {
		t.Errorf("note: got %v", pr.Donation["note"])
	}
	if pr.Donation["status"] != "inactive" {
		t.Errorf("status: got %v", pr.Donation["status"])
	}

	// Verify in DB.
	d, _ := store.GetDonation(donID)
	if d == nil {
		t.Fatal("donation missing after patch")
	}
	if d.RpmLimit != 20 {
		t.Errorf("db rpm_limit = %d, want 20", d.RpmLimit)
	}
	if d.Note != "updated note" {
		t.Errorf("db note = %q, want 'updated note'", d.Note)
	}
	if d.Status != db.DonationInactive {
		t.Errorf("db status = %s, want inactive", d.Status)
	}

	// Patch expired donation returns 400.
	store.SetDonationStatus(donID, db.DonationExpired)
	rec4 := donationRequest(gw, adminC, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{
		"note": "should fail",
	})
	if rec4.Code != http.StatusBadRequest {
		t.Errorf("patch expired: status = %d, want 400", rec4.Code)
	}

	// Patch non-existent donation returns 404.
	rec5 := donationRequest(gw, adminC, "PATCH", "/api/admin/donations/99999", map[string]interface{}{
		"note": "nope",
	})
	if rec5.Code != http.StatusNotFound {
		t.Errorf("patch missing: status = %d, want 404", rec5.Code)
	}

	// Unauthenticated / non-admin blocked.
	rec6 := donationRequest(gw, nil, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{})
	if rec6.Code != http.StatusForbidden {
		t.Errorf("anonymous patch: status = %d, want 403", rec6.Code)
	}
}

// --- Donation reward + source display + default inactive ---

// TestCreateDonation_DefaultInactive verifies that admin-created donations
// are created with inactive status (consistent with three-layer safety policy).
func TestCreateDonation_DefaultInactive(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	deadline := time.Now().Add(24 * time.Hour).Unix()
	body := map[string]interface{}{
		"service":       "general",
		"model":         "default-inactive-test",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"source_text":   "test source",
		"deadline":      deadline,
		"total_count":   5,
		"note":          "test default inactive",
	}
	rec := donationRequest(gw, admin, "POST", "/api/admin/donations", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK       bool                   `json:"ok"`
		Donation map[string]interface{} `json:"donation"`
		Warning  string                 `json:"warning"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
	if resp.Donation["status"] != "inactive" {
		t.Errorf("status = %v, want inactive", resp.Donation["status"])
	}
	// No pricing exists → warning should be present.
	if resp.Warning == "" {
		t.Error("expected pricing warning for un-priced model")
	}
}

func settleCharityForTest(t *testing.T, gw *Gateway, consumerID int64, donation *db.Donation, pricing *db.CharityPricing) {
	t.Helper()
	reservation, err := gw.Store.ReserveCharityCall(context.Background(), consumerID, donation.ID, pricing.Price, pricing.Reward)
	if err != nil {
		t.Fatalf("reserve charity call: %v", err)
	}
	if err := gw.Store.MarkCharityDispatched(context.Background(), reservation.ID); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}
	gw.charityCommitAccounting(reservation)
}

// TestCharitySuccessAccounting_Reward verifies that the reward is granted
// to the donor and cost is deducted from the consumer.
func TestCharitySuccessAccounting_Reward(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	// Create consumer user with 100 credits.
	consumer, err := store.CreateUser("800", "consumer", "")
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	store.SetUserCredits(consumer.ID, 100)

	// Create donor user with 0 credits.
	donor, err := store.CreateUser("801", "donor", "")
	if err != nil {
		t.Fatalf("create donor: %v", err)
	}
	store.SetUserCredits(donor.ID, 0)

	// Create pricing: price=31, reward=ceil(31*0.5)=16.
	pricing, err := store.UpsertPricing("general", "reward-test", 31, nil)
	if err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	if pricing.Reward != 16 {
		t.Fatalf("expected reward=16, got %d", pricing.Reward)
	}

	// Create donation.
	donation := &db.Donation{
		ID:              1,
		Service:         "general",
		Model:           "reward-test",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: donor.ID, Valid: true},
		SourceDiscordID: donor.DiscordID,
		SourceUsername:  donor.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
		RemainingCount:  10,
		Status:          db.DonationActive,
	}
	created, err := store.CreateDonation(donation, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	settleCharityForTest(t, gw, consumer.ID, created, pricing)

	// Verify consumer: 100 - 31 = 69.
	cu, _ := store.GetUserByID(consumer.ID)
	if cu.Credits != 69 {
		t.Errorf("consumer credits = %d, want 69", cu.Credits)
	}

	// Verify donor: 0 + 16 = 16.
	du, _ := store.GetUserByID(donor.ID)
	if du.Credits != 16 {
		t.Errorf("donor credits = %d, want 16", du.Credits)
	}
}

// TestCharitySuccessAccounting_NoRewardWhenZero verifies that reward=0 does
// not grant credits.
func TestCharitySuccessAccounting_NoRewardWhenZero(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	consumer, err := store.CreateUser("802", "consumer2", "")
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	store.SetUserCredits(consumer.ID, 100)

	donor, err := store.CreateUser("803", "donor2", "")
	if err != nil {
		t.Fatalf("create donor: %v", err)
	}
	store.SetUserCredits(donor.ID, 0)

	// Pricing with reward=0 (UpsertPricing auto-fills reward from price;
	// use a low price so reward=ceil(1*0.5)=1 — need to manually set 0 after).
	// UpsertPricing sets reward=1 for price=1. Override via raw SQL.
	_, err = store.UpsertPricing("general", "reward-zero", 0, nil)
	if err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	pricing, _ := store.GetPricing("general", "reward-zero")
	// Force reward=0.
	store.RawExec(`UPDATE charity_pricing SET reward=0 WHERE service='general' AND model='reward-zero'`)
	pricing.Reward = 0

	donation := &db.Donation{
		Service:         "general",
		Model:           "reward-zero",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: donor.ID, Valid: true},
		SourceDiscordID: donor.DiscordID,
		SourceUsername:  donor.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
		RemainingCount:  10,
		Status:          db.DonationActive,
	}
	created, err := store.CreateDonation(donation, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	settleCharityForTest(t, gw, consumer.ID, created, pricing)

	// Consumer: 100 - 0 = 100 (price is 0).
	cu, _ := store.GetUserByID(consumer.ID)
	if cu.Credits != 100 {
		t.Errorf("consumer credits = %d, want 100", cu.Credits)
	}

	// Donor: 0 + 0 = 0.
	du, _ := store.GetUserByID(donor.ID)
	if du.Credits != 0 {
		t.Errorf("donor credits = %d, want 0", du.Credits)
	}
}

// TestCharitySuccessAccounting_NoRewardWhenNoSourceUser verifies that no
// reward is granted when the donation has no source user.
func TestCharitySuccessAccounting_NoRewardWhenNoSourceUser(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")

	consumer, err := store.CreateUser("804", "consumer3", "")
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	store.SetUserCredits(consumer.ID, 100)

	pricing, err := store.UpsertPricing("general", "no-source", 31, nil)
	if err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}

	// No source user — source_text only.
	donation := &db.Donation{
		Service:        "general",
		Model:          "no-source",
		DifyBaseURL:    "https://dify.example.com/v1",
		SourceText:     "第三方捐赠",
		Deadline:       time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:     10,
		RemainingCount: 10,
		Status:         db.DonationActive,
	}
	created, err := store.CreateDonation(donation, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}

	settleCharityForTest(t, gw, consumer.ID, created, pricing)

	// Consumer: 100 - 31 = 69.
	cu, _ := store.GetUserByID(consumer.ID)
	if cu.Credits != 69 {
		t.Errorf("consumer credits = %d, want 69", cu.Credits)
	}
	// No donor to reward.
}

// TestListDonations_SourceDisplay verifies that list endpoint returns
// source_display populated (was a bug where donationJSON was used
// instead of enrichDonationJSON).
func TestListDonations_SourceDisplay(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	// Create a source user and donation.
	u, err := store.CreateUser("900", "source_user_display", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	donation := &db.Donation{
		Service:         "general",
		Model:           "display-test",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
		Status:          db.DonationActive,
		Note:            "test",
	}
	if _, err := store.CreateDonation(donation, "app-secret"); err != nil {
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

	d := resp.Donations[0]
	sd, ok := d["source_display"].(string)
	if !ok || sd == "" {
		t.Errorf("source_display empty or not a string: %v", d["source_display"])
	}
	// Should resolve to the source user's username.
	if sd != "source_user_display" {
		t.Errorf("source_display = %q, want %q", sd, "source_user_display")
	}
}

// --- review_note PATCH ---

// TestDonationPatch_ReviewNote verifies that an admin can update review_note
// on a donation that originated from an application.
func TestDonationPatch_ReviewNote(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	userC := appUserCookie(t, gw, store)
	adminC := adminCookie(t, gw)

	// User submits an application.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	rec := donationRequest(gw, userC, "POST", "/api/me/donations", map[string]interface{}{
		"service":       "general",
		"model":         "review-note-test",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"deadline":      deadline,
		"total_count":   100,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("submit: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var submitResp struct {
		Application map[string]interface{} `json:"application"`
	}
	json.Unmarshal(rec.Body.Bytes(), &submitResp)
	appID := int64(submitResp.Application["id"].(float64))

	// Admin approves, creating a donation linked to the application.
	rec2 := donationRequest(gw, adminC, "POST", fmt.Sprintf("/api/admin/donations/%d/approve", appID), map[string]interface{}{
		"review_note": "初始审核备注",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("approve: status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	var apprResp struct {
		Donation map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &apprResp)
	donID := int64(apprResp.Donation["id"].(float64))

	// Patch review_note on the donation.
	rec3 := donationRequest(gw, adminC, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{
		"review_note": "管理员更新后的备注",
	})
	if rec3.Code != http.StatusOK {
		t.Fatalf("patch review_note: status = %d, body: %s", rec3.Code, rec3.Body.String())
	}
	var patchResp struct {
		OK       bool                   `json:"ok"`
		Donation map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec3.Body.Bytes(), &patchResp)
	if !patchResp.OK {
		t.Fatal("expected ok=true")
	}

	// Verify review_note is actually persisted in the application record.
	app, _ := store.GetApplication(appID)
	if app == nil {
		t.Fatal("application not found")
	}
	if app.ReviewNote != "管理员更新后的备注" {
		t.Errorf("review_note = %q, want %q", app.ReviewNote, "管理员更新后的备注")
	}

	// Verify the original user note on the donation is unchanged.
	d, _ := store.GetDonation(donID)
	if d != nil && d.Note != "" {
		t.Errorf("donation note should be empty after approval (was %q)", d.Note)
	}
}

// TestDonationPatch_ReviewNoteNoApplication verifies that patching review_note
// on a donation without a corresponding application (admin-created) succeeds
// silently without error.
func TestDonationPatch_ReviewNoteNoApplication(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Admin creates a donation directly (no application).
	deadline := time.Now().Add(48 * time.Hour).Unix()
	rec := donationRequest(gw, adminC, "POST", "/api/admin/donations", map[string]interface{}{
		"service":       "general",
		"model":         "direct-donation",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key-2",
		"deadline":      deadline,
		"total_count":   50,
		"source_text":   "管理员直接创建",
		"note":          "管理员直接创建",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var cr struct {
		Donation map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec.Body.Bytes(), &cr)
	donID := int64(cr.Donation["id"].(float64))

	// Patch review_note — should succeed even though no application record exists.
	rec2 := donationRequest(gw, adminC, "PATCH", fmt.Sprintf("/api/admin/donations/%d", donID), map[string]interface{}{
		"review_note": "此捐赠无对应申请",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("patch review_note on direct donation: status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	var patchResp struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &patchResp)
	if !patchResp.OK {
		t.Fatal("expected ok=true")
	}
}

// TestAdminListApplications tests GET /api/admin/donations/applications with filters.
func TestAdminListApplications(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	userC := appUserCookie(t, gw, store)
	adminC := adminCookie(t, gw)

	deadline := time.Now().Add(48 * time.Hour).Unix()

	// Submit several applications to create test data.
	submit := func(model, note string) int64 {
		rec := donationRequest(gw, userC, "POST", "/api/me/donations", map[string]interface{}{
			"service":       "general",
			"model":         model,
			"dify_base_url": "https://dify.example.com/v1",
			"dify_api_key":  "app-key-" + model,
			"deadline":      deadline,
			"total_count":   100,
			"note":          note,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("submit %s: status = %d, body: %s", model, rec.Code, rec.Body.String())
		}
		var resp struct {
			Application map[string]interface{} `json:"application"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return int64(resp.Application["id"].(float64))
	}

	id1 := submit("model-a", "note-a")
	_ = submit("model-b", "note-b")

	// Reject the first application.
	recRej := donationRequest(gw, adminC, "POST", fmt.Sprintf("/api/admin/donations/%d/reject", id1), map[string]interface{}{
		"review_note": "not good",
	})
	if recRej.Code != http.StatusOK {
		t.Fatalf("reject: status = %d, body: %s", recRej.Code, recRej.Body.String())
	}

	// Test: no params → all applications.
	rec := donationRequest(gw, adminC, "GET", "/api/admin/donations/applications", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list all: status = %d", rec.Code)
	}
	var resp struct {
		Applications []map[string]interface{} `json:"applications"`
		Total        int                      `json:"total"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total < 2 {
		t.Errorf("total = %d, want >= 2", resp.Total)
	}

	// Test: ?status=rejected → only rejected.
	rec2 := donationRequest(gw, adminC, "GET", "/api/admin/donations/applications?status=rejected", nil)
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Errorf("status=rejected total = %d, want 1", resp.Total)
	}
	if len(resp.Applications) != 1 {
		t.Errorf("status=rejected apps = %d, want 1", len(resp.Applications))
	}
	if resp.Applications[0]["status"] != "rejected" {
		t.Errorf("status = %v, want rejected", resp.Applications[0]["status"])
	}

	// Test: ?status=pending → only pending.
	rec3 := donationRequest(gw, adminC, "GET", "/api/admin/donations/applications?status=pending", nil)
	json.Unmarshal(rec3.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Errorf("status=pending total = %d, want 1", resp.Total)
	}

	// Test: ?service=general → both.
	rec4 := donationRequest(gw, adminC, "GET", "/api/admin/donations/applications?service=general", nil)
	json.Unmarshal(rec4.Body.Bytes(), &resp)
	if resp.Total < 2 {
		t.Errorf("service=general total = %d, want >= 2", resp.Total)
	}

	// Test: ?user_id=N → only that user's applications.
	// Get the user's ID by fetching from DB.
	u, _ := store.GetUserByDiscordID("200")
	if u == nil {
		t.Fatal("test user not found")
	}
	rec5 := donationRequest(gw, adminC, "GET", fmt.Sprintf("/api/admin/donations/applications?user_id=%d", u.ID), nil)
	json.Unmarshal(rec5.Body.Bytes(), &resp)
	if resp.Total < 2 {
		t.Errorf("user_id filter total = %d, want >= 2", resp.Total)
	}

	// Test: time range filter.
	now := time.Now().Unix()
	rec6 := donationRequest(gw, adminC, "GET",
		fmt.Sprintf("/api/admin/donations/applications?since=%d", now-3600), nil)
	json.Unmarshal(rec6.Body.Bytes(), &resp)
	if resp.Total < 2 {
		t.Errorf("time range total = %d, want >= 2", resp.Total)
	}
	// Future time range should return none.
	rec7 := donationRequest(gw, adminC, "GET",
		fmt.Sprintf("/api/admin/donations/applications?since=%d", now+86400), nil)
	json.Unmarshal(rec7.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Errorf("future time total = %d, want 0", resp.Total)
	}

	// Test: non-admin cannot access.
	rec8 := donationRequest(gw, userC, "GET", "/api/admin/donations/applications", nil)
	if rec8.Code != http.StatusForbidden {
		t.Errorf("user access: status = %d, want 403", rec8.Code)
	}

	// Test: limit/offset pagination.
	rec9 := donationRequest(gw, adminC, "GET", "/api/admin/donations/applications?limit=1&offset=0", nil)
	json.Unmarshal(rec9.Body.Bytes(), &resp)
	if len(resp.Applications) != 1 {
		t.Errorf("limit=1 got %d applications, want 1", len(resp.Applications))
	}
	if resp.Total < 2 {
		t.Errorf("total = %d (should be >= 2 despite limit)", resp.Total)
	}
}

// TestAdminHost_ApplicationsAllowed verifies /api/admin/donations/applications
// is accessible on the admin host (hostSeparation allowlist).
func TestAdminHost_ApplicationsAllowed(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	_ = store

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/donations/applications", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin-host /api/admin/donations/applications: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestApproveDeadlineValidationThroughGateway(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)
	applicant, _ := store.CreateUser("deadline-user", "deadline-user", "")
	past := time.Now().Add(-time.Hour).Unix()
	future := time.Now().Add(24 * time.Hour).Unix()
	expired, err := store.CreateDonationApplication(applicant.ID, "general", "expired-one", "https://dify.example.com/v1", "key-1", 10, past, 5, "")
	if err != nil {
		t.Fatal(err)
	}
	modifiable, err := store.CreateDonationApplication(applicant.ID, "general", "expired-modified", "https://dify.example.com/v1", "key-2", 10, future, 5, "")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)
	request := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://admin.localhost"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://admin.localhost")
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	for _, tc := range []struct {
		path string
		body string
	}{
		{path: fmt.Sprintf("/api/admin/donations/%d/approve", expired.ID), body: `{}`},
		{path: fmt.Sprintf("/api/admin/donations/%d/approve", modifiable.ID), body: fmt.Sprintf(`{"deadline":%d}`, past)},
	} {
		rec := request(tc.path, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		var out struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil || out.Error.Code != "invalid_request" || out.Error.Type != "invalid_request" {
			t.Fatalf("%s response=%+v decode err=%v", tc.path, out, err)
		}
	}

	batchRec := request("/api/admin/donations/approve/batch", fmt.Sprintf(`{"ids":[%d]}`, expired.ID))
	if batchRec.Code != http.StatusBadRequest {
		t.Fatalf("batch status=%d body=%s", batchRec.Code, batchRec.Body.String())
	}
	var batchOut struct {
		FailedID int64 `json:"failed_id"`
		Error    struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(batchRec.Body).Decode(&batchOut); err != nil || batchOut.FailedID != expired.ID || batchOut.Error.Code != "invalid_request" {
		t.Fatalf("batch response=%+v decode err=%v", batchOut, err)
	}
	for _, id := range []int64{expired.ID, modifiable.ID} {
		app, _ := store.GetApplication(id)
		if app == nil || app.Status != db.AppStatusPending || app.DonationID.Valid {
			t.Fatalf("application %d changed after deadline rejection: %+v", id, app)
		}
	}
}

func TestDonationPatch_TotalCountNeverNegativeRemaining(t *testing.T) {
	// Regression: lowering total_count used `remaining_count + (new - old)`
	// without a lower bound, so shrinking the total below the already-used
	// count drove remaining_count negative (unroutable, confusing UI).
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)

	u, _ := store.CreateUser("neg-rem", "neg-rem", "")
	d := &db.Donation{
		Service:         "general",
		Model:           "neg-test",
		DifyBaseURL:     "https://dify.example.com/v1",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
		Status:          db.DonationActive,
	}
	created, err := store.CreateDonation(d, "app-secret")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate 8 uses consumed: remaining 2 of total 10.
	if _, err := store.Exec(`UPDATE donations SET remaining_count=2 WHERE id=?`, created.ID); err != nil {
		t.Fatal(err)
	}

	// Shrink the total below the used count: remaining must clamp at 0.
	rec := donationRequest(gw, admin, "PATCH", fmt.Sprintf("/api/admin/donations/%d", created.ID),
		map[string]interface{}{"total_count": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	updated, _ := store.GetDonation(created.ID)
	if updated.RemainingCount != 0 {
		t.Fatalf("remaining_count = %d, want 0 (clamped, not negative)", updated.RemainingCount)
	}

	// Growing the total again still adds to remaining as before.
	rec2 := donationRequest(gw, admin, "PATCH", fmt.Sprintf("/api/admin/donations/%d", created.ID),
		map[string]interface{}{"total_count": 3})
	if rec2.Code != http.StatusOK {
		t.Fatalf("patch up: status = %d, body: %s", rec2.Code, rec2.Body.String())
	}
	updated2, _ := store.GetDonation(created.ID)
	if updated2.RemainingCount != 2 {
		t.Fatalf("remaining_count = %d, want 2 after top-up", updated2.RemainingCount)
	}
}
