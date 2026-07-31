package dify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

	allowed, err := NewEgressPolicy([]string{"http://127.0.0.1:9000", "http://[::1]:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := allowed.ValidateBaseURL("http://127.0.0.1:9000/v1/"); err != nil || got != "http://127.0.0.1:9000" {
		t.Fatalf("exact-origin allowlist normalization = %q, %v", got, err)
	}
	if _, err := allowed.ValidateBaseURL("http://[::1]:8080"); err != nil {
		t.Fatalf("exact origin allowlist should allow loopback: %v", err)
	}
	if _, err := allowed.ValidateBaseURL("http://127.0.0.1:8081"); err == nil {
		t.Fatalf("loopback on a non-allowlisted port should be rejected")
	}
}

func TestEgressPolicy_ExactOriginOnly(t *testing.T) {
	for _, entry := range []string{
		"10.0.0.0/8",
		"2001:db8::/32",
		"0.0.0.0/0",
		"10.1.2.3",
		"::1",
		"127.0.0.1",
	} {
		if _, err := NewEgressPolicy([]string{entry}); err == nil {
			t.Errorf("NewEgressPolicy(%q) should reject non-origin entry", entry)
		}
	}
	// Exact origins (including loopback and IPv6) still parse.
	for _, entry := range []string{"http://127.0.0.1:9000", "http://[::1]:8080", "https://dify.internal:5001"} {
		if _, err := NewEgressPolicy([]string{entry}); err != nil {
			t.Errorf("NewEgressPolicy(%q) should accept: %v", entry, err)
		}
	}
}

func TestEgressPolicy_RejectsMixedDNSAnswer(t *testing.T) {
	policy, _ := NewEgressPolicy(nil)
	policy.resolver = staticResolver{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("127.0.0.1"),
	}}
	dial := policy.dialContext("https", "dify.example", "443", 0)
	_, err := dial(context.Background(), "tcp", "dify.example:443")
	if err == nil || !strings.Contains(err.Error(), "blocked address") {
		t.Fatalf("mixed DNS answer error = %v", err)
	}
}

