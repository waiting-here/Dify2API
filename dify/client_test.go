package dify

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient_Timeout(t *testing.T) {
	c := NewClient("http://dify.local/", "app-key", 600*time.Second)
	if c.HTTPClient.Timeout != 600*time.Second {
		t.Errorf("timeout = %v, want 600s", c.HTTPClient.Timeout)
	}
	if c.BaseURL != "http://dify.local" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed", c.BaseURL)
	}
}

func TestNewClient_StripsTrailingV1(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.dify.ai":    "https://api.dify.ai",
		"https://api.dify.ai/":   "https://api.dify.ai",
		"https://api.dify.ai/v1": "https://api.dify.ai",
		"https://api.dify.ai/v1/": "https://api.dify.ai",
	} {
		if got := NewClient(in, "k", time.Second).BaseURL; got != want {
			t.Errorf("NewClient(%q).BaseURL = %q, want %q", in, got, want)
		}
	}
}

func parametersServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/parameters" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer app-key" {
			t.Errorf("missing auth header")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestFetchParameters(t *testing.T) {
	body := `{"user_input_form":[` +
		`{"paragraph":{"label":"系统提示词","variable":"system_prompt","required":false}},` +
		`{"paragraph":{"label":"用户输入0","variable":"user_0","required":true}},` +
		`{"text-input":{"label":"X","variable":"assistant_1","required":false}}` +
		`]}`
	srv := parametersServer(t, body, http.StatusOK)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	vars, err := c.FetchParameters()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"system_prompt", "user_0", "assistant_1"} {
		if _, ok := vars[want]; !ok {
			t.Errorf("variable %q not extracted from user_input_form", want)
		}
	}
	if len(vars) != 3 {
		t.Errorf("got %d variables, want 3", len(vars))
	}
	// required flag: the test body marks user_0 required, others optional.
	if vars["user_0"] != true || vars["system_prompt"] != false || vars["assistant_1"] != false {
		t.Errorf("required flags wrong: %v", vars)
	}
}

