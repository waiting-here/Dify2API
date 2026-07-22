package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDonations_CRUD(t *testing.T) {
	st, _ := openTemp(t)

	d := &Donation{
		Service:         "general",
		Model:           "claude-sonnet",
		DifyBaseURL:     "https://api.dify.example.com",
		SourceDiscordID: "111",
		SourceUsername:  "alice",
		Deadline:        time.Now().Add(30 * 24 * time.Hour).Unix(),
		TotalCount:      10,
		Note:            "测试捐赠",
	}

	created, err := st.CreateDonation(d, "app-secret-key-123")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}
	if created.ID == 0 || created.RemainingCount != 10 || created.Status != DonationActive {
		t.Errorf("created donation = %+v", created)
	}

	// Verify encryption roundtrip.
	dec, err := st.Decrypt(created.DifyAPIKeyEnc)
	if err != nil {
		t.Fatalf("decrypt key: %v", err)
	}
	if dec != "app-secret-key-123" {
		t.Errorf("decrypted key = %q, want %q", dec, "app-secret-key-123")
	}

	// GetDonation.
	got, err := st.GetDonation(created.ID)
	if err != nil || got == nil {
		t.Fatalf("GetDonation: %v %v", got, err)
	}
	if got.Service != "general" || got.Model != "claude-sonnet" {
		t.Errorf("got = %+v", got)
	}

	// Missing.
	missing, err := st.GetDonation(99999)
	if err != nil || missing != nil {
		t.Errorf("missing donation should be (nil,nil): %v %v", missing, err)
	}

	// ListDonations.
	list, err := st.ListDonations()
	if err != nil || len(list) != 1 {
		t.Errorf("ListDonations = %d items, err %v", len(list), err)
	}
}

