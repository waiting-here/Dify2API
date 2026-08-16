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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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
	updated_at      INTEGER NOT NULL,
	level           INTEGER,
	leaderboard_anon INTEGER NOT NULL DEFAULT 0
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
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at);

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
	error_detail TEXT NOT NULL DEFAULT '',
	credits_consumed INTEGER NOT NULL DEFAULT 0,
	anti_abuse_info TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_request_logs_user ON request_logs(user_id, started_at);
CREATE INDEX IF NOT EXISTS idx_request_logs_started ON request_logs(started_at);

CREATE TABLE IF NOT EXISTS user_activity_daily (
	day             INTEGER NOT NULL,
	user_id         INTEGER NOT NULL REFERENCES users(id),
	api_attempts    INTEGER NOT NULL DEFAULT 0,
	api_successes   INTEGER NOT NULL DEFAULT 0,
	console_actions INTEGER NOT NULL DEFAULT 0,
	checkins        INTEGER NOT NULL DEFAULT 0,
	game_rounds     INTEGER NOT NULL DEFAULT 0,
	updated_at      INTEGER NOT NULL,
	PRIMARY KEY (day, user_id)
);
CREATE INDEX IF NOT EXISTS idx_uad_user_day ON user_activity_daily(user_id, day);

CREATE TABLE IF NOT EXISTS site_activity_daily (
	day                   INTEGER PRIMARY KEY,
	new_users             INTEGER,
	product_active        INTEGER,
	successful_api_active INTEGER,
	attempted_api_active  INTEGER,
	console_active        INTEGER,
	checkin_only_active   INTEGER,
	api_attempts          INTEGER,
	api_successes         INTEGER,
	game_active           INTEGER,
	wau                   INTEGER,
	active_28d            INTEGER,
	engaged_28d           INTEGER,
	finalized_at          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS donations (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	service              TEXT NOT NULL,
	model                TEXT NOT NULL,
	dify_base_url        TEXT NOT NULL,
	dify_api_key_enc     TEXT NOT NULL,
	dify_api_key_sha256  TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS charity_reservations (
	id            TEXT PRIMARY KEY,
	user_id       INTEGER NOT NULL,
	donation_id   INTEGER NOT NULL,
	donor_user_id INTEGER,
	price         INTEGER NOT NULL DEFAULT 0,
	reward        INTEGER NOT NULL DEFAULT 0,
	status        TEXT NOT NULL DEFAULT 'reserved',
	created_at    INTEGER NOT NULL,
	updated_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cr_user ON charity_reservations(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cr_donor ON charity_reservations(donor_user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_cr_status ON charity_reservations(status, updated_at);

CREATE TABLE IF NOT EXISTS admin_alerts (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	type           TEXT NOT NULL,
	message        TEXT NOT NULL DEFAULT '',
	request_log_id INTEGER,
	donation_id    INTEGER,
	subject_user_id INTEGER,
	created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_alerts_created ON admin_alerts(created_at);

CREATE TABLE IF NOT EXISTS alert_prefs (
	event_type     TEXT PRIMARY KEY,
	show_in_center INTEGER NOT NULL DEFAULT 1,
	email_enabled  INTEGER NOT NULL DEFAULT 1,
	updated_at     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS bulletins (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT NOT NULL DEFAULT '',
	content     TEXT NOT NULL DEFAULT '',
	content_type TEXT NOT NULL DEFAULT 'html',
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
CREATE INDEX IF NOT EXISTS idx_da_donation ON donation_applications(donation_id);

CREATE TABLE IF NOT EXISTS charity_pricing (
    service TEXT NOT NULL,
    model   TEXT NOT NULL,
    price   INTEGER NOT NULL DEFAULT 0,
    reward  INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (service, model)
);

CREATE TABLE IF NOT EXISTS game_rounds (
	id           TEXT PRIMARY KEY,
	game_id      TEXT NOT NULL DEFAULT 'fishing',
	user_id      INTEGER NOT NULL,
	bait_tier    TEXT NOT NULL DEFAULT '',
	price        INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL DEFAULT 'started',
	species_key  TEXT NOT NULL DEFAULT '',
	size_cm      INTEGER NOT NULL DEFAULT 0,
	is_junk      INTEGER NOT NULL DEFAULT 0,
	is_treasure  INTEGER NOT NULL DEFAULT 0,
	credits_won  INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	settled_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_game_rounds_game_created ON game_rounds(game_id, created_at);
CREATE INDEX IF NOT EXISTS idx_game_rounds_user_game ON game_rounds(user_id, game_id, created_at);

CREATE TABLE IF NOT EXISTS game_best (
	user_id     INTEGER NOT NULL REFERENCES users(id),
	game_id     TEXT NOT NULL DEFAULT 'fishing',
	species_key TEXT NOT NULL DEFAULT '',
	size_cm     INTEGER NOT NULL DEFAULT 0,
	caught_at   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, game_id)
);

CREATE TABLE IF NOT EXISTS service_anti_abuse (
    service                TEXT PRIMARY KEY,
    mode                   INTEGER NOT NULL DEFAULT 2,
    min_chars              INTEGER NOT NULL DEFAULT 20,
    penalty_deduct_credits INTEGER NOT NULL DEFAULT 0,
    penalty_ban_hours      INTEGER NOT NULL DEFAULT 0,
    donation_selectable    INTEGER NOT NULL DEFAULT 1,
    created_at             INTEGER NOT NULL DEFAULT 0,
    updated_at             INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dify_model_configs (
    model_key           TEXT PRIMARY KEY,
    display_name        TEXT NOT NULL DEFAULT '',
    provider            TEXT NOT NULL DEFAULT '',
    dependency_plugin   TEXT NOT NULL DEFAULT '',
    dependency_version  TEXT NOT NULL DEFAULT '',
    dependency_hash     TEXT NOT NULL DEFAULT '',
    params_json         TEXT NOT NULL DEFAULT '',
    enabled             INTEGER NOT NULL DEFAULT 1,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    manual              INTEGER NOT NULL DEFAULT 0,
    updated_at          INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS service_generations (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id        INTEGER NOT NULL,
    service        TEXT NOT NULL DEFAULT '',
    model_key      TEXT NOT NULL DEFAULT '',
    purpose        TEXT NOT NULL DEFAULT 'personal',
    seed           TEXT NOT NULL DEFAULT '',
    mapping_json   TEXT NOT NULL DEFAULT '',
    dummy_json     TEXT NOT NULL DEFAULT '[]',
    dummy_count    INTEGER NOT NULL DEFAULT 0,
    download_count INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sg_user ON service_generations(user_id, service, created_at);
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

	// Idempotent column additions (ignore "duplicate column" errors).
	if _, err := sqldb.Exec(`ALTER TABLE bulletins ADD COLUMN content_type TEXT NOT NULL DEFAULT 'html'`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate bulletins.content_type: %w", err)
		}
	}
	if _, err := sqldb.Exec(`ALTER TABLE service_anti_abuse ADD COLUMN donation_selectable INTEGER NOT NULL DEFAULT 1`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate service_anti_abuse.donation_selectable: %w", err)
		}
	}
	// R-A user levels: NULL = automatic, 1-5 = manual override. The automatic
	// level is computed lazily from donation_credit + thresholds at read time.
	if _, err := sqldb.Exec(`ALTER TABLE users ADD COLUMN level INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate users.level: %w", err)
		}
	}
	// v1.4.0 games: per-user leaderboard anonymity switch.
	if _, err := sqldb.Exec(`ALTER TABLE users ADD COLUMN leaderboard_anon INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate users.leaderboard_anon: %w", err)
		}
	}
	// v1.4.0 downloadable-template generation metadata.
	if _, err := sqldb.Exec(`ALTER TABLE service_generations ADD COLUMN dummy_json TEXT NOT NULL DEFAULT '[]'`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate service_generations.dummy_json: %w", err)
		}
	}
	// v1.4.0 B': mapping snapshots for downloadable-template donations.
	if _, err := sqldb.Exec(`ALTER TABLE donation_applications ADD COLUMN mapping_json TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate donation_applications.mapping_json: %w", err)
		}
	}
	if _, err := sqldb.Exec(`ALTER TABLE donations ADD COLUMN mapping_json TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate donations.mapping_json: %w", err)
		}
	}
	// v1.4.0 games: daily game-round counters (product-activity scope B).
	if _, err := sqldb.Exec(`ALTER TABLE user_activity_daily ADD COLUMN game_rounds INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate user_activity_daily.game_rounds: %w", err)
		}
	}
	// v1.4.0 games: site-level daily game-active count (k-anonymity alike).
	if _, err := sqldb.Exec(`ALTER TABLE site_activity_daily ADD COLUMN game_active INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate site_activity_daily.game_active: %w", err)
		}
	}
	// R05 subject alerts: fresh schemas include the column above, while
	// v1.4.0 databases need the idempotent forward migration. The index is
	// deliberately created only after ALTER so an old schema never sees an
	// index expression that references a column it does not have yet.
	if _, err := sqldb.Exec(`ALTER TABLE admin_alerts ADD COLUMN subject_user_id INTEGER`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			sqldb.Close()
			return nil, fmt.Errorf("migrate admin_alerts.subject_user_id: %w", err)
		}
	}
	if _, err := sqldb.Exec(`CREATE INDEX IF NOT EXISTS idx_admin_alerts_subject_user ON admin_alerts(subject_user_id)`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("create admin_alerts subject index: %w", err)
	}

	store := &Store{db: sqldb, key: key}
	if err := store.initializeActivity(time.Now()); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("initialize activity data: %w", err)
	}
	if err := store.SeedModelConfigs(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("seed model configs: %w", err)
	}
	return store, nil
}

// Close closes the database handle.
func (s *Store) Close() error { return s.db.Close() }

// IsUniqueViolation reports whether err (or any error it wraps) is a SQLite
// UNIQUE-constraint violation. Callers must use this instead of matching the
// driver's error text, which is not part of its API contract.
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	code := se.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
		code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

// IsBusyOrLocked reports transient SQLite writer/read-lock contention. The
// low byte is the primary result code; extended BUSY/LOCKED codes retain it.
func IsBusyOrLocked(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	code := se.Code() & 0xff
	return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
}

// Exec executes a raw SQL statement with args. Use with care — prefer
// typed methods when available.
func (s *Store) Exec(query string, args ...interface{}) (sql.Result, error) {
	return s.db.Exec(query, args...)
}

// loadMasterKey reads the base64-encoded 32-byte master key from path,
// generating a fresh random one (mode 0600) when the file does not exist.
// Creation uses a fully-written same-directory temporary file followed by an
// atomic, no-replace hard link. Concurrent starters therefore either publish
// the complete new key or load the complete winner; a partial target file is
// never observable and an existing key is never overwritten.
func loadMasterKey(path string) ([]byte, error) {
	key, err := readMasterKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create master key directory: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary master key: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	complete := false
	defer func() {
		if !complete {
			_ = tmp.Close()
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("set temporary master key permissions: %w", err)
	}
	encoded := []byte(base64.StdEncoding.EncodeToString(key) + "\n")
	if _, err := tmp.Write(encoded); err != nil {
		return nil, fmt.Errorf("write temporary master key: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return nil, fmt.Errorf("sync temporary master key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temporary master key: %w", err)
	}
	complete = true

	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readMasterKey(path)
		}
		return nil, fmt.Errorf("publish master key atomically: %w", err)
	}
	if runtime.GOOS != "windows" {
		dirHandle, err := os.Open(dir)
		if err != nil {
			return nil, fmt.Errorf("open master key directory for sync: %w", err)
		}
		syncErr := dirHandle.Sync()
		closeErr := dirHandle.Close()
		if syncErr != nil {
			return nil, fmt.Errorf("sync master key directory: %w", syncErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close master key directory: %w", closeErr)
		}
	}
	return key, nil
}

func readMasterKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("inspect master key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid master key file %s (must be a regular file)", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open master key: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened master key: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("master key file %s changed during validation", path)
	}
	// Windows reports POSIX mode bits derived from ACLs and cannot reliably
	// express the deployment guarantee. Unix deployments must not expose the
	// encryption root to group or other users.
	if runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("insecure master key permissions %s: got %04o, want 0600", path, openedInfo.Mode().Perm())
	}
	data, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if len(data) > 128 {
		return nil, fmt.Errorf("invalid master key file %s (content too long)", path)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) != base64.StdEncoding.EncodedLen(32) {
		return nil, fmt.Errorf("invalid master key file %s (want base64 of exactly 32 bytes)", path)
	}
	key, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid master key file %s (want base64 of exactly 32 bytes)", path)
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
