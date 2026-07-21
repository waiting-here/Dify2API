package handler

import (
	"net"
	"net/http"
	"strings"
)

// Wrap applies gateway-wide middleware, outermost first:
//  1. HTTPS enforcement (when -force-https is on: plain-HTTP requests get a
//     301 redirect to https://…)
//  2. Host-based separation between the user site and the admin site
func (g *Gateway) Wrap(next http.Handler) http.Handler {
	return g.forceHTTPS(g.hostSeparation(next))
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
//     /api/admin/*, /privacy, /terms — no Discord OAuth, no /v1 API, no user UI.
//   - user site (everything else): Discord OAuth, /v1 API, user UI — but NO
//     admin login and NO /api/admin/*.
func (g *Gateway) hostSeparation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if g.isAdminHost(r) {
			if p == "/" || strings.HasPrefix(p, "/static/") || p == "/api/site-info" ||
				p == "/api/auth/admin/login" || p == "/api/auth/logout" || p == "/api/me" || p == "/privacy" || p == "/terms" || p == "/403" || p == "/404" ||
				strings.HasPrefix(p, "/api/admin/") || p == "/health" || p == "/favicon.ico" {
				next.ServeHTTP(w, r)
				return
			}
			g.writeError(w, http.StatusNotFound, "not_found", "not found")
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
