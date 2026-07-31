package handler

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
)

// Wrap applies gateway-wide middleware, outermost first:
//  1. Reject unknown Host values before constructing any redirect.
//  2. HTTPS enforcement using a configured, fixed redirect authority.
//  3. Host-based separation between the user site and the admin site.
//  4. Maintenance mode and per-IP web rate limiting.
//  5. A hard byte cap for state-changing /api/* request bodies.
func (g *Gateway) Wrap(next http.Handler) http.Handler {
	return g.validateHost(g.forceHTTPS(g.hostSeparation(g.maintenanceCheck(g.webRateLimit(g.webBodyLimit(next))))))
}

// webRateLimit applies the per-IP sliding-window limit to /api/* session
// endpoints and the anonymous Discord-login initializer. Static assets and
// pages are exempt (a normal page load fetches many resources), and the /v1/* OpenAI-compatible API is
// governed by its own defences (caller-key auth + three-class RPM +
// invalid-key throttle). Exceeding the cap yields temporary 429 responses
// (with Retry-After) — no ban, no effect on /v1/*.
func (g *Gateway) webRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/auth/discord/login" {
			now := time.Now()
			ip := g.clientIP(r)
			if !g.webThrottle.allow(ip, now) {
				w.Header().Set("Retry-After", strconv.Itoa(g.webThrottle.retryAfterSec(ip, now)))
				g.writeError(w, http.StatusTooManyRequests, "rate_limited", t(g.resolveLang(r), "请求过于频繁，请稍后再试", "Too many requests, please try again later"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) validateHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := stripPort(r.Host)
		if !strings.EqualFold(host, g.Config.Admin.SiteHost) &&
			!strings.EqualFold(host, stripPort(g.Config.Admin.AdminHost)) {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forceHTTPS redirects HTTP requests to a configured HTTPS origin. A
// forwarding proto is trusted only from an explicitly configured proxy peer.
func (g *Gateway) forceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardedHTTPS := g.trustedProxyRequest(r) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
		if g.Config.ForceHTTPS && r.TLS == nil && !forwardedHTTPS {
			host := g.Config.Admin.SiteURLHost
			if g.isAdminHost(r) {
				host = g.Config.Admin.AdminHost
			}
			target := (&url.URL{Scheme: "https", Host: host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}).String()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) webBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			(r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch && r.Method != http.MethodDelete) {
			next.ServeHTTP(w, r)
			return
		}

		limit := int64(g.Config.MaxWebRequestBodyKB) << 10
		if r.ContentLength > limit {
			g.writeError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				"[Dify2API] request body exceeds the configured limit")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "[Dify2API] unable to read request body")
			return
		}
		if int64(len(body)) > limit {
			g.writeError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				"[Dify2API] request body exceeds the configured limit")
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

// hostSeparation enforces the split between the user site and the admin site:
//   - admin site (admin.<site>): ONLY admin login/logout, /api/me,
//     /api/admin/*, /api/services (read-only registry for the log filter),
//     /privacy, /terms — no Discord OAuth, no /v1 API, no user UI.
//   - user site (everything else): Discord OAuth, /v1 API, user UI — but NO
//     admin login and NO /api/admin/*.
func (g *Gateway) hostSeparation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if g.isAdminHost(r) {
			if p == "/" || strings.HasPrefix(p, "/static/") || p == "/api/site-info" ||
				p == "/api/auth/admin/login" || p == "/api/auth/logout" || p == "/api/me" || p == "/privacy" || p == "/terms" || p == "/403" || p == "/404" ||
				p == "/api/services" || p == "/api/bulletins" ||
				strings.HasPrefix(p, "/api/admin/") || p == "/health" || p == "/favicon.ico" {
				next.ServeHTTP(w, r)
				return
			}
			g.serve404Page(w, r)
			return
		}
		// User site: admin endpoints are not exposed here.
		if p == "/api/auth/admin/login" || strings.HasPrefix(p, "/api/admin/") {
			g.writeError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maintenanceCheck enforces the site-wide maintenance mode.
// It sits after hostSeparation so admin-host requests are already routed;
// the admin host is unconditionally excused.  Static assets, legal pages,
// health checks, and Discord OAuth paths also pass through so that the
// maintenance page itself renders correctly and login flows stay intact.
//
// When maintenance mode is on for the user host:
//   - Web pages (/, /dashboard, …) → 503 + maintenance.html
//   - API endpoints (/api/*, /v1/*) → 503 + JSON error
func (g *Gateway) maintenanceCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Admin host: never affected by maintenance.
		if g.isAdminHost(r) {
			next.ServeHTTP(w, r)
			return
		}

		p := r.URL.Path

		// Static assets and pages that the maintenance page itself needs.
		if strings.HasPrefix(p, "/static/") || p == "/credits-logo" || p == "/favicon.ico" ||
			p == "/privacy" || p == "/terms" || p == "/403" || p == "/404" ||
			p == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Discord OAuth flow: login redirect and callback must work so that
		// users can complete login (they will see the maintenance page after).
		if strings.HasPrefix(p, "/auth/discord/") {
			next.ServeHTTP(w, r)
			return
		}

		// Maintenance mode off — nothing to do.
		if g.Store.GetSettingString(db.SettingMaintenanceMode, "") != "true" {
			next.ServeHTTP(w, r)
			return
		}

		// Maintenance mode ON.
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/v1/") {
			// API endpoints → JSON error.
			g.writeError(w, http.StatusServiceUnavailable, "maintenance", t(g.resolveLang(r), "站点维护中", "Site under maintenance"))
			return
		}

		// All other paths (web pages) → maintenance HTML.
		g.serveMaintenancePage(w, r)
	})
}

// isAdminHost reports whether the request targets the admin site.
func (g *Gateway) isAdminHost(r *http.Request) bool {
	return strings.EqualFold(stripPort(r.Host), stripPort(g.Config.Admin.AdminHost))
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
