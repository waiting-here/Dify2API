package handler

import (
	"sync"
	"time"
)

// defaultProbeLimitPerUser is the per-user cap on Dify App compatibility
// probes (sliding 60s window). The global DIFY_PROBE_IN_FLIGHT semaphore
// bounds concurrency; this bounds each user's request rate on top of it.
const defaultProbeLimitPerUser = 5

// probeLimiter enforces a per-user sliding-window cap on App probe calls.
// Pure in-memory: counters reset on restart (acceptable — windows are short
// and the check is informational only). Fully-expired user entries are
// pruned on access, and a periodic sweep bounds memory when many distinct
// users probe once and never come back.
type probeLimiter struct {
	mu        sync.Mutex
	hits      map[int64][]int64 // userID -> unix seconds of accepted probes
	limit     int
	windowSec int64
	lastSweep time.Time
}

func newProbeLimiter(limit int) *probeLimiter {
	if limit <= 0 {
		limit = defaultProbeLimitPerUser
	}
	return &probeLimiter{
		hits:      make(map[int64][]int64),
		limit:     limit,
		windowSec: 60,
	}
}

// allow records one probe attempt for the user and reports whether it is
// under the cap. Attempts count even when the probe later fails (they cost
// a semaphore slot and a dial); denied attempts are not recorded.
func (l *probeLimiter) allow(userID int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Unix() - l.windowSec
	records := l.hits[userID]
	keep := records[:0]
	for _, t := range records {
		if t > cutoff {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.limit {
		if len(keep) > 0 {
			l.hits[userID] = keep
		} else {
			delete(l.hits, userID)
		}
		return false
	}
	l.hits[userID] = append(keep, now.Unix())
	l.maybeSweepLocked(now)
	return true
}

// maybeSweepLocked drops all fully-expired user entries once the map grows
// large enough. Called on successful allows; bounds memory without a
// background goroutine. Caller must hold l.mu.
func (l *probeLimiter) maybeSweepLocked(now time.Time) {
	if len(l.hits) < 1024 || now.Sub(l.lastSweep) < time.Minute {
		return
	}
	cutoff := now.Unix() - l.windowSec
	for uid, records := range l.hits {
		keep := records[:0]
		for _, t := range records {
			if t > cutoff {
				keep = append(keep, t)
			}
		}
		if len(keep) == 0 {
			delete(l.hits, uid)
		} else {
			l.hits[uid] = keep
		}
	}
	l.lastSweep = now
}
