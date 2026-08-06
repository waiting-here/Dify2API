package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func adminGet(gw *Gateway, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func addTestLog(store *db.Store, userID int64, model, service, status, errorCode string, startedAt, endedAt time.Time) {
	store.AddRequestLog(userID, model, service, startedAt, endedAt, status, errorCode)
}

func TestAdminLogs_ForbiddenForNonAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	rec := adminGet(gw, cookie, "/api/admin/logs")
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminLogs_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")

	rec := adminGet(gw, nil, "/api/admin/logs")
	if rec.Code != http.StatusForbidden {
		t.Errorf("no session: status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminLogs_BasicListing(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u1, _ := store.CreateUser("42", "alice", "")
	u2, _ := store.CreateUser("99", "bob", "")

	now := time.Now()
	addTestLog(store, u1.ID, "[general]claude", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))
	const antiAbuseInfo = `{"triggered":"content_too_short","penalties":[]}`
	if _, err := store.AddRequestLogFull(u2.ID, "[custom]gemini", "custom", now.Add(-5*time.Minute), now.Add(-4*time.Minute), "error", "content_too_short", 400, "too short", 0, 0, antiAbuseInfo); err != nil {
		t.Fatalf("AddRequestLogFull: %v", err)
	}

	rec := adminGet(gw, adminCookie, "/api/admin/logs?limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
	if len(resp.Logs) != 2 {
		t.Fatalf("logs count = %d, want 2", len(resp.Logs))
	}
	// Newest first (bob then alice).
	if resp.Logs[0].Username != "bob" {
		t.Errorf("first log should be bob, got %q", resp.Logs[0].Username)
	}
	if resp.Logs[1].Username != "alice" {
		t.Errorf("second log should be alice, got %q", resp.Logs[1].Username)
	}
	if resp.Logs[0].AntiAbuseInfo != antiAbuseInfo {
		t.Errorf("admin anti_abuse_info = %q, want %q", resp.Logs[0].AntiAbuseInfo, antiAbuseInfo)
	}
	if resp.Logs[1].AntiAbuseInfo != "" {
		t.Errorf("normal admin anti_abuse_info = %q, want empty", resp.Logs[1].AntiAbuseInfo)
	}
}

func TestAdminLogs_FilterByUserID(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u1, _ := store.CreateUser("42", "alice", "")
	u2, _ := store.CreateUser("99", "bob", "")

	now := time.Now()
	addTestLog(store, u1.ID, "[general]x", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))
	addTestLog(store, u2.ID, "[custom]y", "custom", "error", "upstream_error", now.Add(-5*time.Minute), now.Add(-4*time.Minute))

	rec := adminGet(gw, adminCookie, fmt.Sprintf("/api/admin/logs?user_id=%d", u1.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
	if len(resp.Logs) != 1 || resp.Logs[0].UserID != u1.ID {
		t.Errorf("wrong user filtered: %+v", resp.Logs)
	}
}

func TestAdminLogs_FilterByService(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()
	addTestLog(store, u.ID, "[general]x", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))
	addTestLog(store, u.ID, "[custom]y", "custom", "success", "", now.Add(-5*time.Minute), now.Add(-4*time.Minute))

	rec := adminGet(gw, adminCookie, "/api/admin/logs?service=general")
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 || resp.Logs[0].Service != "general" {
		t.Errorf("service filter: total=%d log=%+v", resp.Total, resp.Logs)
	}
}

func TestAdminLogs_FilterByStatus(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()
	addTestLog(store, u.ID, "[general]x", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))
	addTestLog(store, u.ID, "[general]x", "general", "error", "timeout", now.Add(-5*time.Minute), now.Add(-4*time.Minute))

	rec := adminGet(gw, adminCookie, "/api/admin/logs?status=error")
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 || resp.Logs[0].Status != "error" {
		t.Errorf("status filter: total=%d log=%+v", resp.Total, resp.Logs)
	}

	// Invalid status -> 400.
	rec = adminGet(gw, adminCookie, "/api/admin/logs?status=invalid")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid status: code = %d, want 400", rec.Code)
	}
}

