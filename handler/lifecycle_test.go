package handler

import (
	"testing"
	"time"

	"dify2api/config"
)

func TestThrottleShutdownStopsCleanupGoroutines(t *testing.T) {
	ip := newIPThrottle(1, 1, 1)
	ip.shutdown()
	ip.shutdown() // idempotent
	select {
	case <-ip.done:
	default:
		t.Fatal("IP throttle cleanup goroutine did not stop")
	}

	disabledIP := newIPThrottle(0, 1, 1)
	disabledIP.shutdown()
	select {
	case <-disabledIP.done:
	default:
		t.Fatal("disabled IP throttle did not expose completed lifecycle")
	}

	login := newLoginThrottle(&config.Config{
		LoginMaxFailures:  5,
		LoginWindowMin:    1,
		LoginLockMin:      1,
		LoginMinLatencyMs: 1,
	})
	login.shutdown()
	login.shutdown()
	select {
	case <-login.done:
	case <-time.After(time.Second):
		t.Fatal("login throttle cleanup goroutine did not stop")
	}
}
