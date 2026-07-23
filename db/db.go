// Package db provides the gateway's lightweight persistence layer: a SQLite
// database with value-level AES-256-GCM encryption for secrets (Dify App API
// keys, caller keys). The master key is a random 32-byte key generated on
// first startup and stored in a key file with 0600 permissions.
package db

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// schema is applied idempotently on every Open.
const schema = `
CREATE TABLE IF NOT EXISTS users (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	discord_id      TEXT NOT NULL UNIQUE,
	username        TEXT NOT NULL DEFAULT '',
	avatar          TEXT NOT NULL DEFAULT '',
	is_admin        INTEGER NOT NULL DEFAULT 0,
	disabled        INTEGER NOT NULL DEFAULT 0,
	banned_until    INTEGER NOT NULL DEFAULT 0,
	auto_banned     INTEGER NOT NULL DEFAULT 0,
	ban_reason      TEXT NOT NULL DEFAULT '',
	credits         INTEGER NOT NULL DEFAULT 0,
	last_checkin_day TEXT NOT NULL DEFAULT '',
	rpm_limit_a     INTEGER,
	rpm_limit_b     INTEGER,
	rpm_limit_c     INTEGER,
	donation_credit INTEGER NOT NULL DEFAULT 0,
	charity_enabled INTEGER NOT NULL DEFAULT 0,
	lang            TEXT NOT NULL DEFAULT '',
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	key         TEXT PRIMARY KEY,
	value       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS sessions (
	id          TEXT PRIMARY KEY,
	user_id     INTEGER NOT NULL REFERENCES users(id),
	expires_at  INTEGER NOT NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS app_configs (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id         INTEGER NOT NULL REFERENCES users(id),
	model           TEXT NOT NULL,
	dify_base_url   TEXT NOT NULL,
	dify_api_key_enc TEXT NOT NULL,
	note            TEXT NOT NULL DEFAULT '',
	enabled         INTEGER NOT NULL DEFAULT 1,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	UNIQUE(user_id, model)
);
CREATE INDEX IF NOT EXISTS idx_app_configs_user ON app_configs(user_id);

CREATE TABLE IF NOT EXISTS caller_keys (
	user_id     INTEGER PRIMARY KEY REFERENCES users(id),
	key_hash    TEXT NOT NULL UNIQUE,
	key_enc     TEXT NOT NULL,
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS request_logs (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id     INTEGER NOT NULL,
	model       TEXT NOT NULL DEFAULT '',
	service     TEXT NOT NULL DEFAULT '',
	started_at  INTEGER NOT NULL,
	ended_at    INTEGER NOT NULL DEFAULT 0,
	status      TEXT NOT NULL DEFAULT '',
	error_code  TEXT NOT NULL DEFAULT '',
	donation_id INTEGER,
	http_status INTEGER NOT NULL DEFAULT 0,
	error_detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_request_logs_user ON request_logs(user_id, started_at);

CREATE TABLE IF NOT EXISTS donations (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	service              TEXT NOT NULL,
	model                TEXT NOT NULL,
	dify_base_url        TEXT NOT NULL,
	dify_api_key_enc     TEXT NOT NULL,
	source_user_id       INTEGER,
	source_discord_id    TEXT NOT NULL DEFAULT '',
	source_username      TEXT NOT NULL DEFAULT '',
	source_text          TEXT NOT NULL DEFAULT '',
	deadline             INTEGER NOT NULL,
	total_count          INTEGER NOT NULL,
	remaining_count      INTEGER NOT NULL,
	success_count        INTEGER NOT NULL DEFAULT 0,
	failure_count        INTEGER NOT NULL DEFAULT 0,
	consecutive_failures INTEGER NOT NULL DEFAULT 0,
	rpm_limit            INTEGER NOT NULL DEFAULT 10,
	status               TEXT NOT NULL DEFAULT 'active',
	note                 TEXT NOT NULL DEFAULT '',
	created_at           INTEGER NOT NULL,
	updated_at           INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_donations_route ON donations(service, model, status);

CREATE TABLE IF NOT EXISTS admin_alerts (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	type           TEXT NOT NULL,
	message        TEXT NOT NULL DEFAULT '',
	request_log_id INTEGER,
	donation_id    INTEGER,
	created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_alerts_created ON admin_alerts(created_at);

CREATE TABLE IF NOT EXISTS bulletins (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT NOT NULL DEFAULT '',
	content     TEXT NOT NULL DEFAULT '',
	type        TEXT NOT NULL DEFAULT 'info',
	sort_order  INTEGER NOT NULL DEFAULT 0,
	closable    INTEGER NOT NULL DEFAULT 1,
	created_at  INTEGER NOT NULL,
	expires_at  INTEGER,
	is_system   INTEGER NOT NULL DEFAULT 0,
	system_key  TEXT,
	lang        TEXT NOT NULL DEFAULT 'zh'
);

CREATE TABLE IF NOT EXISTS donation_applications (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id           INTEGER NOT NULL REFERENCES users(id),
	service           TEXT NOT NULL,
	model             TEXT NOT NULL,
	dify_base_url     TEXT NOT NULL,
	dify_api_key_enc  TEXT NOT NULL,
	total_count       INTEGER NOT NULL,
	deadline          INTEGER NOT NULL,
	rpm_limit         INTEGER NOT NULL DEFAULT 10,
	note              TEXT NOT NULL DEFAULT '',
	status            TEXT NOT NULL DEFAULT 'pending',
	reviewer_id       INTEGER,
	review_note       TEXT NOT NULL DEFAULT '',
	donation_id       INTEGER,
	created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_da_user ON donation_applications(user_id);
CREATE INDEX IF NOT EXISTS idx_da_status ON donation_applications(status);
`

