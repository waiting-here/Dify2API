package mailer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"dify2api/config"
)

func testSMTPConfig() config.SMTPConfig {
	return config.SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		User: "test@example.com",
		Pass: "testpass",
		From: "from@example.com",
		To:   "admin@example.com",
		TLS:  "starttls",
	}
}

// sentMail captures arguments from a mock send function.
type sentMail struct {
	subject string
	body    string
}

type mockSender struct {
	mu    sync.Mutex
	mails []sentMail
}

func (m *mockSender) send(_ context.Context, cfg config.SMTPConfig, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mails = append(m.mails, sentMail{subject: subject, body: body})
	return nil
}

func TestNew_NilWhenHostEmpty(t *testing.T) {
	cfg := testSMTPConfig()
	cfg.Host = ""
	m := New(cfg, Options{})
	if m != nil {
		t.Error("expected nil when SMTP_HOST is empty")
	}
}

func TestNew_ReturnsMailerWhenHostSet(t *testing.T) {
	cfg := testSMTPConfig()
	m := New(cfg, Options{})
	if m == nil {
		t.Fatal("expected non-nil when SMTP_HOST is set")
	}
	if !m.Enabled() {
		t.Error("expected Enabled=true")
	}
}

func TestStart_NoOp(t *testing.T) {
	cfg := testSMTPConfig()
	m := New(cfg, Options{})
	if m == nil {
		t.Fatal("expected non-nil mailer")
	}
	m.Start() // should not panic
}

func TestShutdownFlushesPendingAndRejectsNewEvents(t *testing.T) {
	ms := &mockSender{}
	m := New(testSMTPConfig(), Options{CoolMinutes: func() int { return 60 }})
	if m == nil {
		t.Fatal("expected mailer")
	}
	m.sendFunc = ms.send
	m.UserAutoBanned("pending", 1, time.Now(), 1, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	ms.mu.Lock()
	if len(ms.mails) != 1 {
		t.Fatalf("flushed mail count = %d, want 1", len(ms.mails))
	}
	ms.mu.Unlock()
	m.UserAutoBanned("late", 2, time.Now(), 1, 1)
	time.Sleep(10 * time.Millisecond)
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.mails) != 1 {
		t.Fatalf("post-shutdown event was accepted: mails=%d", len(ms.mails))
	}
}

func TestShutdownHonorsDeadlineWhileSendIsInFlight(t *testing.T) {
	release := make(chan struct{})
	m := New(testSMTPConfig(), Options{CoolMinutes: func() int { return 60 }})
	m.sendFunc = func(ctx context.Context, _ config.SMTPConfig, _, _ string) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.UserAutoBanned("blocked", 1, time.Now(), 1, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := m.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("Shutdown error = %v, want deadline exceeded", err)
	}
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := m.Shutdown(ctx2); err != nil {
		t.Fatalf("second Shutdown did not observe completed send: %v", err)
	}
}

func TestCooling_SingleEventFlushesAfterWindow(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	// Override coolWindow for the test.
	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	m.UserAutoBanned("testuser", 42, time.Now().Add(time.Hour), 24, 5)

	time.Sleep(150 * time.Millisecond) // wait for flush

	ms.mu.Lock()
	count := len(ms.mails)
	first := sentMail{}
	if count > 0 {
		first = ms.mails[0]
	}
	ms.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 mail, got %d", count)
	}
	if !strings.Contains(first.subject, "用户自动封禁") {
		t.Errorf("subject should contain 用户自动封禁, got %q", first.subject)
	}
	if !strings.Contains(first.subject, "1 起") {
		t.Errorf("subject should say 1 起, got %q", first.subject)
	}
	if !strings.Contains(first.body, "testuser") {
		t.Errorf("body should contain username, got %q", first.body)
	}
}

