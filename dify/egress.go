package dify

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// IPResolver is the subset of net.Resolver used by the egress guard.
type IPResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// EgressPolicy controls which otherwise non-public Dify origins may be used.
// Public global-unicast destinations need no allowlist entry. Private Dify
// deployments must be explicitly allowed by an exact http(s) origin; CIDR
// and bare-IP entries are rejected. Dials that would land on the gateway's
// own sites (self-origin SSRF) are refused with a fake connection-refused
// error that is indistinguishable from dialing an unrelated dead public host.
type EgressPolicy struct {
	allowedOrigins map[string]struct{}
	// selfIPs holds addresses the gateway itself listens on (resolved from
	// SITE_BASE_URL and ADMIN_HOST, plus a literal LISTEN_ADDR IP).
	selfIPs map[netip.Addr]struct{}
	// selfOrigins holds the normalized http(s) origins of the gateway's own
	// sites (scheme://host:port with default ports materialized), used to
	// refuse hostname-based dials even when DNS fronts a CDN or proxy.
	selfOrigins map[string]struct{}
	// selfPorts holds the ports of the gateway HTTP listener.
	selfPorts map[int]struct{}
	// mu guards the self* maps, populated once at startup via AddSelfOrigins.
	mu       sync.RWMutex
	resolver IPResolver
	dialer   *net.Dialer
}

// NewEgressPolicy parses entries as exact http(s) origins. CIDR ranges and
// bare IPs are rejected (config.LoadStartup enforces the same rule).
func NewEgressPolicy(entries []string) (*EgressPolicy, error) {
	p := &EgressPolicy{
		allowedOrigins: make(map[string]struct{}),
		selfIPs:        make(map[netip.Addr]struct{}),
		selfOrigins:    make(map[string]struct{}),
		selfPorts:      make(map[int]struct{}),
		resolver:       net.DefaultResolver,
		dialer: &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		origin, _, err := normalizeHTTPOrigin(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid Dify egress allowlist entry %q: %w", entry, err)
		}
		p.allowedOrigins[origin] = struct{}{}
	}
	return p, nil
}

// ValidateBaseURL validates and normalizes a user-supplied Dify API origin.
// The self-origin check deliberately does NOT happen here: rejecting the
// gateway's own origin at save time would let an attacker discover the VPS
// IP by comparing validation error text, so save behaviour must stay
// identical for self and unrelated public origins.
func (p *EgressPolicy) ValidateBaseURL(raw string) (string, error) {
	origin, u, err := normalizeHTTPOrigin(raw)
	if err != nil {
		return "", err
	}
	_, originAllowed := p.allowedOrigins[origin]
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		if !originAllowed {
			return "", fmt.Errorf("dify origin uses a local hostname that is not allowlisted")
		}
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && p.blocked(addr) && !originAllowed {
		return "", fmt.Errorf("dify origin resolves to a non-public IP that is not allowlisted")
	}
	return origin, nil
}