// Store wraps the SQLite handle and the master encryption key.
type Store struct {
	db  *sql.DB
	key []byte
}

// Open opens (creating if necessary) the SQLite database at path, loads or
// generates the master key at keyPath, and applies the schema.
func Open(path, keyPath string) (*Store, error) {
	key, err := loadMasterKey(keyPath)
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection avoids "database is locked" surprises with the
	// per-request read pattern of the gateway.
	sqldb.SetMaxOpenConns(1)

	if _, err := sqldb.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}
	if _, err := sqldb.Exec(schema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Idempotent column migrations (V1.0.0 is pre-release; ignore "duplicate column" errors).
	for _, m := range []string{
		`ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN banned_until INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE caller_keys ADD COLUMN key_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE app_configs ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN ban_reason TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN auto_banned INTEGER NOT NULL DEFAULT 0`,
		// alpha.3 S1 — users new columns.
		`ALTER TABLE users ADD COLUMN credits INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN last_checkin_day TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN rpm_limit_a INTEGER`,
		`ALTER TABLE users ADD COLUMN rpm_limit_b INTEGER`,
		`ALTER TABLE users ADD COLUMN rpm_limit_c INTEGER`,
		`ALTER TABLE users ADD COLUMN donation_credit INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN charity_enabled INTEGER NOT NULL DEFAULT 0`,
		// alpha.3 S1 — request_logs donation tracking.
		`ALTER TABLE request_logs ADD COLUMN donation_id INTEGER`,
		// alpha.3 — admin log detail (HTTP status + error message).
		`ALTER TABLE request_logs ADD COLUMN http_status INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE request_logs ADD COLUMN error_detail TEXT NOT NULL DEFAULT ''`,
		// alpha.4 — bulletins table.
		`ALTER TABLE bulletins ADD COLUMN lang TEXT NOT NULL DEFAULT 'zh'`,
		// alpha.4 — donation_applications table.
		`ALTER TABLE donation_applications ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		// beta.1 — per-donation RPM limit.
		`ALTER TABLE donations ADD COLUMN rpm_limit INTEGER NOT NULL DEFAULT 10`,
		`ALTER TABLE donation_applications ADD COLUMN rpm_limit INTEGER NOT NULL DEFAULT 10`,
	} {
		if _, err := sqldb.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}

	return &Store{db: sqldb, key: key}, nil
}

// Close closes the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Exec executes a raw SQL statement with args. Use with care — prefer
// typed methods when available.
func (s *Store) Exec(query string, args ...interface{}) (sql.Result, error) {
	return s.db.Exec(query, args...)
}

// loadMasterKey reads the base64-encoded 32-byte master key from path,
// generating a fresh random one (mode 0600) when the file does not exist.
func loadMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, decErr := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
		if decErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid master key file %s (want base64 of 32 bytes)", path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts a UTF-8 string with AES-256-GCM and returns base64.
func (s *Store) Encrypt(plain string) (string, error) {
	gcm, err := s.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Decrypt reverses Encrypt.
func (s *Store) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	gcm, err := s.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

func (s *Store) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
