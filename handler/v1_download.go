package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"dify2api/db"
	"dify2api/difyapp"
)

// templateDownloadLimitPerUser caps template downloads per user per minute.
const templateDownloadLimitPerUser = 3

// marketplaceManifestURL is the fixed Dify marketplace manifest endpoint
// (the only outbound fetch the gateway makes besides Dify App calls).
// Overridable in tests.
var marketplaceManifestURL = "https://marketplace.dify.ai/api/v1/dist/plugins/manifest.json"

// marketplaceMaxBytes caps the manifest response body (5 MiB).
const marketplaceMaxBytes = 5 << 20

// serviceHasTemplate reports whether a service offers a downloadable template.
func serviceHasTemplate(service string) bool {
	return difyapp.TemplateFor(service) != nil
}

// remapInputsForService translates canonical contract inputs into the
// user's latest obfuscated keys for template services. Returns
// (remapped, ok): ok=false when the user has no mapping yet (must download
// first). Unmapped (dummy) keys are never produced.
func (g *Gateway) remapInputsForService(userID int64, service string, inputs map[string]string, purpose string) (map[string]interface{}, bool, error) {
	if !serviceHasTemplate(service) {
		// Regular services pass through with canonical keys.
		out := make(map[string]interface{}, len(inputs))
		for k, v := range inputs {
			out[k] = v
		}
		return out, true, nil
	}
	mapping, err := g.Store.LatestGenerationMapping(userID, service, purpose)
	if err != nil {
		return nil, false, err
	}
	if len(mapping) == 0 {
		return nil, false, nil
	}
	out := make(map[string]interface{}, len(inputs))
	for canonical, val := range inputs {
		if key, ok := mapping[canonical]; ok {
			out[key] = val
		}
	}
	return out, true, nil
}

// normalizeAppParamsForBinding maps a template service's obfuscated app
// parameters back to canonical contract names before the contract check.
// Dummy (unmapped) variables are dropped so they never count as
// uncovered-required.
func (g *Gateway) normalizeAppParamsForBinding(userID int64, service string, params map[string]bool) map[string]bool {
	if !serviceHasTemplate(service) {
		return params
	}
	mapping, err := g.Store.LatestGenerationMapping(userID, service, "personal")
	if err != nil || len(mapping) == 0 {
		return params
	}
	rev := make(map[string]string, len(mapping))
	for canonical, obf := range mapping {
		rev[obf] = canonical
	}
	out := make(map[string]bool, len(params))
	for obf, required := range params {
		if canonical, ok := rev[obf]; ok {
			out[canonical] = required
		}
		// Unmapped keys are dummies — ignored entirely.
	}
	return out
}

// handleServiceModels lists enabled model configs for a template service
// (used by the download modal).
func (g *Gateway) handleServiceModels(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	service := r.PathValue("service")
	if !serviceHasTemplate(service) {
		g.writeError(w, http.StatusNotFound, "not_found", "service has no downloadable template")
		return
	}
	configs, err := g.Store.ListEnabledModelConfigs()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"models": configs})
}

