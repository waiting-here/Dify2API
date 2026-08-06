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

	"dify2api/auth"
	"dify2api/db"
)

// Wrap applies gateway-wide middleware, outermost first:
//  1. Reject unknown Host values before constructing any redirect.
//  2. Security response headers.
//  3. HTTPS enforcement using a configured, fixed redirect authority.
//  4. Host-based separation between the user site and the admin site.
//  5. Origin checks for cookie-authenticated state changes.
//  6. Maintenance mode and per-IP web rate limiting.
//  7. A hard byte cap for state-changing /api/* request bodies.
func (g *Gateway) Wrap(next http.Handler) http.Handler {
	return g.validateHost(g.securityHeaders(g.forceHTTPS(g.hostSeparation(g.csrfOriginCheck(g.maintenanceCheck(g.webRateLimit(g.webBodyLimit(g.apiErrorEnvelope(next)))))))))
}

// apiErrorEnvelope converts the standard library ServeMux's plaintext 404/405
// fallback into the same JSON contract used by application handlers. It only
// touches API paths and preserves successful responses, HTML pages, and SSE.
func (g *Gateway) apiErrorEnvelope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		base := &apiErrorResponseWriter{ResponseWriter: w, gateway: g}
		if _, ok := w.(http.Flusher); ok {
			next.ServeHTTP(&apiErrorFlushingResponseWriter{apiErrorResponseWriter: base}, r)
			return
		}
		next.ServeHTTP(base, r)
	})
}

type apiErrorResponseWriter struct {
	http.ResponseWriter
	gateway  *Gateway
	replaced bool
}

func (w *apiErrorResponseWriter) WriteHeader(status int) {
	contentType := strings.ToLower(w.Header().Get("Content-Type"))
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		!strings.HasPrefix(contentType, "application/json") {
		code := "not_found"
		if status == http.StatusMethodNotAllowed {
			code = "method_not_allowed"
		}
		w.Header().Del("Content-Length")
		w.gateway.writeError(w.ResponseWriter, status, code, strings.ToLower(http.StatusText(status)))
		w.replaced = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiErrorResponseWriter) Write(p []byte) (int, error) {
	if w.replaced {
		return len(p), nil
	}
	return w.ResponseWriter.Write(p)
}

type apiErrorFlushingResponseWriter struct {
	*apiErrorResponseWriter
}

func (w *apiErrorFlushingResponseWriter) Flush() {
	w.ResponseWriter.(http.Flusher).Flush()
}

func (g *Gateway) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if g.requestIsHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || (g.trustedProxyRequest(r) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"))
}

// csrfOriginCheck protects browser session endpoints from cross-site state
// changes. Anonymous requests (including admin login) and caller-key /v1
// requests retain their existing semantics; a request is subject to this check
// only when it carries the session cookie and targets a mutating /api route.
func (g *Gateway) csrfOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isStateChangingMethod(r.Method) || !strings.HasPrefix(r.URL.Path, "/api/") || auth.SessionToken(r) == "" {
			next.ServeHTTP(w, r)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" || !g.sameSiteOrigin(r, origin) {
			g.writeError(w, http.StatusForbidden, "forbidden", "invalid request origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (g *Gateway) sameSiteOrigin(r *http.Request, rawOrigin string) bool {
	originURL, err := url.Parse(rawOrigin)
	if err != nil || originURL.Scheme == "" || originURL.Host == "" || originURL.User != nil || originURL.Path != "" || originURL.RawQuery != "" || originURL.Fragment != "" {
		return false
	}
	siteURL, err := url.Parse(g.Config.Admin.SiteBaseURL)
	if err != nil || siteURL.Scheme == "" || siteURL.Host == "" {
		return false
	}
	expected := siteURL
	if g.isAdminHost(r) {
		expected = &url.URL{Scheme: siteURL.Scheme, Host: g.Config.Admin.AdminHost}
	}
	return canonicalOrigin(originURL) == canonicalOrigin(expected)
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
			g.writeError(w, http.StatusMisdirectedRequest, "invalid_request", "misdirected request")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forceHTTPS redirects HTTP requests to a configured HTTPS origin. A
// forwarding proto is trusted only from an explicitly configured proxy peer.
func (g *Gateway) forceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.Config.ForceHTTPS && !g.requestIsHTTPS(r) {
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
			if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/v1/") {
				g.writeError(w, http.StatusNotFound, "not_found", "not found")
			} else {
				g.serve404Page(w, r)
			}
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