func TestDonations_ListRoutableDonations(t *testing.T) {
	st, _ := openTemp(t)

	active := &Donation{
		Service: "general", Model: "gpt",
		DifyBaseURL: "https://a.example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	a, _ := st.CreateDonation(active, "k1")

	expired := &Donation{
		Service: "general", Model: "gpt",
		DifyBaseURL: "https://b.example.com",
		Deadline:    time.Now().Add(-1 * time.Hour).Unix(),
		TotalCount:  5,
	}
	e, _ := st.CreateDonation(expired, "k2")
	// Manually flip to expired (deadline already past, but status is still active at creation).
	st.SetDonationStatus(e.ID, DonationExpired)

	inactive := &Donation{
		Service: "general", Model: "gpt",
		DifyBaseURL: "https://c.example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	i, _ := st.CreateDonation(inactive, "k3")
	st.SetDonationStatus(i.ID, DonationInactive)

	exhausted := &Donation{
		Service: "general", Model: "gpt",
		DifyBaseURL: "https://d.example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  1,
	}
	x, _ := st.CreateDonation(exhausted, "k4")
	st.RecordDonationSuccess(x.ID) // remaining_count → 0, status → expired

	routable, err := st.ListRoutableDonations("general", "gpt")
	if err != nil {
		t.Fatalf("ListRoutableDonations: %v", err)
	}
	// Only the first (active+not expired+remaining>0) should be returned.
	if len(routable) != 1 {
		t.Fatalf("expected 1 routable, got %d", len(routable))
	}
	if routable[0].ID != a.ID {
		t.Errorf("expected donation %d, got %d", a.ID, routable[0].ID)
	}
}

func TestDonations_RecordSuccess(t *testing.T) {
	st, _ := openTemp(t)

	d := &Donation{
		Service: "general", Model: "test",
		DifyBaseURL:         "https://x.example.com",
		Deadline:            time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:          2,
		ConsecutiveFailures: 5, // pre-seeded failures (simulate prior failures)
	}
	created, _ := st.CreateDonation(d, "k")

	// Record success: remaining 2→1, success 0→1, consecutive_failures 5→0.
	if err := st.RecordDonationSuccess(created.ID); err != nil {
		t.Fatalf("RecordDonationSuccess #1: %v", err)
	}
	got, _ := st.GetDonation(created.ID)
	if got.RemainingCount != 1 || got.SuccessCount != 1 || got.ConsecutiveFailures != 0 {
		t.Errorf("after success #1: remaining=%d success=%d consecutive=%d", got.RemainingCount, got.SuccessCount, got.ConsecutiveFailures)
	}
	if got.Status != DonationActive {
		t.Errorf("status after success #1 = %q, want active", got.Status)
	}

	// Second success: remaining 1→0, auto-expired.
	if err := st.RecordDonationSuccess(created.ID); err != nil {
		t.Fatalf("RecordDonationSuccess #2: %v", err)
	}
	got, _ = st.GetDonation(created.ID)
	if got.RemainingCount != 0 || got.Status != DonationExpired {
		t.Errorf("after success #2: remaining=%d status=%q, want 0/expired", got.RemainingCount, got.Status)
	}

	// Third success should fail (not routable).
	if err := st.RecordDonationSuccess(created.ID); err == nil {
		t.Fatal("expected error on exhausted donation")
	}
}

func TestDonations_RecordFailure(t *testing.T) {
	st, _ := openTemp(t)

	d := &Donation{
		Service: "general", Model: "test",
		DifyBaseURL: "https://x.example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")

	for i := 1; i <= 3; i++ {
		consecutive, err := st.RecordDonationFailure(created.ID)
		if err != nil {
			t.Fatalf("RecordDonationFailure #%d: %v", i, err)
		}
		if consecutive != i {
			t.Errorf("consecutive = %d, want %d", consecutive, i)
		}
	}
	got, _ := st.GetDonation(created.ID)
	if got.FailureCount != 3 || got.ConsecutiveFailures != 3 {
		t.Errorf("failure_count=%d consecutive=%d", got.FailureCount, got.ConsecutiveFailures)
	}
}

func TestDonations_SetStatus(t *testing.T) {
	st, _ := openTemp(t)

	d := &Donation{
		Service: "general", Model: "test",
		DifyBaseURL:         "https://x.example.com",
		Deadline:            time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:          5,
		ConsecutiveFailures: 12,
	}
	created, _ := st.CreateDonation(d, "k")

	// Set to inactive.
	if err := st.SetDonationStatus(created.ID, DonationInactive); err != nil {
		t.Fatalf("SetDonationStatus inactive: %v", err)
	}
	got, _ := st.GetDonation(created.ID)
	if got.Status != DonationInactive {
		t.Errorf("status = %q, want inactive", got.Status)
	}

	// Re-activate: consecutive_failures should reset to 0.
	if err := st.SetDonationStatus(created.ID, DonationActive); err != nil {
		t.Fatalf("SetDonationStatus active: %v", err)
	}
	got, _ = st.GetDonation(created.ID)
	if got.Status != DonationActive || got.ConsecutiveFailures != 0 {
		t.Errorf("after re-activate: status=%q consecutive=%d, want active/0", got.Status, got.ConsecutiveFailures)
	}
}

func TestDonations_ExpireOverdue(t *testing.T) {
	st, _ := openTemp(t)

	// Active, not expired.
	a1 := &Donation{
		Service: "general", Model: "g1",
		DifyBaseURL: "https://a.example.com",
		Deadline:    2000000000, // far future
		TotalCount:  5,
	}
	id1, _ := st.CreateDonation(a1, "k1")

	// Active, overdue.
	a2 := &Donation{
		Service: "general", Model: "g2",
		DifyBaseURL: "https://b.example.com",
		Deadline:    1000000000, // past
		TotalCount:  5,
	}
	id2, _ := st.CreateDonation(a2, "k2")

	// Already expired.
	a3 := &Donation{
		Service: "general", Model: "g3",
		DifyBaseURL: "https://c.example.com",
		Deadline:    1000000000,
		TotalCount:  5,
	}
	id3, _ := st.CreateDonation(a3, "k3")
	st.SetDonationStatus(id3.ID, DonationExpired)

	// Inactive, overdue.
	a4 := &Donation{
		Service: "general", Model: "g4",
		DifyBaseURL: "https://d.example.com",
		Deadline:    1000000000,
		TotalCount:  5,
	}
	id4, _ := st.CreateDonation(a4, "k4")
	st.SetDonationStatus(id4.ID, DonationInactive)

	now := int64(1500000000)
	n, err := st.ExpireOverdueDonations(now)
	if err != nil {
		t.Fatalf("ExpireOverdueDonations: %v", err)
	}
	// a2 (active+overdue) and a4 (inactive+overdue) should flip; a3 was already expired.
	if n != 2 {
		t.Errorf("affected rows = %d, want 2", n)
	}

	d2, _ := st.GetDonation(id2.ID)
	if d2.Status != DonationExpired {
		t.Errorf("d2 status = %q, want expired", d2.Status)
	}
	d4, _ := st.GetDonation(id4.ID)
	if d4.Status != DonationExpired {
		t.Errorf("d4 status = %q, want expired", d4.Status)
	}
	// a1 still active.
	d1, _ := st.GetDonation(id1.ID)
	if d1.Status != DonationActive {
		t.Errorf("d1 status = %q, want active", d1.Status)
	}
}

func TestDonations_DeleteOrphan(t *testing.T) {
	st, _ := openTemp(t)

	d := &Donation{
		Service: "general", Model: "test",
		DifyBaseURL: "https://x.example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")

	// Create a user and a request log pointing to this donation.
	u, _ := st.CreateUser("222", "bob", "")
	st.AddRequestLogDonation(u.ID, "[公益][general]test", "general", time.Now().Add(-1*time.Hour), time.Now(), "success", "", created.ID)

	// Delete the donation.
	if err := st.DeleteDonation(created.ID); err != nil {
		t.Fatalf("DeleteDonation: %v", err)
	}
	gone, _ := st.GetDonation(created.ID)
	if gone != nil {
		t.Error("donation should be deleted")
	}

	// The request log should still exist (orphan retention).
	logs, _ := st.ListRequestLogs(u.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("request logs should survive donation deletion, got %d", len(logs))
	}
}

func TestAdminAlerts(t *testing.T) {
	st, _ := openTemp(t)

	// Add alerts.
	for i := 0; i < 3; i++ {
		if err := st.AddAdminAlert(&AdminAlert{
			Type:    AlertBlockingFailed200,
			Message: "test message",
		}); err != nil {
			t.Fatalf("AddAdminAlert: %v", err)
		}
	}

	// List with pagination.
	list, total, err := st.ListAdminAlerts(10, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Errorf("total=%d len=%d, want 3/3", total, len(list))
	}

	// Batch delete.
	var ids []int64
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	n, err := st.DeleteAdminAlerts(ids)
	if err != nil || n != 3 {
		t.Errorf("DeleteAdminAlerts = %d / %v, want 3", n, err)
	}

	// Empty slice is no-op.
	n2, err := st.DeleteAdminAlerts(nil)
	if err != nil || n2 != 0 {
		t.Errorf("DeleteAdminAlerts(nil) = %d / %v, want 0", n2, err)
	}
}

func TestSetUserCheckin_AntiReplay(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("333", "charlie", "")

	// First check-in on day "2026-07-24": accepted.
	ok, err := st.SetUserCheckin(u.ID, "2026-07-24", 15)
	if err != nil || !ok {
		t.Fatalf("first check-in: ok=%v err=%v", ok, err)
	}
	got, _ := st.GetUserByID(u.ID)
	if got.Credits != 15 || got.LastCheckinDay != "2026-07-24" {
		t.Errorf("credits=%d day=%q, want 15/2026-07-24", got.Credits, got.LastCheckinDay)
	}

	// Second check-in same day: rejected.
	ok2, err := st.SetUserCheckin(u.ID, "2026-07-24", 20)
	if err != nil || ok2 {
		t.Fatalf("duplicate check-in should be rejected: ok=%v err=%v", ok2, err)
	}
	got, _ = st.GetUserByID(u.ID)
	if got.Credits != 15 {
		t.Errorf("credits should still be 15, got %d", got.Credits)
	}

	// Next day: accepted.
	ok3, err := st.SetUserCheckin(u.ID, "2026-07-25", 12)
	if err != nil || !ok3 {
		t.Fatalf("next-day check-in: ok=%v err=%v", ok3, err)
	}
	got, _ = st.GetUserByID(u.ID)
	if got.Credits != 12 || got.LastCheckinDay != "2026-07-25" {
		t.Errorf("credits=%d day=%q, want 12/2026-07-25", got.Credits, got.LastCheckinDay)
	}
}

func TestAdjustUserCredits_Negative(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("444", "dave", "")

	// Set initial credits.
	st.SetUserCredits(u.ID, 10)

	// Add positive.
	newVal, err := st.AdjustUserCredits(u.ID, 5)
	if err != nil || newVal != 15 {
		t.Errorf("adjust +5: val=%d err=%v, want 15", newVal, err)
	}

	// Subtract to negative.
	newVal, err = st.AdjustUserCredits(u.ID, -20)
	if err != nil || newVal != -5 {
		t.Errorf("adjust -20: val=%d err=%v, want -5 (negative allowed)", newVal, err)
	}

	got, _ := st.GetUserByID(u.ID)
	if got.Credits != -5 {
		t.Errorf("credits = %d, want -5", got.Credits)
	}
}

func TestDonationCreditAndCharityEnabled(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("555", "eve", "")

	// Set and adjust donation_credit.
	st.SetUserDonationCredit(u.ID, 3)
	newVal, err := st.AdjustUserDonationCredit(u.ID, 2)
	if err != nil || newVal != 5 {
		t.Errorf("adjust +2: val=%d err=%v, want 5", newVal, err)
	}

	// Charity enabled roundtrip.
	if err := st.SetUserCharityEnabled(u.ID, true); err != nil {
		t.Fatalf("SetUserCharityEnabled: %v", err)
	}
	got, _ := st.GetUserByID(u.ID)
	if !got.CharityEnabled || got.DonationCredit != 5 {
		t.Errorf("charity=%v donation_credit=%d, want true/5", got.CharityEnabled, got.DonationCredit)
	}

	// Disable.
	st.SetUserCharityEnabled(u.ID, false)
	got, _ = st.GetUserByID(u.ID)
	if got.CharityEnabled {
		t.Error("charity should be disabled")
	}
}

func TestExportBundle_Alpha3Fields(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("666", "frank", "")
	st.SetUserCredits(u.ID, 42)
	st.SetUserCharityEnabled(u.ID, true)
	st.SetUserDonationCredit(u.ID, 7)

	bundle, err := st.ExportUserData(u.ID)
	if err != nil || bundle == nil {
		t.Fatalf("ExportUserData: %v", err)
	}
	if bundle.User.Credits != 42 {
		t.Errorf("export credits = %d, want 42", bundle.User.Credits)
	}
	if bundle.User.DonationCredit != 7 {
		t.Errorf("export donation_credit = %d, want 7", bundle.User.DonationCredit)
	}
	if !bundle.User.CharityEnabled {
		t.Error("export charity_enabled should be true")
	}
	if bundle.User.LastCheckinDay != "" {
		t.Errorf("export last_checkin_day = %q, want empty", bundle.User.LastCheckinDay)
	}
}

func TestOpen_MigrateAlpha3Columns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "m.db")
	keyPath := filepath.Join(dir, "m.key")

	st1, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Simulate a legacy user row (no alpha.3 columns).
	if _, err := st1.db.Exec(`INSERT INTO users (discord_id, username, created_at, updated_at) VALUES ('legacy','old',1,1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	st1.Close()

	// Re-open: migrations should be idempotent.
	st2, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("reopen with migration: %v", err)
	}
	defer st2.Close()

	u, err := st2.GetUserByDiscordID("legacy")
	if err != nil || u == nil {
		t.Fatalf("legacy user: err=%v u=%v", err, u)
	}
	if u.Credits != 0 || u.DonationCredit != 0 || u.CharityEnabled {
		t.Errorf("legacy user defaults: credits=%d donation=%d charity=%v", u.Credits, u.DonationCredit, u.CharityEnabled)
	}
}
