package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dify2api/config"
	"dify2api/db"
	"dify2api/dify"
	"dify2api/mailer"
	"dify2api/openai"
	"dify2api/translator"
	"dify2api/web"
)

// Gateway handles HTTP requests. The request path is:
// caller key -> user -> model full name -> per-user App config -> Dify App.
type Gateway struct {
	Config  *config.Config
	Store   *db.Store
	limiter *rateLimiter
	// chatSem is a global in-flight cap for chat requests (backpressure):
	// when full, new requests are rejected with 429 instead of piling up
	// memory-hungry upstream connections.
	chatSem chan struct{}
	// loginThrottle guards the admin login against brute force (L2).
	loginThrottle *loginThrottle
	// webThrottle rate-limits /api/* session endpoints per source IP (F7).
	webThrottle *ipThrottle
	// authFailThrottle rate-limits invalid-caller-key /v1/* requests per
	// source IP (invalid-key flood defence; valid keys are never counted).
	authFailThrottle *ipThrottle
	// mailer delivers optional email alerts (nil when SMTP not configured).
	mailer *mailer.Mailer
	// userDebug manages per-user self-service debug sessions.
	userDebug *userDebugHub
	// donationLimiter enforces per-donation RPM limits.
	donationLimiter *donationRateLimiter
	// difyPolicy pins DNS and blocks non-public Dify egress unless the
	// deployment operator explicitly allowlists an origin/network.
	difyPolicy *dify.EgressPolicy
	// difyProbeSem bounds concurrent user-triggered /parameters probes.
	difyProbeSem chan struct{}
	// probeLimiter caps per-user App compatibility probes (5/min) so the
	// informational check cannot be used as an unlimited Dify cannon.
	probeLimiter *probeLimiter
	// remoteContentOrigins gates URLs fetched inside remote Dify workflows.
	remoteContentOrigins map[string]struct{}
	// antiAbuseCache maps service -> per-service anti-abuse config (hot path).
	antiAbuseMu    sync.RWMutex
	antiAbuseCache map[string]*db.AntiAbuseConfig
	settlement     *charitySettlementWorker
}

// NewGateway creates a new Gateway.
func NewGateway(cfg *config.Config, store *db.Store) *Gateway {
	difyPolicy, err := dify.NewEgressPolicy(cfg.DifyEgressAllowlist)
	if err != nil {
		// LoadStartup validates this configuration. Directly constructed test
		// configs still fail closed rather than silently allowing private egress.
		log.Printf("[ERROR] invalid Dify egress policy: %v", err)
		difyPolicy, _ = dify.NewEgressPolicy(nil)
	}
	// Register the gateway's own sites (SITE_BASE_URL/ADMIN_HOST) and listen
	// ports so Dify egress can never dial back into the gateway itself.
	difyPolicy.AddSelfOrigins(cfg.Admin.SiteBaseURL, cfg.Admin.AdminHost, cfg.ListenAddr)
	probeLimit := cfg.DifyProbeInFlight
	if probeLimit <= 0 {
		probeLimit = 8
	}
	remoteOrigins := make(map[string]struct{}, len(cfg.RemoteContentOriginAllowlist))
	for _, raw := range cfg.RemoteContentOriginAllowlist {
		if u, parseErr := url.Parse(raw); parseErr == nil {
			remoteOrigins[canonicalOrigin(u)] = struct{}{}
		}
	}
	gw := &Gateway{
		Config:           cfg,
		Store:            store,
		limiter:          newRateLimiter(cfg.RPMWindowSec),
		chatSem:          make(chan struct{}, cfg.MaxChatInFlight),
		loginThrottle:    newLoginThrottle(cfg),
		webThrottle:      newIPThrottle(cfg.WebRPMPerIP, cfg.WebThrottleSec, cfg.IPThrottleWindowSec),
		authFailThrottle: newIPThrottle(cfg.AuthFailRPMPerIP, 60, cfg.IPThrottleWindowSec),
		mailer: mailer.New(cfg.SMTP, mailer.Options{
			// Read the aggregation window live so admin settings changes
			// apply without a restart.
			CoolMinutes: func() int {
				return store.GetSettingInt(db.SettingMailerCoolMinutes, db.DefaultMailerCoolMinutes)
			},
			// Per-category email switch (alert center prefs).
			EmailEnabled: func(et mailer.EventType) bool {
				return store.IsAlertEmailEnabled(string(et))
			},
			// Mirror every event into the alert center; the store's
			// show_in_center gate decides whether a record is written.
			Record: func(et mailer.EventType, summary string) {
				if err := store.AddAdminAlert(&db.AdminAlert{Type: string(et), Message: summary}); err != nil {
					log.Printf("[ERROR] record mailer alert %s: %v", et, err)
				}
			},
		}),
		userDebug:            newUserDebugHub(),
		donationLimiter:      newDonationRateLimiter(),
		difyPolicy:           difyPolicy,
		difyProbeSem:         make(chan struct{}, probeLimit),
		probeLimiter:         newProbeLimiter(),
		remoteContentOrigins: remoteOrigins,
	}
	settlementAttemptSec := cfg.CharitySettlementAttemptTimeoutSec
	if settlementAttemptSec <= 0 {
		settlementAttemptSec = config.DefaultCharitySettlementAttemptTimeoutSec
	}
	settlementRetryDelayMs := cfg.CharitySettlementRetryDelayMs
	if settlementRetryDelayMs <= 0 {
		settlementRetryDelayMs = config.DefaultCharitySettlementRetryDelayMs
	}
	reservedStaleSec := cfg.CharitySettlementReservedStaleSec
	if reservedStaleSec <= 0 {
		reservedStaleSec = config.DefaultCharitySettlementReservedStaleSec
	}
	dispatchGraceSec := cfg.CharitySettlementDispatchGraceSec
	if dispatchGraceSec <= 0 {
		dispatchGraceSec = config.DefaultCharitySettlementDispatchGraceSec
	}
	scanSec := cfg.CharitySettlementScanIntervalSec
	if scanSec <= 0 {
		scanSec = config.DefaultCharitySettlementScanIntervalSec
	}
	queueSize := cfg.CharitySettlementQueueSize
	if queueSize <= 0 {
		queueSize = config.DefaultCharitySettlementQueueSize
	}
	gw.settlement = newCharitySettlementWorker(store, charitySettlementOptions{
		attemptTimeout:  time.Duration(settlementAttemptSec) * time.Second,
		retryDelay:      time.Duration(settlementRetryDelayMs) * time.Millisecond,
		reservedStale:   time.Duration(reservedStaleSec) * time.Second,
		dispatchedStale: time.Duration(cfg.DifyHTTPTimeoutMs)*time.Millisecond + time.Duration(dispatchGraceSec)*time.Second,
		scanInterval:    time.Duration(scanSec) * time.Second,
		queueSize:       queueSize,
	})
	// Seed alert-center preference rows (defaults on) for every category.
	if err := store.EnsureAlertPrefs(alertPrefEventTypes()); err != nil {
		log.Printf("[WARN] seed alert prefs: %v", err)
	}
	if err := gw.loadAntiAbuseCache(); err != nil {
		log.Printf("[WARN] load anti-abuse cache: %v", err)
	}
	if gw.mailer != nil {
		gw.mailer.Start()
	}
	return gw
}

