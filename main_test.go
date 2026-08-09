package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"dify2api/db"
)

type fakeShutdownServer struct {
	mu             sync.Mutex
	shutdown       func(context.Context) error
	shutdownCalled bool
	closeCalled    bool
}

func (s *fakeShutdownServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdownCalled = true
	s.mu.Unlock()
	return s.shutdown(ctx)
}

func (s *fakeShutdownServer) Close() error {
	s.mu.Lock()
	s.closeCalled = true
	s.mu.Unlock()
	return nil
}

type fakeShutdownGateway struct {
	called bool
	err    error
}

func (g *fakeShutdownGateway) Shutdown(context.Context) error {
	g.called = true
	return g.err
}

func TestShutdownApplication_NormalPath(t *testing.T) {
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		<-backgroundCtx.Done()
		close(backgroundDone)
	}()
	server := &fakeShutdownServer{shutdown: func(context.Context) error {
		select {
		case <-backgroundCtx.Done():
			return nil
		default:
			t.Fatal("background cancellation must precede HTTP draining")
			return nil
		}
	}}
	gateway := &fakeShutdownGateway{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdownApplication(ctx, server, gateway, cancelBackground, backgroundDone); err != nil {
		t.Fatalf("shutdownApplication: %v", err)
	}
	if !server.shutdownCalled || server.closeCalled {
		t.Fatalf("server calls: shutdown=%v close=%v", server.shutdownCalled, server.closeCalled)
	}
	if !gateway.called {
		t.Fatal("gateway shutdown was not called")
	}
}

func TestShutdownApplication_DeadlineForcesHTTPAndReturns(t *testing.T) {
	backgroundDone := make(chan struct{}) // deliberately never closes
	server := &fakeShutdownServer{shutdown: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	gateway := &fakeShutdownGateway{err: context.DeadlineExceeded}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := shutdownApplication(ctx, server, gateway, func() {}, backgroundDone)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if !server.closeCalled || !gateway.called {
		t.Fatalf("timeout cleanup incomplete: close=%v gateway=%v", server.closeCalled, gateway.called)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("deadline path blocked for %v", elapsed)
	}
}

func TestCleanupWorkerStopsBeforeStoreClose(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "cleanup.db"), filepath.Join(dir, "cleanup.key"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startCleanupWorker(ctx, store, time.Minute)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup worker did not stop after cancellation")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close Store after worker exit: %v", err)
	}
}

func TestCleanupWorkerPurgesRequestLogsAtStartupAndConfiguredInterval(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "retention.db")
	store, err := db.Open(dbPath, filepath.Join(dir, "retention.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateUser("cleanup-retention", "cleanup", "")
	insertExpired := func(label string) {
		t.Helper()
		old := time.Now().Add(-db.RequestLogRetention - time.Hour)
		id, err := store.AddRequestLogFull(u.ID, label, "general", old, old.Add(time.Second), "error", "old", 500, "old", 0, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.AddAdminAlert(&db.AdminAlert{Type: db.AlertBlockingFailed200, Message: label, RequestLogID: &id}); err != nil {
			t.Fatal(err)
		}
	}
	insertExpired("startup")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	waitPurged := func(label string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			var logs, alerts int
			logErr := raw.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE model=?`, label).Scan(&logs)
			alertErr := raw.QueryRow(`SELECT COUNT(*) FROM admin_alerts WHERE message=?`, label).Scan(&alerts)
			if logErr == nil && alertErr == nil && logs == 0 && alerts == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("expired log/alert %q were not purged", label)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startCleanupWorker(ctx, store, 20*time.Millisecond)
	waitPurged("startup")
	insertExpired("periodic")
	waitPurged("periodic")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not stop")
	}
}
