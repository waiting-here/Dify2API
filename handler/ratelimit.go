package handler

import (
	"sync"
	"time"

	"dify2api/db"
)

// rpmClass identifies one of the three sliding-window counters of the
// three-class RPM system (alpha.3 F4):
//
//	A — counted when a request finishes transferring successfully
//	    (streaming: stream fully relayed; blocking: response written).
//	    Failures are NOT counted.
//	B — counted when a request is judged "successful" per the §1.2
//	    definition (Dify returned HTTP 200 / stream started).
//	C — counted when a request is received (after caller-key auth).
//
// The gate check happens once at request start (after auth, before
// anything is sent to Dify): all three windows must be under their caps.
// A and B increment at request end, C increments at request start.
type rpmClass int

const (
	rpmClassA rpmClass = iota
	rpmClassB
	rpmClassC
	rpmClassCount
)

// rateLimiter is an in-memory per-user sliding-window (60s) counter for the
// three RPM classes. State resets on restart (acceptable: windows are one
// minute).
type rateLimiter struct {
	mu   sync.Mutex
	hits [rpmClassCount]map[int64][]int64 // class -> userID -> unix seconds

	// limitCache caches per-user resolved limits [A,B,C] to avoid a DB
	// round-trip on every call; invalidated on admin changes.
	limitCache   map[int64][rpmClassCount]int
	limitCacheMu sync.RWMutex
}

func newRateLimiter() *rateLimiter {
	l := &rateLimiter{
		limitCache: make(map[int64][rpmClassCount]int),
	}
	for c := range l.hits {
		l.hits[c] = make(map[int64][]int64)
	}
	return l
}

// countRecent returns how many hits the user has in the last 60s for the
// given class, pruning expired entries. Caller must hold l.mu.
func (l *rateLimiter) countRecentLocked(class rpmClass, userID int64, now time.Time) int {
	cutoff := now.Unix() - 60
	keep := l.hits[class][userID][:0]
	for _, t := range l.hits[class][userID] {
		if t > cutoff {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.hits[class], userID)
	} else {
		l.hits[class][userID] = keep
	}
	return len(keep)
}

// check reports whether all three windows are strictly under their caps.
// It does NOT record anything. Returns the first violated class (valid
// only when allowed == false).
func (l *rateLimiter) check(userID int64, now time.Time, limits [rpmClassCount]int) (allowed bool, violated rpmClass) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for c := rpmClassA; c < rpmClassCount; c++ {
		if l.countRecentLocked(c, userID, now) >= limits[c] {
			return false, c
		}
	}
	return true, 0
}

// record adds one hit for the class at the given time.
func (l *rateLimiter) record(class rpmClass, userID int64, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Prune while we are here to bound memory.
	l.countRecentLocked(class, userID, now)
	l.hits[class][userID] = append(l.hits[class][userID], now.Unix())
}

// classLabel returns the human label used in error messages and logs.
func classLabel(c rpmClass) string {
	switch c {
	case rpmClassA:
		return "A（传输完成）"
	case rpmClassB:
		return "B（请求成功）"
	default:
		return "C（请求接收）"
	}
}

// effectiveRPMLimits resolves the three caps for a user: per-user override
// (users.rpm_limit_a/b/c) wins over the global settings (settings table),
// which fall back to the compile-time defaults. Results are cached;
// invalidateRPMCache must be called whenever an admin changes any of them.
func (g *Gateway) effectiveRPMLimits(userID int64) [rpmClassCount]int {
	g.limiter.limitCacheMu.RLock()
	if v, ok := g.limiter.limitCache[userID]; ok {
		g.limiter.limitCacheMu.RUnlock()
		return v
	}
	g.limiter.limitCacheMu.RUnlock()

	limits := [rpmClassCount]int{
		g.Store.GetSettingInt(db.SettingRPMLimitA, db.DefaultRPMLimitA),
		g.Store.GetSettingInt(db.SettingRPMLimitB, db.DefaultRPMLimitB),
		g.Store.GetSettingInt(db.SettingRPMLimitC, db.DefaultRPMLimitC),
	}
	u, err := g.Store.GetUserByID(userID)
	if err == nil && u != nil {
		if u.RPMLimitA.Valid {
			limits[rpmClassA] = int(u.RPMLimitA.Int64)
		}
		if u.RPMLimitB.Valid {
			limits[rpmClassB] = int(u.RPMLimitB.Int64)
		}
		if u.RPMLimitC.Valid {
			limits[rpmClassC] = int(u.RPMLimitC.Int64)
		}
	}

	g.limiter.limitCacheMu.Lock()
	g.limiter.limitCache[userID] = limits
	g.limiter.limitCacheMu.Unlock()
	return limits
}

// invalidateRPMCache clears the cached limits for one user (userID > 0) or
// for everyone (userID == 0, used when a global setting changes).
func (g *Gateway) invalidateRPMCache(userID int64) {
	g.limiter.limitCacheMu.Lock()
	if userID == 0 {
		g.limiter.limitCache = make(map[int64][rpmClassCount]int)
	} else {
		delete(g.limiter.limitCache, userID)
	}
	g.limiter.limitCacheMu.Unlock()
}
