package db

import (
	"testing"
	"time"
)

func TestPurgeAlertsForExpiredLogs(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()

	// Create a user.
	u, _ := st.CreateUser("alertuser", "alerttest", "")

	// Insert an old regular log (no donation_id) and a bound alert.
	oldTime := now.Add(-RequestLogRetention - 1*time.Hour)
	st.AddRequestLog(u.ID, "[general]x", "general", oldTime, oldTime.Add(30*time.Second), "success", "")

	logs, _ := st.ListRequestLogs(u.ID, 10)
	if len(logs) == 0 {
		t.Fatal("no logs found")
	}
	logID := logs[0].ID

	// Add an alert bound to this old log.
	if err := st.AddAdminAlert(&AdminAlert{
		Type:         AlertBlockingFailed200,
		Message:      "stale alert for old log",
		RequestLogID: &logID,
	}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	// Add another unbounded alert — should NOT be purged.
	if err := st.AddAdminAlert(&AdminAlert{
		Type:    AlertBlockingFailed200,
		Message: "unbound alert",
	}); err != nil {
		t.Fatalf("AddAdminAlert (unbound): %v", err)
	}

	// Now purge stale alerts.
	n, err := st.PurgeAlertsForExpiredLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeAlertsForExpiredLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d alerts, want 1", n)
	}

	// The unbound alert should still exist.
	alerts, total, err := st.ListAdminAlerts(10, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts: %v", err)
	}
	if total != 1 || len(alerts) != 1 {
		t.Errorf("remaining alerts total=%d len=%d, want 1", total, len(alerts))
	}
	if len(alerts) > 0 && alerts[0].Message != "unbound alert" {
		t.Errorf("unexpected remaining alert: %+v", alerts[0])
	}
}

func TestPurgeAlertsForExpiredLogs_NoRecentLogs(t *testing.T) {
	// Alerts bound to recent logs should not be purged.
	st, _ := openTemp(t)
	now := time.Now()

	u, _ := st.CreateUser("alertuser2", "alerttest2", "")
	st.AddRequestLog(u.ID, "[general]y", "general", now.Add(-1*time.Hour), now.Add(-30*time.Minute), "success", "")

	logs, _ := st.ListRequestLogs(u.ID, 10)
	logID := logs[0].ID

	if err := st.AddAdminAlert(&AdminAlert{
		Type:         AlertBlockingFailed200,
		Message:      "recent alert",
		RequestLogID: &logID,
	}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	n, err := st.PurgeAlertsForExpiredLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeAlertsForExpiredLogs: %v", err)
	}
	if n != 0 {
		t.Errorf("purged %d alerts, want 0 (log is too recent)", n)
	}
}

func TestPurgeAlertsForExpiredLogs_SkipsDonationLogs(t *testing.T) {
	// Alerts bound to donation logs should NOT be purged by this method
	// (those are handled by PurgeExpiredDonationLogs).
	st, _ := openTemp(t)
	now := time.Now()

	u, _ := st.CreateUser("alertuser3", "alerttest3", "")
	d := &Donation{
		Service:     "general",
		Model:       "test",
		DifyBaseURL: "https://x.example.com",
		Deadline:    now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  5,
	}
	created, _ := st.CreateDonation(d, "k1")
	oldTime := now.Add(-RequestLogRetention - 1*time.Hour)
	st.AddRequestLogDonation(u.ID, "[公益][general]test", "general", oldTime, oldTime.Add(30*time.Second), "success", "", created.ID)

	logs, _ := st.ListRequestLogs(u.ID, 10)
	logID := logs[0].ID

	if err := st.AddAdminAlert(&AdminAlert{
		Type:         AlertBlockingFailed200,
		Message:      "donation-bound alert",
		RequestLogID: &logID,
	}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	// PurgeAlertsForExpiredLogs filters on donation_id IS NULL — should not
	// touch the donation-bound log's alert.
	n, err := st.PurgeAlertsForExpiredLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeAlertsForExpiredLogs: %v", err)
	}
	if n != 0 {
		t.Errorf("purged %d alerts, want 0 (donation-bound logs excluded)", n)
	}
}
