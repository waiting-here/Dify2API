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
	// alpha.3 — three-class RPM global defaults.
	SettingRPMLimitA         = "rpm_limit_a"         // class A: transfer complete (default 6)
	SettingRPMLimitB         = "rpm_limit_b"         // class B: request success (default 12)
	SettingRPMLimitC         = "rpm_limit_c"         // class C: request received (default 18)
	SettingRPMViolationLimit = "rpm_violation_limit" // violations before auto-ban (default 5)
	SettingRPMBanHours       = "rpm_ban_hours"       // auto-ban duration in hours (default 24)
	SettingCheckinMin        = "checkin_min"         // check-in credit lower bound (default 10)
	SettingCheckinMax        = "checkin_max"         // check-in credit upper bound (default 20)
	SettingCreditsCap        = "credits_cap"         // check-in credits ceiling (default 50)
)

// Global defaults for the three-class RPM system (alpha.3 F4).
const (
	DefaultRPMLimitA         = 6  // class A: transfer complete (excluding failures)
	DefaultRPMLimitB         = 12 // class B: request success (§1.2 口径)
	DefaultRPMLimitC         = 18 // class C: request received (post-auth)
	DefaultRPMViolationLimit = 5  // violations within 24h before auto-ban
	DefaultRPMBanHours       = 24 // auto-ban duration in hours
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

// GetSetting returns a setting value ("" when unset).
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
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
