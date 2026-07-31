package handler

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestDonationRateLimiter_AtomicAcquire(t *testing.T) {
	limiter := newDonationRateLimiter()
	start := make(chan struct{})
	var successes atomic.Int32
	var release func()
	var releaseMu sync.Mutex
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if gotRelease, ok := limiter.acquire(42, 1); ok {
				successes.Add(1)
				releaseMu.Lock()
				release = gotRelease
				releaseMu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful acquires = %d, want 1", successes.Load())
	}
	releaseMu.Lock()
	release()
	releaseMu.Unlock()
	if _, ok := limiter.acquire(42, 1); !ok {
		t.Fatal("released pre-dispatch lease should be acquirable again")
	}
}
