package db

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"dify2api/diagnostic"
)

func assertBoundDiagnostic(t *testing.T, got string, wantTruncated bool) {
	t.Helper()
	if len(got) > diagnostic.MaxBytes {
		t.Fatalf("diagnostic length = %d, want <= %d", len(got), diagnostic.MaxBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("diagnostic is not valid UTF-8")
	}
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("diagnostic contains a line/control separator: %q", got)
	}
	if wantTruncated && !strings.HasSuffix(got, diagnostic.TruncationMarker) {
		t.Fatalf("diagnostic is missing truncation marker: %q", got[len(got)-min(len(got), 32):])
	}
}

func TestDiagnosticSinks_BoundDirectInputs(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("diagnostic-bound", "diagnostic-bound", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	raw := strings.Repeat("公益", diagnostic.MaxBytes) + "\r\n\tsecret"
	if _, err := st.AddRequestLogFull(u.ID, "[general]diagnostic", "general", nowUTC(), nowUTC(), "error", "code\r\n\t", 502, raw, 0, 0, ""); err != nil {
		t.Fatalf("AddRequestLogFull: %v", err)
	}
	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListRequestLogs len=%d err=%v", len(logs), err)
	}
	assertBoundDiagnostic(t, logs[0].ErrorDetail, true)
	assertBoundDiagnostic(t, logs[0].ErrorCode, false)

	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: raw}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}
	alerts, total, err := st.ListAdminAlerts(10, 0)
	if err != nil || total != 1 || len(alerts) != 1 {
		t.Fatalf("ListAdminAlerts total=%d len=%d err=%v", total, len(alerts), err)
	}
	assertBoundDiagnostic(t, alerts[0].Message, true)
}

func TestDiagnosticSinks_ExactBoundaryPreserved(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("diagnostic-exact", "diagnostic-exact", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	exact := strings.Repeat("x", diagnostic.MaxBytes)
	if _, err := st.AddRequestLogFull(u.ID, "[general]exact", "general", nowUTC(), nowUTC(), "error", "exact", 502, exact, 0, 0, ""); err != nil {
		t.Fatalf("AddRequestLogFull: %v", err)
	}
	logs, err := st.ListRequestLogs(u.ID, 10)
	if err != nil || len(logs) != 1 || logs[0].ErrorDetail != exact {
		t.Fatalf("exact request diagnostic changed: len=%d err=%v", len(logs), err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: AlertBlockingFailed200, Message: exact}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}
	alerts, _, err := st.ListAdminAlerts(10, 0)
	if err != nil || len(alerts) != 1 || alerts[0].Message != exact {
		t.Fatalf("exact alert diagnostic changed: alerts=%d err=%v", len(alerts), err)
	}
}

func nowUTC() time.Time { return time.Now().UTC() }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
