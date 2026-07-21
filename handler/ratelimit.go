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

// effectiveRPM resolves the cap for a user: per-user override wins over
// global default.  Results are cached in-memory; call invalidateRPMCache
// whenever a per-user or global RPM is changed by an admin.
func (g *Gateway) effectiveRPM(userID int64) int {
	// Fast path: cache hit.
	g.limiter.rpmCacheMu.RLock()
	if v, ok := g.limiter.rpmCache[userID]; ok {
		g.limiter.rpmCacheMu.RUnlock()
		if v == -1 {
			return g.Store.GetGlobalRPM()
		}
		return v
	}
	g.limiter.rpmCacheMu.RUnlock()

	// Slow path: query DB and populate cache.
	u, err := g.Store.GetUserByID(userID)
	v := -1
	if err == nil && u != nil && u.RPMLimit.Valid {
		v = int(u.RPMLimit.Int64)
	}
	g.limiter.rpmCacheMu.Lock()
	g.limiter.rpmCache[userID] = v
	g.limiter.rpmCacheMu.Unlock()

	if v == -1 {
		return g.Store.GetGlobalRPM()
	}
	return v
}

// invalidateRPMCache clears RPM caches so the next lookup re-reads from
// the DB.  Call after any admin action that changes per-user or global RPM.
func (g *Gateway) invalidateRPMCache(userID int64) {
	g.limiter.rpmCacheMu.Lock()
	delete(g.limiter.rpmCache, userID)
	g.limiter.rpmCacheMu.Unlock()
}