// AddSelfOrigins registers the gateway's own sites so Dify egress can never
// dial back into the gateway itself. It resolves every address of
// SITE_BASE_URL and ADMIN_HOST (A + AAAA), records the normalized origins of
// both hostnames, and records the ports of the gateway HTTP listener
// (LISTEN_ADDR). Unresolvable hostnames log a warning and remain protected
// by the hostname-based origin rule. Call once at startup before serving.
func (p *EgressPolicy) AddSelfOrigins(siteBaseURL, adminHost, listenAddr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if host, _, err := net.SplitHostPort(listenAddr); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			p.selfIPs[addr.Unmap()] = struct{}{}
		}
	}
	if port, err := listenPort(listenAddr); err == nil {
		if port >= 1 && port <= 65535 {
			p.selfPorts[port] = struct{}{}
		} else {
			log.Printf("[EGRESS] LISTEN_ADDR %q has invalid port %d; self-origin guard disabled for ports", listenAddr, port)
		}
	} else if listenAddr != "" {
		log.Printf("[EGRESS] cannot parse LISTEN_ADDR %q: %v", listenAddr, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.addSelfHost(ctx, siteBaseURL, true)
	p.addSelfHost(ctx, adminHost, false)
}

func (p *EgressPolicy) addSelfHost(ctx context.Context, raw string, isSite bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		// Bare host[:port] (e.g. ADMIN_HOST=admin.example.com:8443) is
		// misparsed by url.Parse as scheme:opaque; retry as a URL so the
		// hostname and port are extracted correctly.
		u, err = url.Parse("http://" + raw)
		if err != nil {
			log.Printf("[EGRESS] cannot parse self host %q: %v", raw, err)
			return
		}
	}
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "" {
		log.Printf("[EGRESS] cannot parse self host %q", raw)
		return
	}

	if isLoopbackHost(hostname) {
		// Loopback-family hosts are already refused by blocked() unless
		// explicitly allowlisted; register the loopback addresses so even
		// an allowlisted self-origin cannot dial the gateway itself.
		p.selfIPs[netip.MustParseAddr("127.0.0.1")] = struct{}{}
		p.selfIPs[netip.MustParseAddr("::1")] = struct{}{}
	} else if addr, err := netip.ParseAddr(hostname); err == nil {
		p.selfIPs[addr.Unmap()] = struct{}{}
	} else {
		addrs, err := p.resolver.LookupNetIP(ctx, "ip", hostname)
		if err != nil {
			log.Printf("[EGRESS] self host %q does not resolve (%v); hostname rule still applies", hostname, err)
		}
		for _, a := range addrs {
			p.selfIPs[a.WithZone("").Unmap()] = struct{}{}
		}
	}

	// Record normalized origins (default ports materialized) so that
	// hostname-based dials are refused even when DNS fronts a CDN or
	// reverse proxy. ADMIN_HOST has no scheme of its own, so both http and
	// https variants are recorded.
	schemes := []string{strings.ToLower(u.Scheme)}
	if !isSite {
		schemes = []string{"http", "https"}
	}
	hostPort := hostname
	if strings.Contains(hostname, ":") {
		hostPort = "[" + hostname + "]"
	}
	for _, scheme := range schemes {
		switch {
		case u.Port() != "":
			p.selfOrigins[scheme+"://"+hostPort+":"+u.Port()] = struct{}{}
		case scheme == "http":
			p.selfOrigins[scheme+"://"+hostPort+":80"] = struct{}{}
		default:
			p.selfOrigins[scheme+"://"+hostPort+":443"] = struct{}{}
		}
	}
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local")
}

