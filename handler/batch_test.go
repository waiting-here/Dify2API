package handler

import (
	"context"
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

// --- Helpers ---

// batchRequest sends a JSON body request and returns the recorder.
func batchRequest(gw *Gateway, cookie *http.Cookie, method, path string, body interface{}) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
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
	mux.ServeHTTP(rec, req)
	return rec
}

// batchUserCookie creates a normal user session and returns the cookie.
func batchUserCookie(t *testing.T, gw *Gateway, store *db.Store) *http.Cookie {
	t.Helper()
	u, err := store.CreateUser("batch_user", "batchuser", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tok, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: tok}
}

// batchCreatePendingApp creates a pending donation application and returns its ID.
func batchCreatePendingApp(t *testing.T, gw *Gateway, store *db.Store, cookie *http.Cookie) int64 {
	t.Helper()
	store.SetSetting(db.SettingDonationEnabled, "true")
	deadline := time.Now().Add(48 * time.Hour).Unix()
	rec := batchRequest(gw, cookie, "POST", "/api/me/donations", map[string]interface{}{
		"service":       "general",
		"model":         "batch-test-model",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "app-test-key",
		"deadline":      deadline,
		"total_count":   100,
		"note":          "batch test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create pending app: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Application map[string]interface{} `json:"application"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return int64(resp.Application["id"].(float64))
}

func ptr(n int) *int { return &n }

// --- 7.1 Donation application batch approve/reject ---

// TestBatchApprove_AllPending tests that batch approving all pending applications succeeds.
func TestBatchApprove_AllPending(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)

	// Create 3 pending applications.
	var ids []int64
	for i := 0; i < 3; i++ {
		id := batchCreatePendingApp(t, gw, store, userC)
		ids = append(ids, id)
	}

	// Batch approve all 3.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/approve/batch", map[string]interface{}{
		"ids":         ids,
		"review_note": "批量通过",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch approve: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok=true")
	}
	if resp.Count != 3 {
		t.Errorf("count = %d, want 3", resp.Count)
	}

	// Verify all applications are now approved.
	for _, id := range ids {
		app, err := store.GetApplication(id)
		if err != nil || app == nil {
			t.Fatalf("get application %d: %v", id, err)
		}
		if app.Status != db.AppStatusApproved {
			t.Errorf("application %d status = %s, want approved", id, app.Status)
		}
	}
}

