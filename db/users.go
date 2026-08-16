package db

import (
	"database/sql"
	"fmt"
	"time"
)

// AdminDiscordID is the sentinel discord_id for the administrator row.
const AdminDiscordID = "__admin__"

// User is a registered account (Discord-authenticated, or the admin row).
type User struct {
	ID          int64
	DiscordID   string
	Username    string
	Avatar      string
	IsAdmin     bool
	Disabled    bool
	BannedUntil int64  `json:"banned_until"` // unix ts; 0 = no timed ban; ban lapses automatically once past
	AutoBanned  bool   `json:"auto_banned"`  // true when RPM-violation auto-ban
	BanReason   string `json:"ban_reason"`   // admin-supplied reason (empty for auto-bans)
	// Credits is the user's public-service credit balance.
	Credits int `json:"credits"`
	// LastCheckinDay is the normalised date of the last successful check-in
	// (e.g. "2026-07-24"), used for once-per-day enforcement. Empty when
	// the user has never checked in.
	LastCheckinDay string `json:"last_checkin_day"`
	// Per-user overrides for three-class RPM (S2). nil means "use global".
	RPMLimitA sql.NullInt64
	RPMLimitB sql.NullInt64
	RPMLimitC sql.NullInt64
	// DonationCredit is the number of successful donations bound to this
	// user (admin-visible only, see §1.1).
	DonationCredit int `json:"donation_credit"`
	// CharityEnabled is the user-side public-resource opt-in switch (§1.3).
	CharityEnabled bool `json:"charity_enabled"`
	// Lang is the user's preferred UI language ("zh" or "en"). Empty means unset.
	Lang string `json:"lang"`
	// Level is the manual level override (1-5), nil when the level is
	// automatic (computed lazily from donation_credit + thresholds).
	Level *int `json:"level"`

	CreatedAt int64
	UpdatedAt int64
}

// IsBanned reports whether the user is currently banned: either permanently
// disabled or inside a timed-ban window.
func IsBanned(u *User) bool {
	return u.Disabled || (u.BannedUntil > 0 && time.Now().Unix() < u.BannedUntil)
}

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var isAdmin, disabled, autoBanned, charityEnabled int
	var level sql.NullInt64
	err := row.Scan(&u.ID, &u.DiscordID, &u.Username, &u.Avatar, &isAdmin, &disabled, &u.BannedUntil, &autoBanned, &u.BanReason, &u.Credits, &u.LastCheckinDay, &u.RPMLimitA, &u.RPMLimitB, &u.RPMLimitC, &u.DonationCredit, &charityEnabled, &u.Lang, &u.CreatedAt, &u.UpdatedAt, &level)
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin != 0
	u.Disabled = disabled != 0
	u.AutoBanned = autoBanned != 0
	u.CharityEnabled = charityEnabled != 0
	if level.Valid {
		v := int(level.Int64)
		u.Level = &v
	}
	return &u, nil
}

const userCols = "id, discord_id, username, avatar, is_admin, disabled, banned_until, auto_banned, ban_reason, credits, last_checkin_day, rpm_limit_a, rpm_limit_b, rpm_limit_c, donation_credit, charity_enabled, lang, created_at, updated_at, level"

