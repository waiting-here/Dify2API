package dify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client calls the Dify Workflow API.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	// SSEBufferSize is the initial per-stream SSE parse buffer in bytes
	// (0 falls back to 1MiB; values are clamped to the SSE line limit).
	SSEBufferSize int
	// MaxResponseBytes caps decompressed JSON responses and cumulative SSE
	// input. Zero falls back to DefaultMaxResponseBytes.
	MaxResponseBytes int64
	initErr          error
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

const (
	DefaultMaxResponseBytes int64 = 32 << 20
	maxErrorResponseBytes   int64 = 64 << 10
	maxSSELineBytes               = 10 << 20
)

// ClientOptions controls transport security and response memory bounds.
type ClientOptions struct {
	Timeout          time.Duration
	EgressPolicy     *EgressPolicy
	MaxResponseBytes int64
	SSEBufferSize    int
}

// NewClient creates a client with the secure default egress policy (public
// addresses only). Callers that support operator-approved private Dify
// origins should use NewClientWithOptions with a shared EgressPolicy.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return NewClientWithOptions(baseURL, apiKey, ClientOptions{Timeout: timeout})
}

func NewClientWithOptions(baseURL, apiKey string, opts ClientOptions) *Client {
	policy := opts.EgressPolicy
	if policy == nil {
		policy, _ = NewEgressPolicy(nil)
	}
	base, err := policy.ValidateBaseURL(baseURL)
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = DefaultMaxResponseBytes
	}
	client := &Client{
		BaseURL:          base,
		APIKey:           apiKey,
		MaxResponseBytes: opts.MaxResponseBytes,
		SSEBufferSize:    opts.SSEBufferSize,
		initErr:          err,
	}
	if err != nil {
		client.BaseURL = strings.TrimSpace(baseURL)
		client.HTTPClient = &http.Client{Timeout: opts.Timeout}
		return client
	}
	client.HTTPClient = policy.newHTTPClient(base, opts.Timeout)
	return client
}

func (c *Client) ready() error {
	if c.initErr != nil {
		return fmt.Errorf("invalid Dify base URL: %w", c.initErr)
	}
	return nil
}

func (c *Client) responseLimit() int64 {
	if c.MaxResponseBytes <= 0 {
		return DefaultMaxResponseBytes
	}
	return c.MaxResponseBytes
}

type responseTooLargeError struct{ limit int64 }

func (e *responseTooLargeError) Error() string {
	return fmt.Sprintf("Dify response exceeds the %d-byte limit", e.limit)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &responseTooLargeError{limit: limit}
	}
	return data, nil
}

func sendStreamError(ctx context.Context, ch chan<- error, err error) {
	select {
	case ch <- err:
	case <-ctx.Done():
	}
}

// UploadFile uploads using a background context. Production request paths
// should call UploadFileContext so client cancellation propagates upstream.
func (c *Client) UploadFile(user, fileName, mimeType string, data []byte) (string, error) {
	return c.UploadFileContext(context.Background(), user, fileName, mimeType, data)
}

// UploadFileContext uploads one file to the Dify App and returns its id.
func (c *Client) UploadFileContext(ctx context.Context, user, fileName, mimeType string, data []byte) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/files/upload", &buf)
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
		bodyBytes, readErr := readLimited(resp.Body, maxErrorResponseBytes)
		if readErr != nil {
			return "", readErr
		}
		return "", parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}
	bodyBytes, err := readLimited(resp.Body, c.responseLimit())
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("upload response missing file id")
	}
	return result.ID, nil
}

// StreamWorkflow calls with a background context. Production request paths
// should use StreamWorkflowContext.
func (c *Client) StreamWorkflow(req *WorkflowRequest) (<-chan StreamEvent, <-chan error) {
	return c.StreamWorkflowContext(context.Background(), req)
}