// Shutdown stops gateway-owned background activity after the HTTP server has
// stopped accepting requests. Database ownership remains with main, which
// closes the Store only after this method and the process cleanup worker have
// returned or the shared shutdown deadline has expired.
func (g *Gateway) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.loginThrottle.shutdown()
	g.webThrottle.shutdown()
	g.authFailThrottle.shutdown()
	g.userDebug.shutdown()
	var errs []error
	if err := g.settlement.shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	if g.mailer != nil {
		if err := g.mailer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RegisterRoutes sets up HTTP routes.
func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", g.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", g.handleChatCompletions)
	mux.HandleFunc("GET /health", g.handleHealth)

	// Auth & web API
	mux.HandleFunc("POST /api/auth/admin/login", g.handleAdminLogin)
	mux.HandleFunc("POST /api/auth/logout", g.handleLogout)
	mux.HandleFunc("GET /api/me", g.handleMe)
	mux.HandleFunc("GET /auth/discord/login", g.handleDiscordLogin)
	mux.HandleFunc("GET /auth/discord/callback", g.handleDiscordCallback)

	// Admin request log viewer
	mux.HandleFunc("GET /api/admin/logs/export", g.handleAdminExportLogs)
	mux.HandleFunc("GET /api/admin/logs/stats", g.handleAdminLogStats)
	mux.HandleFunc("GET /api/admin/logs", g.handleAdminLogs)

	// Admin user management
	mux.HandleFunc("GET /api/admin/users", g.handleAdminListUsers)
	mux.HandleFunc("POST /api/admin/users/{id}/ban", g.handleAdminBanUser)
	mux.HandleFunc("POST /api/admin/users/{id}/unban", g.handleAdminUnbanUser)
	mux.HandleFunc("DELETE /api/admin/users/{id}", g.handleAdminDeleteUser)
	mux.HandleFunc("POST /api/admin/users/{id}/reset-key", g.handleAdminResetUserKey)
	mux.HandleFunc("POST /api/admin/users/{id}/rpm", g.handleAdminSetUserRPM)
	mux.HandleFunc("GET /api/admin/users/{id}/export", g.handleAdminExportUser)
	mux.HandleFunc("GET /api/admin/settings", g.handleAdminGetSettings)
	mux.HandleFunc("PUT /api/admin/settings", g.handleAdminPutSettings)
	mux.HandleFunc("GET /api/admin/level-settings", g.handleAdminGetLevelSettings)
	mux.HandleFunc("PUT /api/admin/level-settings", g.handleAdminPutLevelSettings)
	mux.HandleFunc("PUT /api/admin/users/{id}/level", g.handleAdminSetUserLevel)

	// Alert center (admin)
	mux.HandleFunc("GET /api/admin/alerts", g.handleListAlerts)
	mux.HandleFunc("DELETE /api/admin/alerts", g.handleDeleteAlerts)
	mux.HandleFunc("GET /api/admin/alert-prefs", g.handleListAlertPrefs)
	mux.HandleFunc("PUT /api/admin/alert-prefs", g.handlePutAlertPrefs)

	// Charity / donation admin endpoints
	mux.HandleFunc("POST /api/admin/donations", g.handleCreateDonation)
	mux.HandleFunc("GET /api/admin/donations/applications", g.handleAdminListApplications)
	mux.HandleFunc("GET /api/admin/donations", g.handleListDonations)
	mux.HandleFunc("PATCH /api/admin/donations/{id}", g.handlePatchDonation)
	mux.HandleFunc("POST /api/admin/donations/{id}/status", g.handleDonationStatus)
	mux.HandleFunc("DELETE /api/admin/donations/{id}", g.handleDeleteDonation)

	// Charity pricing admin endpoints (beta.2)
	mux.HandleFunc("GET /api/admin/pricing", g.handleListPricing)
	mux.HandleFunc("PUT /api/admin/pricing", g.handleUpsertPricing)
	mux.HandleFunc("PATCH /api/admin/pricing", g.handlePatchPricing)
	mux.HandleFunc("DELETE /api/admin/pricing", g.handleDeletePricing)

	// Donation application review
	mux.HandleFunc("GET /api/admin/donations/pending", g.handleListPendingApplications)
	mux.HandleFunc("POST /api/admin/donations/{id}/approve", g.handleApproveApplication)
	mux.HandleFunc("POST /api/admin/donations/{id}/reject", g.handleRejectApplication)

	// Anti-abuse admin endpoints (rc.1)
	mux.HandleFunc("GET /api/admin/anti-abuse", g.handleListAntiAbuse)
	mux.HandleFunc("PUT /api/admin/anti-abuse", g.handlePutAntiAbuse)

	// Batch endpoints (beta.2)
	mux.HandleFunc("POST /api/admin/donations/approve/batch", g.handleBatchApproveApplications)
	mux.HandleFunc("POST /api/admin/donations/reject/batch", g.handleBatchRejectApplications)
	mux.HandleFunc("POST /api/admin/donations/status/batch", g.handleBatchDonationStatus)
	mux.HandleFunc("POST /api/admin/donations/delete/batch", g.handleBatchDeleteDonations)
	mux.HandleFunc("POST /api/admin/pricing/delete/batch", g.handleBatchDeletePricing)
	mux.HandleFunc("POST /api/admin/bulletins/delete/batch", g.handleBatchDeleteBulletins)

	// Bulletin board (admin CRUD + public merged list)
	mux.HandleFunc("GET /api/admin/bulletins", g.handleAdminListBulletins)
	mux.HandleFunc("POST /api/admin/bulletins", g.handleAdminCreateBulletin)
	mux.HandleFunc("PUT /api/admin/bulletins/{id}", g.handleAdminUpdateBulletin)
	mux.HandleFunc("DELETE /api/admin/bulletins/{id}", g.handleAdminDeleteBulletin)
	mux.HandleFunc("GET /api/bulletins", g.handleListBulletins)

	// User charity toggle
	mux.HandleFunc("GET /api/me/charity", g.handleGetCharity)
	mux.HandleFunc("PUT /api/me/charity", g.handlePutCharity)

	// User self-service donation applications
	mux.HandleFunc("POST /api/me/donations", g.handleCreateDonationApp)
	mux.HandleFunc("GET /api/me/donations", g.handleListMyApplications)

	// User debug
	mux.HandleFunc("POST /api/me/debug/start", g.handleDebugStart)
	mux.HandleFunc("POST /api/me/debug/stop", g.handleDebugStop)
	mux.HandleFunc("GET /api/me/debug/status", g.handleDebugStatus)
	mux.HandleFunc("GET /api/me/debug/stream", g.handleDebugStream)
	mux.HandleFunc("POST /api/me/debug/dry-run", g.handleDebugDryRun)

	// User check-in (credits system)
	mux.HandleFunc("POST /api/me/checkin", g.handleCheckin)
	mux.HandleFunc("GET /api/me/checkin/status", g.handleCheckinStatus)
	mux.HandleFunc("PUT /api/me/lang", g.handleSetLang)

	// R-A level-gated user-site endpoints (v1.3.0): level 4 review,
	// level 5 charity co-admin + all-site logs. Only reachable on the user
	// host (hostSeparation allowlists /api/me exactly); the admin host
	// returns 404 for these by design.
	mux.HandleFunc("GET /api/me/review/pending", g.handleMeReviewPending)
	mux.HandleFunc("POST /api/me/review/{id}/approve", g.handleMeReviewApprove)
	mux.HandleFunc("POST /api/me/review/{id}/reject", g.handleMeReviewReject)
	mux.HandleFunc("POST /api/me/review/approve/batch", g.handleMeReviewBatchApprove)
	mux.HandleFunc("POST /api/me/review/reject/batch", g.handleMeReviewBatchReject)
	mux.HandleFunc("GET /api/me/charity-admin/donations", g.handleMeCharityListDonations)
	mux.HandleFunc("POST /api/me/charity-admin/donations", g.handleMeCharityCreateDonation)
	mux.HandleFunc("PATCH /api/me/charity-admin/donations/{id}", g.handleMeCharityPatchDonation)
	mux.HandleFunc("POST /api/me/charity-admin/donations/{id}/status", g.handleMeCharityDonationStatus)
	mux.HandleFunc("DELETE /api/me/charity-admin/donations/{id}", g.handleMeCharityDeleteDonation)
	mux.HandleFunc("POST /api/me/charity-admin/donations/status/batch", g.handleMeCharityBatchDonationStatus)
	mux.HandleFunc("POST /api/me/charity-admin/donations/delete/batch", g.handleMeCharityBatchDeleteDonations)
	mux.HandleFunc("GET /api/me/charity-admin/pricing", g.handleMePricingList)
	mux.HandleFunc("PUT /api/me/charity-admin/pricing", g.handleMePricingUpsert)
	mux.HandleFunc("PATCH /api/me/charity-admin/pricing", g.handleMePricingPatch)
	mux.HandleFunc("DELETE /api/me/charity-admin/pricing", g.handleMePricingDelete)
	mux.HandleFunc("POST /api/me/charity-admin/pricing/delete/batch", g.handleMePricingBatchDelete)
	mux.HandleFunc("GET /api/me/all-logs", g.handleMeAllLogs)
	mux.HandleFunc("GET /api/me/all-logs/stats", g.handleMeAllLogsStats)

	// Admin batch credits & donation-credit operations
	mux.HandleFunc("POST /api/admin/users/credits", g.handleAdminBatchCredits)
	mux.HandleFunc("POST /api/admin/users/donation_credit", g.handleAdminBatchDonationCredit)

	// User endpoints (session-authenticated)
	mux.HandleFunc("GET /api/logs", g.handleListLogs)
	mux.HandleFunc("GET /api/configs", g.handleListConfigs)
	mux.HandleFunc("POST /api/configs", g.handleCreateConfig)
	mux.HandleFunc("PUT /api/configs/{id}", g.handleUpdateConfig)
	mux.HandleFunc("POST /api/configs/{id}/toggle", g.handleToggleConfig)
	mux.HandleFunc("DELETE /api/configs/{id}", g.handleDeleteConfig)
	mux.HandleFunc("GET /api/caller-key", g.handleGetCallerKey)
	mux.HandleFunc("POST /api/caller-key/reset", g.handleResetCallerKey)
	mux.HandleFunc("GET /api/services", g.handleListServices)
	mux.HandleFunc("GET /api/me/export", g.handleMeExport)
	mux.HandleFunc("DELETE /api/me", g.handleMeDelete)

	// Embedded SPA + static assets
	g.registerWebRoutes(mux)

	// Catch-all: serve the custom 404 page for any GET request not
	// matched by a more specific pattern.  In Go 1.22+ ServeMux,
	// "GET /" matches all GET paths (prefix match), but more-specific
	// patterns like "GET /{$}" or "GET /privacy" take precedence.
	mux.HandleFunc("GET /", g.serve404Page)
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
}

