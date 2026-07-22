package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all gateway configuration, loaded from the single startup
// file (-admin flag). OS environment variables override any same-named value.
type Config struct {
	// ListenAddr is the address the HTTP server listens on (default localhost:10086).
	ListenAddr string
	// DifyHTTPTimeoutMs is the per-request timeout for calls to Dify Apps (default 900000).
	DifyHTTPTimeoutMs int
	// DBPath is the SQLite database file path (default ./dify2api.db).
	DBPath string
	// MasterKeyPath is the encryption master key file path (default ./dify2api.key).
	MasterKeyPath string
	// FaviconPath is an optional local image file to serve as the browser tab icon
	// (/favicon.ico). Supports .ico, .png, .svg, .gif, .webp, .jpg/.jpeg.
	// When empty, /favicon.ico returns 204 (no icon).
	FaviconPath string

	// Debug enables debug-interception mode (set via the -debug CLI flag).
	Debug bool
	// DebugDir is the directory where debug dumps are written (set via -debug-dir).
	DebugDir string
	// ForceHTTPS redirects plain-HTTP requests to HTTPS (set via -force-https).
	ForceHTTPS bool

	// MaxChatInFlight is the global cap on concurrent chat requests (default 64).
	MaxChatInFlight int
	// MaxRequestBodyMB caps /v1/chat/completions request bodies (default 10).
	MaxRequestBodyMB int
	// SSEBufferMB is the initial per-stream SSE parse buffer in MB (default 10;
	// the hard max per SSE line stays 50MB).
	SSEBufferMB int

	// LoginMaxFailures: failures within the window that trigger a lock (default 5).
	LoginMaxFailures int
	// LoginWindowMin: sliding failure-counting window in minutes (default 10).
	LoginWindowMin int
	// LoginLockMin: lock duration in minutes (default 60).
	LoginLockMin int
	// LoginMinLatencyMs: constant minimum latency per login attempt (default 300).
	LoginMinLatencyMs int

	// Admin holds the administrator account and Discord OAuth credentials.
	Admin AdminConfig

	// --- alpha.3: Public-resource credits ---
	// CreditsName is the name shown to users for the credit statistic.
	CreditsName string
	// CreditsLogoText is an emoji or text displayed next to the credits name.
	CreditsLogoText string
	// CreditsLogoPath is a local image file path for the credits logo; takes
	// precedence over CreditsLogoText when non-empty.
	CreditsLogoPath string
	// CheckinTZOffset is the UTC offset in hours for the check-in day
	// boundary. Allowed range is [-12, 14]; out-of-range values are logged
	// and reset to 0.
	CheckinTZOffset int

	// --- alpha.3: SMTP (email alerts) ---
	SMTP SMTPConfig

	// --- alpha.3: Web IP rate-limit (F7) ---
	// WebRPMPerIP is the per-IP requests-per-minute cap for /api/* endpoints.
	// 0 disables IP rate-limiting.
	WebRPMPerIP int
	// WebThrottleSec is how many seconds the caller gets 429 after exceeding
	// WebRPMPerIP.
	WebThrottleSec int
	// AuthFailRPMPerIP is the per-IP limit for /v1/* invalid-key requests;
	// 0 disables.
	AuthFailRPMPerIP int

	// --- alpha.3: tunable windows ---
	// RPMWindowSec is the three-class RPM sliding window in seconds (default 60).
	RPMWindowSec int
	// IPThrottleWindowSec is the per-IP sliding window in seconds (default 60).
	IPThrottleWindowSec int
	// LogDetailMaxChars is the maximum length of error_detail stored in
	// request_logs (default 500).
	LogDetailMaxChars int
}

// SMTPConfig holds the email-delivery settings for operational alerts (F5).
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
	To   string
	// TLS mode: "starttls" or "implicit". Empty means auto-detect by port
	// (465→implicit, others→starttls).
	TLS string
}