func TestAdminLogs_FilterByTimeWindow(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	base := time.Now()
	// t-2h, t-1h, t.
	addTestLog(store, u.ID, "[g]x", "general", "success", "", base.Add(-2*time.Hour), base.Add(-2*time.Hour))
	addTestLog(store, u.ID, "[g]x", "general", "success", "", base.Add(-1*time.Hour), base.Add(-1*time.Hour))
	addTestLog(store, u.ID, "[g]x", "general", "success", "", base, base)

	since := base.Add(-90 * time.Minute).Unix()
	until := base.Add(-30 * time.Minute).Unix()

	rec := adminGet(gw, adminCookie, fmt.Sprintf("/api/admin/logs?since=%d&until=%d", since, until))
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Errorf("time window: total = %d, want 1 (only the -1h entry)", resp.Total)
	}
}

func TestAdminLogs_Pagination(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()
	for i := 0; i < 5; i++ {
		addTestLog(store, u.ID, "[g]x", "general", "success", "", now.Add(-time.Duration(i)*time.Minute), now.Add(-time.Duration(i)*time.Minute+30*time.Second))
	}

	// Page 1: 3 items (offset 0, limit 3).
	rec := adminGet(gw, adminCookie, "/api/admin/logs?limit=3&offset=0")
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 5 {
		t.Errorf("pagination total = %d, want 5", resp.Total)
	}
	if len(resp.Logs) != 3 {
		t.Errorf("page1 count = %d, want 3", len(resp.Logs))
	}

	// Page 2: 2 items (offset 3, limit 3).
	rec = adminGet(gw, adminCookie, "/api/admin/logs?limit=3&offset=3")
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 5 {
		t.Errorf("pagination total (page2) = %d, want 5", resp.Total)
	}
	if len(resp.Logs) != 2 {
		t.Errorf("page2 count = %d, want 2", len(resp.Logs))
	}
}

func TestAdminLogs_FilterByModel(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()
	addTestLog(store, u.ID, "[general]claude", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))
	addTestLog(store, u.ID, "[general]gemini", "general", "success", "", now.Add(-5*time.Minute), now.Add(-4*time.Minute))

	rec := adminGet(gw, adminCookie, "/api/admin/logs?model="+url.QueryEscape("[general]claude"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 || len(resp.Logs) != 1 {
		t.Errorf("model filter: total=%d len=%d, want 1/1", resp.Total, len(resp.Logs))
	}
	if len(resp.Logs) > 0 && resp.Logs[0].Model != "[general]claude" {
		t.Errorf("wrong model filtered: %s", resp.Logs[0].Model)
	}
}

func TestAdminLogs_DonationSourceDisplay(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()

	// Create a donation and a bound log.
	d := &db.Donation{
		Service:         "general",
		Model:           "test",
		DifyBaseURL:     "https://api.example.com",
		Deadline:        now.Add(7 * 24 * time.Hour).Unix(),
		TotalCount:      5,
		SourceDiscordID: "999",
		SourceUsername:  "donor",
	}
	created, err := store.CreateDonation(d, "k1")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}
	store.AddRequestLogDonation(u.ID, "[公益][general]test", "general", now.Add(-10*time.Minute), now.Add(-9*time.Minute), "success", "", created.ID)

	rec := adminGet(gw, adminCookie, "/api/admin/logs?limit=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var resp struct {
		Total int               `json:"total"`
		Logs  []json.RawMessage `json:"logs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", resp.Total, len(resp.Logs))
	}

	// Check source_display is present in the JSON.
	var logEntry map[string]interface{}
	if err := json.Unmarshal(resp.Logs[0], &logEntry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}
	src, ok := logEntry["source_display"].(string)
	if !ok || src == "" {
		t.Errorf("source_display missing or empty: %v", logEntry)
	}
	t.Logf("source_display = %q", src)

	// After deleting the donation, source_display should show "（条目已删除）".
	store.DeleteDonation(created.ID)
	rec = adminGet(gw, adminCookie, "/api/admin/logs?limit=10")
	json.NewDecoder(rec.Body).Decode(&resp)

	var logEntry2 map[string]interface{}
	json.Unmarshal(resp.Logs[0], &logEntry2)
	src2, _ := logEntry2["source_display"].(string)
	if src2 != "（条目已删除）" {
		t.Errorf("after deletion source_display = %q, want \"（条目已删除）\"", src2)
	}
}

func TestAdminLogs_DeletedUser(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("42", "alice", "")
	now := time.Now()
	addTestLog(store, u.ID, "[g]x", "general", "success", "", now.Add(-10*time.Minute), now.Add(-9*time.Minute))

	// Simulate a user row being deleted while logs remain (e.g. manual DB
	// cleanup that missed the request_logs FK cascade).  DeleteUser() would
	// also wipe the logs, so we bypass it here to exercise the LEFT JOIN path.
	store.RawExec(`DELETE FROM users WHERE id = ?`, u.ID)

	rec := adminGet(gw, adminCookie, "/api/admin/logs")
	var resp struct {
		Total int                  `json:"total"`
		Logs  []db.AdminRequestLog `json:"logs"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1 (logs survive manual user row deletion)", resp.Total)
	}
	// Username should be empty for deleted user (COALESCE returns '').
	if resp.Logs[0].Username != "" {
		t.Errorf("username for deleted user = %q, want empty string", resp.Logs[0].Username)
	}
	if resp.Logs[0].UserID != u.ID {
		t.Errorf("user_id for deleted user = %d, want %d", resp.Logs[0].UserID, u.ID)
	}
}