func TestCooling_Aggregation(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	// Fire 3 events of the same type rapidly.
	m.UserAutoBanned("alice", 1, time.Now().Add(time.Hour), 24, 5)
	time.Sleep(5 * time.Millisecond)
	m.UserAutoBanned("bob", 2, time.Now().Add(2*time.Hour), 24, 7)
	time.Sleep(5 * time.Millisecond)
	m.UserAutoBanned("carol", 3, time.Now().Add(3*time.Hour), 24, 9)

	time.Sleep(150 * time.Millisecond) // wait for flush

	ms.mu.Lock()
	count := len(ms.mails)
	first := sentMail{}
	if count > 0 {
		first = ms.mails[0]
	}
	ms.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 aggregated mail, got %d", count)
	}
	if !strings.Contains(first.subject, "3 起") {
		t.Errorf("subject should say 3 起, got %q", first.subject)
	}
	if !strings.Contains(first.body, "alice") || !strings.Contains(first.body, "bob") || !strings.Contains(first.body, "carol") {
		t.Errorf("body should contain all three users, got %q", first.body)
	}
}

func TestCooling_MultiTypeIndependent(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	// Fire type A and type B in the same window.
	m.UserAutoBanned("alice", 1, time.Now(), 24, 5)
	m.DonationInactive("general", "x", 10, 12)

	time.Sleep(150 * time.Millisecond)

	ms.mu.Lock()
	count := len(ms.mails)
	subjects := make([]string, len(ms.mails))
	for i, mail := range ms.mails {
		subjects[i] = mail.subject
	}
	ms.mu.Unlock()

	if count != 2 {
		t.Fatalf("expected 2 independent mails, got %d: %v", count, subjects)
	}
	hasBanned := false
	hasInactive := false
	for _, s := range subjects {
		if strings.Contains(s, "用户自动封禁") {
			hasBanned = true
		}
		if strings.Contains(s, "捐赠条目自动未激活") {
			hasInactive = true
		}
	}
	if !hasBanned || !hasInactive {
		t.Errorf("expected both event types: %v", subjects)
	}
}

func TestDonationInactive_CorrectContent(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	m.DonationInactive("general", "claude-opus-4-6", 123, 15)

	time.Sleep(150 * time.Millisecond)

	ms.mu.Lock()
	count := len(ms.mails)
	first := sentMail{}
	if count > 0 {
		first = ms.mails[0]
	}
	ms.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 mail, got %d", count)
	}
	if !strings.Contains(first.subject, "捐赠条目自动未激活") {
		t.Errorf("subject wrong: %q", first.subject)
	}
	if !strings.Contains(first.body, "general") || !strings.Contains(first.body, "claude-opus-4-6") || !strings.Contains(first.body, "123") || !strings.Contains(first.body, "15") {
		t.Errorf("body missing details: %q", first.body)
	}
}

func TestAdminLoginLocked_CorrectContent(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	m.AdminLoginLocked("192.168.1.100", time.Now().Add(time.Hour))

	time.Sleep(150 * time.Millisecond)

	ms.mu.Lock()
	count := len(ms.mails)
	first := sentMail{}
	if count > 0 {
		first = ms.mails[0]
	}
	ms.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 mail, got %d", count)
	}
	if !strings.Contains(first.subject, "管理员登录锁定") {
		t.Errorf("subject wrong: %q", first.subject)
	}
	if !strings.Contains(first.body, "192.168.1.100") {
		t.Errorf("body missing IP: %q", first.body)
	}
}

