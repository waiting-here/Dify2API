package handler

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Wrap applies gateway-wide middleware, outermost first:
//  1. HTTPS enforcement (when -force-https is on: plain-HTTP requests get a
//     301 redirect to https://…)
//  2. Host-based separation between the user site and the admin site
//  3. Per-IP rate limiting for /api/* web endpoints (F7)
func (g *Gateway) Wrap(next http.Handler) http.Handler {
	return g.forceHTTPS(g.hostSeparation(g.webRateLimit(next)))
}

// webRateLimit applies the per-IP sliding-window limit to /api/* session
// endpoints only (F7). Static assets and pages are exempt (a normal page
// load fetches many resources), and the /v1/* OpenAI-compatible API is
// governed by its own defences (caller-key auth + three-class RPM +
// invalid-key throttle). Exceeding the cap yields temporary 429 responses
// (with Retry-After) — no ban, no effect on /v1/*.
func (g *Gateway) webRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			now := time.Now()
			ip := clientIP(r)
			if !g.webThrottle.allow(ip, now) {
				w.Header().Set("Retry-After", strconv.Itoa(g.webThrottle.retryAfterSec(ip, now)))
				g.writeError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// forceHTTPS redirects HTTP requests to HTTPS when enabled. Behind a
// TLS-terminating proxy it trusts X-Forwarded-Proto.
//
// SECURITY: the Go server MUST sit behind a trusted reverse proxy (nginx)
// that correctly sets X-Forwarded-Proto.  Do NOT expose the Go listener
// directly to the public internet — an attacker who bypasses the proxy can
// send X-Forwarded-Proto: https and bypass the redirect.
func (g *Gateway) forceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.Config.ForceHTTPS && r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
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
				p == "/api/services" ||
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

// isAdminHost reports whether the request targets the admin site.
func (g *Gateway) isAdminHost(r *http.Request) bool {
	return strings.EqualFold(stripPort(r.Host), g.Config.Admin.AdminHost)
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
