package handler

import (
	"sync"
	"time"
)

// rateLimiter is an in-memory per-user sliding-window (60s) counter.
// State resets on restart (acceptable: windows are one minute).
type rateLimiter struct {
	mu   sync.Mutex
	hits map[int64][]int64 // userID -> unix seconds of recent requests

	// rpmCache avoids a DB round-trip on every API call.  A value of -1
	// means "use global default"; invalidateRPMCache clears the entry so
	// the next lookup re-reads from the DB.
	rpmCache   map[int64]int
	rpmCacheMu sync.RWMutex
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		hits:     make(map[int64][]int64),
		rpmCache: make(map[int64]int),
	}
}

// allow records a request at `now` and reports whether it is within limit.
func (l *rateLimiter) allow(userID int64, now time.Time, limit int) bool {
	cutoff := now.Unix() - 60
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.hits[userID][:0]
	for _, t := range l.hits[userID] {
		if t > cutoff {
			keep = append(keep, t)
		}
	}
	if len(keep) >= limit {
		if len(keep) == 0 {
			delete(l.hits, userID)
		} else {
			l.hits[userID] = keep
		}
		return false
	}
	l.hits[userID] = append(keep, now.Unix())
	return true
}

// effectiveRPM returns a hardcoded RPM cap (3).  This is a temporary stub
// for alpha.3 S1 — S2 will implement the three-class RPM system with
// per-user overrides from rpm_limit_a/b/c columns and the new settings keys.
func (g *Gateway) effectiveRPM(userID int64) int {
	return 3
}

// invalidateRPMCache is a no-op stub for alpha.3 S1. S2 will reinstate
// real cache invalidation for the three-class RPM system.
func (g *Gateway) invalidateRPMCache(userID int64) {}
