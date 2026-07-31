package config

import (
	"bytes"
	"fmt"
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
	if cfg.ListenAddr != "localhost:10086" || cfg.DifyHTTPTimeoutMs != 900000 ||
		cfg.DBPath != "dify2api.db" || cfg.MasterKeyPath != "dify2api.key" {
		t.Errorf("base defaults wrong: %+v", cfg)
	}
	if cfg.MaxChatInFlight != 32 || cfg.MaxRequestBodyMB != 10 || cfg.MaxWebRequestBodyKB != 256 || cfg.SSEBufferMB != 1 ||
		cfg.DifyMaxResponseMB != 32 || cfg.DifyProbeInFlight != 8 {
		t.Errorf("perf defaults wrong: %+v", cfg)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 || cfg.TrustedProxyCIDRs[0].String() != "127.0.0.0/8" || cfg.TrustedProxyCIDRs[1].String() != "::1/128" {
		t.Errorf("trusted proxy defaults wrong: %v", cfg.TrustedProxyCIDRs)
	}
	if cfg.LoginMaxFailures != 5 || cfg.LoginWindowMin != 10 || cfg.LoginLockMin != 60 || cfg.LoginMinLatencyMs != 300 {
		t.Errorf("login throttle defaults wrong: %+v", cfg)
	}
	a := cfg.Admin
	if a.Username != "root" || a.Password != "s3cret" || a.DiscordClientID != "123" || a.DiscordClientSecret != "abc" {
		t.Errorf("admin = %+v", a)
	}
	if a.SiteBaseURL != "https://dify2api.example.com" || a.SiteHost != "dify2api.example.com" || a.SiteURLHost != "dify2api.example.com" || a.AdminHost != "admin.dify2api.example.com" {
		t.Errorf("site hosts = %q / %q / %q / %q", a.SiteBaseURL, a.SiteHost, a.SiteURLHost, a.AdminHost)
	}
}

func TestLoadStartup_CustomValues(t *testing.T) {
	p := writeTemp(t, "admin.env", minimalAdmin+`
LISTEN_ADDR=0.0.0.0:9090
MAX_CHAT_IN_FLIGHT=128
MAX_REQUEST_BODY_MB=4
MAX_WEB_REQUEST_BODY_KB=64
TRUSTED_PROXY_CIDRS=10.0.0.0/8,192.0.2.10
DIFY_MAX_RESPONSE_MB=16
DIFY_PROBE_IN_FLIGHT=3
DIFY_EGRESS_ALLOWLIST=https://dify.internal,10.0.0.0/8
REMOTE_CONTENT_ORIGIN_ALLOWLIST=https://example.com,http://images.example.com:8080
SSE_BUFFER_MB=2
LOGIN_LOCK_MIN=120
ADMIN_HOST=manage.example.com
`)
	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:9090" || cfg.MaxChatInFlight != 128 || cfg.MaxRequestBodyMB != 4 || cfg.MaxWebRequestBodyKB != 64 || cfg.SSEBufferMB != 2 ||
		cfg.DifyMaxResponseMB != 16 || cfg.DifyProbeInFlight != 3 {
		t.Errorf("custom perf values wrong: %+v", cfg)
	}
	if len(cfg.DifyEgressAllowlist) != 2 || len(cfg.RemoteContentOriginAllowlist) != 2 {
		t.Errorf("custom egress lists wrong: Dify=%v remote=%v", cfg.DifyEgressAllowlist, cfg.RemoteContentOriginAllowlist)
	}
	if got := cfg.TrustedProxyCIDRs; len(got) != 2 || got[0].String() != "10.0.0.0/8" || got[1].String() != "192.0.2.10/32" {
		t.Errorf("custom trusted proxies = %v", got)
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
	if cfg.MaxChatInFlight != 32 || cfg.SSEBufferMB != 1 {
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

func TestLoadStartup_RejectsInvalidOriginsAndProxyCIDRs(t *testing.T) {
	cases := []string{
		strings.Replace(minimalAdmin, "https://dify2api.example.com/", "javascript://example.com/", 1),
		strings.Replace(minimalAdmin, "https://dify2api.example.com/", "https://user:pass@example.com/", 1),
		strings.Replace(minimalAdmin, "https://dify2api.example.com/", "https://example.com/path", 1),
		minimalAdmin + "ADMIN_HOST=https://admin.example.com\n",
		minimalAdmin + "TRUSTED_PROXY_CIDRS=127.0.0.1,not-a-cidr\n",
		minimalAdmin + "DIFY_EGRESS_ALLOWLIST=localhost\n",
		minimalAdmin + "REMOTE_CONTENT_ORIGIN_ALLOWLIST=https://example.com/path\n",
	}
	for i, content := range cases {
		if _, err := LoadStartup(writeTemp(t, fmt.Sprintf("bad-%d.env", i), content)); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}

	cfg, err := LoadStartup(writeTemp(t, "none.env", minimalAdmin+"TRUSTED_PROXY_CIDRS=none\n"))
	if err != nil {
		t.Fatalf("TRUSTED_PROXY_CIDRS=none: %v", err)
	}
	if len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("none should trust no proxy, got %v", cfg.TrustedProxyCIDRs)
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

func TestLoadStartup_Alpha3Defaults(t *testing.T) {
	cfg, err := LoadStartup(writeTemp(t, "admin.env", minimalAdmin))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.I18N("credits_name", "zh", DefaultCreditsName) != DefaultCreditsName {
		t.Errorf("credits_name zh = %q, want %q", cfg.I18N("credits_name", "zh", DefaultCreditsName), DefaultCreditsName)
	}
	if cfg.CheckinTZOffset != 0 {
		t.Errorf("CHECKIN_TZ_OFFSET = %d, want 0", cfg.CheckinTZOffset)
	}
	if cfg.SMTP.Port != 587 {
		t.Errorf("SMTP_PORT = %d, want 587", cfg.SMTP.Port)
	}
	if cfg.SMTP.TLS != "" {
		t.Errorf("SMTP_TLS = %q, want empty (auto)", cfg.SMTP.TLS)
	}
	if cfg.WebRPMPerIP != 120 {
		t.Errorf("WEB_RPM_PER_IP = %d, want 120", cfg.WebRPMPerIP)
	}
	if cfg.WebThrottleSec != 60 {
		t.Errorf("WEB_THROTTLE_SEC = %d, want 60", cfg.WebThrottleSec)
	}
	if cfg.AuthFailRPMPerIP != 30 {
		t.Errorf("AUTH_FAIL_RPM_PER_IP = %d, want 30", cfg.AuthFailRPMPerIP)
	}
	if cfg.RPMWindowSec != 60 {
		t.Errorf("RPM_WINDOW_SEC = %d, want 60", cfg.RPMWindowSec)
	}
	if cfg.IPThrottleWindowSec != 60 {
		t.Errorf("IP_THROTTLE_WINDOW_SEC = %d, want 60", cfg.IPThrottleWindowSec)
	}
	if cfg.LogDetailMaxChars != 500 {
		t.Errorf("LOG_DETAIL_MAX_CHARS = %d, want 500", cfg.LogDetailMaxChars)
	}
}

func TestLoadStartup_CheckinTZOffset(t *testing.T) {
	// Valid negative.
	p := writeTemp(t, "admin.env", minimalAdmin+"CHECKIN_TZ_OFFSET=-5\n")
	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CheckinTZOffset != -5 {
		t.Errorf("CHECKIN_TZ_OFFSET = %d, want -5", cfg.CheckinTZOffset)
	}

	// Out of range (too low) → fallback to 0 with warning.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	p2 := writeTemp(t, "admin.env", minimalAdmin+"CHECKIN_TZ_OFFSET=-13\n")
	cfg2, _ := LoadStartup(p2)
	if cfg2.CheckinTZOffset != 0 {
		t.Errorf("CHECKIN_TZ_OFFSET out-of-range low = %d, want 0", cfg2.CheckinTZOffset)
	}
	if !strings.Contains(buf.String(), "[CONFIG]") {
		t.Error("expected [CONFIG] warning for out-of-range offset")
	}
}

func TestLoadStartup_SMTPTLS(t *testing.T) {
	// Valid values.
	for _, mode := range []string{"starttls", "implicit", "STARTTLS", "IMPLICIT"} {
		p := writeTemp(t, "admin.env", minimalAdmin+"SMTP_TLS="+mode+"\n")
		cfg, err := LoadStartup(p)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", mode, err)
		}
		want := strings.ToLower(mode)
		if cfg.SMTP.TLS != want {
			t.Errorf("SMTP_TLS=%q → %q, want %q", mode, cfg.SMTP.TLS, want)
		}
	}

	// Invalid value → fallback to empty with warning.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	p2 := writeTemp(t, "admin.env", minimalAdmin+"SMTP_TLS=tlsv1\n")
	cfg2, _ := LoadStartup(p2)
	if cfg2.SMTP.TLS != "" {
		t.Errorf("SMTP_TLS invalid = %q, want empty", cfg2.SMTP.TLS)
	}
	if !strings.Contains(buf.String(), "[CONFIG]") {
		t.Error("expected [CONFIG] warning for invalid SMTP_TLS")
	}
}

func TestLoadStartup_WebRPMDisabled(t *testing.T) {
	p := writeTemp(t, "admin.env", minimalAdmin+"WEB_RPM_PER_IP=0\nAUTH_FAIL_RPM_PER_IP=0\n")
	cfg, err := LoadStartup(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebRPMPerIP != 0 {
		t.Errorf("WEB_RPM_PER_IP = %d, want 0", cfg.WebRPMPerIP)
	}
	if cfg.AuthFailRPMPerIP != 0 {
		t.Errorf("AUTH_FAIL_RPM_PER_IP = %d, want 0", cfg.AuthFailRPMPerIP)
	}
}

func TestGetIntOrAllowZero_WarnsOnNegative(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Negative → fallback + warn.
	result := getIntOrAllowZero(map[string]string{"NEG_KEY": "-1"}, "NEG_KEY", 100)
	if result != 100 {
		t.Errorf("expected fallback 100, got %d", result)
	}
	if !strings.Contains(buf.String(), "[CONFIG]") || !strings.Contains(buf.String(), "non-negative") {
		t.Errorf("expected [CONFIG] warning for negative, got: %s", buf.String())
	}

	// Zero is valid.
	buf.Reset()
	result2 := getIntOrAllowZero(map[string]string{"ZERO_KEY": "0"}, "ZERO_KEY", 50)
	if result2 != 0 {
		t.Errorf("expected 0, got %d", result2)
	}
	if buf.Len() > 0 {
		t.Errorf("expected no log for valid zero, got: %s", buf.String())
	}

	// Missing key → silent fallback.
	buf.Reset()
	result3 := getIntOrAllowZero(map[string]string{}, "MISSING", 42)
	if result3 != 42 {
		t.Errorf("expected 42, got %d", result3)
	}
	if buf.Len() > 0 {
		t.Errorf("expected no log for unset, got: %s", buf.String())
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