// handleServiceDownload generates and serves a fresh obfuscated DSL.
// purpose=personal (default) or donation (T3.2 gate). Every download
// regenerates with a new seed; the previous mapping is superseded.
func (g *Gateway) handleServiceDownload(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	service := r.PathValue("service")
	if !serviceHasTemplate(service) {
		g.writeError(w, http.StatusNotFound, "not_found", "service has no downloadable template")
		return
	}
	purpose := r.URL.Query().Get("purpose")
	if purpose == "" {
		purpose = "personal"
	}
	if purpose != "personal" && purpose != "donation" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "purpose must be 'personal' or 'donation'")
		return
	}
	modelKey := r.URL.Query().Get("model")
	if modelKey == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	// Per-user download rate limit.
	if !g.templateLimiter.allow(u.ID, templateDownloadLimitPerUser, time.Now()) {
		g.writeError(w, http.StatusTooManyRequests, "rate_limited", t(g.resolveLang(r), "下载过于频繁，请稍后再试", "Too many downloads, please try again later"))
		return
	}
	mc, err := g.Store.GetModelConfig(modelKey)
	if err != nil || mc == nil || !mc.Enabled {
		g.writeError(w, http.StatusNotFound, "model_not_found", "model configuration not found or disabled")
		return
	}
	if purpose == "donation" {
		// T3.2 regeneration gate (B'): active donation rows forbid; inactive
		// entries require explicit confirm and are invalidated.
		gate, err := g.checkDonationRegeneration(u.ID, service, r.URL.Query().Get("confirm") == "true")
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		switch gate {
		case "blocked":
			g.writeError(w, http.StatusForbidden, "donation_locked",
				t(g.resolveLang(r), "您已有该服务的已激活捐赠条目，捐赠 App 不可再生。如需撤销请联系管理员。", "You have an active donation entry for this service; donation Apps cannot be regenerated. Contact an administrator to withdraw."))
			return
		case "need_confirm":
			g.writeError(w, http.StatusConflict, "confirm_required",
				t(g.resolveLang(r), "重新下载将使未激活的旧捐赠条目失效，确认继续？", "Re-downloading will invalidate the old inactive donation entry. Confirm to continue?"))
			return
		}
	}

	startedAt := time.Now()
	tpl := difyapp.TemplateFor(service)
	configuredAsset, err := difyapp.ApplyModelConfig(tpl.Asset, difyapp.ModelOptions{
		ModelKey:         mc.ModelKey,
		Provider:         mc.Provider,
		DependencyPlugin: mc.DependencyPlugin,
		DependencyVer:    mc.DependencyVer,
		DependencyHash:   mc.DependencyHash,
		ParamsJSON:       mc.ParamsJSON,
	})
	if err != nil {
		log.Printf("[ERROR] apply model config (user %d, service %s, model %s): %v", u.ID, service, mc.ModelKey, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "template model configuration failed")
		return
	}
	result, err := difyapp.GenerateObfuscated(configuredAsset, nil)
	if err != nil {
		log.Printf("[ERROR] generate obfuscated template (user %d, service %s): %v", u.ID, service, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "template generation failed")
		return
	}
	if _, err := g.Store.AddServiceGeneration(u.ID, service, modelKey, purpose, result.Seed, result.Mapping, result.DummyKeys, startedAt); err != nil {
		log.Printf("[ERROR] record template generation (user %d): %v", u.ID, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to record download")
		return
	}
	g.logRequest(u.ID, "["+service+"]"+modelKey, service, startedAt, "success", "", http.StatusOK, "", "")

	fileName := service + "-" + modelKey + ".yml"
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(result.DSL)
}

// checkDonationRegeneration implements the B' gate: active donation rows
// block regeneration; inactive (pending/inactive) rows require confirm and
// are invalidated; no rows or all-invalid allow regeneration.
func (g *Gateway) checkDonationRegeneration(userID int64, service string, confirmed bool) (string, error) {
	active, err := g.Store.HasActiveDonationForService(userID, service)
	if err != nil {
		return "", err
	}
	if active {
		return "blocked", nil
	}
	pending, err := g.Store.HasInactiveDonationForService(userID, service)
	if err != nil {
		return "", err
	}
	if pending {
		if !confirmed {
			return "need_confirm", nil
		}
		if err := g.Store.InvalidateDonationsForService(userID, service, time.Now()); err != nil {
			return "", err
		}
	}
	return "allow", nil
}

// --- Marketplace dependency sync ----------------------------------------

