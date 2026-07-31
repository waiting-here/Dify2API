package db

import (
	"database/sql"
	"strconv"
	"time"
)

// Setting keys used by the gateway.
const (
	SettingGuildID = "discord_guild_id"
	SettingRoleID  = "discord_role_id"
	// Charity global switch (alpha.3 F1).
	SettingCharityGlobalEnabled = "charity_global_enabled"
	// alpha.3 — three-class RPM global defaults.
	SettingRPMLimitA         = "rpm_limit_a"         // class A: transfer complete (default 6)
	SettingRPMLimitB         = "rpm_limit_b"         // class B: request success (default 12)
	SettingRPMLimitC         = "rpm_limit_c"         // class C: request received (default 18)
	SettingRPMViolationLimit = "rpm_violation_limit" // violations before auto-ban (default 5)
	SettingRPMBanHours       = "rpm_ban_hours"       // auto-ban duration in hours (default 24)
	// App probe per-user cap (per-minute sliding window; admin-tunable).
	SettingProbeLimitPerUser = "probe_limit_per_user" // Dify compatibility probes per user per minute (default 5)
	SettingCheckinMin        = "checkin_min"          // check-in credit lower bound (default 45)
	SettingCheckinMax        = "checkin_max"          // check-in credit upper bound (default 55)
	SettingCreditsCap        = "credits_cap"          // check-in credits ceiling (default 250)
	// alpha.3 — tunable thresholds.
	SettingDonationFailLimit = "donation_fail_limit" // consecutive failures before auto-inactive (default 10)
	SettingMailerCoolMinutes = "mailer_cool_minutes" // email aggregation window in minutes (default 10)
	// alpha.4 — split charity switches (donation / charity routing).
	SettingDonationEnabled     = "donation_enabled"      // allow user donation submissions (default false)
	SettingCharityEnabled      = "charity_enabled"       // enable charity routing (default false)
	SettingDonationReviewLimit = "donation_review_limit" // pending application cap per user (default 3)
	// alpha.4 — maintenance mode.
	SettingMaintenanceMode = "maintenance_mode" // site-wide maintenance toggle (default false)
)

// Global defaults for the three-class RPM system (alpha.3 F4).
const (
	DefaultRPMLimitA         = 6  // class A: transfer complete (excluding failures)
	DefaultRPMLimitB         = 12 // class B: request success (§1.2 口径)
	DefaultRPMLimitC         = 18 // class C: request received (post-auth)
	DefaultRPMViolationLimit = 5  // violations within 24h before auto-ban
	DefaultRPMBanHours       = 24 // auto-ban duration in hours
)

// Global default for the App probe per-user cap.
const DefaultProbeLimitPerUser = 5

// Global defaults for the check-in system (alpha.3 F2).
const (
	DefaultCheckinMin = 45  // minimum credits awarded per successful check-in
	DefaultCheckinMax = 55  // maximum credits awarded per successful check-in
	DefaultCreditsCap = 250 // check-in fails when credits >= this cap
)

// Global defaults for tunable thresholds.
const (
	DefaultDonationFailLimit   = 10 // consecutive failures before auto-inactive
	DefaultDonationReviewLimit = 3  // pending donation application cap per user
	DefaultMailerCoolMinutes   = 10 // email aggregation window in minutes
)

// GetSettingInt returns the setting parsed as a positive integer, falling
// back to the given default when unset, invalid, or < 1.
func (s *Store) GetSettingInt(key string, fallback int) int {
	v, err := s.GetSetting(key)
	if err != nil || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// GetSettingIntAllowZero is like GetSettingInt but permits 0 as a valid
// value (used for credits_cap, where 0 means "check-in disabled").
func (s *Store) GetSettingIntAllowZero(key string, fallback int) int {
	v, err := s.GetSetting(key)
	if err != nil || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// GetSetting returns a setting value ("" when unset).
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// GetSettingString returns a setting value, falling back to the given default
// when unset or on error (no-op convenience wrapper).
func (s *Store) GetSettingString(key string, fallback string) string {
	v, err := s.GetSetting(key)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

// SetSetting upserts a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// CountRecentErrors counts a user's request logs with the given error code
// since the given time (used for the RPM-violation auto-ban rule).
func (s *Store) CountRecentErrors(userID int64, errorCode string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM request_logs WHERE user_id=? AND error_code=? AND started_at>=?`,
		userID, errorCode, since.Unix(),
	).Scan(&n)
	return n, err
}
