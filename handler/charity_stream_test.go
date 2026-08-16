package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dify2api/db"
)

func charityRPMCounts(gw *Gateway, userID int64) (classA, classB int) {
	gw.limiter.mu.Lock()
	defer gw.limiter.mu.Unlock()
	return len(gw.limiter.hits[rpmClassA][userID]), len(gw.limiter.hits[rpmClassB][userID])
}

func assertCharityStreamLog(t *testing.T, f *charityOutcomeFixture, wantStatus, wantCode string, wantHTTP int) {
	t.Helper()
	logs, err := f.gw.Store.ListRequestLogs(f.consumer.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("request logs = %d, want 1: %+v", len(logs), logs)
	}
	if logs[0].Status != wantStatus || logs[0].ErrorCode != wantCode || logs[0].HTTPStatus != wantHTTP {
		t.Fatalf("request log = status=%q code=%q http=%d, want %q/%q/%d", logs[0].Status, logs[0].ErrorCode, logs[0].HTTPStatus, wantStatus, wantCode, wantHTTP)
	}

	userID := f.consumer.ID
	adminLogs, total, err := f.gw.Store.ListAllRequestLogs(db.LogFilter{UserID: &userID}, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(adminLogs) != 1 {
		t.Fatalf("admin request logs total=%d len=%d, want 1/1", total, len(adminLogs))
	}
	if adminLogs[0].DonationID == nil || *adminLogs[0].DonationID != f.donation.ID {
		t.Fatalf("donation_id = %v, want %d", adminLogs[0].DonationID, f.donation.ID)
	}
	if adminLogs[0].CreditsConsumed != 10 {
		t.Fatalf("credits_consumed = %d, want 10", adminLogs[0].CreditsConsumed)
	}
}

func assertCharityErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v; body=%q", err, rec.Body.String())
	}
	if body.Error.Code != wantCode || body.Error.Type != wantCode {
		t.Fatalf("error envelope = %+v, want code/type %q", body.Error, wantCode)
	}
	if !strings.HasPrefix(body.Error.Message, "[Dify2API] ") {
		t.Fatalf("error message = %q, want [Dify2API] prefix", body.Error.Message)
	}
}

type signalResponseWriter struct {
	*httptest.ResponseRecorder
	wrote chan struct{}
	once  sync.Once
}

func (w *signalResponseWriter) signalWrite() {
	w.once.Do(func() { close(w.wrote) })
}

func (w *signalResponseWriter) WriteHeader(status int) {
	w.signalWrite()
	w.ResponseRecorder.WriteHeader(status)
}

func (w *signalResponseWriter) Write(p []byte) (int, error) {
	w.signalWrite()
	return w.ResponseRecorder.Write(p)
}

func (w *signalResponseWriter) Flush() {
	w.signalWrite()
	w.ResponseRecorder.Flush()
}

func TestCharityStreaming_EmptyCleanEOFIsGatewayError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "empty-clean-eof")
	rec := chatRequest(f.gw, f.key, f.requestBody(true))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%q", rec.Code, rec.Body.String())
	}
	assertCharityErrorEnvelope(t, rec, "upstream_error")
	if strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("empty charity stream emitted [DONE]: %s", rec.Body.String())
	}
	assertCharityStreamLog(t, f, "error", "upstream_empty_stream", http.StatusBadGateway)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 0 {
		t.Fatalf("empty stream RPM A/B = %d/%d, want 0/0", classA, classB)
	}
}

