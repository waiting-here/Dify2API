package dify

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// IPResolver is the subset of net.Resolver used by the egress guard.
type IPResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// EgressPolicy controls which otherwise non-public Dify origins may be used.
// Public global-unicast destinations need no allowlist entry. Private Dify
// deployments must be explicitly allowed by exact origin or IP/CIDR.
type EgressPolicy struct {
	allowedOrigins map[string]struct{}
	allowedCIDRs   []netip.Prefix
	resolver       IPResolver
	dialer         *net.Dialer
}

// NewEgressPolicy parses entries as exact http(s) origins or IP/CIDR ranges.
func NewEgressPolicy(entries []string) (*EgressPolicy, error) {
	p := &EgressPolicy{
		allowedOrigins: make(map[string]struct{}),
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
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			p.allowedCIDRs = append(p.allowedCIDRs, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			p.allowedCIDRs = append(p.allowedCIDRs, netip.PrefixFrom(addr, addr.BitLen()))
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
func (p *EgressPolicy) ValidateBaseURL(raw string) (string, error) {
	origin, u, err := normalizeHTTPOrigin(raw)
	if err != nil {
		return "", err
	}
	_, originAllowed := p.allowedOrigins[origin]
	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local") {
		if !originAllowed {
			return "", fmt.Errorf("Dify origin uses a local hostname that is not allowlisted")
		}
	}
	if addr, err := netip.ParseAddr(u.Hostname()); err == nil && p.blocked(addr) && !p.addressAllowed(addr) && !originAllowed {
		return "", fmt.Errorf("Dify origin resolves to a non-public IP that is not allowlisted")
	}
	return origin, nil
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
		return "", nil, fmt.Errorf("Dify base URL must be an origin with optional /v1 suffix")
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

func (p *EgressPolicy) newHTTPClient(origin string, timeout time.Duration) *http.Client {
	_, parsed, _ := normalizeHTTPOrigin(origin)
	_, originAllowed := p.allowedOrigins[origin]
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Environment proxies can resolve/fetch the original host themselves and
	// would bypass our DNS/IP pinning, so Dify egress is always direct.
	transport.Proxy = nil
	transport.DialContext = p.dialContext(parsed.Hostname(), effectivePort(parsed), originAllowed)
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("Dify redirects are disabled; configure the final API origin")
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

func (p *EgressPolicy) dialContext(expectedHost, expectedPort string, originAllowed bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid Dify dial address: %w", err)
		}
		if !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(expectedHost, ".")) || port != expectedPort {
			return nil, fmt.Errorf("unexpected Dify dial target %q", address)
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
			return nil, fmt.Errorf("Dify host %q resolved to no addresses", host)
		}

		// Reject the complete DNS answer when any address is unsafe. Selecting
		// only a public member would leave room for resolver-order rebinding.
		for _, addr := range addresses {
			addr = addr.WithZone("").Unmap()
			if p.blocked(addr) && !originAllowed && !p.addressAllowed(addr) {
				return nil, fmt.Errorf("Dify host %q resolved to blocked address %s", host, addr)
			}
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

func (p *EgressPolicy) addressAllowed(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	for _, prefix := range p.allowedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
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
