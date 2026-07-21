package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDifySSE serves a scripted SSE stream for /v1/workflows/run.
func mockDifySSE(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			fmt.Fprintf(w, "data: %s\n\n", l)
		}
	}))
}

func streamChatRequest(gw *Gateway, key string) *httptest.ResponseRecorder {
	body := `{"model":"[general]x","messages":[{"role":"user","content":"hi"}],"stream":true}`
	return chatRequest(gw, key, body)
}

func TestStreaming_Success_SendsDone(t *testing.T) {
	srv := mockDifySSE(t, []string{
		`{"event":"text_chunk","data":{"text":"hello"}}`,
		`{"event":"workflow_finished","data":{"status":"succeeded"}}`,
	})
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	rec := streamChatRequest(gw, key)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, body)
	}
	if !strings.Contains(body, `"content":"hello"`) || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("successful stream must contain content and [DONE]: %s", body)
	}
	if strings.Contains(body, `"error"`) {
		t.Errorf("successful stream must not contain an error frame: %s", body)
	}
}

func TestStreaming_WorkflowFailed_ErrorFrameNoDone(t *testing.T) {
	srv := mockDifySSE(t, []string{
		`{"event":"text_chunk","data":{"text":"partial"}}`,
		`{"event":"workflow_finished","data":{"status":"failed","error":"credit exhausted"}}`,
	})
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]x")

	rec := streamChatRequest(gw, key)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (SSE already committed); body: %s", rec.Code, body)
	}
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "credit exhausted") {
		t.Errorf("failed stream must contain an error frame: %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Errorf("failed stream must NOT send [DONE]: %s", body)
	}
	// Logged as error.
	logs, err := gw.Store.ListRequestLogs(uid, 10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("no request log: %v", err)
	}
	if logs[0].Status != "error" || logs[0].ErrorCode != "upstream_error" {
		t.Errorf("log = %s/%s, want error/upstream_error", logs[0].Status, logs[0].ErrorCode)
	}
}

func TestStreaming_ErrorEvent_ErrorFrameNoDone(t *testing.T) {
	srv := mockDifySSE(t, []string{
		`{"event":"error","data":{"error":"internal server error"}}`,
	})
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	rec := streamChatRequest(gw, key)
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, "internal server error") {
		t.Errorf("error event must surface as error frame: %s", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Errorf("error stream must NOT send [DONE]: %s", body)
	}
}

func TestStreaming_ImmediateHTTPError_PassesThrough4xx(t *testing.T) {
	// Dify responds 400 invalid_param before any SSE: the gateway should
	// return a JSON error with the upstream status code (400, not 502).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"code":"invalid_param","message":"missing input"}`)
	}))
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	rec := streamChatRequest(gw, key)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (pass-through); body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "invalid_param") || !strings.Contains(body, "[Dify]") {
		t.Errorf("error body should carry upstream code and [Dify] prefix: %s", body)
	}
}

func TestStreaming_ImmediateHTTPError_5xxMapsTo502(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"code":"server_overloaded","message":"try later"}`)
	}))
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	rec := streamChatRequest(gw, key)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for upstream 5xx; body: %s", rec.Code, rec.Body.String())
	}
}

func TestBlocking_UpstreamError_PassesThrough4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"code":"app_unavailable","message":"app deleted"}`)
	}))
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	body := `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (pass-through); body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "app_unavailable") {
		t.Errorf("body should carry upstream code: %s", rec.Body.String())
	}
}