// userFromCallerKey resolves the Authorization bearer key to a live user.
func (g *Gateway) userFromCallerKey(r *http.Request) *db.User {
	key := r.Header.Get("Authorization")
	if len(key) > 7 && strings.EqualFold(key[:7], "Bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	if key == "" {
		return nil
	}
	u, err := g.Store.GetUserByCallerKey(key)
	if err != nil || u == nil || db.IsBanned(u) {
		return nil
	}
	return u
}

// handleModels: per-user model list (enabled configs of the caller-key user),
// plus charity models when the user has opted in (charity_enabled) and the
// global charity switch is on.
func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	user := g.userFromCallerKey(r)
	if user == nil {
		ip := g.clientIP(r)
		now := time.Now()
		if !g.authFailThrottle.allow(ip, now) {
			w.Header().Set("Retry-After", strconv.Itoa(g.authFailThrottle.retryAfterSec(ip, now)))
			g.writeError(w, http.StatusTooManyRequests, "rate_limited", t(g.resolveLang(r), "认证失败过于频繁，请稍后再试", "Too many authentication failures, please try again later"))
			return
		}
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing API key")
		return
	}
	configs, err := g.Store.ListAppConfigs(user.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	models := make([]openai.Model, 0, len(configs))
	for _, c := range configs {
		if !c.Enabled {
			continue
		}
		models = append(models, openai.Model{ID: c.Model, Object: "model", Created: c.CreatedAt, OwnedBy: "dify2api"})
	}

	// Append charity models when global switch is on and user has opted in.
	// In beta.2, charity model list is sourced from charity_pricing (enabled=1),
	// not from the donations table.
	if user.CharityEnabled && g.Store.GetSettingString(db.SettingCharityEnabled, "") == "true" {
		seen := make(map[string]bool, len(models))
		for _, m := range models {
			seen[m.ID] = true
		}
		pricings, err := g.Store.ListEnabledPricing()
		if err == nil {
			for _, p := range pricings {
				charityID := charityModelName(p.Service, p.Model)
				if !seen[charityID] {
					models = append(models, openai.Model{
						ID:      charityID,
						Object:  "model",
						Created: time.Now().Unix(),
						OwnedBy: "dify2api-charity",
					})
					seen[charityID] = true
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openai.ModelListResponse{Object: "list", Data: models})
}

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	// Require a JSON content type (reject before consuming a semaphore slot).
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		g.writeError(w, http.StatusUnsupportedMediaType, "invalid_request", "Content-Type must be application/json")
		return
	}

	startedAt := time.Now()

	// Global in-flight cap (backpressure): acquire BEFORE reading the body
	// so that attackers cannot pile up memory-hungry requests waiting for
	// the semaphore.  With the default config (MaxChatInFlight=32,
	// MaxRequestBodyMB=10) the worst-case memory footprint for request
	// bodies is 320 MB.
	select {
	case g.chatSem <- struct{}{}:
		defer func() { <-g.chatSem }()
	default:
		w.Header().Set("Retry-After", "3")
		g.writeError(w, http.StatusTooManyRequests, "server_busy", t(g.resolveLang(r), "当前服务繁忙（并发已达上限），请稍后重试", "Service is busy (concurrency limit reached), please try again later"))
		return
	}

	// Cap body size before reading (defends against memory exhaustion).
	maxBody := int64(g.Config.MaxRequestBodyMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		g.writeError(w, http.StatusRequestEntityTooLarge, "request_too_large",
			fmt.Sprintf("request body exceeds the %d MiB limit", g.Config.MaxRequestBodyMB))
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		if g.Config.Debug {
			dumpDebugRequest(g.Config.DebugDir, r, rawBody, "decode failed: "+err.Error(), nil)
		}
		g.writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid request: %v", err))
		return
	}
	if len(req.Messages) == 0 {
		if g.Config.Debug {
			dumpDebugRequest(g.Config.DebugDir, r, rawBody, "messages array is empty", nil)
		}
		g.writeError(w, http.StatusBadRequest, "invalid_request", "messages array is empty")
		return
	}

	// Debug mode: intercept BEFORE any auth/routing (operator tool).
	if g.Config.Debug {
		service := translator.ServiceOfModel(req.Model)
		inputs, _, tErr := translator.TranslateForService(service, req.Messages)
		note := "ok (service: " + service + ")"
		if tErr != nil {
			note = "layout rejected (service: " + service + "): " + tErr.Error()
			inputs = nil
		}
		folder := dumpDebugRequest(g.Config.DebugDir, r, rawBody, note, inputs)
		log.Printf("[DEBUG] intercepted request from %s (%d messages) -> %s", r.RemoteAddr, len(req.Messages), folder)
		g.writeError(w, http.StatusNotFound, "debug_intercept",
			fmt.Sprintf("[debug] request intercepted and saved to %s; nothing was sent to Dify", folder))
		return
	}

	// 1. Caller key -> user. Invalid keys feed the per-IP auth-failure
	// throttle (flood defence); valid keys are never counted there.
	user := g.userFromCallerKey(r)
	if user == nil {
		ip := g.clientIP(r)
		if !g.authFailThrottle.allow(ip, startedAt) {
			w.Header().Set("Retry-After", strconv.Itoa(g.authFailThrottle.retryAfterSec(ip, startedAt)))
			g.writeError(w, http.StatusTooManyRequests, "rate_limited", t(g.resolveLang(r), "认证失败过于频繁，请稍后再试", "Too many authentication failures, please try again later"))
			return
		}
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing API key")
		return
	}

	service := translator.ServiceOfModel(req.Model)
	// Charity names have two prefixes: [公益][service]backend. Use the
	// actual service for every request log while leaving ServiceOfModel's
	// general one-prefix contract unchanged.
	if IsCharityModel(req.Model) {
		if charityService, _ := ParseCharityModel(req.Model); charityService != "" {
			service = charityService
		}
	}

	// 1b. Anti-abuse check — after auth, before RPM.
	if errResp := g.checkAntiAbuse(req.Messages, req.Model, user.ID, service, startedAt); errResp != nil {
		g.writeError(w, http.StatusBadRequest, errResp.code, errResp.message)
		return
	}

	// 2. Three-class RPM gate. The check runs once here — after auth,
	// before anything is sent to Dify — against all three windows:
	//   A (transfer complete) and B (request success) are recorded at
	//   request end; C (request received) is recorded now.
	// Violations are logged; N violations within 24h trigger an automatic
	// timed ban (threshold and duration are admin-tunable settings).
	limits := g.effectiveRPMLimits(user.ID)
	if ok, violated := g.limiter.check(user.ID, startedAt, limits); !ok {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "rpm_exceeded",
			http.StatusForbidden, fmt.Sprintf("超出类别 %s 上限（%d 次/分）", classLabel(violated), limits[violated]), "")
		violationLimit := g.Store.GetSettingInt(db.SettingRPMViolationLimit, db.DefaultRPMViolationLimit)
		banHours := g.Store.GetSettingInt(db.SettingRPMBanHours, db.DefaultRPMBanHours)
		violations, _ := g.Store.CountRecentErrors(user.ID, "rpm_exceeded", startedAt.Add(-24*time.Hour))
		if violations >= violationLimit {
			until := time.Now().Add(time.Duration(banHours) * time.Hour)
			if err := g.Store.AutoBanUser(user.ID, until); err != nil {
				log.Printf("[ERROR] auto-ban user %d: %v", user.ID, err)
			}
			g.Store.DeleteUserSessions(user.ID)
			log.Printf("[AUTH] user %d auto-banned until %v after %d RPM violations (class %s)", user.ID, until, violations, classLabel(violated))
			if g.mailer != nil {
				g.mailer.UserAutoBanned(user.Username, user.ID, until, banHours, violations)
			}
			lang := g.resolveLang(r)
			messageTemplate := t(lang, "已超出类别 %s 每分钟上限（%d 次/分），且因 24 小时内累计 %d 次超限，账号已被自动封禁 %d 小时", "Exceeded class %s RPM limit (%d/min); account auto-banned for %d hours due to %d violations in 24 hours")
			var message string
			if lang == "en" {
				message = fmt.Sprintf(messageTemplate, classLabel(violated), limits[violated], banHours, violations)
			} else {
				message = fmt.Sprintf(messageTemplate, classLabel(violated), limits[violated], violations, banHours)
			}
			g.writeError(w, http.StatusForbidden, "rpm_exceeded", message)
			return
		}
		g.writeError(w, http.StatusForbidden, "rpm_exceeded",
			fmt.Sprintf(t(g.resolveLang(r), "已超出类别 %s 每分钟上限（%d 次/分），请稍后再试（24 小时内第 %d 次超限，累计 %d 次将自动封禁 %d 小时", "Exceeded class %s RPM limit (%d/min), please try again later (violation #%d in 24h; %d violations trigger a %dh auto-ban)"),
				classLabel(violated), limits[violated], violations, violationLimit, banHours))
		return
	}
	// Class C (request received) counts immediately after passing the gate.
	g.limiter.record(rpmClassC, user.ID, startedAt)

	// Charity global gate check: if the model is a charity model and the
	// global switch is off, reject immediately.
	// We check this early to fail fast; the full charity routing in
	// handleCharityAfterRPM also checks this again for safety.
	if IsCharityModel(req.Model) {
		if g.Store.GetSettingString(db.SettingCharityEnabled, "") != "true" {
			g.logRequest(user.ID, req.Model, service, startedAt, "error", "charity_disabled",
				http.StatusForbidden, "全局捐赠/公益开关未开启", "")
			g.writeError(w, http.StatusForbidden, "charity_disabled",
				t(g.resolveLang(r), "捐赠/公益系统尚未被管理员启用", "Donation/charity system has not been enabled by the administrator"))
			return
		}
	}

	// 3a. Charity model routing — takes priority over user App configs.
	if IsCharityModel(req.Model) {
		g.handleCharityAfterRPM(w, r, user, req, startedAt)
		return
	}

	// 3b. Model full name -> the user's enabled App config.
	appCfg, err := g.Store.GetEnabledAppConfigByModel(user.ID, req.Model)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if appCfg == nil {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "model_not_found",
			http.StatusNotFound, fmt.Sprintf("model %q not found", req.Model), "")
		g.writeError(w, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q not found in your configs (check the dashboard or /v1/models)", req.Model))
		return
	}

	// 4. Per-service contract validation & mapping.
	inputs, images, err := translator.TranslateForService(service, req.Messages)
	if err != nil {
		g.debugWrapError(r, user.ID, rawBody, nil, req.Messages, err.Error(), http.StatusBadRequest)
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "invalid_message_sequence",
			http.StatusBadRequest, err.Error(), "")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": err.Error(),
				"type":    "invalid_request_error",
				"code":    "invalid_message_sequence",
			},
		})
		return
	}

	// 4b. Debug interception — after translation, before Dify forward.
	w2, debugFinalize := g.debugWrap(w, r, user.ID, req.Model, rawBody, inputs, req.Messages)
	if debugFinalize == nil && w2 == nil {
		// dry-run: mock response already written in debugWrap.
		g.logRequest(user.ID, req.Model, service, startedAt, "success", "debug_dry_run", http.StatusOK, "", "")
		return
	}
	writer := w
	if debugFinalize != nil {
		defer debugFinalize()
		writer = w2
	}

	// 5. Build the Dify client from the user's config.
	apiKey, err := g.Store.Decrypt(appCfg.DifyAPIKeyEnc)
	if err != nil {
		log.Printf("[ERROR] decrypt app key (user %d, config %d): %v", user.ID, appCfg.ID, err)
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "internal",
			http.StatusInternalServerError, "credential decryption error", "")
		g.writeError(w, http.StatusInternalServerError, "internal", "credential error")
		return
	}
	client, err := g.newDifyClient(user.ID, appCfg.DifyBaseURL, apiKey, time.Duration(g.Config.DifyHTTPTimeoutMs)*time.Millisecond)
	if err != nil {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "upstream_blocked",
			http.StatusBadGateway, err.Error(), "")
		g.writeError(writer, http.StatusBadGateway, "upstream_blocked", "configured Dify origin is blocked by the egress policy")
		return
	}
	if err := g.validateRemoteContent(service, inputs, images); err != nil {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "remote_url_not_allowed",
			http.StatusBadRequest, err.Error(), "")
		g.writeError(writer, http.StatusBadRequest, "remote_url_not_allowed",
			t(userLang(user), "远程内容地址不符合安全策略", "The remote content address is not allowed by the security policy"))
		return
	}

	wfInputs := make(map[string]interface{}, len(inputs)+1)
	for k, v := range inputs {
		wfInputs[k] = v
	}

	// 6. Images (image-processing): http(s) URLs pass through as remote_url;
	// data URIs are uploaded first (/v1/files/upload -> upload_file_id).
	if len(images) > 0 {
		files, err := g.buildImageFiles(r.Context(), client, wfReq_User(user.ID), images)
		if err != nil {
			log.Printf("[ERROR] image files (user %d): %v", user.ID, err)
			g.logRequest(user.ID, req.Model, service, startedAt, "error", "image_upload_failed",
				difyErrorStatus(err), err.Error(), "")
			g.writeDifyError(writer, err, userLang(user))
			return
		}
		wfInputs["input_image_list"] = files
	}

	wfReq := &dify.WorkflowRequest{
		Inputs:       wfInputs,
		ResponseMode: "streaming",
		User:         fmt.Sprintf("u%d", user.ID),
	}

	modelName := req.Model
	if modelName == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}

	// 7. Forward (streaming or blocking).
	if req.Stream {
		g.handleStreaming(writer, client, wfReq, modelName, user.ID, userLang(user), service, startedAt, r.Context())
	} else {
		g.handleBlocking(writer, client, wfReq, modelName, user.ID, userLang(user), service, startedAt, r.Context())
	}
}

