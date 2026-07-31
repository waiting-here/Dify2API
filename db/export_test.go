package db

import (
	"testing"
	"time"
)

func TestExportUserData_Complete(t *testing.T) {
	st, _ := openTemp(t)

	// Create a user with lang set.
	u, err := st.CreateUser("777", "export_user", "ava1")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Set lang via raw exec (CreateUser doesn't take lang).
	if _, err := st.RawExec(`UPDATE users SET lang='en' WHERE id=?`, u.ID); err != nil {
		t.Fatalf("set lang: %v", err)
	}

	// Add credits and a caller key.
	st.SetUserCredits(u.ID, 42)
	if _, err := st.SetCallerKey(u.ID); err != nil {
		t.Fatalf("SetCallerKey: %v", err)
	}

	// Add a request log with full fields.
	if _, err := st.AddRequestLogFull(u.ID, "claude-opus-4-6", "general",
		time.Now().Add(-1*time.Hour), time.Now(),
		"success", "", 200, "", 0, 0, ""); err != nil {
		t.Fatalf("AddRequestLogFull: %v", err)
	}

	// Add a second log with error details, credits consumed, and anti-abuse info.
	if _, err := st.AddRequestLogFull(u.ID, "gpt-5", "general",
		time.Now(), time.Now(),
		"error", "content_too_short", 400, "message too short",
		0, 5, `{"trigger":"content_too_short","penalties":[{"type":"deduct","credits":5}]}`); err != nil {
		t.Fatalf("AddRequestLogFull 2: %v", err)
	}

	// Create a donation application.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	app, err := st.CreateDonationApplication(u.ID, "general", "claude-opus-4-6",
		"https://dify.example.com/v1", "app-secret-key", 100, deadline, 10, "test")
	if err != nil {
		t.Fatalf("CreateDonationApplication: %v", err)
	}

	bundle, err := st.ExportUserData(u.ID)
	if err != nil {
		t.Fatalf("ExportUserData: %v", err)
	}
	if bundle == nil {
		t.Fatal("ExportUserData returned nil")
	}

	// Verify ExportUser fields.
	if bundle.User.Lang != "en" {
		t.Errorf("user.Lang = %q, want en", bundle.User.Lang)
	}
	if bundle.User.Credits != 42 {
		t.Errorf("user.Credits = %d, want 42", bundle.User.Credits)
	}

	// Verify ExportLog fields.
	if len(bundle.Logs) != 2 {
		t.Fatalf("logs count = %d, want 2", len(bundle.Logs))
	}
	l1 := bundle.Logs[0] // newest first
	if l1.HttpStatus != 400 {
		t.Errorf("log[0].HttpStatus = %d, want 400", l1.HttpStatus)
	}
	if l1.ErrorDetail != "message too short" {
		t.Errorf("log[0].ErrorDetail = %q", l1.ErrorDetail)
	}
	if l1.CreditsConsumed != 5 {
		t.Errorf("log[0].CreditsConsumed = %d, want 5", l1.CreditsConsumed)
	}
	if l1.AntiAbuseInfo == "" {
		t.Error("log[0].AntiAbuseInfo is empty")
	}
	l2 := bundle.Logs[1]
	if l2.HttpStatus != 200 {
		t.Errorf("log[1].HttpStatus = %d, want 200", l2.HttpStatus)
	}
	if l2.Status != "success" {
		t.Errorf("log[1].Status = %q, want success", l2.Status)
	}

	// Verify ExportDonationApp fields.
	if len(bundle.DonationApplications) != 1 {
		t.Fatalf("donation applications count = %d, want 1", len(bundle.DonationApplications))
	}
	a := bundle.DonationApplications[0]
	if a.RpmLimit != app.RpmLimit {
		t.Errorf("app.RpmLimit = %d, want %d", a.RpmLimit, app.RpmLimit)
	}
	if a.ID != app.ID {
		t.Errorf("app.ID = %d, want %d", a.ID, app.ID)
	}
	// ReviewerID should be nil (not reviewed yet).
	if a.ReviewerID != nil {
		t.Errorf("app.ReviewerID = %v, want nil", a.ReviewerID)
	}

	// Verify caller key is present.
	if bundle.CallerKey == "" {
		t.Error("CallerKey is empty")
	}
}

