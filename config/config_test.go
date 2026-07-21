package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

const minimalAdmin = `
ADMIN_USERNAME=root
ADMIN_PASSWORD=s3cret
DISCORD_CLIENT_ID=123
DISCORD_CLIENT_SECRET=abc
SITE_BASE_URL=https://dify2api.example.com/
`

func TestLoadStartup_Defaults(t *testing.T) {
	cfg, err := LoadStartup(writeTemp(t, "admin.env", minimalAdmin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "localhost:10086" || cfg.DifyHTTPTimeoutMs != 600000 ||
		cfg.DBPath != "dify2api.db" || cfg.MasterKeyPath != "dify2api.key" {
		t.Errorf("base defaults wrong: %+v", cfg)
	}
	if cfg.MaxChatInFlight != 64 || cfg.MaxRequestBodyMB != 10 || cfg.SSEBufferMB != 10 {
		t.Errorf("perf defaults wrong: %+v", cfg)
	}
	if cfg.LoginMaxFailures != 5 || cfg.LoginWindowMin != 10 || cfg.LoginLockMin != 60 || cfg.LoginMinLatencyMs != 300 {
		t.Errorf("login throttle defaults wrong: %+v", cfg)
	}
	a := cfg.Admin
	if a.Username != "root" || a.Password != "s3cret" || a.DiscordClientID != "123" || a.DiscordClientSecret != "abc" {
		t.Errorf("admin = %+v", a)
	}
	if a.SiteBaseURL != "https://dify2api.example.com" || a.SiteHost != "dify2api.example.com" || a.AdminHost != "admin.dify2api.example.com" {
		t.Errorf("site hosts = %q / %q / %q", a.SiteBaseURL, a.SiteHost, a.AdminHost)
	}
}

func TestLoadStartup_CustomValues(t *testing.T) {
	p := writeTemp(t, "admin.env", minimalAdmin+`
LISTEN_ADDR=0.0.0.0:9090
MAX_CHAT_IN_FLIGHT=128
MAX_REQUEST_BODY_MB=4
SSE_BUFFER_MB=1
LOGIN_LOCK_MIN=120
ADMIN_HOST=manage.example.com
`)
	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:9090" || cfg.MaxChatInFlight != 128 || cfg.MaxRequestBodyMB != 4 || cfg.SSEBufferMB != 1 {
		t.Errorf("custom perf values wrong: %+v", cfg)
	}
	if cfg.LoginLockMin != 120 {
		t.Errorf("LOGIN_LOCK_MIN = %d, want 120", cfg.LoginLockMin)
	}
	if cfg.Admin.AdminHost != "manage.example.com" {
		t.Errorf("AdminHost override = %q", cfg.Admin.AdminHost)
	}
}

func TestLoadStartup_EnvOverridesFile(t *testing.T) {
	p := writeTemp(t, "admin.env", minimalAdmin+"MAX_CHAT_IN_FLIGHT=64\n")
	os.Setenv("MAX_CHAT_IN_FLIGHT", "128")
	defer os.Unsetenv("MAX_CHAT_IN_FLIGHT")
	os.Setenv("ADMIN_HOST", "env-admin.example.com")
	defer os.Unsetenv("ADMIN_HOST")

	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxChatInFlight != 128 {
		t.Errorf("env should override file: %d", cfg.MaxChatInFlight)
	}
	if cfg.Admin.AdminHost != "env-admin.example.com" {
		t.Errorf("env ADMIN_HOST should win: %q", cfg.Admin.AdminHost)
	}
}

func TestLoadStartup_InvalidIntsFallBack(t *testing.T) {
	p := writeTemp(t, "admin.env", minimalAdmin+"MAX_CHAT_IN_FLIGHT=nope\nSSE_BUFFER_MB=-3\n")
	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxChatInFlight != 64 || cfg.SSEBufferMB != 10 {
		t.Errorf("invalid ints should fall back: %+v", cfg)
	}
}

func TestLoadStartup_MissingRequired(t *testing.T) {
	p := writeTemp(t, "admin.env", "ADMIN_USERNAME=root\n")
	_, err := LoadStartup(p)
	if err == nil {
		t.Fatal("expected error for missing keys")
	}
	for _, key := range []string{"ADMIN_PASSWORD", "DISCORD_CLIENT_ID", "DISCORD_CLIENT_SECRET", "SITE_BASE_URL"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should mention %s: %v", key, err)
		}
	}
}

func TestLoadStartup_Required(t *testing.T) {
	if _, err := LoadStartup(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if _, err := LoadStartup("/nonexistent/admin.env"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGetIntOr_WarnsOnInvalid(t *testing.T) {
	// Capture log output.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Non-numeric value: should fall back and warn.
	result := getIntOr(map[string]string{"BAD_KEY": "not-a-number"}, "BAD_KEY", 42)
	if result != 42 {
		t.Errorf("expected fallback 42, got %d", result)
	}
	output := buf.String()
	if !strings.Contains(output, "[CONFIG]") || !strings.Contains(output, "BAD_KEY") ||
		!strings.Contains(output, "not a positive integer") {
		t.Errorf("expected [CONFIG] warning in log, got: %s", output)
	}

	// Negative value: should fall back and warn.
	buf.Reset()
	result2 := getIntOr(map[string]string{"NEG_KEY": "-5"}, "NEG_KEY", 99)
	if result2 != 99 {
		t.Errorf("expected fallback 99, got %d", result2)
	}
	output2 := buf.String()
	if !strings.Contains(output2, "[CONFIG]") || !strings.Contains(output2, "NEG_KEY") ||
		!strings.Contains(output2, "not a positive integer") {
		t.Errorf("expected [CONFIG] warning for negative, got: %s", output2)
	}

	// Zero value: should fall back and warn.
	buf.Reset()
	result3 := getIntOr(map[string]string{"ZERO_KEY": "0"}, "ZERO_KEY", 77)
	if result3 != 77 {
		t.Errorf("expected fallback 77, got %d", result3)
	}
	output3 := buf.String()
	if !strings.Contains(output3, "[CONFIG]") || !strings.Contains(output3, "ZERO_KEY") ||
		!strings.Contains(output3, "not a positive integer") {
		t.Errorf("expected [CONFIG] warning for zero, got: %s", output3)
	}

	// Missing key: silent fallback, no warning.
	buf.Reset()
	result4 := getIntOr(map[string]string{}, "MISSING", 7)
	if result4 != 7 {
		t.Errorf("expected default 7, got %d", result4)
	}
	if buf.Len() > 0 {
		t.Errorf("expected no log for unset key, got: %s", buf.String())
	}

	// Empty value: silent fallback, no warning.
	buf.Reset()
	result5 := getIntOr(map[string]string{"EMPTY_KEY": ""}, "EMPTY_KEY", 11)
	if result5 != 11 {
		t.Errorf("expected default 11, got %d", result5)
	}
	if buf.Len() > 0 {
		t.Errorf("expected no log for empty value, got: %s", buf.String())
	}
}
