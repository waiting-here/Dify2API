package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
	"dify2api/dify"
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

// checkAppBinding fetches the App's parameter list and validates it against
// the service contract of the model's prefix. Informational only (never blocks).
func (g *Gateway) checkAppBinding(model, baseURL, apiKey string) map[string]interface{} {
	service := translator.ServiceOfModel(model)
	client := dify.NewClient(baseURL, apiKey, 15*time.Second)
	params, err := client.FetchParameters()
	if err != nil {
		return map[string]interface{}{"service": service, "error": err.Error()}
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

func (req *configRequest) validate() string {
	req.Model = strings.TrimSpace(req.Model)
	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Note = strings.TrimSpace(req.Note)
	if req.Model == "" {
		return "model is required"
	}
	// Model names must use a registered service prefix (dropdown-selected in
	// the UI). Free-form model names are not accepted.
	svc := translator.ServiceOfModel(req.Model)
	if svc == "" {
		return "模型名需以 [服务] 前缀开头,如 [general]claude-opus-4-6"
	}
	if !translator.IsSupportedService(svc) {
		names := []string{}
		for _, s := range translator.SupportedServices() {
			names = append(names, s.Name)
		}
		return fmt.Sprintf("不支持的服务 %q;当前支持: %s", svc, strings.Join(names, ", "))
	}
	if req.BaseURL == "" || !(strings.HasPrefix(req.BaseURL, "http://") || strings.HasPrefix(req.BaseURL, "https://")) {
		return "dify_base_url must be a non-empty http(s) URL"
	}
	if req.APIKey == "" {
		return "dify_api_key is required"
	}
	return ""
}

// --- GET /api/services ---
// Lists the services registered in code (drives the dashboard dropdown).
func (g *Gateway) handleListServices(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"services": translator.SupportedServices()})
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
	if msg := req.validate(); msg != "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	cfg, err := g.Store.CreateAppConfig(u.ID, req.Model, req.BaseURL, req.APIKey, req.Note)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			g.writeError(w, http.StatusConflict, "conflict", "该模型名已在你的配置中存在")
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"config":     appConfigJSON(cfg),
		"app_check":  g.checkAppBinding(req.Model, req.BaseURL, req.APIKey),
	})
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
	if msg := req.validate(); msg != "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}

	if err := g.Store.UpdateAppConfig(id, u.ID, req.Model, req.BaseURL, req.APIKey, req.Note); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			g.writeError(w, http.StatusConflict, "conflict", "该模型名已在你的配置中存在")
			return
		}
		if strings.Contains(err.Error(), "not found") {
			g.writeError(w, http.StatusNotFound, "not_found", "config not found")
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	cfg, _ := g.Store.GetAppConfig(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"config":     appConfigJSON(cfg),
		"app_check":  g.checkAppBinding(req.Model, req.BaseURL, req.APIKey),
	})
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}