// handleCharityAfterRPM runs the full charity routing path after the RPM
// gate has been passed (class C already counted). It checks charity_enabled,
// credits, routable donations, weighted selection, contract validation,
// and then forwards to the streaming/blocking handler.
func (g *Gateway) handleCharityAfterRPM(w http.ResponseWriter, r *http.Request, user *db.User, req openai.ChatCompletionRequest, startedAt time.Time) {
	service, backend := ParseCharityModel(req.Model)

	// 1. User charity_enabled (model_not_found to not leak existence)
	if !user.CharityEnabled {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "model_not_found",
			http.StatusNotFound, fmt.Sprintf("model %q not found", req.Model), "")
		g.writeError(w, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q not found in your configs", req.Model))
		return
	}

	// 2. Pricing gate (beta.2): check charity_pricing for this (service, backend).
	pricing, err := g.Store.GetPricing(service, backend)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if pricing == nil || !pricing.Enabled {
		// Pricing missing or disabled — check if donations exist for alert.
		has, _ := g.Store.HasDonationsForPair(service, backend)
		if has {
			// Donations exist but pricing is missing/disabled — alert admin.
			if g.mailer != nil {
				g.mailer.PricingMissing(service, backend)
			}
		}
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "service_unavailable",
			http.StatusServiceUnavailable, "pricing not found or disabled", "")
		g.writeError(w, http.StatusServiceUnavailable, "service_unavailable",
			t(g.resolveLang(r), "当前该公益模型不可用", "This charity model is currently unavailable"))
		return
	}

	// Fast rejection avoids exposing pool availability to callers that cannot
	// pay. ReserveCharityCall repeats this check atomically to enforce the hard
	// concurrency boundary.
	if user.Credits < pricing.Price {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "insufficient_credits",
			http.StatusForbidden, fmt.Sprintf("credits %d < price %d", user.Credits, pricing.Price), "")
		g.writeError(w, http.StatusForbidden, "insufficient_credits",
			t(g.resolveLang(r),
				fmt.Sprintf("您的%s不足（需要 %d，当前 %d），无法调用公益模型",
					g.Config.I18N("credits_name", "zh", config.DefaultCreditsName), pricing.Price, user.Credits),
				fmt.Sprintf("Insufficient %s (need %d, have %d), cannot use charity model",
					g.Config.I18N("credits_name", "en", config.DefaultCreditsName), pricing.Price, user.Credits)))
		return
	}

	// 3. Validate the contract and remote-content policy before touching any
	// donation RPM/count/credits.
	inputs, images, err := translator.TranslateForService(service, req.Messages)
	if err != nil {
		raw, _ := json.Marshal(req)
		g.debugWrapError(r, user.ID, raw, nil, req.Messages, err.Error(), http.StatusBadRequest)
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "invalid_message_sequence",
			http.StatusBadRequest, err.Error(), "")
		g.writeError(w, http.StatusBadRequest, "invalid_message_sequence", err.Error())
		return
	}
	logModel := charityModelName(service, backend)
	rawCharity, _ := json.Marshal(req)
	wCharity, dbgFinalize := g.debugWrap(w, r, user.ID, logModel, rawCharity, inputs, req.Messages)
	if dbgFinalize == nil && wCharity == nil {
		g.logRequest(user.ID, logModel, service, startedAt, "success", "debug_dry_run", http.StatusOK, "", "")
		return
	}
	charityWriter := w
	if dbgFinalize != nil {
		defer dbgFinalize()
		charityWriter = wCharity
	}
	if err := g.validateRemoteContent(service, inputs, images); err != nil {
		g.logRequest(user.ID, logModel, service, startedAt, "error", "remote_url_not_allowed",
			http.StatusBadRequest, err.Error(), "")
		g.writeError(charityWriter, http.StatusBadRequest, "remote_url_not_allowed",
			t(userLang(user), "远程内容地址不符合安全策略", "The remote content address is not allowed by the security policy"))
		return
	}

	// 4. List routable donations.
	donations, err := g.Store.ListRoutableDonations(service, backend)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if len(donations) == 0 {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "service_unavailable",
			http.StatusServiceUnavailable, "no routable donations", "")
		g.writeError(w, http.StatusServiceUnavailable, "service_unavailable",
			t(g.resolveLang(r), "当前该公益模型无可用捐赠条目", "No active donation entry found for this charity model"))
		return
	}

	// 5. Atomically acquire donation RPM and reserve one donation use plus the
	// consumer's price. Candidates that lose a DB race are retried.
	remainingCandidates := append([]*db.Donation(nil), donations...)
	var picked *db.Donation
	var releaseRPM func()
	var reservation *db.CharityReservation
	var client *dify.Client
	for len(remainingCandidates) > 0 {
		picked, releaseRPM = pickWeightedDonation(remainingCandidates, g.donationLimiter)
		if picked == nil {
			g.logRequest(user.ID, logModel, service, startedAt, "error", "charity_overloaded",
				http.StatusTooManyRequests, "all routable donations at RPM limit", "")
			g.writeError(charityWriter, http.StatusTooManyRequests, "charity_overloaded",
				t(g.resolveLang(r), "当前该公益模型所有捐赠条目均已达速率上限，请稍后重试", "All donation entries for this charity model have reached their RPM limit, please try again later"))
			return
		}

		reservation, err = g.Store.ReserveCharityCall(r.Context(), user.ID, picked.ID, pricing.Price, pricing.Reward)
		if errors.Is(err, db.ErrDonationUnavailable) {
			releaseRPM()
			for i, candidate := range remainingCandidates {
				if candidate.ID == picked.ID {
					remainingCandidates = append(remainingCandidates[:i], remainingCandidates[i+1:]...)
					break
				}
			}
			continue
		}
		if errors.Is(err, db.ErrInsufficientCredits) {
			releaseRPM()
			latest, _ := g.Store.GetUserByID(user.ID)
			credits := 0
			if latest != nil {
				credits = latest.Credits
			}
			g.logRequest(user.ID, logModel, service, startedAt, "error", "insufficient_credits",
				http.StatusForbidden, fmt.Sprintf("credits %d < price %d", credits, pricing.Price), "")
			g.writeError(charityWriter, http.StatusForbidden, "insufficient_credits",
				t(g.resolveLang(r),
					fmt.Sprintf("您的%s不足（需要 %d，当前 %d），无法调用公益模型",
						g.Config.I18N("credits_name", "zh", config.DefaultCreditsName), pricing.Price, credits),
					fmt.Sprintf("Insufficient %s (need %d, have %d), cannot use charity model",
						g.Config.I18N("credits_name", "en", config.DefaultCreditsName), pricing.Price, credits)))
			return
		}
		if err != nil {
			releaseRPM()
			if errors.Is(err, context.Canceled) {
				return
			}
			g.writeError(charityWriter, http.StatusInternalServerError, "internal", "failed to reserve charity capacity")
			return
		}

		apiKey, decryptErr := g.Store.Decrypt(picked.DifyAPIKeyEnc)
		if decryptErr != nil {
			g.releaseCharitySetup(reservation)
			releaseRPM()
			log.Printf("[ERROR] decrypt donation key (donation %d): %v", picked.ID, decryptErr)
			g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "internal",
				http.StatusInternalServerError, "credential decryption error", picked.ID, 0, "")
			g.writeError(charityWriter, http.StatusInternalServerError, "internal", "credential error")
			return
		}
		client, err = g.newDifyClient(user.ID, picked.DifyBaseURL, apiKey, time.Duration(g.Config.DifyHTTPTimeoutMs)*time.Millisecond)
		if err != nil {
			g.releaseCharitySetup(reservation)
			releaseRPM()
			g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "upstream_blocked",
				http.StatusBadGateway, err.Error(), picked.ID, 0, "")
			g.writeError(charityWriter, http.StatusBadGateway, "upstream_blocked", "configured Dify origin is blocked by the egress policy")
			return
		}
		break
	}
	if reservation == nil {
		g.logRequest(user.ID, logModel, service, startedAt, "error", "service_unavailable",
			http.StatusServiceUnavailable, "donations exhausted during reservation", "")
		g.writeError(charityWriter, http.StatusServiceUnavailable, "service_unavailable",
			t(g.resolveLang(r), "公益捐赠额度刚刚用尽，请稍后重试", "Charity capacity was just exhausted; please retry"))
		return
	}

	// Build workflow inputs
	wfInputs := make(map[string]interface{}, len(inputs)+1)
	for k, v := range inputs {
		wfInputs[k] = v
	}

	// Mark before the first upload/workflow request. If the process dies after
	// this point, startup recovery conservatively commits the reservation.
	if err := g.Store.MarkCharityDispatched(r.Context(), reservation.ID); err != nil {
		g.releaseCharitySetup(reservation)
		releaseRPM()
		if errors.Is(err, context.Canceled) {
			g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "client_canceled",
				statusClientClosedRequest, "client disconnected before charity dispatch", picked.ID, 0, "")
			return
		}
		g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "internal",
			http.StatusInternalServerError, "failed to dispatch charity reservation", picked.ID, 0, "")
		g.writeError(charityWriter, http.StatusInternalServerError, "internal", "failed to dispatch charity reservation")
		return
	}

	// Handle images if applicable
	if len(images) > 0 {
		files, err := g.buildImageFiles(r.Context(), client, wfReq_User(user.ID), images)
		if err != nil {
			if r.Context().Err() != nil || errors.Is(err, context.Canceled) {
				g.charityCommitAccounting(reservation)
				g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "client_canceled",
					statusClientClosedRequest, "client disconnected during image upload", picked.ID, reservation.Price, "")
				return
			}
			log.Printf("[ERROR] charity image files (user %d): %v", user.ID, err)
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(user.ID, logModel, service, startedAt, "error", "image_upload_failed",
				difyErrorStatus(err), err.Error(), picked.ID, reservation.Price, "")
			g.writeDifyError(charityWriter, err, userLang(user))
			return
		}
		wfInputs["input_image_list"] = files
	}

	wfReq := &dify.WorkflowRequest{
		Inputs:       wfInputs,
		ResponseMode: "streaming",
		User:         fmt.Sprintf("u%d", user.ID),
	}

	// 7. Forward (streaming or blocking)
	if req.Stream {
		g.charityStreaming(charityWriter, client, wfReq, logModel, user.ID, userLang(user), service, startedAt, picked, reservation, r.Context())
	} else {
		g.charityBlocking(charityWriter, client, wfReq, logModel, user.ID, userLang(user), service, startedAt, picked, reservation, r.Context())
	}
}