func TestExportRequestLogs_NoLimit(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("776", "no_limit_user", "")

	// Insert 510 rows (above the 500 UI limit) to prove ExportRequestLogs has no cap.
	const n = 510
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := st.AddRequestLog(u.ID, "m", "general", ts, ts.Add(5*time.Second), "success", ""); err != nil {
			t.Fatalf("AddRequestLog %d: %v", i, err)
		}
	}

	// ExportRequestLogs must return all 510 rows.
	logs, err := st.ExportRequestLogs(u.ID)
	if err != nil {
		t.Fatalf("ExportRequestLogs: %v", err)
	}
	if len(logs) != n {
		t.Errorf("ExportRequestLogs count = %d, want %d", len(logs), n)
	}

	// Verify ListRequestLogs still caps at 500 (regression test for UI path).
	limited, err := st.ListRequestLogs(u.ID, 500)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(limited) != 500 {
		t.Errorf("ListRequestLogs(500) count = %d, want 500", len(limited))
	}
}

// TestExportBundle_ExportLang exercises the new Lang field in ExportUserData.
func TestExportBundle_ExportLang(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("665", "lang_user", "")

	// Default lang is empty string.
	bundle, err := st.ExportUserData(u.ID)
	if err != nil || bundle == nil {
		t.Fatalf("ExportUserData: %v", err)
	}
	if bundle.User.Lang != "" {
		t.Errorf("default lang = %q, want empty", bundle.User.Lang)
	}

	// Set to "en" and re-export.
	if _, err := st.RawExec(`UPDATE users SET lang='en' WHERE id=?`, u.ID); err != nil {
		t.Fatalf("set lang: %v", err)
	}
	bundle, err = st.ExportUserData(u.ID)
	if err != nil || bundle == nil {
		t.Fatalf("ExportUserData 2: %v", err)
	}
	if bundle.User.Lang != "en" {
		t.Errorf("lang = %q, want en", bundle.User.Lang)
	}
}

// TestExportBundle_ReviewerID verifies that reviewer_id is exported when set.
func TestExportBundle_ReviewerID(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("775", "applicant", "")
	reviewer, _ := st.CreateUser("774", "reviewer", "")

	deadline := time.Now().Add(48 * time.Hour).Unix()
	app, err := st.CreateDonationApplication(u.ID, "general", "claude-opus-4-6",
		"https://dify.example.com/v1", "app-secret-key", 100, deadline, 10, "test")
	if err != nil {
		t.Fatalf("CreateDonationApplication: %v", err)
	}

	// Approve the application (sets reviewer_id).
	fields := &ApproveApplicationFields{
		TotalCount:  100,
		Deadline:    deadline,
		RpmLimit:    20,
		Service:     "general",
		Model:       "claude-opus-4-6",
		DifyBaseURL: "https://dify.example.com/v1",
	}
	_, _, err = st.ApproveApplication(app.ID, reviewer.ID, fields, "approved")
	if err != nil {
		t.Fatalf("ApproveApplication: %v", err)
	}

	bundle, err := st.ExportUserData(u.ID)
	if err != nil || bundle == nil {
		t.Fatalf("ExportUserData: %v", err)
	}
	if len(bundle.DonationApplications) != 1 {
		t.Fatalf("apps count = %d, want 1", len(bundle.DonationApplications))
	}
	exported := bundle.DonationApplications[0]
	if exported.ReviewerID == nil || *exported.ReviewerID != reviewer.ID {
		t.Errorf("ReviewerID = %v, want %d", exported.ReviewerID, reviewer.ID)
	}
}
