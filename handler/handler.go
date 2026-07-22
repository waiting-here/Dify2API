package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"dify2api/config"
	"dify2api/db"
	"dify2api/dify"
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
}

// NewGateway creates a new Gateway.
func NewGateway(cfg *config.Config, store *db.Store) *Gateway {
	return &Gateway{
		Config:        cfg,
		Store:         store,
		limiter:       newRateLimiter(),
		chatSem:       make(chan struct{}, cfg.MaxChatInFlight),
		loginThrottle: newLoginThrottle(cfg),
	}
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
	mux.HandleFunc("GET /api/admin/logs", g.handleAdminLogs)

	// Admin user management
	mux.HandleFunc("GET /api/admin/users", g.handleAdminListUsers)
	mux.HandleFunc("POST /api/admin/users/{id}/ban", g.handleAdminBanUser)
	mux.HandleFunc("POST /api/admin/users/{id}/unban", g.handleAdminUnbanUser)
	mux.HandleFunc("DELETE /api/admin/users/{id}", g.handleAdminDeleteUser)
	mux.HandleFunc("POST /api/admin/users/{id}/reset-key", g.handleAdminResetUserKey)
	mux.HandleFunc("GET /api/admin/users/{id}/export", g.handleAdminExportUser)
	mux.HandleFunc("GET /api/admin/settings", g.handleAdminGetSettings)
	mux.HandleFunc("PUT /api/admin/settings", g.handleAdminPutSettings)

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

// handleModels: per-user model list (enabled configs of the caller-key user).
func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := g.userFromCallerKey(r)
	if user == nil {
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openai.ModelListResponse{Object: "list", Data: models})
}

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	// the semaphore.  With the default config (MaxChatInFlight=64,
	// MaxRequestBodyMB=10) the worst-case memory footprint for request
	// bodies is 640 MB.
	select {
	case g.chatSem <- struct{}{}:
		defer func() { <-g.chatSem }()
	default:
		w.Header().Set("Retry-After", "3")
		g.writeError(w, http.StatusTooManyRequests, "server_busy", "当前服务繁忙（并发已达上限），请稍后重试")
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
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		if g.Config.Debug {
			dumpDebugRequest(g.Config.DebugDir, r, rawBody, "messages array is empty", nil)
		}
		http.Error(w, "messages array is empty", http.StatusBadRequest)
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

	// 1. Caller key -> user.
	user := g.userFromCallerKey(r)
	if user == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing API key")
		return
	}

	service := translator.ServiceOfModel(req.Model)

	// 2. RPM limiting (per-user override or global default). Violations are
	// logged; 5 violations within 24h trigger an automatic 24h ban.
	rpmLimit := g.effectiveRPM(user.ID)
	if !g.limiter.allow(user.ID, startedAt, rpmLimit) {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "rpm_exceeded")
		violations, _ := g.Store.CountRecentErrors(user.ID, "rpm_exceeded", startedAt.Add(-24*time.Hour))
		if violations >= 5 {
			until := time.Now().Add(24 * time.Hour)
			if err := g.Store.AutoBanUser(user.ID, until); err != nil {
				log.Printf("[ERROR] auto-ban user %d: %v", user.ID, err)
			}
			g.Store.DeleteUserSessions(user.ID)
			log.Printf("[AUTH] user %d auto-banned until %v after %d RPM violations", user.ID, until, violations)
			g.writeError(w, http.StatusForbidden, "rpm_exceeded",
				fmt.Sprintf("已超出每分钟请求上限（%d 次/分），且因 24 小时内累计 %d 次超限，账号已被自动封禁 24 小时", rpmLimit, violations))
			return
		}
		g.writeError(w, http.StatusForbidden, "rpm_exceeded",
			fmt.Sprintf("已超出每分钟请求上限（%d 次/分），请稍后再试（24 小时内第 %d 次超限，累计 5 次将自动封禁 24 小时）", rpmLimit, violations))
		return
	}

	// 3. Model full name -> the user's enabled App config.
	appCfg, err := g.Store.GetEnabledAppConfigByModel(user.ID, req.Model)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if appCfg == nil {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "model_not_found")
		g.writeError(w, http.StatusNotFound, "model_not_found",
			fmt.Sprintf("model %q not found in your configs (check the dashboard or /v1/models)", req.Model))
		return
	}

	// 4. Per-service contract validation & mapping.
	inputs, images, err := translator.TranslateForService(service, req.Messages)
	if err != nil {
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "invalid_message_sequence")
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

	// 5. Build the Dify client from the user's config.
	apiKey, err := g.Store.Decrypt(appCfg.DifyAPIKeyEnc)
	if err != nil {
		log.Printf("[ERROR] decrypt app key (user %d, config %d): %v", user.ID, appCfg.ID, err)
		g.logRequest(user.ID, req.Model, service, startedAt, "error", "internal")
		g.writeError(w, http.StatusInternalServerError, "internal", "credential error")
		return
	}
	client := dify.NewClient(appCfg.DifyBaseURL, apiKey, time.Duration(g.Config.DifyHTTPTimeoutMs)*time.Millisecond)
	client.SSEBufferSize = g.Config.SSEBufferMB << 20

	wfInputs := make(map[string]interface{}, len(inputs)+1)
	for k, v := range inputs {
		wfInputs[k] = v
	}

	// 6. Images (image-processing): http(s) URLs pass through as remote_url;
	// data URIs are uploaded first (/v1/files/upload -> upload_file_id).
	if len(images) > 0 {
		files, err := g.buildImageFiles(client, wfReq_User(user.ID), images)
		if err != nil {
			log.Printf("[ERROR] image files (user %d): %v", user.ID, err)
			g.logRequest(user.ID, req.Model, service, startedAt, "error", "image_upload_failed")
			g.writeDifyError(w, err)
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
		g.handleStreaming(w, client, wfReq, modelName, user.ID, service, startedAt)
	} else {
		g.handleBlocking(w, client, wfReq, modelName, user.ID, service, startedAt)
	}
}

