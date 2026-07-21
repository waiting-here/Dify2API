package dify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Client calls the Dify Workflow API.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	// SSEBufferSize is the initial per-stream SSE parse buffer in bytes
	// (0 falls back to 1MB; hard max per SSE line stays 50MB).
	SSEBufferSize int
}

// WorkflowRequest is the body sent to Dify's /workflows/run endpoint.
type WorkflowRequest struct {
	Inputs       map[string]interface{} `json:"inputs"`
	ResponseMode string                 `json:"response_mode"`
	User         string                 `json:"user"`
}

// StreamEventData is the payload of a Dify Workflow SSE event.
type StreamEventData struct {
	ID        string                 `json:"id"`
	Text      string                 `json:"text"`      // text_chunk payload
	Reasoning string                 `json:"reasoning"` // reasoning_chunk payload
	IsFinal   bool                   `json:"is_final"`  // last reasoning chunk
	Outputs   map[string]interface{} `json:"outputs"`
	Status    string                 `json:"status"`
	Error     string                 `json:"error"`
}

// StreamEvent represents a single SSE event from the Dify Workflow API.
type StreamEvent struct {
	Event         string          `json:"event"`
	TaskID        string          `json:"task_id"`
	WorkflowRunID string          `json:"workflow_run_id"`
	Data          StreamEventData `json:"data"`
}

// DifyError is a structured error returned by the Dify API.  It implements
// the error interface so it can be passed through existing error channels,
// while carrying a machine-readable code for the gateway to forward.
type DifyError struct {
	Code    string // Dify error code, e.g. "invalid_param"
	Message string // Dify error message
	Status  int    // HTTP status from Dify
}

func (e *DifyError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("[%s] %s", e.Code, e.Message)
	}
	return e.Message
}

// parseDifyErrorBody attempts to extract a structured error from a Dify
// JSON response body.  Falls back to using the raw body as the message.
func parseDifyErrorBody(statusCode int, bodyBytes []byte) *DifyError {
	de := &DifyError{Status: statusCode}
	var raw struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(bodyBytes, &raw) == nil {
		de.Code = raw.Code
		de.Message = raw.Message
	}
	if de.Message == "" {
		de.Message = string(bodyBytes)
	}
	if de.Message == "" {
		de.Message = fmt.Sprintf("HTTP %d", statusCode)
	}
	return de
}

// NewClient creates a new Dify API client. timeout applies to each call to
// the Dify API (V0.1.0: raised from a hardcoded 300s to a configurable 600s,
// so long-running workflows for the dify-subagent integration can complete).
//
// baseURL is normalized: trailing slashes and a trailing "/v1" are stripped
// (users habitually paste the documented base "https://api.dify.ai/v1").
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	base := strings.TrimRight(baseURL, "/")
	base = strings.TrimSuffix(base, "/v1")
	return &Client{
		BaseURL: base,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// UploadFile uploads one file to the Dify App (POST /v1/files/upload) and
// returns its upload_file_id for use in workflow file inputs.
func (c *Client) UploadFile(user, fileName, mimeType string, data []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("multipart file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("multipart write: %w", err)
	}
	if err := mw.WriteField("user", user); err != nil {
		return "", fmt.Errorf("multipart user field: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("multipart close: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/v1/files/upload", &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("upload response missing file id")
	}
	return result.ID, nil
}

// StreamWorkflow calls Dify Workflow API (/workflows/run) in streaming mode.
func (c *Client) StreamWorkflow(req *WorkflowRequest) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		body, err := json.Marshal(req)
		if err != nil {
			errCh <- fmt.Errorf("marshal request: %w", err)
			return
		}

		httpReq, err := http.NewRequest("POST", c.BaseURL+"/v1/workflows/run", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("create request: %w", err)
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			errCh <- fmt.Errorf("http request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			errCh <- parseDifyErrorBody(resp.StatusCode, bodyBytes)
			return
		}

		parseSSE(resp.Body, eventCh, errCh, c.SSEBufferSize)
	}()

	return eventCh, errCh
}

// BlockingWorkflow calls Dify Workflow API in blocking mode.
func (c *Client) BlockingWorkflow(req *WorkflowRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.BaseURL+"/v1/workflows/run", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}

	// Blocking workflow response
	var result struct {
		Data struct {
			Outputs map[string]interface{} `json:"outputs"`
			Status  string                 `json:"status"`
			Error   string                 `json:"error"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if result.Data.Status == "failed" {
		return "", &DifyError{Code: result.Data.Error, Message: result.Data.Error, Status: resp.StatusCode}
	}

	// Extract text from outputs — Dify end node maps the LLM's text output
	if text, ok := result.Data.Outputs["text"]; ok {
		return fmt.Sprintf("%v", text), nil
	}

	// Fallback: try to find any text in outputs
	for _, v := range result.Data.Outputs {
		return fmt.Sprintf("%v", v), nil
	}

	return "", fmt.Errorf("no output text in workflow response")
}

// FetchParameters calls the Dify App's GET /parameters endpoint and returns
// the App's input variables as a map of variable name -> required-by-App.
// Used to validate an App against a service contract when users bind Apps.
func (c *Client) FetchParameters() (map[string]bool, error) {
	// Independent short timeout: the startup check must not hang on the
	// (potentially very long) workflow timeout of the main HTTP client.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/v1/parameters", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}

	// user_input_form is a list of single-key objects:
	//   {"paragraph": {"label": "...", "variable": "system_prompt", ...}}
	var result struct {
		UserInputForm []map[string]json.RawMessage `json:"user_input_form"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	variables := make(map[string]bool)
	for _, item := range result.UserInputForm {
		for _, raw := range item {
			var field struct {
				Variable string `json:"variable"`
				Required bool   `json:"required"`
			}
			if err := json.Unmarshal(raw, &field); err == nil && field.Variable != "" {
				variables[field.Variable] = field.Required
			}
		}
	}
	return variables, nil
}

// CheckApp verifies the Dify App is available and its input variables match
// the expected slot list. It returns a human-readable status line and never
// fails hard — it is a startup self-check, not a gate.
func CheckApp(c *Client, expected []string) string {
	variables, err := c.FetchParameters()
	if err != nil {
		return fmt.Sprintf("[app-check] Dify App UNAVAILABLE: %v", err)
	}

	var missing, unexpected []string
	for _, name := range expected {
		if _, ok := variables[name]; !ok {
			missing = append(missing, name)
		}
	}
	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	for name := range variables {
		if !expectedSet[name] {
			unexpected = append(unexpected, name)
		}
	}

	if len(missing) == 0 && len(unexpected) == 0 {
		return fmt.Sprintf("[app-check] OK: Dify App available, parameter list matches (%d inputs)", len(expected))
	}
	return fmt.Sprintf("[app-check] MISMATCH: missing expected inputs %v; unexpected inputs %v", missing, unexpected)
}

// parseSSE reads Server-Sent Events from the response body and emits StreamEvents.
// initialBuf is the initial scanner buffer size in bytes (<=0 means 1MB).
func parseSSE(r io.Reader, eventCh chan<- StreamEvent, errCh chan<- error, initialBuf int) {
	if initialBuf <= 0 {
		initialBuf = 1 * 1024 * 1024
	}
	scanner := bufio.NewScanner(r)
	// Small initial buffer (grows on demand), 50MB max per SSE line.
	scanner.Buffer(make([]byte, 0, initialBuf), 50*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")

		var evt StreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		eventCh <- evt
	}

	if err := scanner.Err(); err != nil {
		errCh <- fmt.Errorf("sse scan error: %w", err)
	}
}
