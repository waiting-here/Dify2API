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
	BannedUntil int64 `json:"banned_until"` // unix ts; 0 = no timed ban; ban lapses automatically once past
	AutoBanned  bool  `json:"auto_banned"`  // true when RPM-violation auto-ban
	BanReason   string `json:"ban_reason"`   // admin-supplied reason (empty for auto-bans)
	// RPMLimit is the per-user requests-per-minute override; Valid == false
	// means "use the global default".
	RPMLimit   sql.NullInt64
	CreatedAt  int64
	UpdatedAt  int64
}

// IsBanned reports whether the user is currently banned: either permanently
// disabled or inside a timed-ban window.
func IsBanned(u *User) bool {
	return u.Disabled || (u.BannedUntil > 0 && time.Now().Unix() < u.BannedUntil)
}

func scanUser(row interface{ Scan(...interface{}) error }) (*User, error) {
	var u User
	var isAdmin, disabled, autoBanned int
	err := row.Scan(&u.ID, &u.DiscordID, &u.Username, &u.Avatar, &isAdmin, &disabled, &u.BannedUntil, &autoBanned, &u.BanReason, &u.RPMLimit, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	u.IsAdmin = isAdmin != 0
	u.Disabled = disabled != 0
	u.AutoBanned = autoBanned != 0
	return &u, nil
}

const userCols = "id, discord_id, username, avatar, is_admin, disabled, banned_until, auto_banned, ban_reason, rpm_limit, created_at, updated_at"

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
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO users (discord_id, username, avatar, created_at, updated_at) VALUES (?,?,?,?,?)`,
		discordID, username, avatar, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
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

// SetUserRPMLimit sets (limit >= 1) or clears (limit == nil) the per-user
// RPM override. Clearing reverts the user to the global default.
func (s *Store) SetUserRPMLimit(id int64, limit *int64) error {
	var v interface{}
	if limit != nil {
		v = *limit
	}
	_, err := s.db.Exec(`UPDATE users SET rpm_limit=?, updated_at=? WHERE id=? AND is_admin=0`, v, time.Now().Unix(), id)
	return err
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
	_, err := s.db.Exec(`UPDATE users SET banned_until=0, disabled=0, auto_banned=0, ban_reason='', updated_at=? WHERE id=? AND is_admin=0`, time.Now().Unix(), id)
	return err
}

// SetUserDisabled marks a user disabled/enabled with an optional reason.
func (s *Store) SetUserDisabled(id int64, disabled bool, reason string) error {
	v := 0
	if disabled {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE users SET disabled=?, ban_reason=?, updated_at=? WHERE id=?`, v, reason, time.Now().Unix(), id)
	return err
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

// DeleteUser removes a user and their dependent rows.
func (s *Store) DeleteUser(id int64) error {
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id=?`,
		`DELETE FROM app_configs WHERE user_id=?`,
		`DELETE FROM caller_keys WHERE user_id=?`,
		`DELETE FROM request_logs WHERE user_id=?`,
		`DELETE FROM users WHERE id=? AND is_admin=0`,
	} {
		if _, err := s.db.Exec(q, id); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
	}
	return nil
}