func TestCharityStreaming_PartialCleanEOFIsCutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"event":"text_chunk","task_id":"partial-task","data":{"text":"partial"}}`+"\n\n")
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "partial-clean-eof")
	rec := chatRequest(f.gw, f.key, f.requestBody(true))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after SSE headers; body=%q", rec.Code, body)
	}
	if !strings.Contains(body, `"content":"partial"`) || !strings.Contains(body, `"error"`) {
		t.Fatalf("partial stream must contain content and error frame: %s", body)
	}
	if strings.Contains(body, `"finish_reason":"stop"`) || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("partial stream must not synthesize stop/[DONE]: %s", body)
	}
	assertCharityStreamLog(t, f, "error", "upstream_stream_cut", http.StatusOK)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 1 {
		t.Fatalf("partial stream RPM A/B = %d/%d, want 0/1", classA, classB)
	}
}

func TestCharityStreaming_NormalCompletionKeepsSuccessSemantics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"event":"text_chunk","data":{"text":"complete"}}`+"\n\n")
		fmt.Fprint(w, `data: {"event":"workflow_finished","data":{"status":"succeeded"}}`+"\n\n")
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "normal-completion")
	rec := chatRequest(f.gw, f.key, f.requestBody(true))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"content":"complete"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("normal charity stream = %d %s", rec.Code, body)
	}
	if strings.Contains(body, `"error"`) {
		t.Fatalf("normal charity stream emitted an error frame: %s", body)
	}
	assertCharityStreamLog(t, f, "success", "", http.StatusOK)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 1 || classB != 1 {
		t.Fatalf("normal stream RPM A/B = %d/%d, want 1/1", classA, classB)
	}
}

func TestCharityStreaming_LateTransportErrorKeepsAccounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test response writer does not support flushing")
		}
		fmt.Fprint(w, `data: {"event":"text_chunk","task_id":"late-error-task","data":{"text":"partial"}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "late-transport-error")
	rec := chatRequest(f.gw, f.key, f.requestBody(true))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"error"`) || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("late transport error response = %d %s", rec.Code, body)
	}
	assertCharityStreamLog(t, f, "error", "upstream_error", http.StatusOK)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 1 {
		t.Fatalf("late transport error RPM A/B = %d/%d, want 0/1", classA, classB)
	}
}

func TestCharityStreaming_WorkflowFailedKeepsAccounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"event":"text_chunk","data":{"text":"partial"}}`+"\n\n")
		fmt.Fprint(w, `data: {"event":"workflow_finished","data":{"status":"failed","error":"workflow failed"}}`+"\n\n")
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "workflow-failed")
	rec := chatRequest(f.gw, f.key, f.requestBody(true))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"error"`) || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("workflow failed response = %d %s", rec.Code, body)
	}
	assertCharityStreamLog(t, f, "error", "upstream_error", http.StatusOK)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 1 {
		t.Fatalf("workflow failed RPM A/B = %d/%d, want 0/1", classA, classB)
	}
}

func TestCharityStreaming_ClientCancelKeepsAccounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workflows/run":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"event":"text_chunk","task_id":"cancel-task","data":{"text":"partial"}}`+"\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		case "/v1/workflows/tasks/cancel-task/stop":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":"success"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "client-cancel-stream")
	mux := http.NewServeMux()
	f.gw.RegisterRoutes(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.requestBody(true))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	responseStarted := make(chan struct{})
	signalWriter := &signalResponseWriter{ResponseRecorder: rec, wrote: responseStarted}
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(signalWriter, req)
		close(done)
	}()

	select {
	case <-responseStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("downstream did not receive the first stream frame")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("charity streaming request did not stop after cancellation")
	}

	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("client-canceled stream = %d %s", rec.Code, rec.Body.String())
	}
	assertCharityStreamLog(t, f, "error", "client_canceled", statusClientClosedRequest)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 1 {
		t.Fatalf("client-canceled stream RPM A/B = %d/%d, want 0/1", classA, classB)
	}
}

func TestCharityStreaming_ClientCancelBeforeFirstEvent(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	f := setupCharityOutcomeFixture(t, srv.URL, "general", "client-cancel-before-first")
	mux := http.NewServeMux()
	f.gw.RegisterRoutes(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(f.requestBody(true))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.key)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("charity request did not stop after cancellation")
	}

	assertCharityStreamLog(t, f, "error", "client_canceled", statusClientClosedRequest)
	f.assertCommitted(t)
	classA, classB := charityRPMCounts(f.gw, f.consumer.ID)
	if classA != 0 || classB != 0 {
		t.Fatalf("pre-first-event client-canceled RPM A/B = %d/%d, want 0/0", classA, classB)
	}
}
