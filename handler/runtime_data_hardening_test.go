package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func settlementStoreFixture(t *testing.T, name string) (*db.Store, *db.User, *db.User, *db.Donation, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, name+".db")
	st, err := db.Open(dbPath, filepath.Join(dir, name+".key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	consumer, _ := st.CreateUser(name+"-consumer", "consumer", "")
	donor, _ := st.CreateUser(name+"-donor", "donor", "")
	if err := st.SetUserCredits(consumer.ID, 100); err != nil {
		t.Fatal(err)
	}
	donation, err := st.CreateDonation(&db.Donation{
		Service: "general", Model: "settlement", DifyBaseURL: "https://example.com",
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(), TotalCount: 10, Status: db.DonationActive,
	}, "key")
	if err != nil {
		t.Fatal(err)
	}
	return st, consumer, donor, donation, dbPath
}

func fastSettlementOptions() charitySettlementOptions {
	return charitySettlementOptions{
		attemptTimeout:  100 * time.Millisecond,
		retryDelay:      5 * time.Millisecond,
		reservedStale:   10 * time.Millisecond,
		dispatchedStale: 10 * time.Millisecond,
		scanInterval:    10 * time.Millisecond,
		queueSize:       8,
	}
}

func waitReservationStatus(t *testing.T, st *db.Store, id, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r, err := st.GetCharityReservation(id)
		if err == nil && r != nil && r.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r, err := st.GetCharityReservation(id)
	t.Fatalf("reservation %s status=%v err=%v, want %s", id, r, err, want)
}

func TestSettlementWorkerRoutesDurableStatesExactlyOnce(t *testing.T) {
	st, consumer, donor, donation, _ := settlementStoreFixture(t, "state-routing")
	ctx := context.Background()
	reserved, _ := st.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 3)
	dispatched, _ := st.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 3)
	terminal, _ := st.ReserveCharityCall(ctx, consumer.ID, donation.ID, 10, 3)
	if err := st.MarkCharityDispatched(ctx, dispatched.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCharityDispatched(ctx, terminal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitCharityReservation(ctx, terminal.ID); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Second).Unix()
	if _, err := st.RawExec(`UPDATE charity_reservations SET updated_at=? WHERE id IN (?,?)`, old, reserved.ID, dispatched.ID); err != nil {
		t.Fatal(err)
	}
	w := newCharitySettlementWorker(st, fastSettlementOptions())
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = w.shutdown(shutdownCtx)
	})
	w.wake(terminal.ID)
	w.wake(terminal.ID)
	waitReservationStatus(t, st, reserved.ID, db.ReservationReleased, time.Second)
	waitReservationStatus(t, st, dispatched.ID, db.ReservationCommitted, time.Second)
	waitReservationStatus(t, st, terminal.ID, db.ReservationCommitted, time.Second)

	gotDonation, _ := st.GetDonation(donation.ID)
	gotConsumer, _ := st.GetUserByID(consumer.ID)
	gotDonor, _ := st.GetUserByID(donor.ID)
	if gotDonation.RemainingCount != 8 || gotDonation.SuccessCount != 2 || gotConsumer.Credits != 80 || gotDonor.Credits != 6 || gotDonor.DonationCredit != 2 {
		t.Fatalf("accounting after recovery: remaining=%d success=%d consumer=%d donor=%d contribution=%d",
			gotDonation.RemainingCount, gotDonation.SuccessCount, gotConsumer.Credits, gotDonor.Credits, gotDonor.DonationCredit)
	}
	for i := 0; i < 20; i++ {
		w.wake(reserved.ID)
		w.wake(dispatched.ID)
	}
	time.Sleep(50 * time.Millisecond)
	again, _ := st.GetDonation(donation.ID)
	againDonor, _ := st.GetUserByID(donor.ID)
	if again.RemainingCount != 8 || again.SuccessCount != 2 || againDonor.Credits != 6 || againDonor.DonationCredit != 2 {
		t.Fatalf("duplicate wake changed accounting: donation=%+v donor=%+v", again, againDonor)
	}
}