// TestEgressPolicy_AllowlistedOriginMayDialLoopback verifies that the exact
// origin exception still exists at dial time: an allowlisted non-public
// origin must actually be reachable (private Dify is a supported scenario).
func TestEgressPolicy_AllowlistedOriginMayDialLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"user_input_form":[]}`)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	policy, err := NewEgressPolicy([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	dial := policy.dialContext("http", "127.0.0.1", u.Port(), 0)
	conn, err := dial(context.Background(), "tcp", u.Host)
	if err != nil {
		t.Fatalf("allowlisted loopback origin should dial: %v", err)
	}
	conn.Close()
}

// ---- Part B: self-origin SSRF guard ----

func TestEgressPolicy_SelfOriginLiteralIPBlocked(t *testing.T) {
	policy, err := NewEgressPolicy([]string{"http://127.0.0.1:9000"})
	if err != nil {
		t.Fatal(err)
	}
	policy.AddSelfOrigins("http://127.0.0.1:9000", "", "127.0.0.1:9000")
	dial := policy.dialContext("http", "127.0.0.1", "9000", 42)
	start := time.Now()
	_, err = dial(context.Background(), "tcp", "127.0.0.1:9000")
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("self-origin dial error = %v", err)
	}
	// Jitter is 150–350 ms; tolerate a little scheduling noise on CI.
	if elapsed < 140*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Errorf("self-origin dial jitter = %v, want 150–350ms", elapsed)
	}
}

func TestEgressPolicy_SelfOriginNonGatewayPortDialed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	policy, err := NewEgressPolicy([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	// Gateway site is 127.0.0.1:1; the mock Dify listens on 127.0.0.1:<other>.
	policy.AddSelfOrigins("http://127.0.0.1:1", "", "127.0.0.1:1")
	dial := policy.dialContext("http", "127.0.0.1", u.Port(), 7)
	conn, err := dial(context.Background(), "tcp", u.Host)
	if err != nil {
		t.Fatalf("same-host non-gateway port must dial normally: %v", err)
	}
	conn.Close()
}

func TestEgressPolicy_SelfOriginHostnameBlocked(t *testing.T) {
	policy, _ := NewEgressPolicy(nil)
	// Site hostname fronted by a CDN: DNS does NOT resolve to a self IP.
	policy.resolver = staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	policy.AddSelfOrigins("https://gw.example.com", "admin.example.com", ":10086")
	dial := policy.dialContext("https", "gw.example.com", "443", 1)
	start := time.Now()
	_, err := dial(context.Background(), "tcp", "gw.example.com:443")
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("self hostname dial error = %v", err)
	}
	// Jitter is 150–350 ms; tolerate a little scheduling noise on CI.
	if elapsed < 140*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Errorf("self hostname jitter = %v, want 150–350ms", elapsed)
	}

	// The same hostname on the gateway's own port is refused by the IP+port
	// rule (DNS resolves to a self IP): same fake error, same delay.
	dial86 := policy.dialContext("https", "gw.example.com", "10086", 1)
	start = time.Now()
	_, err = dial86(context.Background(), "tcp", "gw.example.com:10086")
	elapsed = time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("self hostname on self port error = %v", err)
	}
	if elapsed < 140*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Errorf("self hostname on self port jitter = %v, want 150–350ms", elapsed)
	}
}

func TestEgressPolicy_SelfOriginUnrelatedIPNotBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	policy, err := NewEgressPolicy([]string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	// The gateway is "on" 127.0.0.2; the unrelated mock listens on 127.0.0.1.
	policy.AddSelfOrigins("http://127.0.0.2:1", "", "127.0.0.2:1")
	dial := policy.dialContext("http", "127.0.0.1", u.Port(), 0)
	conn, err := dial(context.Background(), "tcp", u.Host)
	if err != nil {
		t.Fatalf("unrelated IP must dial normally: %v", err)
	}
	conn.Close()
}

func TestEgressPolicy_SelfOriginAllowlistCannotOverride(t *testing.T) {
	// Even when the operator explicitly allowlists the gateway's own origin,
	// the dial-time self guard must still refuse it.
	policy, err := NewEgressPolicy([]string{"http://127.0.0.1:9000"})
	if err != nil {
		t.Fatal(err)
	}
	policy.AddSelfOrigins("http://127.0.0.1:9000", "", "127.0.0.1:9000")
	dial := policy.dialContext("http", "127.0.0.1", "9000", 5)
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:9000"); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("allowlisted self origin must still be refused, got %v", err)
	}
}

func TestEgressPolicy_SelfLoopbackAllowedListedWithNonLiteralListenAddr(t *testing.T) {
	// Regression: with the common default LISTEN_ADDR=localhost:port (a
	// non-literal host), an allowlisted origin pointing at the gateway's own
	// loopback listener must still be refused by the self guard. Before the
	// fix the loopback addresses were only registered when LISTEN_ADDR was a
	// literal IP or SITE/ADMIN hosts were loopback, so this dial succeeded.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	allowlisted := fmt.Sprintf("http://127.0.0.1:%d", port)
	policy, err := NewEgressPolicy([]string{allowlisted})
	if err != nil {
		t.Fatal(err)
	}
	// SITE/ADMIN are public hostnames; LISTEN_ADDR uses the non-literal
	// "localhost" host — the exact configuration that used to leave the gap.
	policy.AddSelfOrigins("https://gw.example.com", "admin.example.com", "localhost:"+strconv.Itoa(port))

	// The listener really accepts connections, so a refusal can only come
	// from the policy guard, not from the network.
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err != nil {
		t.Fatalf("control dial to listener failed: %v", err)
	} else {
		conn.Close()
	}

	dial := policy.dialContext("http", "127.0.0.1", strconv.Itoa(port), 7)
	if _, err := dial(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("allowlisted self loopback origin must be refused, got %v", err)
	}
}

func TestEgressPolicy_SaveStageDoesNotRevealSelf(t *testing.T) {
	policy, _ := NewEgressPolicy(nil)
	policy.resolver = staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	policy.AddSelfOrigins("https://gw.example.com", "admin.example.com", ":10086")
	// Save stage must accept self origins exactly like any other public
	// target — no error-text side channel.
	for _, raw := range []string{"https://gw.example.com", "https://93.184.216.34"} {
		if _, err := policy.ValidateBaseURL(raw); err != nil {
			t.Errorf("ValidateBaseURL(%q) must pass at save time: %v", raw, err)
		}
	}
}

func TestEgressPolicy_SelfIPsIncludeV4AndV6(t *testing.T) {
	policy, _ := NewEgressPolicy(nil)
	policy.resolver = staticResolver{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946"),
	}}
	policy.AddSelfOrigins("https://gw.example.com", "", ":10086")
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	for _, want := range []string{"93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		if _, ok := policy.selfIPs[netip.MustParseAddr(want)]; !ok {
			t.Errorf("selfIPs missing %s", want)
		}
	}
	if len(policy.selfPorts) != 1 {
		t.Errorf("selfPorts = %v, want [10086]", policy.selfPorts)
	}
	if _, ok := policy.selfOrigins["https://gw.example.com:443"]; !ok {
		t.Errorf("selfOrigins missing https://gw.example.com:443: %v", policy.selfOrigins)
	}
}

func TestEgressPolicy_PortSuffixedAdminHostRegistered(t *testing.T) {
	// Regression: url.Parse("admin.example.com:8443") treats the hostname as
	// a scheme and yields an empty Hostname; the self guard must still
	// register the admin site (hostname + port) so dials to it are refused.
	policy, err := NewEgressPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	policy.resolver = staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	policy.AddSelfOrigins("https://gw.example.com", "admin.example.com:8443", ":10086")

	policy.mu.RLock()
	_, hasHTTP := policy.selfOrigins["http://admin.example.com:8443"]
	_, hasHTTPS := policy.selfOrigins["https://admin.example.com:8443"]
	policy.mu.RUnlock()
	if !hasHTTP || !hasHTTPS {
		t.Fatalf("port-suffixed admin host not registered: http=%v https=%v", hasHTTP, hasHTTPS)
	}

	dial := policy.dialContext("https", "admin.example.com", "8443", 9)
	start := time.Now()
	_, err = dial(context.Background(), "tcp", "admin.example.com:8443")
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("port-suffixed admin host dial error = %v", err)
	}
	if elapsed < 140*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Errorf("admin host dial jitter = %v, want 150–350ms", elapsed)
	}
}

func TestEgressPolicy_SelfOriginConcurrentDials(t *testing.T) {
	policy, err := NewEgressPolicy([]string{"http://127.0.0.1:9000"})
	if err != nil {
		t.Fatal(err)
	}
	policy.AddSelfOrigins("http://127.0.0.1:9000", "", "127.0.0.1:9000")
	dial := policy.dialContext("http", "127.0.0.1", "9000", 3)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dial(context.Background(), "tcp", "127.0.0.1:9000")
			if err == nil || !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("concurrent self dial error = %v", err)
			}
		}()
	}
	wg.Wait()
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
	policy, _ := NewEgressPolicy([]string{srv.URL})
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