func TestAdminLogs_InvalidParams(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Invalid user_id.
	rec := adminGet(gw, adminCookie, "/api/admin/logs?user_id=abc")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid user_id: status = %d, want 400", rec.Code)
	}

	// Invalid status.
	rec = adminGet(gw, adminCookie, "/api/admin/logs?status=partial")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid status: status = %d, want 400", rec.Code)
	}

	// Invalid limit (negative).
	rec = adminGet(gw, adminCookie, "/api/admin/logs?limit=-1")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("negative limit: status = %d, want 400", rec.Code)
	}

	// Invalid since.
	rec = adminGet(gw, adminCookie, "/api/admin/logs?since=notanumber")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid since: status = %d, want 400", rec.Code)
	}
}

func TestPurgeExpiredSessions(t *testing.T) {
	_, store := setupAuthGateway(t, "s3cret")

	// Create a session that's already expired.
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)

	// Manually set the session expiry to the past.
	store.RawExec(`UPDATE sessions SET expires_at = ? WHERE id = ?`, time.Now().Add(-1*time.Hour).Unix(), token)

	// Purge.
	n, err := store.PurgeExpiredSessions()
	if err != nil {
		t.Fatalf("purge error: %v", err)
	}
	if n < 1 {
		t.Errorf("purged %d sessions, want >= 1", n)
	}

	// The token should no longer resolve.
	u2, _ := store.GetSessionUser(token)
	if u2 != nil {
		t.Error("expired session should not resolve after purge")
	}
}

func TestAdminLogs_HostSeparation_UserHost(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Use Wrap to exercise hostSeparation middleware.
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	// User-host access to /api/admin/logs should return 404 (hostSeparation).
	req := httptest.NewRequest(http.MethodGet, "/api/admin/logs", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.SiteHost // user site host
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("user-host admin logs: status = %d, want 404 (hostSeparation)", rec.Code)
	}
	// The error should have our standard format or be a 404 page.
	body := rec.Body.String()
	if !strings.Contains(body, "not_found") && !strings.Contains(body, "404") {
		t.Logf("user-host 404 body: %s", body)
	}

	// Admin-host access should work.
	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/logs?limit=1", nil)
	req2.AddCookie(adminCookie)
	req2.Host = gw.Config.Admin.AdminHost
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("admin-host logs: status = %d, want 200; body: %s", rec2.Code, rec2.Body.String())
	}
}

func TestAdminHost_ServicesAllowed(t *testing.T) {
	// Regression (VPS deploy 2026-07-22): the admin dashboard's log filter
	// fetches /api/services, which must be reachable on the admin host —
	// it was missing from the hostSeparation allowlist and returned 404,
	// aborting renderAdminDashboard mid-way.
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	_ = store

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin-host /api/services: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "general") {
		t.Errorf("services response should list registry entries: %s", rec.Body.String())
	}
}

