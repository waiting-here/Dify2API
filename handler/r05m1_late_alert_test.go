package handler

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"dify2api/db"
)

func setupR05M1Gateway(t *testing.T) (*Gateway, *db.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := testConfig()
	cfg.SMTP.Host = "smtp.example.com"
	cfg.SMTP.Port = 587
	cfg.SMTP.To = "admin@example.com"
	cfg.SMTP.From = "from@example.com"
	gateway := NewGateway(cfg, store)
	cleanupGatewayForTest(t, gateway)
	for _, eventType := range []string{"debug_abuse", "donation_inactive"} {
		if err := store.SetAlertPref(eventType, true, false); err != nil {
			t.Fatalf("disable %s email: %v", eventType, err)
		}
	}
	return gateway, store
}

func alertCountForSubject(t *testing.T, store *db.Store, subjectID int64) int {
	t.Helper()
	alerts, _, err := store.ListAdminAlerts(1000, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts: %v", err)
	}
	count := 0
	for _, alert := range alerts {
		if alert.SubjectUserID != nil && *alert.SubjectUserID == subjectID {
			count++
		}
	}
	return count
}

func TestR05M1GatewayMailerCallbackLinearization(t *testing.T) {
	t.Run("callback before delete is cleaned", func(t *testing.T) {
		gateway, store := setupR05M1Gateway(t)
		u, err := store.CreateUser("r05m1-before", "before", "")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		gateway.mailer.DebugAbuse(u.Username, u.ID, 6, 10)
		if got := alertCountForSubject(t, store, u.ID); got != 1 {
			t.Fatalf("subject alerts before delete=%d, want 1", got)
		}
		if err := store.DeleteUser(u.ID); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		if got := alertCountForSubject(t, store, u.ID); got != 0 {
			t.Fatalf("subject alerts after delete=%d, want 0", got)
		}
	})

	t.Run("callback after delete is suppressed", func(t *testing.T) {
		gateway, store := setupR05M1Gateway(t)
		u, err := store.CreateUser("r05m1-after", "after", "")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := store.DeleteUser(u.ID); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		gateway.mailer.DebugAbuse(u.Username, u.ID, 6, 10)
		if got := alertCountForSubject(t, store, u.ID); got != 0 {
			t.Fatalf("late subject alerts=%d, want 0", got)
		}
	})

	t.Run("null subject event and show gate remain unchanged", func(t *testing.T) {
		gateway, store := setupR05M1Gateway(t)
		u, err := store.CreateUser("r05m1-null", "null", "")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		gateway.mailer.DonationInactive("general", "model", 42, 3)
		alerts, total, err := store.ListAdminAlerts(100, 0)
		if err != nil || total != 1 || len(alerts) != 1 || alerts[0].SubjectUserID != nil {
			t.Fatalf("null subject callback total=%d alerts=%+v err=%v", total, alerts, err)
		}
		if err := store.SetAlertPref("debug_abuse", false, false); err != nil {
			t.Fatalf("disable debug alert center: %v", err)
		}
		gateway.mailer.DebugAbuse(u.Username, u.ID, 6, 10)
		if _, total, err := store.ListAdminAlerts(100, 0); err != nil || total != 1 {
			t.Fatalf("show gate changed total=%d err=%v, want 1", total, err)
		}
	})
}

func TestR05M1GatewayMailerDeleteRace(t *testing.T) {
	gateway, store := setupR05M1Gateway(t)
	const iterations = 40
	for i := 0; i < iterations; i++ {
		u, err := store.CreateUser(fmt.Sprintf("r05m1-race-%d", i), fmt.Sprintf("race-%d", i), "")
		if err != nil {
			t.Fatalf("CreateUser(%d): %v", i, err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var deleteErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			deleteErr = store.DeleteUser(u.ID)
		}()
		go func() {
			defer wg.Done()
			<-start
			gateway.mailer.DebugAbuse(u.Username, u.ID, 6, 10)
		}()
		close(start)
		wg.Wait()
		if deleteErr != nil {
			t.Fatalf("DeleteUser(%d): %v", i, deleteErr)
		}
		if got, err := store.GetUserByID(u.ID); err != nil || got != nil {
			t.Fatalf("user %d after race = %+v err=%v, want deleted", u.ID, got, err)
		}
		if got := alertCountForSubject(t, store, u.ID); got != 0 {
			t.Fatalf("orphan subject alerts for user %d = %d", u.ID, got)
		}
	}

	alerts, _, err := store.ListAdminAlerts(1000, 0)
	if err != nil {
		t.Fatalf("ListAdminAlerts final: %v", err)
	}
	for _, alert := range alerts {
		if alert.SubjectUserID == nil {
			continue
		}
		user, err := store.GetUserByID(*alert.SubjectUserID)
		if err != nil {
			t.Fatalf("lookup subject %d: %v", *alert.SubjectUserID, err)
		}
		if user == nil {
			t.Fatalf("orphan subject alert at end: %+v", alert)
		}
	}
}
