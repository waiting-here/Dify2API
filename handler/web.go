package handler

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dify2api/config"
	"dify2api/db"

	"dify2api/web"
)

// registerWebRoutes serves the embedded SPA and its static assets.
// The SPA is host-aware: it renders the user site or the admin site depending
// on the request host (see hostSeparation).
func (g *Gateway) registerWebRoutes(mux *http.ServeMux) {
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err)
	}
	// Favicon: serve the user-supplied image file (if configured), otherwise 204.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		g.serveFavicon(w)
	})
	// Credits logo: serve the user-supplied image/emoji for the credits card.
	mux.HandleFunc("GET /credits-logo", func(w http.ResponseWriter, r *http.Request) {
		g.serveCreditsLogo(w)
	})
	// versions after redeploys (browser and Cloudflare revalidate every load).
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticFS))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		staticHandler.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			http.Error(w, "frontend missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	})

	// Public site info for the SPA's host-mode detection, branding, and legal pages.
	mux.HandleFunc("GET /api/site-info", func(w http.ResponseWriter, r *http.Request) {
		donationEnabled := g.Store.GetSettingString(db.SettingDonationEnabled, "") == "true"
		charityEnabled := g.Store.GetSettingString(db.SettingCharityEnabled, "") == "true"
		maintenanceMode := g.Store.GetSettingString(db.SettingMaintenanceMode, "") == "true"
		donationReviewLimit := g.Store.GetSettingIntAllowZero(db.SettingDonationReviewLimit, db.DefaultDonationReviewLimit)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"site_host":             g.Config.Admin.SiteHost,
			"admin_host":            g.Config.Admin.AdminHost,
			"site_name":             g.Config.Admin.SiteName,
			"site_name_zh":          g.Config.I18N("site_name", "zh", g.Config.Admin.SiteName),
			"site_name_en":          g.Config.I18N("site_name", "en", g.Config.Admin.SiteName),
			"report_email":          g.Config.Admin.ReportEmail,
			"site_base_url":         g.Config.Admin.SiteBaseURL,
			"donation_enabled":      donationEnabled,
			"charity_enabled":       charityEnabled,
			"maintenance_mode":      maintenanceMode,
			"donation_review_limit": donationReviewLimit,
			"credits_name":          g.Config.I18N("credits_name", "zh", config.DefaultCreditsName),
			"credits_name_zh":       g.Config.I18N("credits_name", "zh", config.DefaultCreditsName),
			"credits_name_en":       g.Config.I18N("credits_name", "en", config.DefaultCreditsName),
			"credits_logo_text":     g.Config.CreditsLogoText,
		})
	})

	// Legal pages, served as static HTML with server-side placeholder substitution.
	// Language routing: ?lang=en or user's Lang preference selects the .en.html
	// variant; silently falls back to the default (Chinese) if the English file
	// is absent.
	servePage := func(w http.ResponseWriter, r *http.Request, name string, maxAge int) {
		fileName := name
		if g.resolvePageLang(r) == "en" {
			enName := strings.Replace(name, ".html", ".en.html", 1)
			if _, err := fs.ReadFile(staticFS, enName); err == nil {
				fileName = enName
			}
		}
		data, err := fs.ReadFile(staticFS, fileName)
		if err != nil {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}
		body := strings.ReplaceAll(string(data), "__SITE_NAME__", g.Config.Admin.SiteName)
		body = strings.ReplaceAll(body, "__REPORT_EMAIL__", g.Config.Admin.ReportEmail)
		body = strings.ReplaceAll(body, "__SITE_BASE_URL__", g.Config.Admin.SiteBaseURL)
		body = strings.ReplaceAll(body, "__SOURCE_URL__", g.Config.Admin.SourceURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
		w.Write([]byte(body))
	}
	mux.HandleFunc("GET /privacy", func(w http.ResponseWriter, r *http.Request) { servePage(w, r, "privacy.html", 0) })
	mux.HandleFunc("GET /terms", func(w http.ResponseWriter, r *http.Request) { servePage(w, r, "terms.html", 0) })
	mux.HandleFunc("GET /403", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); servePage(w, r, "403.html", 0) })
	mux.HandleFunc("GET /404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404); servePage(w, r, "404.html", 0) })
}

// resolvePageLang delegates to resolveLang (the canonical language detection).
func (g *Gateway) resolvePageLang(r *http.Request) string {
	return g.resolveLang(r)
}

// serveCreditsLogo reads the configured credits logo file and serves it,
// mirroring serveFavicon's semantics (204 when unconfigured).
func (g *Gateway) serveCreditsLogo(w http.ResponseWriter) {
	p := g.Config.CreditsLogoPath
	if p == "" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		// Short cache to avoid browsers caching a transient error.
		w.Header().Set("Cache-Control", "public, max-age=30")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ext := filepath.Ext(p)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400, s-maxage=86400")
	w.Write(data)
}

// serveFavicon reads the configured favicon file and serves it with a
// long cache lifetime and a correct Content-Type. When no favicon is
// configured it returns HTTP 204 (no content) so browsers don't show
// a broken-image icon.
func (g *Gateway) serveFavicon(w http.ResponseWriter) {
	p := g.Config.FaviconPath
	if p == "" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ext := filepath.Ext(p)
	if ext == "" {
		ext = ".ico"
	}
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "image/x-icon"
	}
	w.Header().Set("Content-Type", ct)
	// Long cache but NOT immutable — the deployer may change the icon file
	// without renaming it, and browsers/CDN need to be able to revalidate.
	w.Header().Set("Cache-Control", "public, max-age=86400, s-maxage=86400")
	w.Write(data)
}
