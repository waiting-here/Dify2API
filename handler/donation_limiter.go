package handler

import (
	"sync"
	"time"
)

// donationRateLimiter implements a per-donation sliding-window RPM counter.
// All counters are in-memory only; they are lost on restart.
type donationRateLimiter struct {
	mu      sync.Mutex
	windows map[int64][]time.Time // donationID → recent call timestamps
}

// newDonationRateLimiter creates a new limiter with an empty window map.
func newDonationRateLimiter() *donationRateLimiter {
	return &donationRateLimiter{
		windows: make(map[int64][]time.Time),
	}
}

// allow checks whether a call for the given donation is within its RPM
// limit. It returns (true, record) if allowed, where record() must be
// called after the call is actually committed (to avoid counting calls
// that fail before reaching Dify). Returns (false, nil) when the limit
// has been reached.
func (l *donationRateLimiter) allow(donationID int64, rpmLimit int) (allowed bool, record func()) {
	if rpmLimit <= 0 {
		rpmLimit = 10
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-60 * time.Second)

	records := l.windows[donationID]

	// Purge stale records.
	keep := records[:0]
	for _, t := range records {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	l.windows[donationID] = keep

	if len(keep) >= rpmLimit {
		return false, nil
	}

	// Return a closure that records the call only when actually used.
	return true, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.windows[donationID] = append(l.windows[donationID], time.Now())
	}
}
