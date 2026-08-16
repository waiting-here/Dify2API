package handler

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"dify2api/diagnostic"
	"dify2api/dify"
)

func assertR03Diagnostic(t *testing.T, got string, wantMarker bool) {
	t.Helper()
	if len(got) > diagnostic.MaxBytes {
		t.Fatalf("diagnostic length = %d, want <= %d", len(got), diagnostic.MaxBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("diagnostic is not valid UTF-8")
	}
	if strings.ContainsAny(got, "\r\n\t") {
		t.Fatalf("diagnostic contains CR/LF/TAB: %q", got)
	}
	if wantMarker && !strings.Contains(got, diagnostic.TruncationMarker) {
		t.Fatalf("diagnostic has no truncation marker: %q", got)
	}
}

func TestBoundedProcessError_WrappedUpstream(t *testing.T) {
	err := fmt.Errorf("image 1: %w", &dify.DifyError{
		Code:    dify.WorkflowFailedCode,
		Message: strings.Repeat("x", 64*1024) + "\r\n\tsecret",
		Status:  http.StatusOK,
	})
	got := boundedProcessError(err)
	if len(got) > diagnostic.ProcessMaxBytes {
		t.Fatalf("wrapped process diagnostic length = %d, want <= %d", len(got), diagnostic.ProcessMaxBytes)
	}
	assertR03Diagnostic(t, got, true)
}

func TestStopWorkflowDiagnostic_BoundsTaskIDAndError(t *testing.T) {
	taskID := strings.Repeat("task", (10<<20)/len("task")) + "\r\n\t"
	err := errors.New(strings.Repeat("stop failed", 8*1024) + "\r\n\t")
	message := boundedStopWorkflowDiagnostic(taskID, err)
	if len(message) > diagnostic.ProcessMaxBytes {
		t.Fatalf("stop diagnostic length = %d, want <= %d", len(message), diagnostic.ProcessMaxBytes)
	}
	if !strings.HasPrefix(message, stopWorkflowLogPrefix) {
		t.Fatalf("stop diagnostic lost fixed prefix: %q", message[:min(len(message), 64)])
	}
	assertR03Diagnostic(t, message, true)

	var process bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&process)
	defer log.SetOutput(previous)
	client := dify.NewClient("://invalid", "key", time.Second)
	(&Gateway{}).stopDifyWorkflow(client, taskID, "u1")
	line := strings.TrimSuffix(process.String(), "\n")
	if len(line) > diagnostic.MaxBytes || strings.ContainsAny(line, "\r\n\t") {
		t.Fatalf("captured stop log is not bounded/single-line: len=%d value=%q", len(line), line[:min(len(line), 128)])
	}
}

func TestBlockingFailed200_DiagnosticBoundsOrdinary(t *testing.T) {
	raw := strings.Repeat("上游错误", 16*1024) + "\r\n\tsecret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":"failed","error":%q,"outputs":{}}}`, raw)
	}))
	defer srv.Close()

	gw, key, userID := setupRoutedUser(t, srv.URL, "[general]r03-ordinary")
	var process bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&process)
	defer log.SetOutput(previous)

	rec := chatRequest(gw, key, `{"model":"[general]r03-ordinary","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), raw) || strings.Contains(rec.Body.String(), "上游错误") {
		t.Fatal("public response leaked upstream diagnostic")
	}

	logs, err := gw.Store.ListRequestLogs(userID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("request logs len=%d err=%v", len(logs), err)
	}
	assertR03Diagnostic(t, logs[0].ErrorDetail, true)

	alerts, total, err := gw.Store.ListAdminAlerts(10, 0)
	if err != nil || total != 1 || len(alerts) != 1 {
		t.Fatalf("alerts total=%d len=%d err=%v", total, len(alerts), err)
	}
	assertR03Diagnostic(t, alerts[0].Message, true)
	if strings.Count(alerts[0].Message, "上游错误") > 1300 {
		t.Fatalf("alert appears to repeat the full upstream error: len=%d", len(alerts[0].Message))
	}

	processText := process.String()
	if !strings.Contains(processText, "[ERROR] dify blocking") {
		t.Fatalf("process log missing blocking diagnostic: %q", processText)
	}
	for _, line := range strings.Split(processText, "\n") {
		if strings.Contains(line, "[ERROR] dify blocking") {
			assertR03Diagnostic(t, line, true)
			break
		}
	}
}

func TestBlockingFailed200_DiagnosticBoundsCharity(t *testing.T) {
	raw := strings.Repeat("公益上游失败", 16*1024) + "\r\n\tsecret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":"failed","error":%q,"outputs":{}}}`, raw)
	}))
	defer srv.Close()

	fixture := setupCharityOutcomeFixture(t, srv.URL, "general", "r03-charity")
	rec := chatRequest(fixture.gw, fixture.key, fixture.requestBody(false))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), raw) || strings.Contains(rec.Body.String(), "公益上游失败") {
		t.Fatal("public charity response leaked upstream diagnostic")
	}

	logs, err := fixture.gw.Store.ListRequestLogs(fixture.consumer.ID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("charity request logs len=%d err=%v", len(logs), err)
	}
	assertR03Diagnostic(t, logs[0].ErrorDetail, true)
	alerts, total, err := fixture.gw.Store.ListAdminAlerts(10, 0)
	if err != nil || total != 1 || len(alerts) != 1 {
		t.Fatalf("charity alerts total=%d len=%d err=%v", total, len(alerts), err)
	}
	assertR03Diagnostic(t, alerts[0].Message, true)
	fixture.assertCommitted(t)
}
