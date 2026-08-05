// Package mailer provides email alert delivery for operational events
// (auto-bans, donation inactivations, admin login locks). When SMTP is not
// configured (SMTP_HOST empty), the entire mailer is a no-op.
package mailer

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"dify2api/config"
)

// Mailer buffers and sends email alerts. Nil means disabled (SMTP not configured).
type Mailer struct {
	cfg          config.SMTPConfig
	enabled      bool
	coolMinutes  func() int // dynamic getter for the aggregation window
	emailEnabled func(EventType) bool
	record       func(EventType, string) // optional alert-center sink
	mu           sync.Mutex
	coolers      map[EventType]*cooler
	sendFunc     func(config.SMTPConfig, string, string) error // injectable for tests
}

// Options wires the mailer to live settings. All fields are optional; nil
// callbacks keep the previous behavior (defaults on, static window).
type Options struct {
	// CoolMinutes returns the aggregation window in minutes (admin setting).
	CoolMinutes func() int
	// EmailEnabled reports whether the category may send email.
	EmailEnabled func(EventType) bool
	// Record, when set, is invoked for every queued event so the gateway can
	// write an alert-center record (subject to its own show_in_center gate).
	Record func(EventType, string)
}

// New creates a Mailer or returns nil when SMTP_HOST is empty.
func New(cfg config.SMTPConfig, opts Options) *Mailer {
	if strings.TrimSpace(cfg.Host) == "" {
		log.Printf("[MAILER] disabled (SMTP_HOST not set)")
		return nil
	}
	// Resolve TLS mode string for the startup log.
	tlsMode := cfg.TLS
	if tlsMode == "" {
		tlsMode = detectTLSMode(cfg.Port)
	}
	log.Printf("[MAILER] enabled: %s:%d → %s (TLS=%s)", cfg.Host, cfg.Port, cfg.To, tlsMode)

	coolMinutes := opts.CoolMinutes
	if coolMinutes == nil {
		coolMinutes = func() int { return 10 }
	}
	m := &Mailer{
		cfg:          cfg,
		enabled:      true,
		coolMinutes:  coolMinutes,
		emailEnabled: opts.EmailEnabled,
		record:       opts.Record,
		coolers:      make(map[EventType]*cooler),
		sendFunc:     sendSMTP,
	}
	return m
}

// Start is a no-op (each cooler launches its own timer goroutine on first
// event).  Kept for clarity: the caller can explicitly indicate that the
// mailer is ready.
func (m *Mailer) Start() {
	if m == nil {
		return
	}
	// No-op: coolers are self-starting.
}

// Enabled reports whether this mailer is ready to deliver.
func (m *Mailer) Enabled() bool {
	return m != nil && m.enabled
}

// UserAutoBanned queues a notification for a user auto-ban event.
func (m *Mailer) UserAutoBanned(username string, userID int64, banUntil time.Time, banHours int, violations int) {
	if m == nil {
		return
	}
	summary := fmt.Sprintf("%s（ID：%d）因 %d 次超限被自动封禁 %d 小时，至 %s",
		username, userID, violations, banHours, banUntil.Format("15:04:05"))
	m.queue(EventUserAutoBanned, summary)
}

// DonationInactive queues a notification for donation auto-inactivation.
func (m *Mailer) DonationInactive(service, model string, donationID int64, consecutiveFailures int) {
	if m == nil {
		return
	}
	summary := fmt.Sprintf("捐赠条目 %d（服务 %s，模型 %s）连续 %d 次失败后自动转为未激活",
		donationID, service, model, consecutiveFailures)
	m.queue(EventDonationInactive, summary)
}

// PricingMissing queues a notification when a charity request finds pricing
// is missing for a (service, model) pair that has active donations.
func (m *Mailer) PricingMissing(service, model string) {
	if m == nil {
		return
	}
	summary := fmt.Sprintf("公益模型 (%s, %s) 存在捐赠条目但缺少定价配置", service, model)
	m.queue(EventPricingMissing, summary)
}

// DebugAbuse queues a notification for user debug abuse detection.
func (m *Mailer) DebugAbuse(username string, userID int64, sessionCount int, windowMinutes int) {
	if m == nil {
		return
	}
	summary := fmt.Sprintf("%s（ID：%d）在 %d 分钟内开启了 %d 次 Debug session",
		username, userID, windowMinutes, sessionCount)
	m.queue(EventDebugAbuse, summary)
}

// AdminLoginLocked queues a notification for admin login lockout.
func (m *Mailer) AdminLoginLocked(ip string, lockUntil time.Time) {
	if m == nil {
		return
	}
	summary := fmt.Sprintf("IP %s 因登录失败次数过多被锁定，至 %s",
		ip, lockUntil.Format("15:04:05"))
	m.queue(EventAdminLoginLocked, summary)
}

func (m *Mailer) queue(et EventType, summary string) {
	// Per-category email switch (admin alert prefs). Off categories are
	// dropped entirely: no email, no record.
	if m.emailEnabled != nil && !m.emailEnabled(et) {
		return
	}
	// Alert-center record (its own show_in_center gate lives in the store).
	if m.record != nil {
		m.record(et, summary)
	}
	m.mu.Lock()
	c, ok := m.coolers[et]
	if !ok {
		c = newCooler(et, m.coolMinutes, m.cfg, m.sendFunc)
		m.coolers[et] = c
	}
	m.mu.Unlock()
	c.add(summary)
}

// detectTLSMode returns a human-readable label for the auto-detected TLS
// mode (used in startup logging only).
func detectTLSMode(port int) string {
	if port == 465 {
		return "implicit (auto)"
	}
	return "starttls (auto)"
}

// smtpHostForAuth strips the port from an address like "smtp.example.com:587"
// and returns just the hostname, as required by smtp.PlainAuth.
func smtpHostForAuth(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
