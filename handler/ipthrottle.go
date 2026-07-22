package handler

import (
	"sync"
	"time"
)

// ipThrottle is a per-IP sliding-window (60s) rate limiter with a fixed
// penalty period: once an IP exceeds rpm hits per minute, further requests
// from it are rejected until penaltyDur elapses. In-memory by design
// (short windows; restart resets counters).
//
// Two instances are used (alpha.3):
//   - webThrottle: /api/* session endpoints (F7) — throttles all hits.
//   - authFailThrottle: /v1/* invalid-caller-key rejections — only
//     failures are recorded, so legitimate keys are never affected.
type ipThrottle struct {
	mu         sync.Mutex
	hits       map[string][]int64 // ip -> unix seconds of recent hits
	blockUntil map[string]int64   // ip -> unix second the penalty ends
	rpm        int                // per-minute cap; 0 disables the throttle
	penaltyDur time.Duration
}

func newIPThrottle(rpm, penaltySec int) *ipThrottle {
	t := &ipThrottle{
		hits:       make(map[string][]int64),
		blockUntil: make(map[string]int64),
		rpm:        rpm,
		penaltyDur: time.Duration(penaltySec) * time.Second,
	}
	if rpm > 0 {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				t.purge(time.Now())
			}
		}()
	}
	return t
}

// allow records a hit for the IP and reports whether it is within limits.
// When the cap is crossed, the IP enters the penalty period and subsequent
// calls return false until it ends. A disabled throttle (rpm == 0) always
// allows.
func (t *ipThrottle) allow(ip string, now time.Time) bool {
	if t.rpm <= 0 {
		return true
	}
	nowSec := now.Unix()
	cutoff := nowSec - 60
	t.mu.Lock()
	defer t.mu.Unlock()

	if until, ok := t.blockUntil[ip]; ok {
		if nowSec < until {
			return false
		}
		delete(t.blockUntil, ip)
		delete(t.hits, ip) // fresh window after the penalty
	}

	keep := t.hits[ip][:0]
	for _, ts := range t.hits[ip] {
		if ts > cutoff {
			keep = append(keep, ts)
		}
	}
	keep = append(keep, nowSec)
	t.hits[ip] = keep
	if len(keep) > t.rpm {
		t.blockUntil[ip] = nowSec + int64(t.penaltyDur/time.Second)
		delete(t.hits, ip)
		return false
	}
	return true
}

// retryAfterSec returns the remaining penalty in seconds (min 1) for the
// Retry-After header.
func (t *ipThrottle) retryAfterSec(ip string, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if until, ok := t.blockUntil[ip]; ok {
		if d := until - now.Unix(); d > 0 {
			return int(d)
		}
	}
	return 1
}

// purge drops IPs with no recent hits and expired penalties.
func (t *ipThrottle) purge(now time.Time) {
	nowSec := now.Unix()
	cutoff := nowSec - 60
	t.mu.Lock()
	defer t.mu.Unlock()
	for ip, until := range t.blockUntil {
		if nowSec >= until {
			delete(t.blockUntil, ip)
		}
	}
	for ip, times := range t.hits {
		recent := false
		for _, ts := range times {
			if ts > cutoff {
				recent = true
				break
			}
		}
		if !recent {
			delete(t.hits, ip)
		}
	}
}