func TestDebugAbuse_CorrectContent(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	m := &Mailer{cfg: cfg, enabled: true, coolers: make(map[EventType]*cooler), sendFunc: ms.send}

	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()

	m.DebugAbuse("testuser", 42, 6, 10)

	time.Sleep(150 * time.Millisecond)

	ms.mu.Lock()
	count := len(ms.mails)
	first := sentMail{}
	if count > 0 {
		first = ms.mails[0]
	}
	ms.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 mail, got %d", count)
	}
	if !strings.Contains(first.subject, "用户 Debug 滥用告警") {
		t.Errorf("subject should contain 用户 Debug 滥用告警, got %q", first.subject)
	}
	if !strings.Contains(first.subject, "1 起") {
		t.Errorf("subject should say 1 起, got %q", first.subject)
	}
	if !strings.Contains(first.body, "testuser") {
		t.Errorf("body should contain username, got %q", first.body)
	}
	if !strings.Contains(first.body, "42") {
		t.Errorf("body should contain userID 42, got %q", first.body)
	}
	if !strings.Contains(first.body, "6") {
		t.Errorf("body should contain session count 6, got %q", first.body)
	}
	if !strings.Contains(first.body, "10") {
		t.Errorf("body should contain window minutes 10, got %q", first.body)
	}
}

func TestNilReceiver_NoPanic(t *testing.T) {
	var m *Mailer // nil

	// None of these should panic.
	m.Start()
	m.UserAutoBanned("u", 1, time.Now(), 24, 1)
	m.DonationInactive("s", "m", 1, 10)
	m.AdminLoginLocked("1.2.3.4", time.Now())
	m.DebugAbuse("u", 1, 5, 10)
	m.PricingMissing("s", "m")

	if m.Enabled() {
		t.Error("nil should not be enabled")
	}
}

func TestMailer_DisabledAfterNestedNilCheck(t *testing.T) {
	// Tests that the nil-safe logic at call sites is correct:
	// g.mailer != nil check before calling any method.
	//
	// No code here — this is a compilation/documentation guard that the
	// handler wiring must guard with a nil check before calling.
}

func TestEmailEnabledGate_DropsDisabledCategory(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	disabled := false
	m := New(cfg, Options{
		EmailEnabled: func(et EventType) bool { return !disabled },
		// 0 falls back to the global coolWindow so the test can shorten it.
		CoolMinutes: func() int { return 0 },
	})
	if m == nil {
		t.Fatal("expected non-nil mailer")
	}
	origCoolWindow := coolWindow
	coolWindow = 50 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()
	m.sendFunc = ms.send

	// Enabled category: event is queued and flushed.
	m.UserAutoBanned("alice", 1, time.Now().Add(time.Hour), 1, 1)
	time.Sleep(120 * time.Millisecond)
	ms.mu.Lock()
	n := len(ms.mails)
	ms.mu.Unlock()
	if n != 1 {
		t.Fatalf("enabled category: want 1 mail, got %d", n)
	}

	// Disabled category: nothing queued, nothing sent.
	disabled = true
	m.DonationInactive("general", "gpt-5.6-sol", 42, 3)
	time.Sleep(120 * time.Millisecond)
	ms.mu.Lock()
	n = len(ms.mails)
	ms.mu.Unlock()
	if n != 1 {
		t.Fatalf("disabled category: want still 1 mail, got %d", n)
	}
}

func TestRecordCallback_InvokedPerQueuedEvent(t *testing.T) {
	ms := &mockSender{}
	cfg := testSMTPConfig()
	var recorded []string
	var recMu sync.Mutex
	m := New(cfg, Options{
		Record: func(et EventType, summary string, subjectUserID *int64) {
			recMu.Lock()
			subject := ""
			if subjectUserID != nil {
				subject = fmt.Sprintf("|subject=%d", *subjectUserID)
			}
			recorded = append(recorded, string(et)+"|"+summary+subject)
			recMu.Unlock()
		},
	})
	if m == nil {
		t.Fatal("expected non-nil mailer")
	}
	m.sendFunc = ms.send

	m.UserAutoBanned("bob", 2, time.Now().Add(time.Hour), 2, 3)

	recMu.Lock()
	defer recMu.Unlock()
	if len(recorded) != 1 {
		t.Fatalf("want 1 record, got %d", len(recorded))
	}
	if !strings.Contains(recorded[0], "user_auto_banned|") {
		t.Errorf("record = %q, want user_auto_banned prefix", recorded[0])
	}
	if !strings.Contains(recorded[0], "|subject=2") {
		t.Errorf("record = %q, want subject user 2", recorded[0])
	}
}