func TestAdminLogs_ExportNotTruncated(t *testing.T) {
	// Regression: /api/admin/logs/export silently capped the export at 100
	// rows (ListAllRequestLogs page clamp); the export must be complete.
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	u, _ := store.CreateUser("export-e2e", "export-e2e", "")
	now := time.Now().Truncate(time.Second)
	const n = 150
	for i := 0; i < n; i++ {
		addTestLog(store, u.ID, "[general]m", "general", "success", "", now.Add(-time.Duration(n-i)*time.Second), now)
	}

	rec := adminGet(gw, adminCookie, "/api/admin/logs/export?format=json")
	if rec.Code != http.StatusOK {
		t.Fatalf("export: status = %d, body %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("export decode: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("export rows = %d, want %d (export must not be page-clamped)", len(rows), n)
	}

	// CSV export is equally complete.
	recCSV := adminGet(gw, adminCookie, "/api/admin/logs/export?format=csv")
	if recCSV.Code != http.StatusOK {
		t.Fatalf("csv export: status = %d", recCSV.Code)
	}
	lines := strings.Split(strings.TrimSpace(recCSV.Body.String()), "\n")
	if len(lines) != n+1 { // header + data rows
		t.Fatalf("csv lines = %d, want %d", len(lines), n+1)
	}
}

func TestSafeCSVCell_AllExportFields(t *testing.T) {
	fields := []string{
		"ID", "User", "Username", "Model", "Service", "StartedAt", "EndedAt",
		"Status", "ErrorCode", "HTTPStatus", "ErrorDetail", "DonationID",
		"CreditsConsumed", "AntiAbuseInfo", "DonationSource",
	}
	for _, field := range fields {
		for _, prefix := range []string{"=", "+", "-", "@"} {
			t.Run(field+"/"+prefix, func(t *testing.T) {
				input := prefix + "dangerous()"
				if got := safeCSVCell(input); got != "'"+input {
					t.Fatalf("safeCSVCell(%q) = %q, want apostrophe-prefixed text", input, got)
				}
			})
		}
	}
	for _, safe := range []string{"", "plain", "  =leading-space", "123"} {
		if got := safeCSVCell(safe); got != safe {
			t.Errorf("safeCSVCell(%q) = %q, want unchanged", safe, got)
		}
	}
}

func TestAdminLogs_CSVFormulaInjectionAndSecretFields(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	u, err := store.CreateUser("=discord", "+username", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddRequestLogFull(u.ID, "=model", "+service", time.Unix(100, 0), time.Unix(101, 0),
		"-status", "@error", -418, "=detail,with,commas", 0, -7, "+anti-abuse"); err != nil {
		t.Fatal(err)
	}

	rec := adminGet(gw, adminCookie, "/api/admin/logs/export?format=csv")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 2 || len(rows[1]) != 15 {
		t.Fatalf("CSV shape = %d rows / %d columns; body=%q", len(rows), len(rows[1]), rec.Body.String())
	}
	for _, index := range []int{2, 3, 4, 7, 8, 9, 10, 12, 13} {
		if !strings.HasPrefix(rows[1][index], "'") {
			t.Errorf("column %s = %q, want formula-safe apostrophe", rows[0][index], rows[1][index])
		}
	}
	for _, header := range rows[0] {
		if strings.Contains(strings.ToLower(header), "key") {
			t.Errorf("CSV schema unexpectedly exposes key material: header=%q", header)
		}
	}

	// Formula protection is CSV-only; the existing JSON export contract must
	// remain byte-for-value compatible for the same log fields.
	jsonRec := adminGet(gw, adminCookie, "/api/admin/logs/export?format=json")
	var jsonRows []struct {
		Username        string `json:"username"`
		Model           string `json:"model"`
		Service         string `json:"service"`
		Status          string `json:"status"`
		ErrorCode       string `json:"error_code"`
		HTTPStatus      int    `json:"http_status"`
		ErrorDetail     string `json:"error_detail"`
		CreditsConsumed int    `json:"credits_consumed"`
		AntiAbuseInfo   string `json:"anti_abuse_info"`
	}
	if err := json.Unmarshal(jsonRec.Body.Bytes(), &jsonRows); err != nil || len(jsonRows) != 1 {
		t.Fatalf("JSON export decode: rows=%d err=%v body=%s", len(jsonRows), err, jsonRec.Body.String())
	}
	got := jsonRows[0]
	if got.Username != "+username" || got.Model != "=model" || got.Service != "+service" ||
		got.Status != "-status" || got.ErrorCode != "@error" || got.HTTPStatus != -418 ||
		got.ErrorDetail != "=detail,with,commas" || got.CreditsConsumed != -7 || got.AntiAbuseInfo != "+anti-abuse" {
		t.Fatalf("JSON semantics changed: %+v", got)
	}
}
