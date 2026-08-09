package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
	"dify2api/translator"
)

// appConfigJSON is the API representation of an AppConfig (never includes the key).
func appConfigJSON(c *db.AppConfig) map[string]interface{} {
	return map[string]interface{}{
		"id":            c.ID,
		"model":         c.Model,
		"dify_base_url": c.DifyBaseURL,
		"note":          c.Note,
		"has_key":       c.DifyAPIKeyEnc != "",
		"enabled":       c.Enabled,
		"created_at":    c.CreatedAt,
		"updated_at":    c.UpdatedAt,
	}
}

// appProbeTimeout is the single total deadline for one App compatibility
// probe: it covers semaphore queueing, DNS, TCP connect, TLS, response
// headers and the full body.
const appProbeTimeout = 15 * time.Second

// checkAppBinding fetches the App's parameter list and validates it against
// the service contract of the model's prefix. Informational only (never blocks).
// The per-user probe cap and the total deadline keep the check cheap for the
// operator to expose: rate-limited or timed-out probes report app_check.error
// and never carry a "compatible" verdict.
func (g *Gateway) checkAppBinding(ctx context.Context, userID int64, model, baseURL, apiKey, lang string) map[string]interface{} {
	service := translator.ServiceOfModel(model)
	// Per-user cap first: a rate-limited attempt must not consume a global
	// semaphore slot (nor wait for one). The cap is admin-tunable via the
	// probe_limit_per_user setting; <=0 falls back to the default.
	limit := g.Store.GetSettingInt(db.SettingProbeLimitPerUser, db.DefaultProbeLimitPerUser)
	if !g.probeLimiter.allow(userID, limit, time.Now()) {
		return map[string]interface{}{"service": service, "error": t(lang, "App 检查请求过于频繁，请稍后重试。", "App check rate limited. Try again later.")}
	}
	// Single total deadline for the whole probe (queueing included). The
	// request context's own cancellation (client disconnect) still applies.
	probeCtx, cancel := context.WithTimeout(ctx, appProbeTimeout)
	defer cancel()
	release, err := g.acquireDifyProbe(probeCtx)
	if err != nil {
		return map[string]interface{}{"service": service, "error": probeError(err, lang)}
	}
	defer release()
	client, err := g.newDifyClient(userID, baseURL, apiKey, appProbeTimeout)
	if err != nil {
		return map[string]interface{}{"service": service, "error": sanitizePublicUpstreamError(err, err.Error(), lang)}
	}
	params, err := client.FetchParametersContext(probeCtx)
	if err != nil {
		return map[string]interface{}{"service": service, "error": probeError(err, lang)}
	}
	res := translator.CheckAppParams(service, params)
	out := map[string]interface{}{
		"service":    service,
		"compatible": res.Compatible,
	}
	if len(res.MissingContractVars) > 0 {
		out["missing_contract_vars"] = res.MissingContractVars
	}
	if len(res.UncoveredAppRequired) > 0 {
		out["uncovered_app_required"] = res.UncoveredAppRequired
	}
	if len(res.ExtraAppOptional) > 0 {
		out["extra_app_optional"] = res.ExtraAppOptional
	}
	return out
}

// probeError maps a probe failure to its app_check.error text: deadline
// exhaustion (semaphore queueing or upstream) is always reported as a
// timeout; all other failures are mapped to safe localized categories.
func probeError(err error, lang string) string {
	return sanitizePublicUpstreamError(err, err.Error(), lang)
}

// bindingUnchanged reports whether an update changes nothing the App probe
// depends on: model, normalized base URL and API key. Keys are compared on
// decrypted plaintext (the stored ciphertext is re-randomized on every
// encrypt); a decryption failure conservatively counts as changed.
func (g *Gateway) bindingUnchanged(existing *db.AppConfig, req *configRequest) bool {
	if existing == nil || existing.Model != req.Model || existing.DifyBaseURL != req.BaseURL {
		return false
	}
	key, err := g.Store.Decrypt(existing.DifyAPIKeyEnc)
	return err == nil && key == req.APIKey
}

// --- GET /api/configs ---
func (g *Gateway) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	configs, err := g.Store.ListAppConfigs(u.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]interface{}, 0, len(configs))
	for _, c := range configs {
		out = append(out, appConfigJSON(c))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"configs": out})
}

// configRequest is the create/update payload.
type configRequest struct {
	Model   string `json:"model"`
	BaseURL string `json:"dify_base_url"`
	APIKey  string `json:"dify_api_key"`
	Note    string `json:"note"`
}

func (req *configRequest) validate(g *Gateway) string {
	req.Model = strings.TrimSpace(req.Model)
	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Note = strings.TrimSpace(req.Note)
	if req.Model == "" {
		return "model is required"
	}
	// Block model names that contain the reserved charity prefix.
	// Block brackets in the backend portion (after the "[service]" prefix).
	// The message deliberately does not reveal which check triggered the
	// rejection to avoid leaking internal detection logic.
	if strings.Contains(req.Model, "[公益]") {
		return "模型名不得包含方括号（[ ]）或保留前缀"
	}
	// Backend part (after the service bracket) must not contain brackets.
	if idx := strings.Index(req.Model, "]"); idx > 0 && idx+1 < len(req.Model) {
		backend := req.Model[idx+1:]
		if strings.ContainsAny(backend, "[]") {
			return "模型名不得包含方括号（[ ]）或保留前缀"
		}
	}
	// Model names must use a registered service prefix (dropdown-selected in
	// the UI). Free-form model names are not accepted.
	svc := translator.ServiceOfModel(req.Model)
	if svc == "" {
		return "模型名需以 [服务] 前缀开头，如 [general]claude-opus-4-6"
	}
	if !translator.IsSupportedService(svc) {
		names := []string{}
		for _, s := range translator.SupportedServices() {
			names = append(names, s.Name)
		}
		return fmt.Sprintf("不支持的服务 %q；当前支持：%s", svc, strings.Join(names, "，"))
	}
	normalizedBaseURL, err := g.difyPolicy.ValidateBaseURL(req.BaseURL)
	if err != nil {
		return "dify_base_url is not allowed by the egress policy"
	}
	req.BaseURL = normalizedBaseURL
	if req.APIKey == "" {
		return "dify_api_key is required"
	}
	return ""
}