// TestBatchApprove_OneNotPending verifies atomic rejection when one application
// is not pending.
func TestBatchApprove_OneNotPending(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)

	// Create 2 pending applications.
	id1 := batchCreatePendingApp(t, gw, store, userC)
	id2 := batchCreatePendingApp(t, gw, store, userC)

	// Approve the second one individually to change its status.
	rec := batchRequest(gw, adminC, "POST", fmt.Sprintf("/api/admin/donations/%d/approve", id2), map[string]interface{}{
		"review_note": "单独通过",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("individual approve id2: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Now batch approve [id1, id2] — id2 is already approved, should fail entirely.
	rec2 := batchRequest(gw, adminC, "POST", "/api/admin/donations/approve/batch", map[string]interface{}{
		"ids":         []int64{id1, id2},
		"review_note": "批量通过",
	})
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("batch approve with non-pending: status = %d, want 400; body: %s",
			rec2.Code, rec2.Body.String())
	}

	var errResp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		FailedID int64 `json:"failed_id"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if errResp.OK {
		t.Fatal("expected ok=false")
	}
	if errResp.FailedID != id2 {
		t.Errorf("failed_id = %d, want %d", errResp.FailedID, id2)
	}
	if errResp.Error.Code != "invalid_request" || !strings.Contains(errResp.Error.Message, "不是 pending") {
		t.Errorf("error should be invalid_request and mention '不是 pending', got: %+v", errResp.Error)
	}

	// Verify id1 is still pending (not partially approved).
	app1, _ := store.GetApplication(id1)
	if app1.Status != db.AppStatusPending {
		t.Errorf("application %d status = %s, want pending (should not have been touched)", id1, app1.Status)
	}
}

func TestBatchApprove_DuplicateIDRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)
	id := batchCreatePendingApp(t, gw, store, userC)

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/approve/batch", map[string]interface{}{
		"ids": []int64{id, id},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate approve: status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	app, err := store.GetApplication(id)
	if err != nil || app == nil || app.Status != db.AppStatusPending || app.DonationID.Valid {
		t.Fatalf("application after rollback=%+v, err=%v", app, err)
	}
	donations, err := store.ListDonations()
	if err != nil || len(donations) != 0 {
		t.Fatalf("donations after rollback=%d, err=%v", len(donations), err)
	}
}

func TestBatchApprove_FieldValidationFailureLeavesAllPending(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)
	goodID := batchCreatePendingApp(t, gw, store, userC)
	u, err := store.CreateUser("batch-invalid-owner", "invalid", "")
	if err != nil {
		t.Fatal(err)
	}
	bad, err := store.CreateDonationApplication(
		u.ID, "general", "batch-invalid-url", "ftp://invalid.example.com",
		"invalid-url-key", 10, time.Now().Add(time.Hour).Unix(), 10, "",
	)
	if err != nil {
		t.Fatal(err)
	}

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/approve/batch", map[string]interface{}{
		"ids": []int64{goodID, bad.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid field batch: status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{goodID, bad.ID} {
		app, err := store.GetApplication(id)
		if err != nil || app == nil || app.Status != db.AppStatusPending || app.DonationID.Valid {
			t.Fatalf("application %d after validation failure=%+v, err=%v", id, app, err)
		}
	}
}

// TestBatchReject_AllPending tests batch reject succeeds.
func TestBatchReject_AllPending(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)

	// Create 2 pending applications.
	id1 := batchCreatePendingApp(t, gw, store, userC)
	id2 := batchCreatePendingApp(t, gw, store, userC)

	// Batch reject.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/reject/batch", map[string]interface{}{
		"ids":         []int64{id1, id2},
		"review_note": "批量拒绝",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch reject: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Count != 2 {
		t.Errorf("ok=%v count=%d, want true/2", resp.OK, resp.Count)
	}

	// Verify both rejected.
	for _, id := range []int64{id1, id2} {
		app, _ := store.GetApplication(id)
		if app.Status != db.AppStatusRejected {
			t.Errorf("application %d status = %s, want rejected", id, app.Status)
		}
	}
}

func TestBatchReject_DuplicateIDRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	adminC := adminCookie(t, gw)
	userC := batchUserCookie(t, gw, store)
	id := batchCreatePendingApp(t, gw, store, userC)

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/reject/batch", map[string]interface{}{
		"ids": []int64{id, id},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate reject: status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	app, err := store.GetApplication(id)
	if err != nil || app == nil || app.Status != db.AppStatusPending || app.ReviewerID.Valid {
		t.Fatalf("application after rollback=%+v, err=%v", app, err)
	}
}

// TestBatchApprove_EmptyIDs rejects empty ids array.
func TestBatchApprove_EmptyIDs(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/approve/batch", map[string]interface{}{
		"ids": []int64{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty ids: status = %d, want 400", rec.Code)
	}
}

// --- 7.2 Donation resource batch status/delete ---

// TestBatchDonationStatus_AllInactive tests batch status change to inactive.
func TestBatchDonationStatus_AllInactive(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create 2 active donations.
	var ids []int64
	for i := 0; i < 2; i++ {
		d := &db.Donation{
			Service:     "general",
			Model:       fmt.Sprintf("batch-status-%d", i),
			DifyBaseURL: "https://dify.example.com/v1",
			Deadline:    time.Now().Add(24 * time.Hour).Unix(),
			TotalCount:  10,
			Status:      db.DonationActive,
			Note:        "test",
		}
		created, err := store.CreateDonation(d, "app-secret")
		if err != nil {
			t.Fatalf("create donation %d: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	// Batch set to inactive.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/status/batch", map[string]interface{}{
		"ids":    ids,
		"status": "inactive",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch status: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Count != 2 {
		t.Errorf("ok=%v count=%d, want true/2", resp.OK, resp.Count)
	}

	// Verify both inactive.
	for _, id := range ids {
		d, _ := store.GetDonation(id)
		if d.Status != db.DonationInactive {
			t.Errorf("donation %d status = %s, want inactive", id, d.Status)
		}
	}
}

// TestBatchActivate_NoPricing verifies that activating without pricing rejects
// the entire batch.
func TestBatchActivate_NoPricing(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create 2 inactive donations with no pricing.
	id1 := createInactiveDonation(t, store, "general", "batch-noprice-1")
	id2 := createInactiveDonation(t, store, "general", "batch-noprice-2")

	// Batch activate — should fail because no pricing exists.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/status/batch", map[string]interface{}{
		"ids":    []int64{id1, id2},
		"status": "active",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch activate without pricing: status = %d, want 400; body: %s",
			rec.Code, rec.Body.String())
	}

	var errResp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		FailedID int64 `json:"failed_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.OK {
		t.Fatal("expected ok=false")
	}
	if errResp.FailedID != id1 {
		t.Errorf("failed_id = %d, want %d (first one with missing pricing)", errResp.FailedID, id1)
	}
	if errResp.Error.Code != "invalid_request" || !strings.Contains(errResp.Error.Message, "尚未设定价格") {
		t.Errorf("error should be invalid_request and mention pricing, got: %+v", errResp.Error)
	}

	// Verify both still inactive.
	d1, _ := store.GetDonation(id1)
	if d1.Status != db.DonationInactive {
		t.Errorf("donation %d status = %s, want inactive", id1, d1.Status)
	}
	d2, _ := store.GetDonation(id2)
	if d2.Status != db.DonationInactive {
		t.Errorf("donation %d status = %s, want inactive", id2, d2.Status)
	}
}

