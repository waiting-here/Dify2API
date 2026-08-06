package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// SessionTTL is the idle session lifetime (7 days, renewed on activity).
const SessionTTL = 7 * 24 * time.Hour

// SessionAbsoluteTTL caps a session's lifetime even when it is used often
// enough to keep renewing the idle expiry. This limits replay of a stolen
// token while preserving the existing seven-day idle-session behaviour.
const SessionAbsoluteTTL = 30 * 24 * time.Hour

// CreateSession issues a new opaque session token for userID.
func (s *Store) CreateSession(userID int64) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	expiresAt = now.Add(SessionTTL)
	_, err = s.db.Exec(
		`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?,?,?,?)`,
		token, userID, expiresAt.Unix(), now.Unix(),
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expiresAt, nil
}

// GetSessionUser resolves a session token to its user, sliding the idle expiry
// forward on success without exceeding SessionAbsoluteTTL. Returns (nil, nil)
// for unknown/expired tokens.
func (s *Store) GetSessionUser(token string) (*User, error) {
	return s.getSessionUserAt(token, time.Now())
}

func (s *Store) getSessionUserAt(token string, now time.Time) (*User, error) {
	var userID int64
	var expiresAt, createdAt int64
	err := s.db.QueryRow(`SELECT user_id, expires_at, created_at FROM sessions WHERE id=?`, token).Scan(&userID, &expiresAt, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	absoluteExpiry := time.Unix(createdAt, 0).Add(SessionAbsoluteTTL)
	if now.Unix() >= expiresAt || !now.Before(absoluteExpiry) {
		s.DeleteSession(token)
		return nil, nil
	}
	// Sliding renewal, capped by the immutable creation-time deadline.
	renewedExpiry := now.Add(SessionTTL)
	if renewedExpiry.After(absoluteExpiry) {
		renewedExpiry = absoluteExpiry
	}
	if _, err := s.db.Exec(`UPDATE sessions SET expires_at=? WHERE id=?`, renewedExpiry.Unix(), token); err != nil {
		return nil, err
	}
	return s.GetUserByID(userID)
}

// DeleteSession invalidates a token.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, token)
	return err
}

// DeleteUserSessions invalidates all tokens of a user (e.g. after disable/delete).
func (s *Store) DeleteUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

// PurgeExpiredSessions deletes sessions whose expiry has passed.
func (s *Store) PurgeExpiredSessions() (int64, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`DELETE FROM sessions WHERE expires_at <= ? OR created_at <= ?`,
		now.Unix(), now.Add(-SessionAbsoluteTTL).Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
