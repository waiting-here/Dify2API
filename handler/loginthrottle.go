package handler

import (
	"net"
	"net/http"
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
	}
	// Background goroutine removes entries that are no longer locked and
	// have no recent failures within the sliding window.
	go func() {
		// Use the window as the clean-up interval: once per window any
		// stale timestamps are naturally expired.
		ticker := time.NewTicker(t.window)
		defer ticker.Stop()
		for range ticker.C {
			t.purge()
		}
	}()
	return t
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

// clientIP extracts the real client IP, trusting the proxy headers set by our
// own nginx (the origin is firewalled to Cloudflare/nginx only).
//
// SECURITY: this function trusts X-Forwarded-For / X-Real-IP unconditionally.
// The Go server MUST listen on a loopback or firewalled address (default
// localhost:10086) so that only the trusted reverse proxy (nginx) can reach
// it.  Do NOT expose the Go listener directly to the public internet — an
// attacker who bypasses the proxy can spoof these headers and evade login
// throttling.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
