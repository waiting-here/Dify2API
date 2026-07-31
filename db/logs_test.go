package db

import (
	"testing"
	"time"
)

func TestRequestLogs_AntiAbuseInfoRoundTrip(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("anti-abuse-log", "tester", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now()
	const info = `{"triggered":"content_too_short","penalties":["credits_deducted:5","banned:24h"]}`
	if err := st.AddRequestLogFull(u.ID, "[general]test", "general", now, now.Add(time.Second), "error", "content_too_short", 400, "too short", 0, 0, info); err != nil {
		t.Fatalf("AddRequestLogFull: %v", err)
	}
	if err := st.AddRequestLog(u.ID, "[general]normal", "general", now.Add(-time.Second), now, "success", ""); err != nil {
		t.Fatalf("AddRequestLog: %v", err)
	}

	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("ListRequestLogs returned %d rows, want 2", len(logs))
	}
	byModel := make(map[string]*RequestLog, len(logs))
	for _, entry := range logs {
		byModel[entry.Model] = entry
	}
	antiAbuseLog, ok := byModel["[general]test"]
	if !ok {
		t.Fatal("anti-abuse request log is missing")
	}
	if antiAbuseLog.AntiAbuseInfo != info {
		t.Errorf("user log anti_abuse_info = %q, want %q", antiAbuseLog.AntiAbuseInfo, info)
	}
	normalLog, ok := byModel["[general]normal"]
	if !ok {
		t.Fatal("normal request log is missing")
	}
	if normalLog.AntiAbuseInfo != "" {
		t.Errorf("normal user log anti_abuse_info = %q, want empty", normalLog.AntiAbuseInfo)
	}

	adminLogs, total, err := st.ListAllRequestLogs(LogFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("ListAllRequestLogs: %v", err)
	}
	if total != 2 || len(adminLogs) != 2 {
		t.Fatalf("admin logs total=%d len=%d, want 2/2", total, len(adminLogs))
	}
	var adminInfo string
	for _, entry := range adminLogs {
		if entry.Model == "[general]test" {
			adminInfo = entry.AntiAbuseInfo
		}
	}
	if adminInfo != info {
		t.Errorf("admin log anti_abuse_info = %q, want %q", adminInfo, info)
	}
}

func createRetentionDonation(t *testing.T, st *Store, model string) *Donation {
	t.Helper()
	d, err := st.CreateDonation(&Donation{
		Service:     "general",
		Model:       model,
		DifyBaseURL: "https://" + model + ".example.com",
		Deadline:    time.Now().Add(7 * 24 * time.Hour).Unix(),
		TotalCount:  10,
	}, "key-"+model)
	if err != nil {
		t.Fatalf("CreateDonation(%s): %v", model, err)
	}
	return d
}

func TestPurgeExpiredRequestLogs_StrictPerRowAndOrphans(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now().Truncate(time.Second)
	cutoff := now.Add(-RequestLogRetention)
	old := cutoff.Add(-time.Hour)
	recent := cutoff.Add(time.Hour)
	u, err := st.CreateUser("retention-user", "retention", "")
	if err != nil {
		t.Fatal(err)
	}
	active := createRetentionDonation(t, st, "active")
	orphan := createRetentionDonation(t, st, "orphan")

	entries := []struct {
		model      string
		started    time.Time
		donationID int64
	}{
		{"regular-old", old, 0},
		{"active-old", old, active.ID},
		{"active-recent", recent, active.ID},
		{"orphan-old", old, orphan.ID},
		{"exact-cutoff", cutoff, active.ID},
	}
	for _, entry := range entries {
		if err := st.AddRequestLogFull(u.ID, entry.model, "general", entry.started, entry.started.Add(time.Second), "success", "", 200, "", entry.donationID, 0, ""); err != nil {
			t.Fatalf("AddRequestLogFull(%s): %v", entry.model, err)
		}
	}
	if err := st.DeleteDonation(orphan.ID); err != nil {
		t.Fatalf("DeleteDonation: %v", err)
	}

	logsDeleted, alertsDeleted, err := st.PurgeExpiredRequestLogs(now.Unix())
	if err != nil {
		t.Fatalf("PurgeExpiredRequestLogs: %v", err)
	}
	if logsDeleted != 3 || alertsDeleted != 0 {
		t.Fatalf("deleted logs=%d alerts=%d, want 3/0", logsDeleted, alertsDeleted)
	}

	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("remaining logs=%d, want 2", len(logs))
	}
	remaining := map[string]bool{}
	for _, entry := range logs {
		remaining[entry.Model] = true
	}
	if !remaining["active-recent"] || !remaining["exact-cutoff"] {
		t.Fatalf("unexpected remaining logs: %#v", remaining)
	}
}