func (g *Gateway) handleStreaming(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, lang, service string, startedAt time.Time, ctx context.Context) {
	wfReq.ResponseMode = "streaming"
	events, errCh := client.StreamWorkflowContext(ctx, wfReq)

	// Wait for the FIRST event or an error before committing to SSE
	// response headers.  This is a blocking wait (not a racy non-blocking
	// select): an immediate HTTP error from Dify (non-200, connection
	// refused, …) must be reported as plain JSON with a real status code,
	// which is impossible once the SSE headers are sent.
	var firstEvt *dify.StreamEvent
	select {
	case evt, ok := <-events:
		if ok {
			firstEvt = &evt
		}
		// !ok: stream closed without events; fall through — the error
		// (if any) is picked up from errCh below.
	case err := <-errCh:
		if ctx.Err() != nil {
			g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, ctx.Err().Error(), "")
			return
		}
		if err != nil {
			g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error", difyErrorStatus(err), err.Error(), "")
			g.writeDifyError(w, err, lang)
			return
		}
	case <-ctx.Done():
		g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, ctx.Err().Error(), "")
		return
	}
	if firstEvt == nil {
		// Channel closed with no events: check for a late error, else treat
		// as an empty-but-successful stream.
		select {
		case err := <-errCh:
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				detail := "client disconnected"
				if ctx.Err() != nil {
					detail = ctx.Err().Error()
				}
				g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, detail, "")
				return
			}
			if err != nil {
				g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error", difyErrorStatus(err), err.Error(), "")
				g.writeDifyError(w, err, lang)
				return
			}
		default:
		}
	}

	if ctx.Err() != nil {
		g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, ctx.Err().Error(), "")
		return
	}

	// The stream has started (Dify returned HTTP 200): this is a "success"
	// per the §1.2 definition — class B records here, even if the stream is
	// truncated later.
	g.limiter.record(rpmClassB, userID, time.Now())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.logRequest(userID, modelName, service, startedAt, "error", "stream_unsupported", http.StatusInternalServerError, "response writer does not support streaming", "")
		g.writeError(w, http.StatusInternalServerError, "internal", "streaming not supported")
		return
	}

	conv := translator.NewStreamConverter(modelName, func(raw string) string {
		return sanitizePublicUpstreamError(nil, raw, lang)
	})

	difyUser := fmt.Sprintf("u%d", userID)
	var taskID string
	if firstEvt != nil {
		if firstEvt.TaskID != "" {
			taskID = firstEvt.TaskID
		}
		if msg := conv.Convert(*firstEvt); msg != nil {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
	}
	clientCanceled := false
loop:
	for {
		select {
		case <-ctx.Done():
			clientCanceled = true
			g.stopDifyWorkflow(client, taskID, difyUser)
			break loop
		case evt, ok := <-events:
			if !ok {
				break loop
			}
			if evt.TaskID != "" {
				taskID = evt.TaskID
			}
			if msg := conv.Convert(evt); msg != nil {
				fmt.Fprint(w, msg.Data)
				flusher.Flush()
			}
		}
	}
	if clientCanceled || ctx.Err() != nil {
		detail := "client disconnected"
		if ctx.Err() != nil {
			detail = ctx.Err().Error()
		}
		g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, detail, "")
		return
	}

	// Drain errCh after the event channel closes: a transport-level failure
	// (connection drop, SSE scan error) surfaces here without any Dify
	// error event having been converted.
	status, code, detail := "success", "", ""
	var streamErr error
	select {
	case err := <-errCh:
		streamErr = err
	default:
	}

	switch {
	case conv.Failed():
		// The converter already emitted an in-stream error frame
		// (Dify error event or failed workflow_finished).  Per OpenAI
		// streaming behavior, do NOT send [DONE] after an error frame —
		// SDK clients treat the error frame as terminal and raise.
		status, code = "error", "upstream_error"
		detail = conv.FailMessage()
		if streamErr != nil {
			log.Printf("[ERROR] dify stream (user %d): %v", userID, streamErr)
			if detail == "" {
				detail = streamErr.Error()
			}
		}
	case streamErr != nil:
		// Transport-level failure with no error event: emit the error
		// frame ourselves, and likewise skip [DONE].
		log.Printf("[ERROR] dify stream (user %d): %v", userID, streamErr)
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] "+sanitizePublicUpstreamError(streamErr, streamErr.Error(), lang)))
		flusher.Flush()
		status, code = "error", "upstream_error"
		detail = streamErr.Error()
	default:
		for _, msg := range conv.Finalize() {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		// Class A (transfer complete) only counts fully-relayed streams.
		g.limiter.record(rpmClassA, userID, time.Now())
	}
	// Streaming responses always went out with HTTP 200 (headers were
	// committed before the body); mid-stream failures surface via the
	// error frame, not the status code.
	g.logRequest(userID, modelName, service, startedAt, status, code, http.StatusOK, detail, "")
}