func TestSettlementWorkerQueueFullUsesPeriodicDatabaseFallback(t *testing.T) {
	st, consumer, _, donation, _ := settlementStoreFixture(t, "queue-full")
	r, err := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawExec(`UPDATE charity_reservations SET updated_at=? WHERE id=?`, time.Now().Add(-time.Second).Unix(), r.ID); err != nil {
		t.Fatal(err)
	}
	opts := fastSettlementOptions()
	opts.queueSize = 1
	ctx, cancel := context.WithCancel(context.Background())
	w := &charitySettlementWorker{
		store: st, opts: opts, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), queue: make(chan string, 1), pending: make(map[string]bool, 1),
	}
	if !w.wake("missing-placeholder") {
		t.Fatal("failed to fill queue")
	}
	if w.wake(r.ID) {
		t.Fatal("queue-full wake unexpectedly retained")
	}
	go w.run()
	waitReservationStatus(t, st, r.ID, db.ReservationReleased, time.Second)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := w.shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestSettlementWorkerConcurrentDedupAndShutdown(t *testing.T) {
	st, _, _, _, _ := settlementStoreFixture(t, "dedup")
	opts := fastSettlementOptions()
	ctx, cancel := context.WithCancel(context.Background())
	w := &charitySettlementWorker{
		store: st, opts: opts, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), queue: make(chan string, opts.queueSize), pending: make(map[string]bool, opts.queueSize),
	}
	const goroutines = 64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.wake("same-id")
		}()
	}
	wg.Wait()
	if got := len(w.queue); got != 1 {
		t.Fatalf("deduplicated queue length=%d, want 1", got)
	}
	go w.run()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := w.shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if w.wake("after-shutdown") {
		t.Fatal("wake succeeded after shutdown")
	}
}

func TestSettlementRecoversOnlineAfterDatabaseLockExceedsAttemptWindow(t *testing.T) {
	st, consumer, donor, donation, dbPath := settlementStoreFixture(t, "db-lock")
	r, err := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCharityDispatched(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawExec(`PRAGMA busy_timeout=5000`); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.CharitySettlementAttemptTimeoutSec = 5
	cfg.CharitySettlementRetryDelayMs = 25
	cfg.CharitySettlementScanIntervalSec = 1
	cfg.CharitySettlementQueueSize = 8
	gw := NewGateway(cfg, st)
	cleanupGatewayForTest(t, gw)

	locker, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	lockTx, err := locker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES ('settlement-lock','1')`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	gw.charityCommitAccounting(r)
	if elapsed := time.Since(started); elapsed < 4500*time.Millisecond {
		t.Fatalf("synchronous BUSY retry returned after %v, want bounded retries across the 5s attempt window", elapsed)
	}
	gw.settlement.mu.Lock()
	_, pending := gw.settlement.pending[r.ID]
	gw.settlement.mu.Unlock()
	if !pending {
		t.Fatal("failed synchronous settlement was not handed to the online worker")
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	waitReservationStatus(t, st, r.ID, db.ReservationCommitted, 4*time.Second)
	gotDonation, _ := st.GetDonation(donation.ID)
	gotDonor, _ := st.GetUserByID(donor.ID)
	if gotDonation.RemainingCount != 9 || gotDonation.SuccessCount != 1 || gotDonor.Credits != 4 || gotDonor.DonationCredit != 1 {
		t.Fatalf("online recovery accounting: donation=%+v donor=%+v", gotDonation, gotDonor)
	}
}

func TestSettlementBusyBackoffStopsOnContextCancellation(t *testing.T) {
	st, consumer, _, donation, dbPath := settlementStoreFixture(t, "cancel-lock")
	r, err := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCharityDispatched(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	locker, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	lockTx, err := locker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES ('settlement-cancel-lock','1')`); err != nil {
		t.Fatal(err)
	}
	w := &charitySettlementWorker{store: st, opts: charitySettlementOptions{retryDelay: time.Hour}}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.settleCurrentState(ctx, r.ID) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) || !db.IsBusyOrLocked(err) {
			t.Fatalf("cancelled BUSY retry error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt settlement backoff")
	}
}

