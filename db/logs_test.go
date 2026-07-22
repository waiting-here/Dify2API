package db

import (
	"testing"
	"time"
)

func TestPurgeOldRequestLogs_SkipsDonationLogs(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()

	// Create a user.
	u, _ := st.CreateUser("111", "tester", "")

	// Insert an old regular log (no donation_id) — should be purged.
	oldCutoff := now.Add(-RequestLogRetention - 1*time.Hour)
	st.AddRequestLog(u.ID, "[general]x", "general", oldCutoff, oldCutoff.Add(30*time.Second), "success", "")

	// Insert an old donation-bound log — should NOT be purged by
	// PurgeOldRequestLogs (it has donation_id set).
	d := &Donation{
		Service:     "general",
		Model:       "test",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, err := st.CreateDonation(d, "k1")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}
	st.AddRequestLogDonation(u.ID, "[公益][general]test", "general", oldCutoff, oldCutoff.Add(30*time.Second), "success", "", created.ID)

	n, err := st.PurgeOldRequestLogs()
	if err != nil {
		t.Fatalf("PurgeOldRequestLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want 1 (only the regular log)", n)
	}

	// The donation-bound log should still exist.
	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("remaining logs = %d, want 1 (donation-bound log preserved)", len(logs))
	}
	if logs[0].Model != "[公益][general]test" {
		t.Errorf("unexpected remaining log: %+v", logs[0])
	}
}

func TestPurgeExpiredDonationLogs_Gate(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()
	oldCutoff := now.Add(-RequestLogRetention - 1*time.Hour).Unix()

	// Create two donations.
	u, _ := st.CreateUser("222", "tester2", "")
	d1 := &Donation{
		Service:     "general",
		Model:       "d1",
		DifyBaseURL: "https://a.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created1, _ := st.CreateDonation(d1, "k1")

	d2 := &Donation{
		Service:     "general",
		Model:       "d2",
		DifyBaseURL: "https://b.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created2, _ := st.CreateDonation(d2, "k2")

	// d1: oldest log is >30d (should be cleaned).
	st.AddRequestLogDonation(u.ID, "[公益][general]d1", "general", time.Unix(oldCutoff, 0), time.Unix(oldCutoff+30, 0), "success", "", created1.ID)

	// d2: has a recent log (<30d, should NOT be cleaned).
	st.AddRequestLogDonation(u.ID, "[公益][general]d2", "general", now.Add(-1*time.Hour), now.Add(-30*time.Minute), "success", "", created2.ID)

	n, err := st.PurgeExpiredDonationLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredDonationLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want 1 (only d1's log)", n)
	}

	// Check remaining logs.
	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("remaining logs = %d, want 1 (only d2's log)", len(logs))
	}
	if len(logs) > 0 && logs[0].Model != "[公益][general]d2" {
		t.Errorf("unexpected remaining log: %+v", logs[0])
	}
}

func TestPurgeExpiredDonationLogs_UsesMaxStartedAt(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()

	u, _ := st.CreateUser("333", "tester3", "")
	d := &Donation{
		Service:     "general",
		Model:       "multi",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")

	// Insert two logs for the same donation: one old, one recent.
	oldCutoff := now.Add(-RequestLogRetention - 1*time.Hour)
	st.AddRequestLogDonation(u.ID, "[公益][general]multi", "general", oldCutoff, oldCutoff.Add(30*time.Second), "success", "", created.ID)
	st.AddRequestLogDonation(u.ID, "[公益][general]multi", "general", now.Add(-1*time.Hour), now.Add(-30*time.Minute), "success", "", created.ID)

	// Even though there's an old log, the most recent log is not old
	// enough — so nothing should be purged.
	n, err := st.PurgeExpiredDonationLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredDonationLogs: %v", err)
	}
	if n != 0 {
		t.Errorf("purged %d rows, want 0 (gate requires LATEST log to be old)", n)
	}
}

func TestPurgeExpiredDonationLogs_CascadeDeletesAlerts(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()
	oldCutoff := now.Add(-RequestLogRetention - 1*time.Hour)

	u, _ := st.CreateUser("444", "tester4", "")
	d := &Donation{
		Service:     "general",
		Model:       "cascade",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")

	// Insert an old donation-bound log and an alert bound to it.
	started := time.Unix(oldCutoff.Unix(), 0)
	st.AddRequestLogFull(u.ID, "[公益][general]cascade", "general", started, started.Add(30*time.Second), "error", "upstream_error", 502, "test error", created.ID)

	// Find the log id to construct the alert.
	logs, _ := st.ListRequestLogs(u.ID, 10)
	if len(logs) == 0 {
		t.Fatal("no logs found")
	}
	logID := logs[0].ID

	if err := st.AddAdminAlert(&AdminAlert{
		Type:         AlertBlockingFailed200,
		Message:      "test",
		RequestLogID: &logID,
	}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	n, err := st.PurgeExpiredDonationLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredDonationLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d log rows, want 1", n)
	}

	// The alert bound to that log should also be gone.
	alerts, total, err := st.ListAdminAlerts(10, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts: %v", err)
	}
	if total != 0 || len(alerts) != 0 {
		t.Errorf("alerts total=%d len=%d, want 0/0 (cascade-deleted)", total, len(alerts))
	}
}

func TestPurgeExpiredDonationLogs_ExpiredDonation(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()
	oldCutoff := now.Add(-RequestLogRetention - 1*time.Hour).Unix()

	u, _ := st.CreateUser("exp1", "tester_exp", "")
	d := &Donation{
		Service:     "general",
		Model:       "expired",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(-7 * 24 * time.Hour).Unix(), // past
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")
	// Manually set this donation to expired via RawExec.
	st.RawExec(`UPDATE donations SET status='expired', remaining_count=0 WHERE id=?`, created.ID)

	// Add an old log bound to this expired donation.
	oldTime := time.Unix(oldCutoff, 0)
	st.AddRequestLogFull(u.ID, "[公益][general]expired", "general", oldTime, oldTime.Add(30*time.Second), "success", "", 200, "", created.ID)

	// Should still clean logs even though donation is expired.
	n, err := st.PurgeExpiredDonationLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredDonationLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want 1 (expired donation logs should be cleaned)", n)
	}
}

func TestPurgeExpiredDonationLogs_NoOrphans(t *testing.T) {
	// Ensure no rows are purged when there are no old donation-bound logs.
	st, _ := openTemp(t)
	now := time.Now()

	// Only a recent donation-bound log.
	u, _ := st.CreateUser("555", "tester5", "")
	d := &Donation{
		Service:     "general",
		Model:       "recent",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k")
	st.AddRequestLogDonation(u.ID, "[公益][general]recent", "general", now.Add(-1*time.Hour), now.Add(-30*time.Minute), "success", "", created.ID)

	n, err := st.PurgeExpiredDonationLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredDonationLogs: %v", err)
	}
	if n != 0 {
		t.Errorf("purged %d rows, want 0", n)
	}
}
