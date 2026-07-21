package db

import (
	"database/sql"
	"strconv"
	"time"
)

// Setting keys used by the gateway.
const (
	SettingGuildID  = "discord_guild_id"
	SettingRoleID   = "discord_role_id"
	SettingRPMLimit = "rpm_limit"
)

// DefaultGlobalRPM is the global requests-per-minute cap when unset (3).
const DefaultGlobalRPM = 3

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

// GetGlobalRPM returns the global RPM cap (DefaultGlobalRPM when unset or invalid).
func (s *Store) GetGlobalRPM() int {
	v, err := s.GetSetting(SettingRPMLimit)
	if err != nil || v == "" {
		return DefaultGlobalRPM
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return DefaultGlobalRPM
	}
	return n
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