func (g *Gateway) handleBlocking(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, lang, service string, startedAt time.Time, ctx context.Context) {
	wfReq.ResponseMode = "blocking"
	text, err := client.BlockingWorkflowContext(ctx, wfReq)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			detail := err.Error()
			if ctx.Err() != nil {
				detail = ctx.Err().Error()
			}
			g.logRequest(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, detail, "")
			return
		}
		// Per the §1.2 definition, an upstream HTTP 200 counts as a
		// "success" for class B even when the workflow status is "failed"
		// (rare; surfaced to the admin alert center in S3).
		var de *dify.DifyError
		failed200 := errors.As(err, &de) && de.Status == http.StatusOK
		if failed200 {
			g.limiter.record(rpmClassB, userID, time.Now())
		}
		// Transport-level truncation (Cloudflare 100s timeout, connection
		// reset, etc.): the Dify App has likely consumed its quota, but no
		// response body was returned.  Give the caller a clear diagnosis
		// and suggest switching to streaming mode.
		if dify.IsTimeoutError(err) {
			log.Printf("[ERROR] dify blocking timeout (user %d): %v", userID, err)
			g.logRequest(userID, modelName, service, startedAt, "error", "upstream_timeout", http.StatusGatewayTimeout, err.Error(), "")
			g.writeError(w, http.StatusGatewayTimeout, "upstream_timeout",
				t(lang, "上游 Dify 服务响应超时：请求可能因 Cloudflare 100 秒限制被截断。建议使用流式传输（stream: true）或拆分任务后重试。", "Upstream Dify service timeout: the request may have been truncated by Cloudflare's 100-second limit. Consider using streaming (stream: true) or splitting the task."))
			return
		}
		log.Printf("[ERROR] dify blocking (user %d): %v", userID, err)
		logID := g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error", difyErrorStatus(err), err.Error(), "")
		if failed200 {
			// Write the admin alert after the log row so the alert center's
			// "view linked request" action can jump to this request.
			g.maybeRecordBlockingFailedAlert(userID, modelName, service, de, nil, logID)
		}
		g.writeDifyError(w, err, lang)
		return
	}
	g.limiter.record(rpmClassB, userID, time.Now())
	g.logRequest(userID, modelName, service, startedAt, "success", "", http.StatusOK, "", "")

	resp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()/1000%1000000000000),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelName,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	// Class A (transfer complete): the response has been written.
	g.limiter.record(rpmClassA, userID, time.Now())
}