// GetUserByID fetches a user by primary key. Returns (nil, nil) when absent.
func (s *Store) GetUserByID(id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// GetUserByDiscordID fetches a user by Discord ID. Returns (nil, nil) when absent.
func (s *Store) GetUserByDiscordID(discordID string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE discord_id=?`, discordID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// CreateUser inserts a Discord-registered user.
func (s *Store) CreateUser(discordID, username, avatar string) (*User, error) {
	nowTime := time.Now()
	now := nowTime.Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO users (discord_id, username, avatar, created_at, updated_at) VALUES (?,?,?,?,?)`,
		discordID, username, avatar, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if err := recordNewUserTx(tx, nowTime); err != nil {
		return nil, fmt.Errorf("create user activity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return s.GetUserByID(id)
}

// EnsureAdminUser upserts the administrator row (username follows the startup
// file, which is authoritative on every boot) and returns it.
func (s *Store) EnsureAdminUser(username string) (*User, error) {
	u, err := s.GetUserByDiscordID(AdminDiscordID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if u == nil {
		res, err := s.db.Exec(
			`INSERT INTO users (discord_id, username, is_admin, created_at, updated_at) VALUES (?,?,1,?,?)`,
			AdminDiscordID, username, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("create admin user: %w", err)
		}
		id, _ := res.LastInsertId()
		return s.GetUserByID(id)
	}
	if u.Username != username || !u.IsAdmin {
		if _, err := s.db.Exec(`UPDATE users SET username=?, is_admin=1, updated_at=? WHERE id=?`, username, now, u.ID); err != nil {
			return nil, fmt.Errorf("update admin user: %w", err)
		}
		u.Username = username
		u.IsAdmin = true
	}
	return u, nil
}

// BanUser sets a timed ban with an optional admin-supplied reason.
// Permanent bans use SetUserDisabled.
func (s *Store) BanUser(id int64, until time.Time, reason string) error {
	_, err := s.db.Exec(`UPDATE users SET banned_until=?, ban_reason=?, updated_at=? WHERE id=? AND is_admin=0`, until.Unix(), reason, time.Now().Unix(), id)
	return err
}

// AutoBanUser sets a timed ban triggered by RPM violations.
func (s *Store) AutoBanUser(id int64, until time.Time) error {
	_, err := s.db.Exec(`UPDATE users SET banned_until=?, auto_banned=1, ban_reason='RPM 超限自动封禁 (24h)', updated_at=? WHERE id=? AND is_admin=0`, until.Unix(), time.Now().Unix(), id)
	return err
}

// UnbanUser clears both the timed ban, the permanent disabled flag,
// the auto-ban flag, and the ban reason.
func (s *Store) UnbanUser(id int64) error {
	return s.unbanUserAt(id, time.Now())
}

func (s *Store) unbanUserAt(id int64, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var disabled int
	var createdAt int64
	if err := tx.QueryRow(`SELECT disabled,created_at FROM users WHERE id=? AND is_admin=0`, id).Scan(&disabled, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if disabled != 0 {
		if err := s.finalizeCompletedActivityDaysTx(tx, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE users SET banned_until=0, disabled=0, auto_banned=0, ban_reason='', updated_at=? WHERE id=? AND is_admin=0`, now.Unix(), id); err != nil {
		return err
	}
	if disabled != 0 {
		day := utcDay(now)
		if err := adjustOpenSiteForUserTx(tx, id, day, 1, utcDay(time.Unix(createdAt, 0)) == day); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetUserLang updates the user's preferred UI language.
func (s *Store) SetUserLang(id int64, lang string) error {
	_, err := s.db.Exec(`UPDATE users SET lang=?, updated_at=? WHERE id=?`, lang, time.Now().Unix(), id)
	return err
}

// SetUserDisabled marks a user disabled/enabled with an optional reason.
func (s *Store) SetUserDisabled(id int64, disabled bool, reason string) error {
	return s.setUserDisabledAt(id, disabled, reason, time.Now())
}

func (s *Store) setUserDisabledAt(id int64, disabled bool, reason string, now time.Time) error {
	v := 0
	if disabled {
		v = 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldDisabled, isAdmin int
	var createdAt int64
	if err := tx.QueryRow(`SELECT disabled,is_admin,created_at FROM users WHERE id=?`, id).Scan(&oldDisabled, &isAdmin, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if isAdmin == 0 && oldDisabled != v {
		if err := s.finalizeCompletedActivityDaysTx(tx, now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE users SET disabled=?, ban_reason=?, updated_at=? WHERE id=?`, v, reason, now.Unix(), id); err != nil {
		return err
	}
	if isAdmin == 0 && oldDisabled != v {
		day := utcDay(now)
		direction := int64(1)
		if disabled {
			direction = -1
		}
		if err := adjustOpenSiteForUserTx(tx, id, day, direction, utcDay(time.Unix(createdAt, 0)) == day); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListUsers returns all non-admin users, newest first.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users WHERE is_admin=0 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserCredits directly sets the user's credit balance.
func (s *Store) SetUserCredits(userID int64, credits int) error {
	_, err := s.db.Exec(`UPDATE users SET credits=?, updated_at=? WHERE id=?`, credits, time.Now().Unix(), userID)
	return err
}

// AdjustUserCredits atomically adds delta to credits and returns the new value.
// Negative values are allowed (admin deduction).
func (s *Store) AdjustUserCredits(userID int64, delta int) (int, error) {
	res, err := s.db.Exec(`UPDATE users SET credits=credits+?, updated_at=? WHERE id=?`, delta, time.Now().Unix(), userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, fmt.Errorf("user %d not found", userID)
	}
	var newVal int
	if err := s.db.QueryRow(`SELECT credits FROM users WHERE id=?`, userID).Scan(&newVal); err != nil {
		return 0, err
	}
	return newVal, nil
}

const (
	CheckinApplied = "applied"
	CheckinAlready = "already"
	CheckinCapped  = "capped"
)

// ApplyUserCheckin serializes the day check and an incremental credit
// award in one transaction. It cannot overwrite a concurrent charity debit.
// The award is NOT clamped to the cap: a check-in may push the balance
// above credits_cap (e.g. 499 + 150 with cap 500). The cap only gates
// check-in initiation: when credits >= cap the check-in is refused
// (CheckinCapped) and no day is consumed. bypassCap skips the cap gate for
// level-3+ users (the R-A privilege) while still applying the award.
func (s *Store) ApplyUserCheckin(userID int64, day string, bonus, cap int, bypassCap bool) (status string, awarded, credits int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, 0, err
	}
	defer tx.Rollback()
	var lastDay string
	if err := tx.QueryRow(`SELECT last_checkin_day, credits FROM users WHERE id=?`, userID).Scan(&lastDay, &credits); err != nil {
		return "", 0, 0, err
	}
	if lastDay == day {
		return CheckinAlready, 0, credits, nil
	}
	if !bypassCap && credits >= cap {
		return CheckinCapped, 0, credits, nil
	}
	newCredits := credits + bonus
	awarded = bonus
	if _, err := tx.Exec(
		`UPDATE users SET last_checkin_day=?, credits=?, updated_at=? WHERE id=?`,
		day, newCredits, time.Now().Unix(), userID,
	); err != nil {
		return "", 0, 0, err
	}
	if err := recordActivityTx(tx, userID, time.Now(), activityDelta{checkins: 1}); err != nil {
		return "", 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, 0, err
	}
	return CheckinApplied, awarded, newCredits, nil
}

// SetUserRPMLimits sets (non-nil) or clears (nil) the per-user three-class
// RPM overrides. S2 will use these.
func (s *Store) SetUserRPMLimits(userID int64, a, b, c *int) error {
	var va, vb, vc interface{}
	if a != nil {
		va = *a
	}
	if b != nil {
		vb = *b
	}
	if c != nil {
		vc = *c
	}
	_, err := s.db.Exec(
		`UPDATE users SET rpm_limit_a=?, rpm_limit_b=?, rpm_limit_c=?, updated_at=? WHERE id=? AND is_admin=0`,
		va, vb, vc, time.Now().Unix(), userID,
	)
	return err
}

// SetUserLevel sets (1-5) or clears (nil, restoring automatic) the manual
// level override. Administrators are excluded from the level system.
func (s *Store) SetUserLevel(userID int64, level *int) error {
	_, err := s.db.Exec(
		`UPDATE users SET level=?, updated_at=? WHERE id=? AND is_admin=0`,
		level, time.Now().Unix(), userID,
	)
	return err
}

// SetUserDonationCredit directly sets the user's donation-credit counter.
func (s *Store) SetUserDonationCredit(userID int64, v int) error {
	_, err := s.db.Exec(`UPDATE users SET donation_credit=?, updated_at=? WHERE id=?`, v, time.Now().Unix(), userID)
	return err
}

// AdjustUserDonationCredit atomically adds delta to donation_credit and
// returns the new value.
func (s *Store) AdjustUserDonationCredit(userID int64, delta int) (int, error) {
	res, err := s.db.Exec(`UPDATE users SET donation_credit=donation_credit+?, updated_at=? WHERE id=?`, delta, time.Now().Unix(), userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, fmt.Errorf("user %d not found", userID)
	}
	var newVal int
	if err := s.db.QueryRow(`SELECT donation_credit FROM users WHERE id=?`, userID).Scan(&newVal); err != nil {
		return 0, err
	}
	return newVal, nil
}

// BatchUpdateUserBalance applies a set/add/sub operation to either credits or
// donation_credit in one transaction. Missing users and administrators are
// skipped to preserve the admin API's existing response semantics.
func (s *Store) BatchUpdateUserBalance(userIDs []int64, field, action string, amount int) (int, error) {
	if field != "credits" && field != "donation_credit" {
		return 0, fmt.Errorf("unsupported balance field %q", field)
	}
	if action != "set" && action != "add" && action != "sub" {
		return 0, fmt.Errorf("unsupported balance action %q", action)
	}
	if amount < 0 {
		return 0, fmt.Errorf("amount must be >= 0")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	updated := 0
	now := time.Now().Unix()
	for _, userID := range userIDs {
		var isAdmin bool
		if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id=?`, userID).Scan(&isAdmin); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return 0, err
		}
		if isAdmin {
			continue
		}
		var query string
		value := amount
		switch action {
		case "set":
			query = `UPDATE users SET ` + field + `=?, updated_at=? WHERE id=? AND is_admin=0`
		case "add":
			query = `UPDATE users SET ` + field + `=` + field + `+?, updated_at=? WHERE id=? AND is_admin=0`
		case "sub":
			query = `UPDATE users SET ` + field + `=` + field + `-?, updated_at=? WHERE id=? AND is_admin=0`
		}
		res, err := tx.Exec(query, value, now, userID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

// UpdateUserProfile updates username and avatar for an existing user
// (used to refresh Discord profile changes on re-login).
func (s *Store) UpdateUserProfile(userID int64, username, avatar string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`UPDATE users SET username=?, avatar=?, updated_at=? WHERE id=?`, username, avatar, now, userID)
	return err
}

// SetUserCharityEnabled sets the user-side public-resource opt-in switch.
func (s *Store) SetUserCharityEnabled(userID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE users SET charity_enabled=?, updated_at=? WHERE id=?`, v, time.Now().Unix(), userID)
	return err
}

// DeleteUser removes a user and their dependent rows.
func (s *Store) DeleteUser(id int64) error {
	return s.deleteUserAt(id, time.Now())
}

func (s *Store) deleteUserAt(id int64, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	defer tx.Rollback()

	var isAdmin bool
	if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id=?`, id).Scan(&isAdmin); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("delete user: user %d not found", id)
		}
		return fmt.Errorf("delete user: %w", err)
	}
	if isAdmin {
		return fmt.Errorf("delete user: administrator cannot be deleted")
	}
	// Subject alerts can survive without a request log. Remove the new
	// explicit link first, then parse only the two legacy mailer categories
	// whose old messages carried the fixed event-specific body separator.
	if _, err := tx.Exec(`DELETE FROM admin_alerts WHERE subject_user_id=?`, id); err != nil {
		return fmt.Errorf("delete user subject alerts: %w", err)
	}
	rows, err := tx.Query(
		`SELECT id, type, message FROM admin_alerts
		 WHERE subject_user_id IS NULL
		   AND type IN ('user_auto_banned', 'debug_abuse')`,
	)
	if err != nil {
		return fmt.Errorf("find user legacy subject alerts: %w", err)
	}
	var legacyAlertIDs []int64
	for rows.Next() {
		var alertID int64
		var alertType, message string
		if err := rows.Scan(&alertID, &alertType, &message); err != nil {
			rows.Close()
			return fmt.Errorf("scan user legacy subject alert: %w", err)
		}
		if subjectID, ok := legacyAlertSubjectID(alertType, message); ok && subjectID == id {
			legacyAlertIDs = append(legacyAlertIDs, alertID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read user legacy subject alerts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close user legacy subject alerts: %w", err)
	}
	for _, alertID := range legacyAlertIDs {
		if _, err := tx.Exec(`DELETE FROM admin_alerts WHERE id=? AND subject_user_id IS NULL`, alertID); err != nil {
			return fmt.Errorf("delete user legacy subject alert: %w", err)
		}
	}
	var disabled int
	var createdAt int64
	if err := tx.QueryRow(`SELECT disabled,created_at FROM users WHERE id=?`, id).Scan(&disabled, &createdAt); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := s.finalizeCompletedActivityDaysTx(tx, now); err != nil {
		return fmt.Errorf("delete user: finalize completed activity: %w", err)
	}

	var activeReservations int
	if err := tx.QueryRow(
		`SELECT COUNT(1) FROM charity_reservations
		 WHERE (user_id=? OR donor_user_id=?) AND status IN (?,?)`,
		id, id, ReservationReserved, ReservationDispatched,
	).Scan(&activeReservations); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if activeReservations > 0 {
		return fmt.Errorf("delete user: %d charity call(s) are still in flight", activeReservations)
	}
	if _, err := tx.Exec(
		`DELETE FROM charity_reservations
		 WHERE (user_id=? AND (donor_user_id IS NULL OR donor_user_id=?))
		    OR (donor_user_id=? AND user_id=0)`, id, id, id,
	); err != nil {
		return fmt.Errorf("delete solely-associated reservations: %w", err)
	}
	if _, err := tx.Exec(`UPDATE charity_reservations SET user_id=0 WHERE user_id=?`, id); err != nil {
		return fmt.Errorf("anonymize consumer reservations: %w", err)
	}
	if _, err := tx.Exec(`UPDATE charity_reservations SET donor_user_id=NULL WHERE donor_user_id=?`, id); err != nil {
		return fmt.Errorf("anonymize donor reservations: %w", err)
	}

	// Alerts bound to request logs share the log's retention/deletion fate.
	if _, err := tx.Exec(
		`DELETE FROM admin_alerts WHERE request_log_id IN (SELECT id FROM request_logs WHERE user_id=?)`, id,
	); err != nil {
		return fmt.Errorf("delete user alerts: %w", err)
	}
	// A normal user may be a reviewer in the level-based workflow. Preserve
	// other users' applications but remove the dangling reviewer reference.
	if _, err := tx.Exec(`UPDATE donation_applications SET reviewer_id=NULL WHERE reviewer_id=?`, id); err != nil {
		return fmt.Errorf("anonymize application reviewer: %w", err)
	}
	if disabled == 0 {
		day := utcDay(now)
		registrationDay := utcDay(time.Unix(createdAt, 0))
		if err := adjustOpenSiteForUserTx(tx, id, day, -1, registrationDay == day); err != nil {
			return fmt.Errorf("subtract current activity: %w", err)
		}
	}
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id=?`,
		`DELETE FROM app_configs WHERE user_id=?`,
		`DELETE FROM caller_keys WHERE user_id=?`,
		`DELETE FROM request_logs WHERE user_id=?`,
		`DELETE FROM user_activity_daily WHERE user_id=?`,
		`DELETE FROM donation_applications WHERE user_id=?`,
		`DELETE FROM game_rounds WHERE user_id=?`,
		`DELETE FROM game_best WHERE user_id=?`,
		`DELETE FROM service_generations WHERE user_id=?`,
		`UPDATE donations SET source_user_id=NULL, source_discord_id='', source_username='' WHERE source_user_id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id=? AND is_admin=0`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return fmt.Errorf("delete user: %w", rowsErr)
	} else if n != 1 {
		return fmt.Errorf("delete user: user %d was not deleted", id)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}
