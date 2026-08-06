package handler

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"dify2api/config"
)

// loginThrottle tracks failed admin-login attempts per (ip, username) and
// temporarily locks after too many failures. In-memory by design (windows
// are short; process restart simply resets counters).
type loginThrottle struct {
	mu          sync.Mutex
	fails       map[string]*failWindow
	maxFailures int
	window      time.Duration
	lockDur     time.Duration
	minLatency  time.Duration
	stop        chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
}

type failWindow struct {
	times       []time.Time
	lockedUntil time.Time
}

func newLoginThrottle(cfg *config.Config) *loginThrottle {
	t := &loginThrottle{
		fails:       make(map[string]*failWindow),
		maxFailures: cfg.LoginMaxFailures,
		window:      time.Duration(cfg.LoginWindowMin) * time.Minute,
		lockDur:     time.Duration(cfg.LoginLockMin) * time.Minute,
		minLatency:  time.Duration(cfg.LoginMinLatencyMs) * time.Millisecond,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	// Background goroutine removes entries that are no longer locked and
	// have no recent failures within the sliding window.
	go func() {
		defer close(t.done)
		// Use the window as the clean-up interval: once per window any
		// stale timestamps are naturally expired.
		ticker := time.NewTicker(t.window)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.purge()
			case <-t.stop:
				return
			}
		}
	}()
	return t
}

func (t *loginThrottle) shutdown() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stop) })
	<-t.done
}

// locked reports whether the key is currently locked.
func (t *loginThrottle) locked(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.fails[key]
	return w != nil && now.Before(w.lockedUntil)
}

// fail records a failure and returns whether the key just became locked.
func (t *loginThrottle) fail(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.fails[key]
	if w == nil {
		w = &failWindow{}
		t.fails[key] = w
	}
	cutoff := now.Add(-t.window)
	keep := w.times[:0]
	for _, ts := range w.times {
		if ts.After(cutoff) {
			keep = append(keep, ts)
		}
	}
	w.times = append(keep, now)
	if len(w.times) >= t.maxFailures {
		w.lockedUntil = now.Add(t.lockDur)
		w.times = nil
		return true
	}
	return false
}

// succeed clears the failure window for the key.
func (t *loginThrottle) succeed(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, key)
}

// purge removes entries whose lock has expired and whose recent-failure
// timestamps are all older than the sliding window.  Called periodically
// by the background goroutine started in newLoginThrottle.
func (t *loginThrottle) purge() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-t.window)
	for key, w := range t.fails {
		if now.Before(w.lockedUntil) {
			continue // still locked
		}
		// Keep if any failure timestamp falls within the window.
		anyRecent := false
		for _, ts := range w.times {
			if ts.After(cutoff) {
				anyRecent = true
				break
			}
		}
		if !anyRecent {
			delete(t.fails, key)
		}
	}
}

const (
	maxForwardedForBytes = 4096
	maxForwardedHops     = 32
)

// clientIP returns the effective client address. Forwarding headers are
// considered only when the TCP peer belongs to TRUSTED_PROXY_CIDRS. For a
// trusted multi-hop chain, addresses are examined right-to-left and trusted
// proxies are stripped until the first untrusted client is found.
func (g *Gateway) clientIP(r *http.Request) string {
	peer, ok := parseIP(r.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if !g.isTrustedProxy(peer) {
		return peer.String()
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if len(xff) > maxForwardedForBytes {
			return peer.String()
		}
		parts := strings.Split(xff, ",")
		if len(parts) > maxForwardedHops {
			return peer.String()
		}
		var leftmost netip.Addr
		for i := len(parts) - 1; i >= 0; i-- {
			addr, valid := parseIP(parts[i])
			if !valid {
				return peer.String()
			}
			leftmost = addr
			if !g.isTrustedProxy(addr) {
				return addr.String()
			}
		}
		if leftmost.IsValid() {
			return leftmost.String()
		}
	}

	if xri := r.Header.Get("X-Real-IP"); len(xri) <= 128 {
		if addr, valid := parseIP(xri); valid {
			return addr.String()
		}
	}
	return peer.String()
}

func (g *Gateway) isTrustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range g.Config.TrustedProxyCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (g *Gateway) trustedProxyRequest(r *http.Request) bool {
	peer, ok := parseIP(r.RemoteAddr)
	return ok && g.isTrustedProxy(peer)
}

func parseIP(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.WithZone("").Unmap(), true
	}
	if addrPort, err := netip.ParseAddrPort(raw); err == nil {
		return addrPort.Addr().WithZone("").Unmap(), true
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.WithZone("").Unmap(), true
		}
	}
	return netip.Addr{}, false
}