// AdminConfig is the administrator/site section of the startup file.
type AdminConfig struct {
	// Username and Password form the unique administrator account, re-loaded
	// from the startup file on every boot (the file is authoritative).
	// Password may be plaintext or a bcrypt hash ("$2a$"/"$2b$"/"$2y$" prefix).
	Username string
	Password string
	// Discord OAuth application credentials for user login.
	DiscordClientID     string
	DiscordClientSecret string
	// SiteBaseURL is the public base URL of this gateway (Discord redirect URI,
	// session cookie Secure flag).
	SiteBaseURL string
	// SiteHost is derived from SiteBaseURL at load.
	SiteHost string
	// AdminHost is the admin-site hostname. Defaults to "admin."+SiteHost;
	// overridable via ADMIN_HOST.
	AdminHost string
	// SiteName is the public-facing name of this deployment (default "Dify2API").
	SiteName string
	// ReportEmail is the contact address for DMCA/copyright complaints and
	// CSAM reports (shown in the Terms of Service and Privacy Policy).
	// Default "report@example.com".  Set to a monitored address before deploying.
	ReportEmail string
}

// LoadStartup reads the single startup file (path required) and builds the
// full configuration: OS environment variables take precedence over the file.
func LoadStartup(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("startup file is required (use -admin <path>)")
	}
	envMap := make(map[string]string)
	if err := loadEnvFile(path, envMap); err != nil {
		return nil, fmt.Errorf("cannot read startup file %s: %w", path, err)
	}

	cfg := &Config{
		ListenAddr:        getOr(envMap, "LISTEN_ADDR", "localhost:10086"),
		DifyHTTPTimeoutMs: getIntOr(envMap, "DIFY_HTTP_TIMEOUT_MS", 900000),
		DBPath:            getOr(envMap, "DIFY2API_DB_PATH", "dify2api.db"),
		MasterKeyPath:     getOr(envMap, "DIFY2API_MASTER_KEY_PATH", "dify2api.key"),
		FaviconPath:       getOr(envMap, "FAVICON_PATH", ""),
		MaxChatInFlight:   getIntOr(envMap, "MAX_CHAT_IN_FLIGHT", 64),
		MaxRequestBodyMB:  getIntOr(envMap, "MAX_REQUEST_BODY_MB", 10),
		SSEBufferMB:       getIntOr(envMap, "SSE_BUFFER_MB", 10),
		LoginMaxFailures:  getIntOr(envMap, "LOGIN_MAX_FAILURES", 5),
		LoginWindowMin:    getIntOr(envMap, "LOGIN_WINDOW_MIN", 10),
		LoginLockMin:      getIntOr(envMap, "LOGIN_LOCK_MIN", 60),
		LoginMinLatencyMs: getIntOr(envMap, "LOGIN_MIN_LATENCY_MS", 300),

		// alpha.3 — public-resource credits.
		CreditsName:     getOr(envMap, "CREDITS_NAME", "公益 Dify2API 积分"),
		CreditsLogoText: getOr(envMap, "CREDITS_LOGO_TEXT", "🌟"),
		CreditsLogoPath: getOr(envMap, "CREDITS_LOGO_PATH", ""),
		CheckinTZOffset: getCheckinTZOffset(envMap),

		// alpha.3 — SMTP.
		SMTP: SMTPConfig{
			Host: getOr(envMap, "SMTP_HOST", ""),
			Port: getIntOrAllowZero(envMap, "SMTP_PORT", 587),
			User: getOr(envMap, "SMTP_USER", ""),
			Pass: getOr(envMap, "SMTP_PASS", ""),
			From: getOr(envMap, "SMTP_FROM", ""),
			To:   getOr(envMap, "SMTP_TO", ""),
			TLS:  getSMTPTLS(envMap),
		},

		// alpha.3 — Web IP rate-limit.
		WebRPMPerIP:      getIntOrAllowZero(envMap, "WEB_RPM_PER_IP", 120),
		WebThrottleSec:   getIntOr(envMap, "WEB_THROTTLE_SEC", 60),
		AuthFailRPMPerIP: getIntOrAllowZero(envMap, "AUTH_FAIL_RPM_PER_IP", 30),

		// alpha.3 — tunable windows.
		RPMWindowSec:        getIntOr(envMap, "RPM_WINDOW_SEC", 60),
		IPThrottleWindowSec: getIntOr(envMap, "IP_THROTTLE_WINDOW_SEC", 60),
		LogDetailMaxChars:   getIntOr(envMap, "LOG_DETAIL_MAX_CHARS", 500),
	}

	a := &cfg.Admin
	a.Username = getOr(envMap, "ADMIN_USERNAME", "")
	a.Password = getOr(envMap, "ADMIN_PASSWORD", "")
	a.DiscordClientID = getOr(envMap, "DISCORD_CLIENT_ID", "")
	a.DiscordClientSecret = getOr(envMap, "DISCORD_CLIENT_SECRET", "")
	a.SiteBaseURL = strings.TrimRight(getOr(envMap, "SITE_BASE_URL", ""), "/")
	if u, err := url.Parse(a.SiteBaseURL); err == nil {
		a.SiteHost = u.Hostname()
	}
	a.AdminHost = getOr(envMap, "ADMIN_HOST", "")
	if a.AdminHost == "" && a.SiteHost != "" {
		a.AdminHost = "admin." + a.SiteHost
	}
	a.SiteName = getOr(envMap, "SITE_NAME", "Dify2API")
	a.ReportEmail = getOr(envMap, "REPORT_EMAIL", "report@example.com")

	missing := []string{}
	if a.Username == "" {
		missing = append(missing, "ADMIN_USERNAME")
	}
	if a.Password == "" {
		missing = append(missing, "ADMIN_PASSWORD")
	}
	if a.DiscordClientID == "" {
		missing = append(missing, "DISCORD_CLIENT_ID")
	}
	if a.DiscordClientSecret == "" {
		missing = append(missing, "DISCORD_CLIENT_SECRET")
	}
	if a.SiteBaseURL == "" {
		missing = append(missing, "SITE_BASE_URL")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("startup file %s missing required keys: %s", path, strings.Join(missing, ", "))
	}

	// SMTP_FROM falls back to SMTP_USER when empty.
	if cfg.SMTP.From == "" {
		cfg.SMTP.From = cfg.SMTP.User
	}

	return cfg, nil
}