func listenPort(listenAddr string) (int, error) {
	_, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

func normalizeHTTPOrigin(raw string) (string, *url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("URL scheme must be http or https")
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", nil, fmt.Errorf("URL must include a hostname")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, fmt.Errorf("URL must not include credentials, query, or fragment")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path != "" && path != "/v1" {
		return "", nil, fmt.Errorf("dify base URL must be an origin with optional /v1 suffix")
	}
	if strings.ContainsAny(u.Hostname(), "\x00\r\n\t /\\@") {
		return "", nil, fmt.Errorf("invalid URL hostname")
	}
	for _, ch := range u.Hostname() {
		if ch > 127 {
			return "", nil, fmt.Errorf("URL hostname must use ASCII/punycode form")
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", nil, fmt.Errorf("invalid URL port")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return "", nil, fmt.Errorf("invalid URL hostname")
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if u.Port() != "" {
		host += ":" + u.Port()
	}
	normalized := &url.URL{Scheme: strings.ToLower(u.Scheme), Host: host}
	return normalized.String(), normalized, nil
}

func (p *EgressPolicy) newHTTPClient(origin string, timeout time.Duration, userID int64) *http.Client {
	_, parsed, _ := normalizeHTTPOrigin(origin)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Environment proxies can resolve/fetch the original host themselves and
	// would bypass our DNS/IP pinning, so Dify egress is always direct.
	transport.Proxy = nil
	transport.DialContext = p.dialContext(strings.ToLower(parsed.Scheme), parsed.Hostname(), effectivePort(parsed), userID)
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("dify redirects are disabled; configure the final API origin")
		},
	}
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// originKey builds the canonical "scheme://host:port" key of a dial target,
// matching the normalized origins stored in allowedOrigins/selfOrigins.
func originKey(scheme, host, port string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.ToLower(scheme) + "://" + host + ":" + port
}

// originAllowedForDial reports whether the pinned dial target matches an
// allowlist entry. Entries may omit the scheme-default port.
func (p *EgressPolicy) originAllowedForDial(scheme, host, port string) bool {
	key := originKey(scheme, host, port)
	if _, ok := p.allowedOrigins[key]; ok {
		return true
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		key = strings.TrimSuffix(key, ":"+port)
		_, ok := p.allowedOrigins[key]
		return ok
	}
	return false
}

func (p *EgressPolicy) isSelfTarget(addr netip.Addr, port int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.selfIPs[addr]; !ok {
		return false
	}
	_, ok := p.selfPorts[port]
	return ok
}

func (p *EgressPolicy) isSelfOrigin(scheme, host, port string) bool {
	key := originKey(scheme, host, port)
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.selfOrigins[key]
	return ok
}

// fakeConnRefused returns an error identical in shape to what net.Dialer
// produces when nothing listens on a remote host:port.
func fakeConnRefused(network string, ip net.IP, port int) *net.OpError {
	return &net.OpError{
		Op:   "dial",
		Net:  network,
		Addr: &net.TCPAddr{IP: ip, Port: port},
		Err:  syscall.ECONNREFUSED,
	}
}

func (p *EgressPolicy) dialContext(scheme, expectedHost, expectedPort string, userID int64) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid dify dial address: %w", err)
		}
		if !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(expectedHost, ".")) || port != expectedPort {
			return nil, fmt.Errorf("unexpected dify dial target %q", address)
		}

		var addresses []netip.Addr
		if literal, err := netip.ParseAddr(host); err == nil {
			addresses = []netip.Addr{literal}
		} else {
			addresses, err = p.resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve Dify host %q: %w", host, err)
			}
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("dify host %q resolved to no addresses", host)
		}

		// Reject the complete DNS answer when any address is unsafe.
		// Selecting only a public member would leave room for
		// resolver-order rebinding. An exact allowlisted origin is the only
		// exception (the operator explicitly trusts that whole target).
		originAllowed := p.originAllowedForDial(scheme, host, port)
		for _, addr := range addresses {
			addr = addr.WithZone("").Unmap()
			if p.blocked(addr) && !originAllowed {
				return nil, fmt.Errorf("dify host %q resolved to blocked address %s", host, addr)
			}
		}

		// Self-origin SSRF guard: a dial that would land on the gateway's
		// own sites is refused with a fake connection-refused after a
		// random delay, so it is indistinguishable from dialing an
		// unrelated public IP with no listener — no error-type, message or
		// timing side channel. The check covers both the resolved
		// addresses (self IP + gateway port) and the dialed hostname
		// itself (site/admin origins fronted by a CDN or reverse proxy).
		// The allowlist cannot override this guard.
		portNum, _ := strconv.Atoi(port)
		selfTarget := false
		for _, addr := range addresses {
			if p.isSelfTarget(addr.WithZone("").Unmap(), portNum) {
				selfTarget = true
				break
			}
		}
		if !selfTarget {
			selfTarget = p.isSelfOrigin(scheme, host, port)
		}
		if selfTarget {
			// 150–350 ms uniform jitter: within the natural range of a
			// refused connection to a distant public host, so probing the
			// gateway's own IP:port cannot be told apart from probing any
			// dead public IP.
			jitter := time.Duration(150+rand.Intn(201)) * time.Millisecond
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			log.Printf("[EGRESS] self-origin dial blocked: user=%d target=%s (returned fake ECONNREFUSED)", userID, address)
			first := addresses[0].WithZone("").Unmap()
			return nil, fakeConnRefused(network, net.IP(first.AsSlice()), portNum)
		}

		var lastErr error
		for _, addr := range addresses {
			target := net.JoinHostPort(addr.WithZone("").Unmap().String(), port)
			conn, err := p.dialer.DialContext(ctx, network, target)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("dial Dify host %q: %w", host, lastErr)
	}
}

var additionallyBlocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func (p *EgressPolicy) blocked(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range additionallyBlocked {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
