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
	// DifyHTTPTimeoutMs is the per-request timeout for calls to Dify Apps (default 600000).
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
		DifyHTTPTimeoutMs: getIntOr(envMap, "DIFY_HTTP_TIMEOUT_MS", 600000),
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