func TestPurgeExpiredRequestLogs_DeletesBoundAlertsAtomically(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now().Truncate(time.Second)
	old := now.Add(-RequestLogRetention - time.Hour)
	recent := now.Add(-time.Hour)
	u, _ := st.CreateUser("retention-alert", "retention-alert", "")
	d := createRetentionDonation(t, st, "alerts")

	if err := st.AddRequestLogDonation(u.ID, "old", "general", old, old.Add(time.Second), "error", "upstream", d.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddRequestLogDonation(u.ID, "recent", "general", recent, recent.Add(time.Second), "success", "", d.ID); err != nil {
		t.Fatal(err)
	}
	logs, _ := st.ListRequestLogs(u.ID, 10)
	var oldID, recentID int64
	for _, entry := range logs {
		switch entry.Model {
		case "old":
			oldID = entry.ID
		case "recent":
			recentID = entry.ID
		}
	}
	for _, alert := range []*AdminAlert{
		{Type: AlertBlockingFailed200, Message: "old-bound", RequestLogID: &oldID},
		{Type: AlertBlockingFailed200, Message: "recent-bound", RequestLogID: &recentID},
		{Type: AlertBlockingFailed200, Message: "unbound"},
	} {
		if err := st.AddAdminAlert(alert); err != nil {
			t.Fatal(err)
		}
	}

	logsDeleted, alertsDeleted, err := st.PurgeExpiredRequestLogs(now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if logsDeleted != 1 || alertsDeleted != 1 {
		t.Fatalf("deleted logs=%d alerts=%d, want 1/1", logsDeleted, alertsDeleted)
	}
	alerts, total, err := st.ListAdminAlerts(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(alerts) != 2 {
		t.Fatalf("remaining alerts total=%d len=%d, want 2/2", total, len(alerts))
	}
	for _, alert := range alerts {
		if alert.Message == "old-bound" {
			t.Fatal("alert bound to expired donation log survived")
		}
	}
}

func TestPurgeExpiredRequestLogs_NoExpiredRows(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now().Truncate(time.Second)
	u, _ := st.CreateUser("retention-recent", "retention-recent", "")
	if err := st.AddRequestLog(u.ID, "recent", "general", now.Add(-time.Hour), now, "success", ""); err != nil {
		t.Fatal(err)
	}
	logs, alerts, err := st.PurgeExpiredRequestLogs(now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if logs != 0 || alerts != 0 {
		t.Fatalf("deleted logs=%d alerts=%d, want 0/0", logs, alerts)
	}
}

func TestExportAllRequestLogs_NoPageClamp(t *testing.T) {
	// Regression: the admin CSV/JSON export used ListAllRequestLogs(f, 0, 0),
	// whose limit<=0 clamps to a 100-row page — exports were silently
	// truncated. ExportAllRequestLogs must return every matching row.
	st, _ := openTemp(t)
	u, _ := st.CreateUser("export-all", "export-all", "")
	now := time.Now().Truncate(time.Second)
	const n = 150 // > the 100-row page clamp of ListAllRequestLogs
	for i := 0; i < n; i++ {
		if err := st.AddRequestLog(u.ID, "m", "general", now.Add(-time.Duration(n-i)*time.Second), now, "success", ""); err != nil {
			t.Fatal(err)
		}
	}

	// The list endpoint still paginates at 100 max per page.
	paged, total, err := st.ListAllRequestLogs(LogFilter{}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(paged) != 100 || total != n {
		t.Fatalf("list: rows=%d total=%d, want 100/%d", len(paged), total, n)
	}

	// The export path returns everything.
	all, err := st.ExportAllRequestLogs(LogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != n {
		t.Fatalf("export: rows=%d, want %d", len(all), n)
	}

	// Filters still apply on the export path.
	filtered, err := st.ExportAllRequestLogs(LogFilter{UserID: &u.ID, Status: "success"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != n {
		t.Fatalf("export filtered: rows=%d, want %d", len(filtered), n)
	}
	none, err := st.ExportAllRequestLogs(LogFilter{Status: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("export filtered out: rows=%d, want 0", len(none))
	}
}