func (g *Gateway) handleStreaming(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, service string, startedAt time.Time) {
	wfReq.ResponseMode = "streaming"
	events, errCh := client.StreamWorkflow(wfReq)

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
		if err != nil {
			g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error")
			g.writeDifyError(w, err)
			return
		}
	}
	if firstEvt == nil {
		// Channel closed with no events: check for a late error, else treat
		// as an empty-but-successful stream.
		select {
		case err := <-errCh:
			if err != nil {
				g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error")
				g.writeDifyError(w, err)
				return
			}
		default:
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.logRequest(userID, modelName, service, startedAt, "error", "stream_unsupported")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	conv := translator.NewStreamConverter(modelName)

	if firstEvt != nil {
		if msg := conv.Convert(*firstEvt); msg != nil {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
	}
	for evt := range events {
		if msg := conv.Convert(evt); msg != nil {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
	}

	// Drain errCh after the event channel closes: a transport-level failure
	// (connection drop, SSE scan error) surfaces here without any Dify
	// error event having been converted.
	status, code := "success", ""
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
		if streamErr != nil {
			log.Printf("[ERROR] dify stream (user %d): %v", userID, streamErr)
		}
	case streamErr != nil:
		// Transport-level failure with no error event: emit the error
		// frame ourselves, and likewise skip [DONE].
		log.Printf("[ERROR] dify stream (user %d): %v", userID, streamErr)
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] "+streamErr.Error()))
		flusher.Flush()
		status, code = "error", "upstream_error"
	default:
		for _, msg := range conv.Finalize() {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
	g.logRequest(userID, modelName, service, startedAt, status, code)
}

func (g *Gateway) handleBlocking(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, service string, startedAt time.Time) {
	wfReq.ResponseMode = "blocking"
	text, err := client.BlockingWorkflow(wfReq)
	if err != nil {
		log.Printf("[ERROR] dify blocking (user %d): %v", userID, err)
		g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error")
		g.writeDifyError(w, err)
		return
	}
	g.logRequest(userID, modelName, service, startedAt, "success", "")

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
}

// wfReq_User formats the Dify user id.
func wfReq_User(userID int64) string { return fmt.Sprintf("u%d", userID) }

// buildImageFiles turns image references (data URIs / http(s) URLs) into Dify
// workflow file objects.
func (g *Gateway) buildImageFiles(client *dify.Client, difyUser string, images []string) ([]map[string]interface{}, error) {
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
			id, err := client.UploadFile(difyUser, fmt.Sprintf("image-%d.%s", i+1, ext), mime, data)
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

// logRequest records one completed call (metadata only).
func (g *Gateway) logRequest(userID int64, model, service string, startedAt time.Time, status, errorCode string) {
	if err := g.Store.AddRequestLog(userID, model, service, startedAt, time.Now(), status, errorCode); err != nil {
		log.Printf("[WARN] write request log: %v", err)
	}
}

// handleListLogs returns the session user's recent request logs.
func (g *Gateway) handleListLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	json.NewEncoder(w).Encode(map[string]interface{}{"logs": logs})
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

// writeDifyError forwards a Dify error to the client, preserving the
// upstream error code when available.  It builds its own response body
// with a [Dify] prefix (distinct from writeError's [Dify2API] prefix).
//
// Status mapping: upstream 4xx are passed through unchanged (they indicate
// a caller-side problem, e.g. invalid_param → 400, so client retry logic
// is not misled); everything else (5xx, network errors) maps to 502.
func (g *Gateway) writeDifyError(w http.ResponseWriter, err error) {
	var de *dify.DifyError
	code := "upstream_error"
	message := err.Error()
	status := http.StatusBadGateway
	if errors.As(err, &de) {
		if de.Code != "" {
			code = de.Code
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
