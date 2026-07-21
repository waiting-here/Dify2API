package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// SessionTTL is the sliding session lifetime (7 days, renewed on activity).
const SessionTTL = 7 * 24 * time.Hour

// CreateSession issues a new opaque session token for userID.
func (s *Store) CreateSession(userID int64) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("session token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	expiresAt = time.Now().Add(SessionTTL)
	_, err = s.db.Exec(
		`INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES (?,?,?,?)`,
		token, userID, expiresAt.Unix(), time.Now().Unix(),
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return token, expiresAt, nil
}

// GetSessionUser resolves a session token to its user, sliding the expiry
// forward on success. Returns (nil, nil) for unknown/expired tokens.
//
// DESIGN NOTE: every successful lookup extends the session by SessionTTL
// (7 days).  This means a stolen token can be kept alive indefinitely.
// Mitigations in place: (a) admin ban/hard-delete calls DeleteUserSessions
// to mass-invalidate; (b) the token is HttpOnly + Secure + SameSite,
// so XSS and MITM (when HTTPS is used) cannot trivially steal it.
func (s *Store) GetSessionUser(token string) (*User, error) {
	var userID int64
	var expiresAt int64
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE id=?`, token).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() > expiresAt {
		s.DeleteSession(token)
		return nil, nil
	}
	// Sliding renewal.
	if _, err := s.db.Exec(`UPDATE sessions SET expires_at=? WHERE id=?`, time.Now().Add(SessionTTL).Unix(), token); err != nil {
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
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
