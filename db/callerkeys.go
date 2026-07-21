package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// CallerKeyPrefix identifies gateway-issued caller keys.
const CallerKeyPrefix = "d2a_"

// generateCallerKeyMaterial returns a new random caller key.
func generateCallerKeyMaterial() (string, error) {
	raw := make([]byte, 24) // 24 bytes -> 32 base64url chars
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return CallerKeyPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashCallerKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// SetCallerKey generates (or regenerates) the single caller key of a user and
// stores its SHA-256 hash (for lookup) plus an encrypted copy (for the UI's
// copy-to-clipboard). Returns the plaintext key (shown once on (re)generation).
func (s *Store) SetCallerKey(userID int64) (string, error) {
	key, err := generateCallerKeyMaterial()
	if err != nil {
		return "", err
	}
	enc, err := s.Encrypt(key)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	_, err = s.db.Exec(
		`INSERT INTO caller_keys (user_id, key_hash, key_enc, created_at, updated_at) VALUES (?,?,?,?,?)
		 ON CONFLICT(user_id) DO UPDATE SET key_hash=excluded.key_hash, key_enc=excluded.key_enc, updated_at=excluded.updated_at`,
		userID, hashCallerKey(key), enc, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("set caller key: %w", err)
	}
	return key, nil
}

// GetUserByCallerKey resolves a plaintext caller key to its user.
// Returns (nil, nil) when the key is unknown.
func (s *Store) GetUserByCallerKey(key string) (*User, error) {
	var userID int64
	err := s.db.QueryRow(`SELECT user_id FROM caller_keys WHERE key_hash=?`, hashCallerKey(key)).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetUserByID(userID)
}

// GetCallerKeyPlain decrypts the stored copy of the user's caller key
// (for the UI's copy-to-clipboard button; never rendered fully in HTML).
// Returns ("", nil) when the user has no key yet.
func (s *Store) GetCallerKeyPlain(userID int64) (string, error) {
	var enc string
	err := s.db.QueryRow(`SELECT key_enc FROM caller_keys WHERE user_id=?`, userID).Scan(&enc)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.Decrypt(enc)
}

// CallerKeyExists reports whether the user already has a key.
func (s *Store) CallerKeyExists(userID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM caller_keys WHERE user_id=?`, userID).Scan(&n)
	return n > 0, err
}
