package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, dir
}

func TestOpen_CreatesTables(t *testing.T) {
	st, _ := openTemp(t)
	for _, table := range []string{"users", "sessions", "app_configs", "caller_keys", "request_logs"} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
}

func TestOpen_MigratesRequestLogsAntiAbuseInfo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	keyPath := filepath.Join(dir, "legacy.key")

	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = legacy.Exec(`CREATE TABLE request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		model TEXT NOT NULL DEFAULT '',
		service TEXT NOT NULL DEFAULT '',
		started_at INTEGER NOT NULL,
		ended_at INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT '',
		error_code TEXT NOT NULL DEFAULT '',
		donation_id INTEGER,
		http_status INTEGER NOT NULL DEFAULT 0,
		error_detail TEXT NOT NULL DEFAULT '',
		credits_consumed INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		legacy.Close()
		t.Fatalf("create legacy request_logs: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	st, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("Open legacy db: %v", err)
	}
	if _, err := st.db.Exec(`INSERT INTO request_logs (user_id, started_at) VALUES (1, 1)`); err != nil {
		st.Close()
		t.Fatalf("insert using migrated schema: %v", err)
	}
	var antiAbuseInfo string
	if err := st.db.QueryRow(`SELECT anti_abuse_info FROM request_logs WHERE user_id=1`).Scan(&antiAbuseInfo); err != nil {
		st.Close()
		t.Fatalf("read migrated anti_abuse_info: %v", err)
	}
	if antiAbuseInfo != "" {
		st.Close()
		t.Fatalf("migrated anti_abuse_info = %q, want empty default", antiAbuseInfo)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close migrated db: %v", err)
	}

	// Reopening exercises the duplicate-column path and must remain idempotent.
	st, err = Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	defer st.Close()
}

func TestMasterKey_GeneratedAndReused(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "k.key")

	st1, err := Open(filepath.Join(dir, "a.db"), keyPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	// Windows maps file modes onto its own ACL model (reports 0666);
	// the 0600 guarantee applies to the Linux deployment target.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %o, want 600", info.Mode().Perm())
	}
	// Second Open must load the SAME key (data encrypted by st1 decrypts under st2).
	enc, err := st1.Encrypt("secret-value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	st1.Close()

	st2, err := Open(filepath.Join(dir, "b.db"), keyPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	dec, err := st2.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt with reloaded key: %v", err)
	}
	if dec != "secret-value" {
		t.Errorf("roundtrip across restarts = %q, want %q", dec, "secret-value")
	}
}

func TestMasterKey_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.key")
	os.WriteFile(keyPath, []byte("not-base64!!"), 0o600)
	if _, err := Open(filepath.Join(dir, "a.db"), keyPath); err == nil {
		t.Fatal("expected error for invalid key file")
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	st, _ := openTemp(t)
	for _, plain := range []string{"", "a", "app-带有中文的密钥🔑", strings.Repeat("x", 4096)} {
		enc, err := st.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		dec, err := st.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != plain {
			t.Errorf("roundtrip = %q, want %q", dec, plain)
		}
	}
}

func TestDecrypt_Tampered(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.Decrypt("AAAA"); err == nil {
		t.Fatal("expected error for short ciphertext")
	}
	enc, _ := st.Encrypt("hello")
	tampered := enc[:len(enc)-4] + "XXXX"
	if _, err := st.Decrypt(tampered); err == nil {
		t.Fatal("expected error for tampered ciphertext")
	}
}