// TestBatchDonationStatus_ExpiredRejected verifies expired donations are rejected.
func TestBatchDonationStatus_ExpiredRejected(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create an active donation then mark it expired.
	d := &db.Donation{
		Service:     "general",
		Model:       "batch-expired",
		DifyBaseURL: "https://dify.example.com/v1",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
		Status:      db.DonationActive,
		Note:        "test",
	}
	created, err := store.CreateDonation(d, "app-secret")
	if err != nil {
		t.Fatalf("create donation: %v", err)
	}
	store.SetDonationStatus(created.ID, db.DonationExpired)

	// Create another active donation.
	id2 := createInactiveDonation(t, store, "general", "batch-ok")

	// Batch set to inactive with expired in the list → should fail.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/status/batch", map[string]interface{}{
		"ids":    []int64{created.ID, id2},
		"status": "inactive",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch with expired: status = %d, want 400; body: %s",
			rec.Code, rec.Body.String())
	}

	var errResp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		FailedID int64 `json:"failed_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.FailedID != created.ID {
		t.Errorf("failed_id = %d, want %d", errResp.FailedID, created.ID)
	}
	if errResp.Error.Code != "invalid_request" || !strings.Contains(errResp.Error.Message, "已失效") {
		t.Errorf("error should be invalid_request and mention expired, got: %+v", errResp.Error)
	}
}

func TestBatchDonationStatus_SQLFailureRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)
	first := createInactiveDonation(t, store, "general", "status-rollback-1")
	second := createInactiveDonation(t, store, "general", "status-rollback-2")
	if err := store.SetDonationStatus(first, db.DonationActive); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDonationStatus(second, db.DonationActive); err != nil {
		t.Fatal(err)
	}
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_status_second BEFORE UPDATE OF status ON donations
		WHEN NEW.id=%d BEGIN SELECT RAISE(ABORT, 'injected status failure'); END`, second)
	if _, err := store.RawExec(trigger); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	rec := batchRequest(gw, adminC, http.MethodPost, "/api/admin/donations/status/batch", map[string]interface{}{
		"ids": []int64{first, second}, "status": db.DonationInactive,
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{first, second} {
		got, _ := store.GetDonation(id)
		if got == nil || got.Status != db.DonationActive {
			t.Errorf("donation %d partially updated: %+v", id, got)
		}
	}
}

// TestBatchDeleteDonations deletes multiple donations.
func TestBatchDeleteDonations(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create 3 donations.
	var ids []int64
	for i := 0; i < 3; i++ {
		d := &db.Donation{
			Service:     "general",
			Model:       fmt.Sprintf("batch-del-%d", i),
			DifyBaseURL: "https://dify.example.com/v1",
			Deadline:    time.Now().Add(24 * time.Hour).Unix(),
			TotalCount:  10,
			Status:      db.DonationActive,
			Note:        "test",
		}
		created, err := store.CreateDonation(d, "app-secret")
		if err != nil {
			t.Fatalf("create donation %d: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	// Batch delete.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/delete/batch", map[string]interface{}{
		"ids": ids,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Count != 3 {
		t.Errorf("ok=%v count=%d, want true/3", resp.OK, resp.Count)
	}

	// Verify all deleted.
	for _, id := range ids {
		gone, _ := store.GetDonation(id)
		if gone != nil {
			t.Errorf("donation %d should be deleted", id)
		}
	}
}

// TestBatchDeleteDonations_MissingID rejects batch with non-existent ID.
func TestBatchDeleteDonations_MissingID(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	d := &db.Donation{
		Service:     "general",
		Model:       "batch-del-exist",
		DifyBaseURL: "https://dify.example.com/v1",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
		Status:      db.DonationActive,
		Note:        "test",
	}
	created, _ := store.CreateDonation(d, "app-secret")

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/delete/batch", map[string]interface{}{
		"ids": []int64{created.ID, 99999},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch delete with missing: status = %d, want 400; body: %s",
			rec.Code, rec.Body.String())
	}

	// Verify the existing one was NOT deleted.
	still, _ := store.GetDonation(created.ID)
	if still == nil {
		t.Error("existing donation should NOT have been deleted")
	}
}

func TestBatchDeleteDonations_ActiveReservationRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)
	consumer, err := store.CreateUser("batch-delete-consumer", "consumer", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserCredits(consumer.ID, 10); err != nil {
		t.Fatal(err)
	}
	create := func(model string) *db.Donation {
		d, err := store.CreateDonation(&db.Donation{
			Service: "general", Model: model, DifyBaseURL: "https://dify.example.com/v1",
			Deadline: time.Now().Add(time.Hour).Unix(), TotalCount: 2, Status: db.DonationActive,
		}, "batch-delete-key-"+model)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first := create("batch-delete-first")
	second := create("batch-delete-reserved")
	if _, err := store.ReserveCharityCall(context.Background(), consumer.ID, second.ID, 1, 0); err != nil {
		t.Fatal(err)
	}

	rec := batchRequest(gw, adminC, "POST", "/api/admin/donations/delete/batch", map[string]interface{}{
		"ids": []int64{first.ID, second.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch delete reserved: status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{first.ID, second.ID} {
		if got, err := store.GetDonation(id); err != nil || got == nil {
			t.Fatalf("donation %d was partially deleted: got=%+v err=%v", id, got, err)
		}
	}
}

// --- 7.3 Pricing batch delete ---

// TestBatchDeletePricing_AllClean tests batch deleting pricing entries with no donations.
func TestBatchDeletePricing_AllClean(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create 3 pricing entries.
	pairs := []struct{ s, m string }{
		{"general", "p1"}, {"general", "p2"}, {"sillytavern-main-trimmed", "p3"},
	}
	for _, p := range pairs {
		store.UpsertPricing(p.s, p.m, 10, ptr(5))
	}

	rec := batchRequest(gw, adminC, "POST", "/api/admin/pricing/delete/batch", map[string]interface{}{
		"pairs": []map[string]string{
			{"service": "general", "model": "p1"},
			{"service": "general", "model": "p2"},
			{"service": "sillytavern-main-trimmed", "model": "p3"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete pricing: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Count != 3 {
		t.Errorf("ok=%v count=%d, want true/3", resp.OK, resp.Count)
	}

	// Verify deleted.
	for _, p := range pairs {
		got, _ := store.GetPricing(p.s, p.m)
		if got != nil {
			t.Errorf("pricing (%s, %s) should be deleted", p.s, p.m)
		}
	}
}

// TestBatchDeletePricing_HasDonation verifies atomic rejection when a pricing
// entry has associated donations.
func TestBatchDeletePricing_HasDonation(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create pricing with a donation.
	store.UpsertPricing("general", "has-donation", 10, ptr(5))
	d := &db.Donation{
		Service:     "general",
		Model:       "has-donation",
		DifyBaseURL: "https://dify.example.com/v1",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
		Status:      db.DonationActive,
		Note:        "test",
	}
	store.CreateDonation(d, "app-secret")

	// Create another pricing without donations.
	store.UpsertPricing("general", "no-donation", 10, ptr(5))

	// Batch delete both — should fail entirely.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/pricing/delete/batch", map[string]interface{}{
		"pairs": []map[string]string{
			{"service": "general", "model": "has-donation"},
			{"service": "general", "model": "no-donation"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch delete pricing with donation: status = %d, want 400; body: %s",
			rec.Code, rec.Body.String())
	}

	var errResp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		FailedPair map[string]string `json:"failed_pair"`
	}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.OK {
		t.Fatal("expected ok=false")
	}
	if errResp.FailedPair["service"] != "general" || errResp.FailedPair["model"] != "has-donation" {
		t.Errorf("failed_pair = %v, want {general, has-donation}", errResp.FailedPair)
	}
	if errResp.Error.Code != "invalid_request" || !strings.Contains(errResp.Error.Message, "存在捐赠条目") {
		t.Errorf("error should be invalid_request and mention donation dependency, got: %+v", errResp.Error)
	}

	// Verify no-donation pricing still exists (not partially deleted).
	got, _ := store.GetPricing("general", "no-donation")
	if got == nil {
		t.Error("pricing (general, no-donation) should NOT have been deleted")
	}
}

func TestBatchDeletePricing_SQLFailureRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)
	for _, model := range []string{"rollback-p1", "rollback-p2"} {
		if _, err := store.UpsertPricing("general", model, 10, ptr(5)); err != nil {
			t.Fatalf("seed pricing: %v", err)
		}
	}
	if _, err := store.RawExec(`CREATE TRIGGER fail_pricing_delete BEFORE DELETE ON charity_pricing
		WHEN OLD.model='rollback-p2' BEGIN SELECT RAISE(ABORT, 'injected pricing failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	rec := batchRequest(gw, adminC, http.MethodPost, "/api/admin/pricing/delete/batch", map[string]interface{}{
		"pairs": []map[string]string{
			{"service": "general", "model": "rollback-p1"},
			{"service": "general", "model": "rollback-p2"},
		},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, model := range []string{"rollback-p1", "rollback-p2"} {
		if got, _ := store.GetPricing("general", model); got == nil {
			t.Errorf("pricing %s was partially deleted", model)
		}
	}
}

// --- 7.4 Bulletin batch delete ---

// TestBatchDeleteBulletins_AllSuccess tests batch bulletin deletion.
func TestBatchDeleteBulletins_AllSuccess(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	// Create 3 bulletins.
	var ids []int64
	for i := 0; i < 3; i++ {
		b := &db.Bulletin{
			Title:   fmt.Sprintf("Batch Bulletin %d", i),
			Content: "<p>test</p>",
			Type:    db.BulletinTypeInfo,
		}
		created, err := store.CreateBulletin(b)
		if err != nil {
			t.Fatalf("create bulletin %d: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	// Batch delete.
	rec := batchRequest(gw, adminC, "POST", "/api/admin/bulletins/delete/batch", map[string]interface{}{
		"ids": ids,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete bulletins: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Count != 3 {
		t.Errorf("ok=%v count=%d, want true/3", resp.OK, resp.Count)
	}

	// Verify all deleted.
	for _, id := range ids {
		got, _ := store.GetBulletin(id)
		if got != nil {
			t.Errorf("bulletin %d should be deleted", id)
		}
	}
}

// TestBatchDeleteBulletins_MissingID rejects batch with non-existent bulletin.
func TestBatchDeleteBulletins_MissingID(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	b := &db.Bulletin{
		Title:   "Existing",
		Content: "<p>test</p>",
		Type:    db.BulletinTypeInfo,
	}
	created, _ := store.CreateBulletin(b)

	rec := batchRequest(gw, adminC, "POST", "/api/admin/bulletins/delete/batch", map[string]interface{}{
		"ids": []int64{created.ID, 99999},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch delete with missing: status = %d, want 400; body: %s",
			rec.Code, rec.Body.String())
	}

	// Verify existing bulletin not deleted.
	got, _ := store.GetBulletin(created.ID)
	if got == nil {
		t.Error("existing bulletin should NOT have been deleted")
	}
}

func TestBatchDeleteBulletins_SQLFailureRollsBack(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)
	first, _ := store.CreateBulletin(&db.Bulletin{Title: "rollback-1", Content: "x", Type: db.BulletinTypeInfo})
	second, _ := store.CreateBulletin(&db.Bulletin{Title: "rollback-2", Content: "x", Type: db.BulletinTypeInfo})
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_bulletin_delete BEFORE DELETE ON bulletins
		WHEN OLD.id=%d BEGIN SELECT RAISE(ABORT, 'injected bulletin failure'); END`, second.ID)
	if _, err := store.RawExec(trigger); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	rec := batchRequest(gw, adminC, http.MethodPost, "/api/admin/bulletins/delete/batch", map[string]interface{}{
		"ids": []int64{first.ID, second.ID},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, id := range []int64{first.ID, second.ID} {
		if got, _ := store.GetBulletin(id); got == nil {
			t.Errorf("bulletin %d was partially deleted", id)
		}
	}
}

// --- Admin access control ---

// TestBatchEndpoints_AdminAccess verifies that all batch endpoints require admin.
func TestBatchEndpoints_AdminAccess(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	userC := batchUserCookie(t, gw, store)

	tests := []struct {
		method string
		path   string
		body   interface{}
	}{
		{"POST", "/api/admin/donations/approve/batch", map[string]interface{}{"ids": []int64{1}}},
		{"POST", "/api/admin/donations/reject/batch", map[string]interface{}{"ids": []int64{1}}},
		{"POST", "/api/admin/donations/status/batch", map[string]interface{}{"ids": []int64{1}, "status": "active"}},
		{"POST", "/api/admin/donations/delete/batch", map[string]interface{}{"ids": []int64{1}}},
		{"POST", "/api/admin/pricing/delete/batch", map[string]interface{}{"pairs": []map[string]string{{"service": "general", "model": "x"}}}},
		{"POST", "/api/admin/bulletins/delete/batch", map[string]interface{}{"ids": []int64{1}}},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			rec := batchRequest(gw, userC, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestBatchEndpoints_Unauthenticated verifies unauthenticated access is rejected.
func TestBatchEndpoints_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")

	rec := batchRequest(gw, nil, "POST", "/api/admin/donations/approve/batch",
		map[string]interface{}{"ids": []int64{1}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("unauth batch approve: status = %d, want 403", rec.Code)
	}
}

// --- Helpers ---

func createInactiveDonation(t *testing.T, store *db.Store, service, model string) int64 {
	t.Helper()
	d := &db.Donation{
		Service:     service,
		Model:       model,
		DifyBaseURL: "https://dify.example.com/v1",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
		Status:      db.DonationInactive,
		Note:        "test",
	}
	created, err := store.CreateDonation(d, "app-secret")
	if err != nil {
		t.Fatalf("create inactive donation: %v", err)
	}
	return created.ID
}
