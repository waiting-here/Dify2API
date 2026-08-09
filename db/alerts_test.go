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
	var logID int64
	if err := st.db.QueryRow(`SELECT id FROM request_logs WHERE user_id=?`, u.ID).Scan(&logID); err != nil {
		t.Fatalf("read physical old log: %v", err)
	}

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

func TestPurgeAlertsForExpiredLogs_IncludesDonationLogs(t *testing.T) {
	// The legacy alert-only wrapper follows the same per-row retention rule
	// for donation and ordinary request logs.
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
	var logID int64
	if err := st.db.QueryRow(`SELECT id FROM request_logs WHERE user_id=?`, u.ID).Scan(&logID); err != nil {
		t.Fatalf("read physical old donation log: %v", err)
	}

	if err := st.AddAdminAlert(&AdminAlert{
		Type:         AlertBlockingFailed200,
		Message:      "donation-bound alert",
		RequestLogID: &logID,
	}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	n, err := st.PurgeAlertsForExpiredLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeAlertsForExpiredLogs: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d alerts, want 1 (expired donation log included)", n)
	}
}

func TestAlertPrefs_RoundTripAndDefaults(t *testing.T) {
	st, _ := openTemp(t)

	// Missing rows default to on for both switches.
	if !st.IsAlertShownInCenter("unknown_type") {
		t.Error("unknown type: show_in_center should default true")
	}
	if !st.IsAlertEmailEnabled("unknown_type") {
		t.Error("unknown type: email_enabled should default true")
	}

	// Seed and verify defaults.
	if err := st.EnsureAlertPrefs([]string{"user_auto_banned", "blocking_failed_200"}); err != nil {
		t.Fatalf("EnsureAlertPrefs: %v", err)
	}
	if !st.IsAlertShownInCenter("user_auto_banned") || !st.IsAlertEmailEnabled("user_auto_banned") {
		t.Error("seeded pref should default to both on")
	}

	// Turn both switches off.
	if err := st.SetAlertPref("user_auto_banned", false, false); err != nil {
		t.Fatalf("SetAlertPref: %v", err)
	}
	if st.IsAlertShownInCenter("user_auto_banned") {
		t.Error("show_in_center should be off")
	}
	if st.IsAlertEmailEnabled("user_auto_banned") {
		t.Error("email_enabled should be off")
	}

	// Setting an unknown type fails.
	if err := st.SetAlertPref("nope", false, true); err == nil {
		t.Error("expected error for unknown event type")
	}

	// List reflects the stored rows.
	prefs, err := st.ListAlertPrefs()
	if err != nil {
		t.Fatalf("ListAlertPrefs: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("want 2 prefs, got %d", len(prefs))
	}
	seen := map[string]bool{}
	for _, p := range prefs {
		seen[p.EventType] = true
		if p.EventType == "user_auto_banned" && (p.ShowInCenter || p.EmailEnabled) {
			t.Error("user_auto_banned should be fully off")
		}
	}
	if !seen["blocking_failed_200"] || !seen["user_auto_banned"] {
		t.Errorf("missing pref rows: %v", seen)
	}
}

func TestAddAdminAlert_ShowInCenterGate(t *testing.T) {
	st, _ := openTemp(t)

	// Unknown type: recorded by default.
	if err := st.AddAdminAlert(&AdminAlert{Type: "mystery", Message: "m1"}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}

	// Disable the blocking_failed_200 category: records are skipped.
	if err := st.EnsureAlertPrefs([]string{AlertBlockingFailed200}); err != nil {
		t.Fatalf("EnsureAlertPrefs: %v", err)
	}
	if err := st.SetAlertPref(AlertBlockingFailed200, false, true); err != nil {
		t.Fatalf("SetAlertPref: %v", err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: "m2"}); err != nil {
		t.Fatalf("AddAdminAlert (gated): %v", err)
	}

	alerts, total, err := st.ListAdminAlerts(100, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts: %v", err)
	}
	if total != 1 {
		t.Fatalf("want 1 alert (gated one skipped), got %d", total)
	}
	if alerts[0].Type != "mystery" {
		t.Errorf("remaining alert type = %s, want mystery", alerts[0].Type)
	}

	// Re-enable: records flow again.
	if err := st.SetAlertPref(AlertBlockingFailed200, true, true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: "m3"}); err != nil {
		t.Fatalf("AddAdminAlert (re-enabled): %v", err)
	}
	if _, total, _ := st.ListAdminAlerts(100, 0); total != 2 {
		t.Fatalf("want 2 alerts after re-enable, got %d", total)
	}
}