// StreamWorkflowContext calls Dify Workflow API in streaming mode.
func (c *Client) StreamWorkflowContext(ctx context.Context, req *WorkflowRequest) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 4)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)
		if err := c.ready(); err != nil {
			sendStreamError(ctx, errCh, err)
			return
		}

		body, err := json.Marshal(req)
		if err != nil {
			sendStreamError(ctx, errCh, fmt.Errorf("marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/workflows/run", bytes.NewReader(body))
		if err != nil {
			sendStreamError(ctx, errCh, fmt.Errorf("create request: %w", err))
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			sendStreamError(ctx, errCh, fmt.Errorf("http request: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, readErr := readLimited(resp.Body, maxErrorResponseBytes)
			if readErr != nil {
				sendStreamError(ctx, errCh, readErr)
				return
			}
			sendStreamError(ctx, errCh, parseDifyErrorBody(resp.StatusCode, bodyBytes))
			return
		}

		parseSSEContext(ctx, resp.Body, eventCh, errCh, c.SSEBufferSize, c.responseLimit())
	}()

	return eventCh, errCh
}

// BlockingWorkflow calls with a background context. Production request paths
// should use BlockingWorkflowContext.
func (c *Client) BlockingWorkflow(req *WorkflowRequest) (string, error) {
	return c.BlockingWorkflowContext(context.Background(), req)
}

func (c *Client) BlockingWorkflowContext(ctx context.Context, req *WorkflowRequest) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/workflows/run", bytes.NewReader(body))
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
		bodyBytes, readErr := readLimited(resp.Body, maxErrorResponseBytes)
		if readErr != nil {
			return "", readErr
		}
		return "", parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}

	bodyBytes, err := readLimited(resp.Body, c.responseLimit())
	if err != nil {
		return "", err
	}
	// Blocking workflow response
	var result struct {
		Data struct {
			Outputs map[string]interface{} `json:"outputs"`
			Status  string                 `json:"status"`
			Error   string                 `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if result.Data.Status == "failed" {
		return "", &DifyError{Code: result.Data.Error, Message: result.Data.Error, Status: resp.StatusCode}
	}

	// Extract text from outputs — Dify end node maps the LLM's text output
	if text, ok := result.Data.Outputs["text"]; ok {
		return fmt.Sprintf("%v", text), nil
	}

	// Fallback: sort keys lexicographically for deterministic behaviour.
	if len(result.Data.Outputs) > 0 {
		keys := make([]string, 0, len(result.Data.Outputs))
		for k := range result.Data.Outputs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		firstKey := keys[0]
		log.Printf("[WARN] blocking workflow outputs has no \"text\" key; using %q", firstKey)
		return fmt.Sprintf("%v", result.Data.Outputs[firstKey]), nil
	}

	return "", fmt.Errorf("no output text in workflow response")
}

// FetchParameters calls the Dify App's GET /parameters endpoint and returns
// the App's input variables as a map of variable name -> required-by-App.
// Used to validate an App against a service contract when users bind Apps.
func (c *Client) FetchParameters() (map[string]bool, error) {
	return c.FetchParametersContext(context.Background())
}

func (c *Client) FetchParametersContext(parent context.Context) (map[string]bool, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	// Independent short timeout: App probes must not inherit the potentially
	// very long workflow timeout, but caller cancellation still propagates.
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
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
		bodyBytes, readErr := readLimited(resp.Body, maxErrorResponseBytes)
		if readErr != nil {
			return nil, readErr
		}
		return nil, parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}
	bodyBytes, err := readLimited(resp.Body, c.responseLimit())
	if err != nil {
		return nil, err
	}

	// user_input_form is a list of single-key objects:
	//   {"paragraph": {"label": "...", "variable": "system_prompt", ...}}
	var result struct {
		UserInputForm []map[string]json.RawMessage `json:"user_input_form"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
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

// parseSSE is retained for focused parser tests. Production streams use the
// context-aware implementation below.
func parseSSE(r io.Reader, eventCh chan<- StreamEvent, errCh chan<- error, initialBuf int) {
	parseSSEContext(context.Background(), r, eventCh, errCh, initialBuf, DefaultMaxResponseBytes)
}

func parseSSEContext(ctx context.Context, r io.Reader, eventCh chan<- StreamEvent, errCh chan<- error, initialBuf int, maxBytes int64) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	lineLimit := int64(maxSSELineBytes)
	if maxBytes < lineLimit {
		lineLimit = maxBytes
	}
	if lineLimit < 64<<10 {
		lineLimit = 64 << 10
	}
	if initialBuf <= 0 {
		initialBuf = 1 << 20
	}
	if int64(initialBuf) > lineLimit {
		initialBuf = int(lineLimit)
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, initialBuf), int(lineLimit))
	var total int64
	for scanner.Scan() {
		line := scanner.Text()
		total += int64(len(line)) + 1
		if total > maxBytes {
			sendStreamError(ctx, errCh, &responseTooLargeError{limit: maxBytes})
			return
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}

		var evt StreamEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			sendStreamError(ctx, errCh, fmt.Errorf("invalid Dify SSE JSON: %w", err))
			return
		}
		select {
		case eventCh <- evt:
		case <-ctx.Done():
			return
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		sendStreamError(ctx, errCh, fmt.Errorf("sse scan error: %w", err))
	}
}

// StopWorkflow sends a stop request for a streaming workflow task.
// This is a best-effort call (failures are returned but should not block the
// caller's exit path).
func (c *Client) StopWorkflow(taskID, user string) error {
	return c.StopWorkflowContext(context.Background(), taskID, user)
}

func (c *Client) StopWorkflowContext(ctx context.Context, taskID, user string) error {
	if err := c.ready(); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"user": user})
	if err != nil {
		return fmt.Errorf("marshal stop request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/v1/workflows/tasks/"+taskID+"/stop", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create stop request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, readErr := readLimited(resp.Body, maxErrorResponseBytes)
		if readErr != nil {
			return readErr
		}
		return parseDifyErrorBody(resp.StatusCode, bodyBytes)
	}

	bodyBytes, err := readLimited(resp.Body, c.responseLimit())
	if err != nil {
		return err
	}
	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return fmt.Errorf("decode stop response: %w", err)
	}
	if result.Result != "success" {
		return fmt.Errorf("unexpected stop result: %q", result.Result)
	}
	return nil
}

// IsTimeoutError reports whether err indicates that the upstream Dify server
// likely received and processed the request, but the response was cut short
// by a transport-level truncation (Cloudflare 100-second timeout on blocking
// requests, connection reset, etc.).  In these cases the Dify App has already
// consumed its message quota, so the gateway should account for the usage
// even though no response text was returned.
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	// Cloudflare 524: "A Timeout Occurred" (origin didn't respond in time).
	var de *DifyError
	if errors.As(err, &de) && de.Status == 524 {
		return true
	}
	// Our own HTTP client timeout (context deadline exceeded, etc.).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Connection reset (TCP RST) or truncated response body during
	// JSON decode — typical when Cloudflare kills the connection
	// mid-transfer.
	msg := err.Error()
	if strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "connection reset by peer") {
		return true
	}
	return false
}