func TestFetchParameters_HTTPError(t *testing.T) {
	srv := parametersServer(t, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	if _, err := c.FetchParameters(); err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestCheckApp(t *testing.T) {
	expected := []string{"system_prompt", "user_0"}

	// OK: exact match
	okSrv := parametersServer(t, `{"user_input_form":[`+
		`{"paragraph":{"variable":"system_prompt"}},`+
		`{"paragraph":{"variable":"user_0"}}]}`, http.StatusOK)
	defer okSrv.Close()
	if got := CheckApp(NewClient(okSrv.URL, "app-key", 15*time.Second), expected); !strings.Contains(got, "OK") {
		t.Errorf("expected OK, got: %s", got)
	}

	// Mismatch: missing + unexpected
	mmSrv := parametersServer(t, `{"user_input_form":[`+
		`{"paragraph":{"variable":"system_prompt"}},`+
		`{"paragraph":{"variable":"something_else"}}]}`, http.StatusOK)
	defer mmSrv.Close()
	got := CheckApp(NewClient(mmSrv.URL, "app-key", 15*time.Second), expected)
	if !strings.Contains(got, "MISMATCH") || !strings.Contains(got, "user_0") || !strings.Contains(got, "something_else") {
		t.Errorf("expected MISMATCH with details, got: %s", got)
	}

	// Unavailable: connection refused
	if got := CheckApp(NewClient("http://127.0.0.1:1", "app-key", 15*time.Second), expected); !strings.Contains(got, "UNAVAILABLE") {
		t.Errorf("expected UNAVAILABLE, got: %s", got)
	}
}

func TestParseSSE_Workflow(t *testing.T) {
	// Simulate Dify Workflow SSE stream
	input := `
data: {"event":"workflow_started","task_id":"t1","workflow_run_id":"wr-1","data":{"id":"n1"}}
data: {"event":"node_started","task_id":"t1","workflow_run_id":"wr-1","data":{"id":"n2","title":"LLM"}}
data: {"event":"text_chunk","task_id":"t1","workflow_run_id":"wr-1","data":{"text":"Hello"}}
data: {"event":"text_chunk","task_id":"t1","workflow_run_id":"wr-1","data":{"text":" world"}}
data: {"event":"node_finished","task_id":"t1","workflow_run_id":"wr-1","data":{"id":"n2","outputs":{"text":"Hello world"}}}
data: {"event":"workflow_finished","task_id":"t1","workflow_run_id":"wr-1","data":{"id":"wr-1","status":"succeeded","outputs":{"text":"Hello world"}}}
`

	eventCh := make(chan StreamEvent, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)
		parseSSE(strings.NewReader(input), eventCh, errCh, 0)
	}()

	var events []StreamEvent
	for evt := range eventCh {
		events = append(events, evt)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
	}

	if len(events) != 6 {
		t.Fatalf("got %d events, want 6", len(events))
	}

	// Check text_chunk content
	var fullText string
	for _, e := range events {
		if e.Event == "text_chunk" {
			fullText += e.Data.Text
		}
	}
	if fullText != "Hello world" {
		t.Errorf("full text = %q, want %q", fullText, "Hello world!")
	}
}

func TestParseSSE_EmptyLines(t *testing.T) {
	input := `
data: {"event":"workflow_started","task_id":"t1","workflow_run_id":"wr-1","data":{}}

data: {"event":"workflow_finished","task_id":"t1","workflow_run_id":"wr-1","data":{"status":"succeeded"}}
`

	eventCh := make(chan StreamEvent, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)
		parseSSE(strings.NewReader(input), eventCh, errCh, 0)
	}()

	var events []StreamEvent
	for evt := range eventCh {
		events = append(events, evt)
	}

	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestParseSSE_ChannelClose(t *testing.T) {
	input := `data: {"event":"workflow_started","task_id":"t1","workflow_run_id":"wr-1","data":{}}`

	eventCh := make(chan StreamEvent, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)
		parseSSE(strings.NewReader(input), eventCh, errCh, 0)
	}()

	for range eventCh {
	}

	select {
	case _, ok := <-eventCh:
		if ok {
			t.Error("eventCh should be closed")
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for eventCh to close")
	}
}

// blockingServer returns a test server that responds to POST /v1/workflows/run
// with a blocking-mode JSON payload.
func blockingServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workflows/run" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestBlockingWorkflow_TextOutput(t *testing.T) {
	body := `{"data":{"outputs":{"text":"hello world"},"status":"succeeded"}}`
	srv := blockingServer(t, body, http.StatusOK)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	result, err := c.BlockingWorkflow(&WorkflowRequest{
		Inputs:       map[string]interface{}{"prompt": "hi"},
		ResponseMode: "blocking",
		User:         "u1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestBlockingWorkflow_NoTextKey_Deterministic(t *testing.T) {
	// Outputs with no "text" key: should pick the lexicographically first key.
	body := `{"data":{"outputs":{"z_result":"last","a_result":"first","m_result":"middle"},"status":"succeeded"}}`
	srv := blockingServer(t, body, http.StatusOK)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	req := &WorkflowRequest{
		Inputs:       map[string]interface{}{"prompt": "hi"},
		ResponseMode: "blocking",
		User:         "u1",
	}

	// Run twice — must be deterministic ("a_result" < "m_result" < "z_result").
	r1, err := c.BlockingWorkflow(req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	r2, err := c.BlockingWorkflow(req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if r1 != r2 {
		t.Errorf("non-deterministic: first=%q second=%q", r1, r2)
	}
	if r1 != "first" {
		t.Errorf("result = %q, want %q (lexicographically first key)", r1, "first")
	}
}

func TestBlockingWorkflow_EmptyOutputs(t *testing.T) {
	body := `{"data":{"outputs":{},"status":"succeeded"}}`
	srv := blockingServer(t, body, http.StatusOK)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	_, err := c.BlockingWorkflow(&WorkflowRequest{
		Inputs:       map[string]interface{}{"prompt": "hi"},
		ResponseMode: "blocking",
		User:         "u1",
	})
	if err == nil {
		t.Fatal("expected error for empty outputs")
	}
	if !strings.Contains(err.Error(), "no output text") {
		t.Errorf("error = %v, want 'no output text'", err)
	}
}

func TestBlockingWorkflow_FailedStatus(t *testing.T) {
	body := `{"data":{"outputs":{},"status":"failed","error":"something broke"}}`
	srv := blockingServer(t, body, http.StatusOK)
	defer srv.Close()

	c := NewClient(srv.URL, "app-key", 15*time.Second)
	_, err := c.BlockingWorkflow(&WorkflowRequest{
		Inputs:       map[string]interface{}{"prompt": "hi"},
		ResponseMode: "blocking",
		User:         "u1",
	})
	if err == nil {
		t.Fatal("expected error for failed status")
	}
}

func TestIsTimeoutError_Cloudflare524(t *testing.T) {
	err := &DifyError{Status: 524, Code: "timeout", Message: "cloudflare timeout"}
	if !IsTimeoutError(err) {
		t.Error("IsTimeoutError(524) = false, want true")
	}
}

func TestIsTimeoutError_HTTP500(t *testing.T) {
	err := &DifyError{Status: 500, Code: "internal", Message: "server error"}
	if IsTimeoutError(err) {
		t.Error("IsTimeoutError(500) = true, want false")
	}
}

func TestIsTimeoutError_UnexpectedEOF(t *testing.T) {
	err := fmt.Errorf("decode response: unexpected EOF")
	if !IsTimeoutError(err) {
		t.Error("IsTimeoutError(unexpected EOF) = false, want true")
	}
}

func TestIsTimeoutError_ConnectionReset(t *testing.T) {
	err := fmt.Errorf("http request: read tcp 10.0.0.1:443: connection reset by peer")
	if !IsTimeoutError(err) {
		t.Error("IsTimeoutError(connection reset) = false, want true")
	}
}

func TestIsTimeoutError_GenericError(t *testing.T) {
	err := fmt.Errorf("something went wrong")
	if IsTimeoutError(err) {
		t.Error("IsTimeoutError(generic) = true, want false")
	}
}

func TestIsTimeoutError_Nil(t *testing.T) {
	if IsTimeoutError(nil) {
		t.Error("IsTimeoutError(nil) = true, want false")
	}
}