// wfReq_User formats the Dify user id.
func wfReq_User(userID int64) string { return fmt.Sprintf("u%d", userID) }

// buildImageFiles turns image references (data URIs / http(s) URLs) into Dify
// workflow file objects.
func (g *Gateway) buildImageFiles(ctx context.Context, client *dify.Client, difyUser string, images []string) ([]map[string]interface{}, error) {
	files := make([]map[string]interface{}, 0, len(images))
	for i, img := range images {
		switch {
		case strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://"):
			files = append(files, map[string]interface{}{
				"type":            "image",
				"transfer_method": "remote_url",
				"url":             img,
			})
		case strings.HasPrefix(img, "data:"):
			mime, data, err := parseDataURI(img)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", i+1, err)
			}
			if len(data) > 10<<20 {
				return nil, fmt.Errorf("image %d exceeds the 10MB limit (%d bytes)", i+1, len(data))
			}
			ext := mime[strings.LastIndex(mime, "/")+1:]
			if ext == "jpeg" {
				ext = "jpg"
			}
			id, err := client.UploadFileContext(ctx, difyUser, fmt.Sprintf("image-%d.%s", i+1, ext), mime, data)
			if err != nil {
				return nil, err
			}
			files = append(files, map[string]interface{}{
				"type":            "image",
				"transfer_method": "local_file",
				"upload_file_id":  id,
			})
		default:
			return nil, fmt.Errorf("image %d: unsupported reference (expect http(s) URL or data URI)", i+1)
		}
	}
	return files, nil
}

// parseDataURI decodes a data URI ("data:<mime>;base64,<payload>").
func parseDataURI(uri string) (mime string, data []byte, err error) {
	rest, ok := strings.CutPrefix(uri, "data:")
	if !ok {
		return "", nil, fmt.Errorf("not a data URI")
	}
	mime, payload, ok := strings.Cut(rest, ";base64,")
	if !ok {
		return "", nil, fmt.Errorf("data URI must be base64-encoded")
	}
	data, err = base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("invalid base64: %w", err)
	}
	if mime == "" {
		mime = "image/png"
	}
	return mime, data, nil
}

// logRequest records one completed call (metadata only). httpStatus is the
// status returned to the caller; detail is a short error message for admin
// diagnostics (never request/response content — see db.RequestLog).
// Returns the new log row id (0 when the write failed) so callers can link
// dependent rows such as admin alerts.
func (g *Gateway) logRequest(userID int64, model, service string, startedAt time.Time, status, errorCode string, httpStatus int, detail string, antiAbuseInfo string) int64 {
	if len(detail) > g.Config.LogDetailMaxChars {
		detail = detail[:g.Config.LogDetailMaxChars] + "…"
	}
	id, err := g.Store.AddRequestLogFull(userID, model, service, startedAt, time.Now(), status, errorCode, httpStatus, detail, 0, 0, antiAbuseInfo)
	if err != nil {
		log.Printf("[WARN] write request log: %v", err)
		return 0
	}
	return id
}

// handleListLogs returns the session user's recent request logs.
func (g *Gateway) handleListLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	logs, err := g.Store.ListRequestLogs(u.ID, 500)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"logs": sanitizePublicRequestLogs(logs, g.resolveLang(r))})
}

// writeError emits an OpenAI-style error body with a [Dify2API] prefix
// to distinguish gateway errors from upstream Dify errors.
func (g *Gateway) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": "[Dify2API] " + message,
			"type":    code,
			"code":    code,
		},
	})
}

