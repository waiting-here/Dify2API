package db

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func TestIsUniqueViolation(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("d1", "alice", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := st.CreateAppConfig(u.ID, "[general]m", "https://api.dify.ai/v1", "app-x", ""); err != nil {
		t.Fatalf("CreateAppConfig: %v", err)
	}
	// Same (user_id, model) violates the UNIQUE constraint; the wrapped
	// driver error must be classified without matching error text.
	_, err = st.CreateAppConfig(u.ID, "[general]m", "https://api.dify.ai/v1", "app-x", "")
	if err == nil {
		t.Fatal("duplicate CreateAppConfig: expected error")
	}
	if !IsUniqueViolation(err) {
		t.Errorf("IsUniqueViolation(%v) = false, want true", err)
	}
	if IsUniqueViolation(nil) {
		t.Error("IsUniqueViolation(nil) = true, want false")
	}
	if IsUniqueViolation(os.ErrNotExist) {
		t.Error("IsUniqueViolation(unrelated error) = true, want false")
	}
}

func TestOpen_CreatesTables(t *testing.T) {
	st, _ := openTemp(t)
	for _, table := range []string{"users", "sessions", "app_configs", "caller_keys", "request_logs", "user_activity_daily", "site_activity_daily"} {
		var name string
		err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
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

func TestMasterKey_ConcurrentCreationPublishesOneCompleteKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "concurrent.key")
	const starters = 24
	keys := make([][]byte, starters)
	errs := make([]error, starters)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range starters {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			keys[i], errs[i] = loadMasterKey(keyPath)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("starter %d: %v", i, err)
		}
		if len(keys[i]) != 32 {
			t.Fatalf("starter %d key length = %d", i, len(keys[i]))
		}
		if !bytes.Equal(keys[0], keys[i]) {
			t.Fatalf("starter %d observed a different key", i)
		}
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || !bytes.Equal(decoded, keys[0]) {
		t.Fatalf("published key is incomplete or differs: decoded=%d err=%v", len(decoded), err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".concurrent.key.tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary key files left behind: %v, err=%v", matches, err)
	}
}

func TestMasterKey_ExistingFileIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "existing.key")
	want := bytes.Repeat([]byte{0x5a}, 32)
	wantFile := []byte(base64.StdEncoding.EncodeToString(want) + "\n")
	if err := os.WriteFile(keyPath, wantFile, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := loadMasterKey(keyPath)
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("load %d changed key", i)
		}
	}
	after, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(after, wantFile) {
		t.Fatalf("existing key file was overwritten: err=%v data=%q", err, after)
	}
}

func TestMasterKey_RejectsWrongLengthAndPermissions(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"short.key": []byte(base64.StdEncoding.EncodeToString(make([]byte, 31)) + "\n"),
		"long.key":  []byte(base64.StdEncoding.EncodeToString(make([]byte, 33)) + "\n"),
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMasterKey(path); err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
			t.Errorf("%s error = %v, want explicit length failure", name, err)
		} else if strings.Contains(err.Error(), strings.TrimSpace(string(data))) {
			t.Errorf("%s error leaked key-file contents: %v", name, err)
		}
	}
	directoryPath := filepath.Join(dir, "not-a-file.key")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMasterKey(directoryPath); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Errorf("directory key error = %v, want regular-file failure", err)
	}
	if runtime.GOOS != "windows" {
		path := filepath.Join(dir, "open.key")
		data := []byte(base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\n")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMasterKey(path); err == nil || !strings.Contains(err.Error(), "permissions") {
			t.Fatalf("insecure permissions error = %v", err)
		}
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