// --- GET /api/services ---
// Lists the services registered in code (drives the dashboard dropdown).
// With ?donation=1 only services the admin allows in the self-service
// donation form (donation_selectable) are returned.
func (g *Gateway) handleListServices(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	all := translator.SupportedServices()
	if r.URL.Query().Get("donation") == "1" {
		filtered := all[:0]
		for _, s := range all {
			if g.Store.IsServiceDonationSelectable(s.Name) {
				filtered = append(filtered, s)
			}
		}
		all = filtered
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"services": all})
}

// selfSiteNotice returns a localized hint when a Dify base URL points at the
// gateway's own sites (SITE_BASE_URL / ADMIN_HOST hostname). Novice users
// sometimes paste this site's address into the App API endpoint field instead
// of their own Dify App URL. Informational only — the config is still saved;
// only public hostnames are compared, so the hint adds no egress side channel
// (the dial-time self-origin guard remains the only enforcement).
func (g *Gateway) selfSiteNotice(r *http.Request, baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	site := strings.ToLower(strings.TrimSuffix(stripPort(g.Config.Admin.SiteHost), "."))
	admin := strings.ToLower(strings.TrimSuffix(stripPort(g.Config.Admin.AdminHost), "."))
	if host != site && host != admin {
		return ""
	}
	return t(g.resolveLang(r),
		"提示：这个地址看起来是本站（Dify2API 控制台）的地址，不是你的 Dify App 的 API 端点。请填写你自己的 Dify 应用地址，例如 https://api.dify.ai/v1",
		"Hint: this address looks like this site (the Dify2API console), not your Dify App's API endpoint. Please enter your own Dify App URL, e.g. https://api.dify.ai/v1")
}

// --- POST /api/configs ---
// Creates a config, then validates the App's parameter list against the
// service contract (informational verdict in "app_check"; never blocks).
func (g *Gateway) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if msg := req.validate(g); msg != "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	cfg, err := g.Store.CreateAppConfig(u.ID, req.Model, req.BaseURL, req.APIKey, req.Note)
	if err != nil {
		if db.IsUniqueViolation(err) {
			g.writeError(w, http.StatusConflict, "conflict", t(g.resolveLang(r), "该模型名已在你的配置中存在", "This model name already exists in your configuration"))
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	g.recordConsoleActivity(u.ID)

	resp := map[string]interface{}{
		"ok":        true,
		"config":    appConfigJSON(cfg),
		"app_check": g.checkAppBinding(r.Context(), u.ID, req.Model, req.BaseURL, req.APIKey, g.resolveLang(r)),
	}
	if n := g.selfSiteNotice(r, req.BaseURL); n != "" {
		resp["notice"] = n
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- PUT /api/configs/{id} ---
func (g *Gateway) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid config id")
		return
	}
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if msg := req.validate(g); msg != "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	// Fetch the current row before the update: an update that changes nothing
	// the probe depends on (model, base URL, API key) skips the compatibility
	// check entirely — note-only edits must not probe.
	existing, _ := g.Store.GetAppConfig(id)
	if err := g.Store.UpdateAppConfig(id, u.ID, req.Model, req.BaseURL, req.APIKey, req.Note); err != nil {
		if db.IsUniqueViolation(err) {
			g.writeError(w, http.StatusConflict, "conflict", t(g.resolveLang(r), "该模型名已在你的配置中存在", "This model name already exists in your configuration"))
			return
		}
		if strings.Contains(err.Error(), "not found") {
			g.writeError(w, http.StatusNotFound, "not_found", "config not found")
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	g.recordConsoleActivity(u.ID)

	cfg, _ := g.Store.GetAppConfig(id)
	resp := map[string]interface{}{
		"ok":     true,
		"config": appConfigJSON(cfg),
	}
	if !g.bindingUnchanged(existing, &req) {
		resp["app_check"] = g.checkAppBinding(r.Context(), u.ID, req.Model, req.BaseURL, req.APIKey, g.resolveLang(r))
	}
	if n := g.selfSiteNotice(r, req.BaseURL); n != "" {
		resp["notice"] = n
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- POST /api/configs/{id}/toggle ---
func (g *Gateway) handleToggleConfig(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid config id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := g.Store.SetAppConfigEnabled(id, u.ID, req.Enabled); err != nil {
		g.writeError(w, http.StatusNotFound, "not_found", "config not found")
		return
	}
	g.recordConsoleActivity(u.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- DELETE /api/configs/{id} ---
func (g *Gateway) handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid config id")
		return
	}
	if err := g.Store.DeleteAppConfig(id, u.ID); err != nil {
		g.writeError(w, http.StatusNotFound, "not_found", "config not found")
		return
	}
	g.recordConsoleActivity(u.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