// serveMaintenancePage serves the maintenance.html page with placeholder
// substitution and a 503 status code.  Used by the maintenanceCheck
// middleware when the site-wide maintenance mode is on.
// Language selection follows the same priority as servePage:
// ?lang query param → user's Lang field → default "zh".
func (g *Gateway) serveMaintenancePage(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		http.Error(w, "maintenance page not found", http.StatusServiceUnavailable)
		return
	}
	fileName := "maintenance.html"
	if g.resolvePageLang(r) == "en" {
		if _, err := fs.ReadFile(staticFS, "maintenance.en.html"); err == nil {
			fileName = "maintenance.en.html"
		}
	}
	data, err := fs.ReadFile(staticFS, fileName)
	if err != nil {
		http.Error(w, "maintenance page not found", http.StatusServiceUnavailable)
		return
	}
	body := strings.ReplaceAll(string(data), "__SITE_NAME__", g.Config.Admin.SiteName)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(body))
}

// serve404Page serves the custom 404.html page (from the embedded static
// filesystem) with placeholder substitution and a 404 status code.
func (g *Gateway) serve404Page(w http.ResponseWriter, r *http.Request) {
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	data, err := fs.ReadFile(staticFS, "404.html")
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	body := strings.ReplaceAll(string(data), "__SITE_NAME__", g.Config.Admin.SiteName)
	body = strings.ReplaceAll(body, "__REPORT_EMAIL__", g.Config.Admin.ReportEmail)
	body = strings.ReplaceAll(body, "__SITE_BASE_URL__", g.Config.Admin.SiteBaseURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(body))
}

// difyErrorStatus returns the HTTP status writeDifyError would send for
// the given error (used to record the same value in request logs).
func difyErrorStatus(err error) int {
	var de *dify.DifyError
	if errors.As(err, &de) && de.Status >= 400 && de.Status < 500 {
		return de.Status
	}
	return http.StatusBadGateway
}

// writeDifyError forwards a Dify error to the client, preserving the
// upstream error code when available.  It builds its own response body
// with a [Dify] prefix (distinct from writeError's [Dify2API] prefix).
//
// Status mapping: upstream 4xx are passed through unchanged (they indicate
// a caller-side problem, e.g. invalid_param → 400, so client retry logic
// is not misled); everything else (5xx, network errors) maps to 502.
func (g *Gateway) writeDifyError(w http.ResponseWriter, err error, lang string) {
	var de *dify.DifyError
	code := "upstream_error"
	message := sanitizePublicUpstreamError(err, err.Error(), lang)
	status := http.StatusBadGateway
	if errors.As(err, &de) {
		if de.Code != "" {
			code = publicDifyErrorCode(de.Code)
		}
		if de.Status >= 400 && de.Status < 500 {
			status = de.Status
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": "[Dify] " + message,
			"type":    code,
			"code":    code,
		},
	})
}

// --- Anti-abuse ---

// loadAntiAbuseCache loads all anti-abuse configs into memory.
func (g *Gateway) loadAntiAbuseCache() error {
	svcInfos := translator.SupportedServices()
	services := make([]string, len(svcInfos))
	for i, s := range svcInfos {
		services[i] = s.Name
	}
	configs, err := g.Store.GetAntiAbuseConfigs(services)
	if err != nil {
		return err
	}
	g.antiAbuseMu.Lock()
	g.antiAbuseCache = configs
	g.antiAbuseMu.Unlock()
	return nil
}

// refreshAntiAbuseCache reloads the cache (called after admin updates).
func (g *Gateway) refreshAntiAbuseCache() {
	if err := g.loadAntiAbuseCache(); err != nil {
		log.Printf("[WARN] refresh anti-abuse cache: %v", err)
	}
}

func (g *Gateway) antiAbuseConfig(service string) *db.AntiAbuseConfig {
	g.antiAbuseMu.RLock()
	defer g.antiAbuseMu.RUnlock()
	return g.antiAbuseCache[service]
}

func (g *Gateway) antiAbuseConfigList() []*db.AntiAbuseConfig {
	g.antiAbuseMu.RLock()
	defer g.antiAbuseMu.RUnlock()
	list := make([]*db.AntiAbuseConfig, 0, len(g.antiAbuseCache))
	for _, cfg := range g.antiAbuseCache {
		list = append(list, cfg)
	}
	return list
}

type antiAbuseErr struct {
	code    string
	message string
}

// checkAntiAbuse validates message roles and content length against the
// per-service anti-abuse config. Returns nil when the check passes.
// On failure it records a request log, executes penalties, and returns
// the error response details.
func (g *Gateway) checkAntiAbuse(messages []openai.Message, model string, userID int64, service string, startedAt time.Time) *antiAbuseErr {
	// Validate roles and compute total content length.
	var totalChars int
	for i, m := range messages {
		switch m.Role {
		case "system", "user", "assistant":
		default:
			g.logRequest(userID, model, service, startedAt, "error", "invalid_role",
				http.StatusBadRequest,
				fmt.Sprintf("messages[%d]: unsupported role %q", i, m.Role),
				`{"triggered":"invalid_role","penalties":[]}`)
			return &antiAbuseErr{
				code:    "invalid_role",
				message: "消息包含不支持的角色类型，仅支持 system、user、assistant。",
			}
		}
		totalChars += utf8.RuneCountInString(string(m.Content))
	}

	// For charity models, use the actual service (strip [公益] prefix).
	lookupService := service
	if IsCharityModel(model) {
		if s, _ := ParseCharityModel(model); s != "" {
			lookupService = s
		}
	}

	// Look up config from cache; fall back to defaults if missing.
	cfg := g.antiAbuseConfig(lookupService)
	if cfg == nil {
		cfg = &db.AntiAbuseConfig{Mode: 2, MinChars: 20}
	}

	// Determine whether the content-length check applies.
	var shouldCheck bool
	switch cfg.Mode {
	case 2:
		shouldCheck = true
	case 1:
		shouldCheck = IsCharityModel(model)
	case 0:
		shouldCheck = false
	}

	if shouldCheck && totalChars < cfg.MinChars {
		// Build anti-abuse info JSON for logging.
		var aaPenalties []string
		if cfg.PenaltyDeductCredits > 0 {
			aaPenalties = append(aaPenalties, fmt.Sprintf(`"credits_deducted:%d"`, cfg.PenaltyDeductCredits))
		}
		if cfg.PenaltyBanHours > 0 {
			aaPenalties = append(aaPenalties, fmt.Sprintf(`"banned:%dh"`, cfg.PenaltyBanHours))
		}
		antiAbuseInfo := fmt.Sprintf(`{"triggered":"content_too_short","penalties":[%s]}`, strings.Join(aaPenalties, ","))
		g.logRequest(userID, model, service, startedAt, "error", "content_too_short",
			http.StatusBadRequest,
			fmt.Sprintf("total chars %d < min %d (service %s, mode %d)",
				totalChars, cfg.MinChars, service, cfg.Mode),
			antiAbuseInfo)

		// Execute penalties.
		if cfg.PenaltyDeductCredits > 0 {
			if _, err := g.Store.AdjustUserCredits(userID, -cfg.PenaltyDeductCredits); err != nil {
				log.Printf("[ERROR] anti-abuse deduct credits user %d: %v", userID, err)
			}
		}
		if cfg.PenaltyBanHours > 0 {
			until := time.Now().Add(time.Duration(cfg.PenaltyBanHours) * time.Hour)
			if err := g.Store.AutoBanUser(userID, until); err != nil {
				log.Printf("[ERROR] anti-abuse ban user %d: %v", userID, err)
			}
			g.Store.DeleteUserSessions(userID)
			log.Printf("[AUTH] user %d anti-abuse banned until %v (%d hours)", userID, until, cfg.PenaltyBanHours)
		}

		return &antiAbuseErr{
			code:    "content_too_short",
			message: "请求内容过短，请提供更详细的输入。",
		}
	}

	return nil
}