// marketplaceSyncOnce fetches the Dify marketplace manifest and updates
// non-manual model configs whose plugin versions changed. On any failure an
// alert-center record is written (the daily task never retries mid-run).
func (g *Gateway) marketplaceSyncOnce(now time.Time) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Host != "marketplace.dify.ai" {
				return fmt.Errorf("redirect to non-marketplace host %q", req.URL.Host)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, marketplaceManifestURL, nil)
	if err != nil {
		g.recordMarketplaceAlert("request build failed: " + err.Error())
		return
	}
	req.Header.Set("User-Agent", "dify2api/1.4")
	resp, err := client.Do(req)
	if err != nil {
		g.recordMarketplaceAlert("fetch failed: " + err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		g.recordMarketplaceAlert(fmt.Sprintf("unexpected status %d", resp.StatusCode))
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, marketplaceMaxBytes))
	if err != nil {
		g.recordMarketplaceAlert("read failed: " + err.Error())
		return
	}
	var manifest struct {
		Plugins []marketplacePlugin `json:"plugins"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		g.recordMarketplaceAlert("manifest parse failed: " + err.Error())
		return
	}
	updated := 0
	seen := map[string]bool{}
	for _, p := range manifest.Plugins {
		plugin, version, hash, ok := p.dependencyPin()
		if !ok || seen[plugin] {
			continue
		}
		seen[plugin] = true
		n, err := g.Store.UpdateModelDependency(plugin, version, hash, now)
		if err != nil {
			g.recordMarketplaceAlert("update failed for " + plugin + ": " + err.Error())
			continue
		}
		updated += n
	}
	if updated > 0 {
		log.Printf("[MARKETPLACE] updated %d model config dependency pins", updated)
	}
}

// marketplacePlugin matches the live dist manifest contract. The current
// endpoint exposes org/name/latest_version/latest_package_identifier; the
// legacy fields remain accepted for compatibility with older mirrors.
type marketplacePlugin struct {
	Org                     string `json:"org"`
	Name                    string `json:"name"`
	LatestVersion           string `json:"latest_version"`
	LatestPackageIdentifier string `json:"latest_package_identifier"`
	UniqueIdentifier        string `json:"unique_identifier"`
	Version                 string `json:"version"`
	Hash                    string `json:"hash"`
}

// dependencyPin returns plugin, version and checksum from identifiers such as
// "langgenius/anthropic:0.3.26@<checksum>". A complete @checksum pin is
// required because generated DSL dependencies use that importable format.
func (p marketplacePlugin) dependencyPin() (string, string, string, bool) {
	plugin := strings.TrimSpace(p.UniqueIdentifier)
	version := strings.TrimSpace(p.Version)
	hash := strings.TrimSpace(p.Hash)
	identifier := strings.TrimSpace(p.LatestPackageIdentifier)
	if identifier != "" {
		at := strings.LastIndex(identifier, "@")
		if at <= 0 || at == len(identifier)-1 {
			return "", "", "", false
		}
		hash = strings.TrimSpace(identifier[at+1:])
		withoutHash := identifier[:at]
		colon := strings.LastIndex(withoutHash, ":")
		if colon <= 0 || colon == len(withoutHash)-1 {
			return "", "", "", false
		}
		plugin = strings.TrimSpace(withoutHash[:colon])
		version = strings.TrimSpace(withoutHash[colon+1:])
	}
	if plugin == "" && strings.TrimSpace(p.Org) != "" && strings.TrimSpace(p.Name) != "" {
		plugin = strings.TrimSpace(p.Org) + "/" + strings.TrimSpace(p.Name)
	}
	if version == "" {
		version = strings.TrimSpace(p.LatestVersion)
	}
	return plugin, version, hash, plugin != "" && strings.Contains(plugin, "/") && version != "" && hash != ""
}

func (g *Gateway) recordMarketplaceAlert(msg string) {
	log.Printf("[MARKETPLACE] %s", msg)
	if err := g.Store.AddAdminAlert(&db.AdminAlert{Type: "marketplace_sync", Message: msg}); err != nil {
		log.Printf("[ERROR] record marketplace alert: %v", err)
	}
}

// marketplaceWorker runs the daily dependency sync at UTC 03:00 plus one
// initial run shortly after startup.
func (g *Gateway) marketplaceWorker(ctx context.Context) {
	run := func() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		g.marketplaceSyncOnce(time.Now())
	}
	// Initial run 10 seconds after startup (DB and alerts are ready).
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
		run()
	}
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			run()
		}
	}
}

// --- Admin model config CRUD -------------------------------------------

// handleAdminListModelConfigs returns all model configs.
func (g *Gateway) handleAdminListModelConfigs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	configs, err := g.Store.ListModelConfigs()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"models": configs})
}

// handleAdminPutModelConfig upserts one config; a manual flag set here is
// sticky (the daily marketplace task never overrides manual rows).
func (g *Gateway) handleAdminPutModelConfig(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	var m db.ModelConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&m); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	m.ModelKey = strings.TrimSpace(m.ModelKey)
	m.DisplayName = strings.TrimSpace(m.DisplayName)
	m.Provider = strings.TrimSpace(m.Provider)
	m.DependencyPlugin = strings.TrimSpace(m.DependencyPlugin)
	m.DependencyVer = strings.TrimSpace(m.DependencyVer)
	m.DependencyHash = strings.TrimSpace(m.DependencyHash)
	m.ParamsJSON = strings.TrimSpace(m.ParamsJSON)
	if m.ModelKey == "" || m.DisplayName == "" || m.Provider == "" || m.DependencyPlugin == "" ||
		m.DependencyVer == "" || m.DependencyHash == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model_key, display_name, provider and dependency pin are required")
		return
	}
	if m.ParamsJSON != "" {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(m.ParamsJSON), &params); err != nil || params == nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "params_json must be a JSON object")
			return
		}
	}
	if !m.Manual {
		// Non-manual upserts from the admin UI keep manual=false only when
		// explicitly requested; an existing manual row must not silently
		// lose its protection — the UI sends manual=true for protected rows.
		if existing, err := g.Store.GetModelConfig(m.ModelKey); err == nil && existing != nil && existing.Manual {
			m.Manual = true
		}
	}
	if err := g.Store.PutModelConfig(m); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// handleAdminDeleteModelConfig removes one config.
func (g *Gateway) handleAdminDeleteModelConfig(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	key := r.PathValue("model_key")
	if key == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model_key is required")
		return
	}
	if err := g.Store.DeleteModelConfig(key); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// marketplaceHost is exported for tests.
var _ = url.Values{}
