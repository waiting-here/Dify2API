package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"dify2api/db"
)

type charitySettlementOptions struct {
	attemptTimeout  time.Duration
	retryDelay      time.Duration
	reservedStale   time.Duration
	dispatchedStale time.Duration
	scanInterval    time.Duration
	queueSize       int
}

// charitySettlementWorker owns all online reservation recovery state. The
// queue and pending set are both bounded; durable stale-state scans are the
// source of truth when an in-memory wakeup cannot be retained.
type charitySettlementWorker struct {
	store   *db.Store
	opts    charitySettlementOptions
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	queue   chan string
	mu      sync.Mutex
	pending map[string]bool // id -> currently queued; false means retry on scan tick
}

func newCharitySettlementWorker(store *db.Store, opts charitySettlementOptions) *charitySettlementWorker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &charitySettlementWorker{
		store: store, opts: opts, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), queue: make(chan string, opts.queueSize),
		pending: make(map[string]bool, opts.queueSize),
	}
	go w.run()
	return w
}

// wake records one best-effort immediate retry. A full queue/set deliberately
// drops only the memory hint; periodic stale-state discovery remains durable.
func (w *charitySettlementWorker) wake(id string) bool {
	if w == nil || id == "" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx.Err() != nil {
		return false
	}
	if _, exists := w.pending[id]; exists {
		return true
	}
	if len(w.pending) >= w.opts.queueSize {
		return false
	}
	w.pending[id] = false
	select {
	case <-w.ctx.Done():
		delete(w.pending, id)
		return false
	case w.queue <- id:
		w.pending[id] = true
		return true
	default:
		delete(w.pending, id)
		return false
	}
}

func (w *charitySettlementWorker) run() {
	defer close(w.done)
	ticker := time.NewTicker(w.opts.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case id := <-w.queue:
			w.processQueued(id)
		case now := <-ticker.C:
			w.retryPending()
			w.discoverStale(now)
		}
	}
}

func (w *charitySettlementWorker) processQueued(id string) {
	w.mu.Lock()
	queued, exists := w.pending[id]
	if exists && queued {
		w.pending[id] = false
	}
	w.mu.Unlock()
	if !exists || !queued {
		return
	}
	w.process(id)
}

func (w *charitySettlementWorker) retryPending() {
	w.mu.Lock()
	ids := make([]string, 0, len(w.pending))
	for id, queued := range w.pending {
		if !queued {
			ids = append(ids, id)
		}
	}
	w.mu.Unlock()
	for _, id := range ids {
		if w.ctx.Err() != nil {
			return
		}
		w.process(id)
	}
}

func (w *charitySettlementWorker) process(id string) {
	ctx, cancel := context.WithTimeout(w.ctx, w.opts.attemptTimeout)
	err := w.settleCurrentState(ctx, id)
	cancel()
	if err != nil {
		if w.ctx.Err() == nil {
			log.Printf("[SETTLEMENT] retry reservation %s: %v", id, err)
		}
		return
	}
	w.mu.Lock()
	delete(w.pending, id)
	w.mu.Unlock()
}

func (w *charitySettlementWorker) discoverStale(now time.Time) {
	ctx, cancel := context.WithTimeout(w.ctx, w.opts.attemptTimeout)
	ids, err := w.store.ListStaleCharityReservationIDs(
		ctx,
		now.Add(-w.opts.reservedStale).Unix(),
		now.Add(-w.opts.dispatchedStale).Unix(),
		w.opts.queueSize,
	)
	cancel()
	if err != nil {
		if w.ctx.Err() == nil {
			log.Printf("[SETTLEMENT] scan stale reservations: %v", err)
		}
		return
	}
	for _, id := range ids {
		w.wake(id)
	}
}

// settleCurrentStateOnce performs exactly one idempotent state transition
// based on the current durable row. It never trusts an earlier in-memory outcome.
func (w *charitySettlementWorker) settleCurrentStateOnce(ctx context.Context, id string) error {
	r, err := w.store.GetCharityReservationContext(ctx, id)
	if err != nil {
		return err
	}
	if r == nil {
		return nil
	}
	switch r.Status {
	case db.ReservationReserved:
		_, err = w.store.ReleaseCharityReservation(ctx, id, false)
	case db.ReservationDispatched:
		_, err = w.store.CommitCharityReservation(ctx, id)
	case db.ReservationCommitted, db.ReservationReleased:
		return nil
	default:
		return fmt.Errorf("invalid durable status %q", r.Status)
	}
	return err
}

// settleCurrentState retries only transient SQLite lock contention. The
// caller's context is the total budget and also makes shutdown cancellation
// interrupt both database calls and backoff waits.
func (w *charitySettlementWorker) settleCurrentState(ctx context.Context, id string) error {
	for {
		err := w.settleCurrentStateOnce(ctx, id)
		if err == nil || !db.IsBusyOrLocked(err) {
			return err
		}
		timer := time.NewTimer(w.opts.retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), err)
		case <-timer.C:
		}
	}
}

// settleNow is the normal request-path attempt. Failure is returned quickly
// enough for the caller to enqueue durable online recovery.
func (w *charitySettlementWorker) settleNow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.attemptTimeout)
	defer cancel()
	return w.settleCurrentState(ctx, id)
}

func (w *charitySettlementWorker) shutdown(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.cancel()
	w.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