// getOr looks up a key (OS environment first, then file), falling back.
func getOr(envFile map[string]string, key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	if val, ok := envFile[key]; ok && val != "" {
		return val
	}
	return fallback
}

// getIntOr is getOr for positive integers (fallback when unset or invalid).
func getIntOr(envFile map[string]string, key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok {
		raw, ok = envFile[key]
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		log.Printf("[CONFIG] %s=%q is not a positive integer; using default %d", key, raw, fallback)
		return fallback
	}
	return n
}

// getIntOrAllowZero is like getIntOr but accepts 0 as a valid value.
// Negative values trigger the [CONFIG] warning and fall back to default.
func getIntOrAllowZero(envFile map[string]string, key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok {
		raw, ok = envFile[key]
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		log.Printf("[CONFIG] %s=%q is not a non-negative integer; using default %d", key, raw, fallback)
		return fallback
	}
	return n
}

// getCheckinTZOffset parses CHECKIN_TZ_OFFSET with range validation [-12, 14].
func getCheckinTZOffset(envFile map[string]string) int {
	raw, ok := os.LookupEnv("CHECKIN_TZ_OFFSET")
	if !ok {
		raw, ok = envFile["CHECKIN_TZ_OFFSET"]
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < -12 || n > 14 {
		log.Printf("[CONFIG] CHECKIN_TZ_OFFSET=%q is out of range [-12,14] or invalid; using default 0", raw)
		return 0
	}
	return n
}

// getSMTPTLS parses SMTP_TLS with valid-value enforcement.
func getSMTPTLS(envFile map[string]string) string {
	raw, ok := os.LookupEnv("SMTP_TLS")
	if !ok {
		raw, ok = envFile["SMTP_TLS"]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "" // auto-detect by port
	}
	raw = strings.ToLower(raw)
	if raw != "starttls" && raw != "implicit" {
		log.Printf("[CONFIG] SMTP_TLS=%q is not one of 'starttls' or 'implicit'; using auto-detect", raw)
		return ""
	}
	return raw
}

// loadEnvFile parses a simple KEY=VALUE file into the provided map.
// Supports comments (# prefix) and blank lines. No quoting or multi-line
// values — intentional to keep the parser small and auditable.
func loadEnvFile(path string, dst map[string]string) error {
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Clean(path)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		dst[key] = value
	}
	return nil
}
