package handler

import (
	"sync"
	"time"
)

type donationRPMRecord struct {
	id uint64
	at time.Time
}

// donationRateLimiter implements an atomic per-donation sliding-window RPM
// lease. Acquisition records immediately; the returned release closure is
// used only when setup fails before any donated Dify credential is used.
type donationRateLimiter struct {
	mu      sync.Mutex
	windows map[int64][]donationRPMRecord
	nextID  uint64
}

func newDonationRateLimiter() *donationRateLimiter {
	return &donationRateLimiter{windows: make(map[int64][]donationRPMRecord)}
}

func (l *donationRateLimiter) acquire(donationID int64, rpmLimit int) (release func(), ok bool) {
	if rpmLimit <= 0 {
		rpmLimit = 10
	}
	l.mu.Lock()
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)
	records := l.windows[donationID]
	keep := records[:0]
	for _, record := range records {
		if record.at.After(cutoff) {
			keep = append(keep, record)
		}
	}
	if len(keep) >= rpmLimit {
		l.windows[donationID] = keep
		l.mu.Unlock()
		return nil, false
	}
	l.nextID++
	id := l.nextID
	l.windows[donationID] = append(keep, donationRPMRecord{id: id, at: now})
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			records := l.windows[donationID]
			for i, record := range records {
				if record.id == id {
					l.windows[donationID] = append(records[:i], records[i+1:]...)
					break
				}
			}
		})
	}, true
}
