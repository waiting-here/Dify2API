package db

import (
	"database/sql"
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
