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
	SettingCheckinMin        = "checkin_min"          // check-in credit lower bound (default 140)
	SettingCheckinMax        = "checkin_max"          // check-in credit upper bound (default 160)
	SettingCreditsCap        = "credits_cap"          // check-in credits ceiling (default 500)
	// alpha.3 — tunable thresholds.
	SettingDonationFailLimit = "donation_fail_limit" // consecutive failures before auto-inactive (default 10)
	SettingMailerCoolMinutes = "mailer_cool_minutes" // email aggregation window in minutes (default 10)
	// alpha.4 — split charity switches (donation / charity routing).
	SettingDonationEnabled     = "donation_enabled"      // allow user donation submissions (default false)
	SettingCharityEnabled      = "charity_enabled"       // enable charity routing (default false)
	SettingDonationReviewLimit = "donation_review_limit" // pending application cap per user (default 3)
	// alpha.4 — maintenance mode.
	SettingMaintenanceMode = "maintenance_mode" // site-wide maintenance toggle (default false)
	// R-A user levels: donation-credit thresholds (lower-closed) and the
	// five custom level names plus the level-2 banner text. Empty names fall
	// back to the numeric level; an empty banner renders no banner.
	SettingLevelThreshold2 = "level_threshold_2"
	SettingLevelThreshold3 = "level_threshold_3"
	SettingLevelThreshold4 = "level_threshold_4"
	SettingLevelName1      = "level_name_1"
	SettingLevelName2      = "level_name_2"
	SettingLevelName3      = "level_name_3"
	SettingLevelName4      = "level_name_4"
	SettingLevelName5      = "level_name_5"
	SettingLevelBannerText = "level_banner_text"
	// v1.4.0 — mini-games (fishing ships first; keys are per-game prefixed
	// to leave room for future games without schema changes).
	SettingGamesEnabled = "games_enabled" // master switch for all mini-games
	// fishing economy (admin-tunable; defaults match the frozen design).
	SettingGameFishingEnabled          = "game_fishing_enabled"
	SettingGameFishingBaitWormPrice    = "game_fishing_bait_worm_price"
	SettingGameFishingBaitLurePrice    = "game_fishing_bait_lure_price"
	SettingGameFishingBaitPremiumPrice = "game_fishing_bait_premium_price"
	SettingGameFishingRTP              = "game_fishing_rtp"
	SettingGameFishingRTPPremium       = "game_fishing_rtp_premium"
	SettingGameFishingTreasureBottle   = "game_fishing_treasure_bottle_mult"
	SettingGameFishingTreasureClover   = "game_fishing_treasure_clover_mult"
	SettingGameFishingTreasureShell    = "game_fishing_treasure_shell_mult"
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
	DefaultCheckinMin = 140 // minimum credits awarded per successful check-in
	DefaultCheckinMax = 160 // maximum credits awarded per successful check-in
	DefaultCreditsCap = 500 // check-in fails when credits >= this cap
)

// Global defaults for tunable thresholds.
const (
	DefaultDonationFailLimit   = 10 // consecutive failures before auto-inactive
	DefaultDonationReviewLimit = 3  // pending donation application cap per user
	DefaultMailerCoolMinutes   = 10 // email aggregation window in minutes
)

// Global defaults for the R-A level thresholds (lower-closed intervals):
// < t2 -> level 1; [t2, t3) -> 2; [t3, t4) -> 3; >= t4 -> 4. Level 5 is
// manual-only. t2 may be 0 (every non-negative credit is level 2+), so the
// getter must permit zero.
const (
	DefaultLevelThreshold2 = 1
	DefaultLevelThreshold3 = 100
	DefaultLevelThreshold4 = 500
)

// Global defaults for the v1.4.0 mini-game system (frozen design: bait
// prices 5/10/15, RTP 90% / premium 88%, treasures 2x/3x/5x).
const (
	DefaultGamesEnabled = true

	DefaultGameFishingEnabled          = true
	DefaultGameFishingBaitWormPrice    = 5
	DefaultGameFishingBaitLurePrice    = 10
	DefaultGameFishingBaitPremiumPrice = 15
	DefaultGameFishingRTP              = 90
	DefaultGameFishingRTPPremium       = 88
	DefaultGameFishingTreasureBottle   = 2
	DefaultGameFishingTreasureClover   = 3
	DefaultGameFishingTreasureShell    = 5
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
// value for settings where zero has an explicit meaning.
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

// SettingUpdate is one explicitly supplied setting value in an atomic update.
type SettingUpdate struct {
	Key   string
	Value string
}

// SetSettings applies all explicitly supplied values in one transaction.
// Callers must validate the complete request before calling this method.
func (s *Store) SetSettings(updates []SettingUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			update.Key, update.Value,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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
