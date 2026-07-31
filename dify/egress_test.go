package dify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type staticResolver struct {
	addresses []netip.Addr
	err       error
}

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}

func TestEgressPolicy_BaseURLValidation(t *testing.T) {
	policy, err := NewEgressPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"file:///etc/passwd",
		"http://user:pass@example.com",
		"https://example.com/api",
		"https://example.com?next=x",
		"http://127.0.0.1:8080",
		"http://169.254.169.254/latest/meta-data",
	} {
		if _, err := policy.ValidateBaseURL(raw); err == nil {
			t.Errorf("ValidateBaseURL(%q) should reject", raw)
		}
	}

	allowed, err := NewEgressPolicy([]string{"127.0.0.0/8", "http://[::1]:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := allowed.ValidateBaseURL("http://127.0.0.1:9000/v1/"); err != nil || got != "http://127.0.0.1:9000" {
		t.Fatalf("CIDR allowlist normalization = %q, %v", got, err)
	}
	if _, err := allowed.ValidateBaseURL("http://[::1]:8080"); err != nil {
		t.Fatalf("exact origin allowlist should allow loopback: %v", err)
	}
}

func TestEgressPolicy_RejectsMixedDNSAnswer(t *testing.T) {
	policy, _ := NewEgressPolicy(nil)
	policy.resolver = staticResolver{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("127.0.0.1"),
	}}
	dial := policy.dialContext("dify.example", "443", false)
	_, err := dial(context.Background(), "tcp", "dify.example:443")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("mixed DNS answer error = %v", err)
	}
}

func TestEgressPolicy_DisablesRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		fmt.Fprint(w, `{"user_input_form":[]}`)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	defer redirect.Close()

	client := newLoopbackClient(t, redirect.URL, "app-key", 5*time.Second)
	_, err := client.FetchParametersContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "redirects are disabled") {
		t.Fatalf("redirect error = %v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target was contacted %d times", targetHits.Load())
	}
}

func TestClient_ResponseLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":{"outputs":{"text":%q},"status":"succeeded"}}`, strings.Repeat("x", 1024))
	}))
	defer srv.Close()
	policy, _ := NewEgressPolicy([]string{"127.0.0.0/8"})
	client := NewClientWithOptions(srv.URL, "app-key", ClientOptions{
		Timeout:          5 * time.Second,
		EgressPolicy:     policy,
		MaxResponseBytes: 128,
	})
	_, err := client.BlockingWorkflowContext(context.Background(), &WorkflowRequest{})
	var tooLarge *responseTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("response limit error = %T %v", err, err)
	}
}

func TestClient_BlockingCancellationPropagates(t *testing.T) {
	releaseServer := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-releaseServer:
		}
	}))
	defer srv.Close()
	client := newLoopbackClient(t, srv.URL, "app-key", 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.BlockingWorkflowContext(ctx, &WorkflowRequest{})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocking workflow did not cancel promptly")
	}
	close(releaseServer)
}

func TestClient_StreamCancellationClosesChannels(t *testing.T) {
	started := make(chan struct{})
	releaseServer := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-releaseServer:
		}
	}))
	defer srv.Close()
	client := newLoopbackClient(t, srv.URL, "app-key", 30*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	events, errs := client.StreamWorkflowContext(ctx, &WorkflowRequest{})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream request did not start")
	}
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel did not close after cancellation")
	}
	select {
	case <-errs:
	case <-time.After(time.Second):
		t.Fatal("error channel did not close after cancellation")
	}
	close(releaseServer)
}

func TestParseSSE_CumulativeLimit(t *testing.T) {
	events := make(chan StreamEvent, 1)
	errs := make(chan error, 1)
	parseSSEContext(context.Background(), strings.NewReader(":"+strings.Repeat("x", 256)+"\n"), events, errs, 64, 128)
	var tooLarge *responseTooLargeError
	if err := <-errs; !errors.As(err, &tooLarge) {
		t.Fatalf("SSE cumulative limit error = %T %v", err, err)
	}
}

func TestParseSSE_BackpressureCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan StreamEvent) // deliberately never consumed
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		parseSSEContext(ctx, strings.NewReader(`data: {"event":"text_chunk","data":{"text":"x"}}`), events, errs, 1024, 1024)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE parser remained blocked on event channel after cancellation")
	}
}
