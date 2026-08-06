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
	if _, err := st.AddRequestLogFull(u.ID, "[general]test", "general", now, now.Add(time.Second), "error", "content_too_short", 400, "too short", 0, 0, info); err != nil {
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
		if _, err := st.AddRequestLogFull(u.ID, entry.model, "general", entry.started, entry.started.Add(time.Second), "success", "", 200, "", entry.donationID, 0, ""); err != nil {
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

func TestAddRequestLogFull_ReturnsID_AndAlertJoinUser(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("log-id-user", "log-id-user", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	// AddRequestLogFull must return the new row id so dependent rows
	// (admin alerts) can link to the request log.
	logID, err := st.AddRequestLogFull(u.ID, "[general]x", "general", now, now.Add(time.Second), "error", "upstream_error", 200, "boom", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if logID <= 0 {
		t.Fatalf("AddRequestLogFull returned id %d, want > 0", logID)
	}

	// A bound alert must surface the log owner's user id via the JOIN.
	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: "m", RequestLogID: &logID}); err != nil {
		t.Fatal(err)
	}
	alerts, total, err := st.ListAdminAlerts(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(alerts) != 1 {
		t.Fatalf("alerts total=%d len=%d, want 1/1", total, len(alerts))
	}
	if alerts[0].UserID == nil || *alerts[0].UserID != u.ID {
		t.Fatalf("alert user_id = %v, want %d (via request_logs JOIN)", alerts[0].UserID, u.ID)
	}
	if alerts[0].RequestLogID == nil || *alerts[0].RequestLogID != logID {
		t.Fatalf("alert request_log_id = %v, want %d", alerts[0].RequestLogID, logID)
	}

	// Unbound alerts keep user_id nil.
	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: "unbound"}); err != nil {
		t.Fatal(err)
	}
	alerts, _, _ = st.ListAdminAlerts(10, 0)
	for _, a := range alerts {
		if a.RequestLogID == nil && a.UserID != nil {
			t.Fatalf("unbound alert must have nil user_id, got %d", *a.UserID)
		}
	}
}

func TestLogStatsByHour_AggregatesHourly(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("stats-user", "stats", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create logs spread across 3 hours within the retention window.
	now := time.Now().Truncate(time.Hour) // align to hour boundaries
	for i := 0; i < 3; i++ {
		hourStart := now.Add(time.Duration(i) * time.Hour)
		// Hour 0: 5 success, 2 error
		for j := 0; j < 5; j++ {
			if err := st.AddRequestLog(u.ID, "model-1", "service-a", hourStart.Add(time.Duration(j)*time.Minute), hourStart.Add(time.Duration(j)*time.Minute+time.Second), "success", ""); err != nil {
				t.Fatal(err)
			}
		}
		for j := 0; j < 2; j++ {
			if err := st.AddRequestLog(u.ID, "model-2", "service-b", hourStart.Add(time.Duration(5+j)*time.Minute), hourStart.Add(time.Duration(5+j)*time.Minute+time.Second), "error", "upstream_error"); err != nil {
				t.Fatal(err)
			}
		}
	}

	stats, err := st.LogStatsByHour(LogFilter{})
	if err != nil {
		t.Fatalf("LogStatsByHour: %v", err)
	}

	if len(stats) != 3 {
		t.Fatalf("got %d hour buckets, want 3", len(stats))
	}

	// Verify each hour's counts.
	for i, h := range stats {
		if h.Total != 7 {
			t.Errorf("hour %d: total=%d, want 7", i, h.Total)
		}
		if h.Success != 5 {
			t.Errorf("hour %d: success=%d, want 5", i, h.Success)
		}
		if h.Error != 2 {
			t.Errorf("hour %d: error=%d, want 2", i, h.Error)
		}
		if h.HourUnix <= 0 {
			t.Errorf("hour %d: hour_unix=%d, want >0", i, h.HourUnix)
		}
	}
}

func TestLogStatsByHour_AppliesFilters(t *testing.T) {
	st, _ := openTemp(t)
	u1, _ := st.CreateUser("stats-user1", "stats1", "")
	u2, _ := st.CreateUser("stats-user2", "stats2", "")

	now := time.Now().Truncate(time.Hour)

	// User 1: service-a, model-1, success
	if err := st.AddRequestLog(u1.ID, "model-1", "service-a", now, now.Add(time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}
	// User 2: service-b, model-2, error
	if err := st.AddRequestLog(u2.ID, "model-2", "service-b", now, now.Add(time.Second), "error", "upstream_error"); err != nil {
		t.Fatal(err)
	}
	// User 1: service-b, model-2, error
	if err := st.AddRequestLog(u1.ID, "model-2", "service-b", now.Add(time.Minute), now.Add(time.Minute+time.Second), "error", "upstream_error"); err != nil {
		t.Fatal(err)
	}

	// Filter by user_id
	stats, err := st.LogStatsByHour(LogFilter{UserID: &u1.ID})
	if err != nil {
		t.Fatalf("LogStatsByHour with user filter: %v", err)
	}
	if len(stats) != 1 || stats[0].Total != 2 {
		t.Fatalf("user filter: got total=%d, want 2", len(stats))
	}

	// Filter by service
	stats, err = st.LogStatsByHour(LogFilter{Service: "service-a"})
	if err != nil {
		t.Fatalf("LogStatsByHour with service filter: %v", err)
	}
	if len(stats) != 1 || stats[0].Total != 1 {
		t.Fatalf("service filter: got total=%d, want 1", stats[0].Total)
	}

	// Filter by model
	stats, err = st.LogStatsByHour(LogFilter{Model: "model-2"})
	if err != nil {
		t.Fatalf("LogStatsByHour with model filter: %v", err)
	}
	if len(stats) != 1 || stats[0].Total != 2 {
		t.Fatalf("model filter: got total=%d, want 2", stats[0].Total)
	}

	// Filter by status
	stats, err = st.LogStatsByHour(LogFilter{Status: "error"})
	if err != nil {
		t.Fatalf("LogStatsByHour with status filter: %v", err)
	}
	if len(stats) != 1 || stats[0].Total != 2 {
		t.Fatalf("status filter: got total=%d, want 2", stats[0].Total)
	}

	// Filter by since/until
	later := now.Add(30 * time.Minute)
	stats, err = st.LogStatsByHour(LogFilter{Since: later.Unix()})
	if err != nil {
		t.Fatalf("LogStatsByHour with since filter: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("since filter (no data): got %d buckets, want 0", len(stats))
	}

	// Combined filters
	stats, err = st.LogStatsByHour(LogFilter{Service: "service-b", Status: "error"})
	if err != nil {
		t.Fatalf("LogStatsByHour with combined filters: %v", err)
	}
	if len(stats) != 1 || stats[0].Total != 2 {
		t.Fatalf("combined filters: got total=%d, want 2", stats[0].Total)
	}
}

func TestLogStatsByHour_EmptyResult(t *testing.T) {
	st, _ := openTemp(t)
	stats, err := st.LogStatsByHour(LogFilter{})
	if err != nil {
		t.Fatalf("LogStatsByHour on empty DB: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("got %d buckets on empty DB, want 0", len(stats))
	}
}

func TestLogStatsByHour_HourUnixIsHourStart(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("hour-utc", "hour-utc", "")

	// Insert a log at a non-hour-aligned minute; the bucket must report the
	// hour-start unix timestamp, not the log's own started_at. Using a fixed
	// UTC time within the retention window.
	known := time.Now().UTC().Truncate(time.Hour).Add(30 * time.Minute)
	if err := st.AddRequestLog(u.ID, "m", "s", known, known.Add(time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}

	stats, err := st.LogStatsByHour(LogFilter{})
	if err != nil {
		t.Fatalf("LogStatsByHour: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("got %d buckets, want 1", len(stats))
	}
	want := known.Truncate(time.Hour).Unix()
	if stats[0].HourUnix != want {
		t.Errorf("hour_unix = %d, want %d (bucketed hour start)", stats[0].HourUnix, want)
	}
	// The bucket must be a whole-hour multiple.
	if stats[0].HourUnix%3600 != 0 {
		t.Errorf("hour_unix %d not divisible by 3600", stats[0].HourUnix)
	}
}

func TestLogStatsByHour_CrossesUTCBoundary(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("utc-boundary", "utc", "")

	// Create logs that span a UTC day boundary.
	// Use a recent time within the retention window.
	utcMidnight := time.Now().UTC().Truncate(24 * time.Hour)

	// 23:50 UTC (previous day) - 1 success
	if err := st.AddRequestLog(u.ID, "model-1", "service-a", utcMidnight.Add(-10*time.Minute), utcMidnight.Add(-10*time.Minute+time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}
	// 00:10 UTC (next day) - 2 errors
	if err := st.AddRequestLog(u.ID, "model-2", "service-b", utcMidnight.Add(10*time.Minute), utcMidnight.Add(10*time.Minute+time.Second), "error", "upstream_error"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddRequestLog(u.ID, "model-2", "service-b", utcMidnight.Add(10*time.Minute+time.Second), utcMidnight.Add(10*time.Minute+2*time.Second), "error", "upstream_error"); err != nil {
		t.Fatal(err)
	}

	stats, err := st.LogStatsByHour(LogFilter{})
	if err != nil {
		t.Fatalf("LogStatsByHour across UTC boundary: %v", err)
	}

	// Should have 2 hour buckets: 23:00 and 00:00
	if len(stats) != 2 {
		t.Fatalf("got %d hour buckets across UTC boundary, want 2", len(stats))
	}

	// Verify buckets are ordered by hour_unix
	if stats[0].HourUnix >= stats[1].HourUnix {
		t.Error("hour buckets not ordered by hour_unix")
	}
}

func TestLogStatsByHour_RespectsRetentionWindow(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("retention-stats", "retention", "")

	now := time.Now()

	// Old log outside retention window (31 days ago)
	oldLog := now.Add(-31 * 24 * time.Hour)
	if err := st.AddRequestLog(u.ID, "old-model", "old-service", oldLog, oldLog.Add(time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}

	// Recent log within retention window
	recentLog := now.Add(-24 * time.Hour)
	if err := st.AddRequestLog(u.ID, "recent-model", "recent-service", recentLog, recentLog.Add(time.Second), "error", "upstream_error"); err != nil {
		t.Fatal(err)
	}

	// Empty filter should apply retention cutoff and only return recent logs
	stats, err := st.LogStatsByHour(LogFilter{})
	if err != nil {
		t.Fatalf("LogStatsByHour with retention: %v", err)
	}

	if len(stats) != 1 || stats[0].Total != 1 {
		t.Fatalf("retention cutoff: got total=%d buckets, want 1 with 1 entry", len(stats))
	}

	// Explicit since should override retention
	stats, err = st.LogStatsByHour(LogFilter{Since: oldLog.Unix()})
	if err != nil {
		t.Fatalf("LogStatsByHour with explicit since: %v", err)
	}

	if len(stats) != 2 || totalStats(stats) != 2 {
		t.Fatalf("explicit since: got %d buckets with total=%d entries, want 2 entries", len(stats), totalStats(stats))
	}
}

func totalStats(stats []LogHourStat) int {
	total := 0
	for _, h := range stats {
		total += h.Total
	}
	return total
}