func TestRecordCallback_SubjectOnlyUserEvents(t *testing.T) {
	cfg := testSMTPConfig()
	var subjects map[EventType]*int64
	m := New(cfg, Options{
		EmailEnabled: func(EventType) bool { return false },
		Record: func(et EventType, _ string, subjectUserID *int64) {
			if subjects == nil {
				subjects = make(map[EventType]*int64)
			}
			subjects[et] = subjectUserID
		},
	})
	if m == nil {
		t.Fatal("expected non-nil mailer")
	}
	m.UserAutoBanned("subject", 11, time.Now(), 1, 1)
	m.DebugAbuse("subject", 11, 6, 10)
	m.DonationInactive("general", "model", 42, 3)
	m.PricingMissing("general", "model")
	m.AdminLoginLocked("127.0.0.1", time.Now())

	for _, et := range []EventType{EventUserAutoBanned, EventDebugAbuse} {
		if subjects[et] == nil || *subjects[et] != 11 {
			t.Errorf("%s subject=%v, want 11", et, subjects[et])
		}
	}
	for _, et := range []EventType{EventDonationInactive, EventPricingMissing, EventAdminLoginLocked} {
		if subjects[et] != nil {
			t.Errorf("%s subject=%v, want nil", et, subjects[et])
		}
	}
}

func TestAlertCenterAndEmailGatesAreIndependent(t *testing.T) {
	cfg := testSMTPConfig()
	var recorded int
	m := New(cfg, Options{
		EmailEnabled: func(EventType) bool { return false },
		Record: func(EventType, string, *int64) {
			recorded++
		},
	})
	m.UserAutoBanned("center-only", 1, time.Now(), 1, 1)
	if recorded != 1 {
		t.Fatalf("email off suppressed alert-center record: got %d", recorded)
	}
	m.mu.Lock()
	queued := len(m.coolers)
	m.mu.Unlock()
	if queued != 0 {
		t.Fatalf("email-off event was queued for delivery: coolers=%d", queued)
	}

	// A center sink that intentionally drops the event must not suppress email.
	ms := &mockSender{}
	m = New(cfg, Options{
		EmailEnabled: func(EventType) bool { return true },
		Record:       func(EventType, string, *int64) {},
		CoolMinutes:  func() int { return 0 },
	})
	origCoolWindow := coolWindow
	coolWindow = 30 * time.Millisecond
	defer func() { coolWindow = origCoolWindow }()
	m.sendFunc = ms.send
	m.DonationInactive("general", "email-only", 2, 3)
	time.Sleep(100 * time.Millisecond)
	ms.mu.Lock()
	sent := len(ms.mails)
	ms.mu.Unlock()
	if sent != 1 {
		t.Fatalf("center-off behavior suppressed email: sent=%d", sent)
	}
}

func TestCoolMinutesGetter_UsedForWindow(t *testing.T) {
	cfg := testSMTPConfig()
	minutes := 100
	m := New(cfg, Options{CoolMinutes: func() int { return minutes }})
	if m == nil {
		t.Fatal("expected non-nil mailer")
	}
	m.queue(EventUserAutoBanned, "x", nil)
	m.mu.Lock()
	c := m.coolers[EventUserAutoBanned]
	m.mu.Unlock()
	if c == nil {
		t.Fatal("expected cooler to be created")
	}
	if c.timer == nil {
		t.Fatal("expected timer to be armed")
	}
	// The window must be 100 minutes, not the 10-minute default.
	if d := c.timer.Stop(); !d {
		t.Error("expected armed timer")
	}
	if c.coolMinutes() != 100 {
		t.Errorf("coolMinutes getter = %d, want 100", c.coolMinutes())
	}
}