func TestIssueSessionUsesAbsoluteCookieExpiry(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("cookie-absolute", "cookie", "")
	rec := httptest.NewRecorder()
	before := time.Now()
	if err := gw.issueSession(rec, u); err != nil {
		t.Fatal(err)
	}
	res := rec.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName {
		t.Fatalf("session cookies=%+v", cookies)
	}
	if cookies[0].Expires.Before(before.Add(db.SessionAbsoluteTTL-time.Minute)) || cookies[0].Expires.After(time.Now().Add(db.SessionAbsoluteTTL+time.Minute)) {
		t.Fatalf("cookie expiry=%v, want creation+30d", cookies[0].Expires)
	}
}

func TestHTTPLogSurfacesHidePhysicallyPresentExpiredRows(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	u, _ := store.CreateUser("strict-http", "strict-http", "")
	five := 5
	if err := store.SetUserLevel(u.ID, &five); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-db.RequestLogRetention - time.Hour)
	if err := store.AddRequestLog(u.ID, "expired", "general", old, old.Add(time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}
	recent := time.Now().Add(-time.Hour)
	if err := store.AddRequestLog(u.ID, "visible", "general", recent, recent.Add(time.Second), "success", ""); err != nil {
		t.Fatal(err)
	}
	userCookie := meUserCookie(t, store, u)

	assertLogPayload := func(path string, cookie *http.Cookie, key string) {
		t.Helper()
		rec := donationRequest(gw, cookie, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(payload[key], &rows); err != nil {
			t.Fatalf("%s decode %s: %v body=%s", path, key, err, rec.Body.String())
		}
		if len(rows) != 1 || rows[0]["model"] != "visible" {
			t.Fatalf("%s rows=%+v", path, rows)
		}
	}
	assertLogPayload("/api/logs", userCookie, "logs")
	assertLogPayload("/api/admin/logs", adminCookie, "logs")
	assertLogPayload("/api/me/all-logs", userCookie, "logs")

	for _, tc := range []struct {
		path   string
		cookie *http.Cookie
	}{
		{"/api/admin/logs/export?format=json", adminCookie},
	} {
		rec := donationRequest(gw, tc.cookie, http.MethodGet, tc.path, nil)
		var rows []map[string]interface{}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0]["model"] != "visible" {
			t.Fatalf("%s status=%d rows=%+v body=%s", tc.path, rec.Code, rows, rec.Body.String())
		}
	}
	for _, tc := range []struct {
		path   string
		cookie *http.Cookie
	}{
		{"/api/admin/logs/stats", adminCookie},
		{"/api/me/all-logs/stats", userCookie},
	} {
		rec := donationRequest(gw, tc.cookie, http.MethodGet, tc.path, nil)
		var payload struct {
			ByHour []db.LogHourStat `json:"by_hour"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &payload) != nil || len(payload.ByHour) != 1 || payload.ByHour[0].Total != 1 {
			t.Fatalf("%s status=%d payload=%+v body=%s", tc.path, rec.Code, payload, rec.Body.String())
		}
	}

	for _, tc := range []struct {
		path   string
		cookie *http.Cookie
	}{
		{"/api/me/export", userCookie},
		{"/api/admin/users/" + fmt.Sprint(u.ID) + "/export", adminCookie},
	} {
		rec := donationRequest(gw, tc.cookie, http.MethodGet, tc.path, nil)
		var bundle db.ExportBundle
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &bundle) != nil || len(bundle.Logs) != 1 || bundle.Logs[0].Model != "visible" {
			t.Fatalf("%s status=%d logs=%+v body=%s", tc.path, rec.Code, bundle.Logs, rec.Body.String())
		}
	}
}

func BenchmarkSettlementRetryTerminal(b *testing.B) {
	dir := b.TempDir()
	st, err := db.Open(filepath.Join(dir, "settlement.db"), filepath.Join(dir, "settlement.key"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	consumer, _ := st.CreateUser("bench-consumer", "consumer", "")
	_ = st.SetUserCredits(consumer.ID, 10)
	donation, _ := st.CreateDonation(&db.Donation{Service: "general", Model: "m", DifyBaseURL: "https://example.com", Deadline: time.Now().Add(time.Hour).Unix(), TotalCount: 1}, "key")
	r, _ := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 1, 0)
	worker := &charitySettlementWorker{store: st, opts: fastSettlementOptions()}
	for i := 0; i < b.N; i++ {
		if err := worker.settleCurrentState(context.Background(), r.ID); err != nil {
			b.Fatal(err)
		}
	}
}
