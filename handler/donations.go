package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
	"dify2api/diagnostic"
	"dify2api/dify"
	"dify2api/translator"
)

// --- Helpers ---

// IsCharityModel reports whether a model name starts with the "[公益]" prefix.
func IsCharityModel(model string) bool {
	return strings.HasPrefix(model, "[公益]")
}

// ParseCharityModel strips the "[公益]" prefix then uses ServiceOfModel to
// extract (service, backend). Example: "[公益][general]x" -> ("general", "x").
func ParseCharityModel(model string) (service, backend string) {
	rest := strings.TrimPrefix(model, "[公益]")
	service = translator.ServiceOfModel(rest)
	if service != "" {
		if idx := strings.Index(rest, "]"); idx > 0 && idx+1 < len(rest) {
			backend = rest[idx+1:]
		}
	}
	return
}

func normalizeDonationService(service string) (string, error) {
	service = strings.TrimSpace(service)
	if !translator.IsSupportedService(service) {
		return "", fmt.Errorf("不支持的服务 %q", service)
	}
	return service, nil
}

func normalizeDonationModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("模型名不得为空")
	}
	if strings.ContainsAny(model, "[]") {
		return "", errors.New("模型名不得包含方括号")
	}
	return model, nil
}

func normalizeDonationTarget(service, model string) (string, string, error) {
	service, err := normalizeDonationService(service)
	if err != nil {
		return "", "", err
	}
	model, err = normalizeDonationModel(model)
	if err != nil {
		return "", "", err
	}
	return service, model, nil
}

func validateDonationAPIKey(apiKey *string, required bool) error {
	if apiKey == nil {
		if required {
			return errors.New("dify_api_key 为必填")
		}
		return nil
	}
	if strings.TrimSpace(*apiKey) == "" {
		return errors.New("dify_api_key 不得为空白")
	}
	return nil
}

// charityModelName builds the model string for logging: "[公益][s]m".
func charityModelName(service, backend string) string {
	return fmt.Sprintf("[公益][%s]%s", service, backend)
}

// pickWeightedDonation atomically acquires an RPM lease for one weighted
// candidate. If a sampled candidate loses an acquire race, it is removed and
// sampling continues. releaseRPM is called only when setup aborts before any
// donated Dify credential is used.
func pickWeightedDonation(candidates []*db.Donation, limiter *donationRateLimiter) (picked *db.Donation, releaseRPM func()) {
	remainingCandidates := append([]*db.Donation(nil), candidates...)
	for len(remainingCandidates) > 0 {
		now := time.Now().Unix()
		total := 0.0
		weights := make([]float64, len(remainingCandidates))
		for i, d := range remainingCandidates {
			remaining := d.Deadline - now
			if remaining < 60 {
				remaining = 60
			}
			weights[i] = 1.0 / float64(remaining)
			total += weights[i]
		}
		index := len(remainingCandidates) - 1
		if total > 0 {
			r := rand.Float64() * total
			for i, weight := range weights {
				r -= weight
				if r <= 0 {
					index = i
					break
				}
			}
		}
		candidate := remainingCandidates[index]
		if release, ok := limiter.acquire(candidate.ID, candidate.RpmLimit); ok {
			return candidate, release
		}
		remainingCandidates = append(remainingCandidates[:index], remainingCandidates[index+1:]...)
	}
	return nil, nil
}

// validateDonationApp checks a Dify App's availability and parameter
// compatibility for a donation entry. Returns a validation result map
// suitable for inclusion in API responses (informational, never blocks).
func (g *Gateway) validateDonationApp(ctx context.Context, userID int64, service, baseURL, apiKey string) map[string]interface{} {
	return g.validateDonationAppWithMapping(ctx, userID, service, baseURL, apiKey, nil)
}

// validateDonationAppWithMapping additionally accepts a canonical->obfuscated
// mapping snapshot (template services): the App's parameter names are
// translated back to canonical names before the contract check, and dummy
// variables are ignored.
func (g *Gateway) validateDonationAppWithMapping(ctx context.Context, userID int64, service, baseURL, apiKey string, mapping map[string]string) map[string]interface{} {
	release, err := g.acquireDifyProbe(ctx)
	if err != nil {
		return map[string]interface{}{"compatible": false, "message": err.Error()}
	}
	defer release()
	client, err := g.newDifyClient(userID, baseURL, apiKey, 15*time.Second)
	if err != nil {
		return map[string]interface{}{"compatible": false, "message": err.Error()}
	}
	params, err := client.FetchParametersContext(ctx)
	if err != nil {
		return map[string]interface{}{
			"compatible": false,
			"message":    fmt.Sprintf("App 不可达: %v", err),
		}
	}
	if len(mapping) > 0 {
		rev := make(map[string]string, len(mapping))
		for canonical, obf := range mapping {
			rev[obf] = canonical
		}
		normalized := make(map[string]bool, len(params))
		for obf, required := range params {
			if canonical, ok := rev[obf]; ok {
				normalized[canonical] = required
			}
			// Unmapped (dummy) variables are ignored.
		}
		params = normalized
	}
	res := translator.CheckAppParams(service, params)
	out := map[string]interface{}{
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
	if res.Compatible {
		out["message"] = "App 参数匹配成功"
	} else {
		out["message"] = "App 参数与契约不兼容"
	}
	return out
}

// validatePublicDonationApp applies the user-facing error boundary while the
// admin validation path above intentionally retains raw diagnostics.
func (g *Gateway) validatePublicDonationApp(ctx context.Context, userID int64, service, baseURL, apiKey, lang string) map[string]interface{} {
	out := g.validateDonationApp(ctx, userID, service, baseURL, apiKey)
	public := make(map[string]interface{}, len(out))
	for key, value := range out {
		public[key] = value
	}
	if compatible, ok := public["compatible"].(bool); ok && !compatible {
		if raw, ok := public["message"].(string); ok && raw != "" {
			public["message"] = sanitizePublicDonationValidationMessage(raw, lang)
		}
	}
	return public
}

func sanitizePublicDonationValidationMessage(raw, lang string) string {
	// This is a local contract diagnosis, not an upstream detail. Keep it
	// useful; only probe/transport messages cross the sanitizer.
	if raw == "App 参数与契约不兼容" || raw == "App parameter contract mismatch" {
		return raw
	}
	return sanitizePublicUpstreamError(nil, raw, lang)
}

// --- Donation CRUD ---

// POST /api/admin/donations
func (g *Gateway) handleCreateDonation(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveCreateDonation(w, r, admin)
}

// serveCreateDonation creates a donation entry; shared by the admin and the
// level-5 charity co-admin endpoints (operator drives the App probe).
func (g *Gateway) serveCreateDonation(w http.ResponseWriter, r *http.Request, operator *db.User) {

	var req struct {
		Service      string `json:"service"`
		Model        string `json:"model"`
		DifyBaseURL  string `json:"dify_base_url"`
		DifyAPIKey   string `json:"dify_api_key"`
		SourceUserID *int64 `json:"source_user_id"`
		SourceText   string `json:"source_text"`
		Deadline     int64  `json:"deadline"`
		TotalCount   int    `json:"total_count"`
		RpmLimit     int    `json:"rpm_limit"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	service, model, inputErr := normalizeDonationTarget(req.Service, req.Model)
	if inputErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
		return
	}
	req.Service, req.Model = service, model

	// Validate deadline
	if req.Deadline <= time.Now().Unix() {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "截止时间必须是将来的 Unix 时间戳", "Deadline must be a future Unix timestamp"))
		return
	}

	// Validate total_count
	if req.TotalCount <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "捐赠次数必须为正整数", "Donation count must be a positive integer"))
		return
	}

	// Validate and normalize dify_base_url against the deployment egress policy.
	normalizedBaseURL, baseErr := g.difyPolicy.ValidateBaseURL(req.DifyBaseURL)
	if baseErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "dify_base_url 不符合出站安全策略", "dify_base_url is not allowed by the egress policy")+": "+baseErr.Error())
		return
	}
	req.DifyBaseURL = normalizedBaseURL

	if err := validateDonationAPIKey(&req.DifyAPIKey, true); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Source validation
	sourceUserID := sql.NullInt64{}
	sourceDiscordID := ""
	sourceUsername := ""
	sourceText := strings.TrimSpace(req.SourceText)
	requireNote := false

	if req.SourceUserID != nil {
		srcUser, err := g.Store.GetUserByID(*req.SourceUserID)
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if srcUser == nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				"来源用户不存在")
			return
		}
		// Admin row is allowed as a source (§8.4#2)，but when the source
		// IS the administrator the note becomes mandatory.
		if srcUser.IsAdmin {
			requireNote = true
		}
		sourceUserID = sql.NullInt64{Int64: *req.SourceUserID, Valid: true}
		sourceDiscordID = srcUser.DiscordID
		sourceUsername = srcUser.Username
	} else if sourceText == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"来源用户或来源文本至少填一项")
		return
	} else {
		// Free-text source: always requires a note (administrator is
		// entering this manually).
		requireNote = true
	}

	req.Note = strings.TrimSpace(req.Note)
	if requireNote && req.Note == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"来源为管理员时备注为必填")
		return
	}

	d := &db.Donation{
		Service:         req.Service,
		Model:           req.Model,
		DifyBaseURL:     req.DifyBaseURL,
		SourceUserID:    sourceUserID,
		SourceDiscordID: sourceDiscordID,
		SourceUsername:  sourceUsername,
		SourceText:      sourceText,
		Deadline:        req.Deadline,
		TotalCount:      req.TotalCount,
		RpmLimit:        req.RpmLimit,
		Status:          db.DonationInactive,
		Note:            req.Note,
	}

	created, err := g.Store.CreateDonation(d, req.DifyAPIKey)
	if err != nil {
		if db.IsUniqueViolation(err) {
			g.writeError(w, http.StatusConflict, "conflict", "donation conflicts with an existing record")
			return
		}
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// beta.2: if the (service, model) pair has no pricing, warn admin.
	pricingWarning := ""
	pricing, pErr := g.Store.GetPricing(created.Service, created.Model)
	if pErr == nil && pricing == nil {
		pricingWarning = "该模型尚未设定价格，捐赠已创建但自动设为未激活状态。请先在定价表中添加该组合后再激活。"
	}

	// Decrypt only for the post-commit Dify compatibility probe. The key is
	// never included in the HTTP response.
	keyPlain, decErr := g.Store.Decrypt(created.DifyAPIKeyEnc)
	if decErr != nil {
		log.Printf("[ERROR] decrypt donation key for creation response: %v", decErr)
		keyPlain = "(decrypt error)"
	}

	validation := g.validateDonationApp(r.Context(), operator.ID, created.Service, created.DifyBaseURL, keyPlain)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":         true,
		"donation":   donationJSON(created),
		"validation": validation,
	}
	if pricingWarning != "" {
		resp["warning"] = pricingWarning
	}
	json.NewEncoder(w).Encode(resp)
}

// GET /api/admin/donations
func (g *Gateway) handleListDonations(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveListDonations(w, r)
}

// serveListDonations lists all donations; shared by the admin and the
// level-5 charity co-admin endpoints.
func (g *Gateway) serveListDonations(w http.ResponseWriter, r *http.Request) {

	// Keep the persisted status in sync even if the background sweep has not
	// reached a deadline yet (or the process was just started). Routing also
	// checks deadline in SQL, but the admin table must show expired rather than
	// leaving an overdue row as active/inactive.
	if _, err := g.Store.ExpireOverdueDonations(time.Now().Unix()); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	list, err := g.Store.ListDonations()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Compute is_dup_key: count SHA-256 hashes across all donations that have a key.
	shaCounts := make(map[string]int)
	for _, d := range list {
		if d.DifyAPIKeySHA256 != "" {
			shaCounts[d.DifyAPIKeySHA256]++
		}
	}

	// Batch-load review notes from original applications.
	donIDs := make([]int64, len(list))
	for i, d := range list {
		donIDs[i] = d.ID
	}
	reviewNotes, err := g.Store.GetReviewNotesByDonationIDs(donIDs)
	if err != nil {
		// Non-fatal: proceed without review notes.
		log.Printf("[WARN] load review notes: %v", err)
		reviewNotes = nil
	}

	out := make([]map[string]interface{}, 0, len(list))
	for _, d := range list {
		j := g.enrichDonationJSON(d)
		j["has_review_record"] = false
		if d.DifyAPIKeySHA256 != "" && shaCounts[d.DifyAPIKeySHA256] >= 2 {
			j["is_dup_key"] = true
		}
		if rn, ok := reviewNotes[d.ID]; ok {
			j["has_review_record"] = true
			j["review_note"] = rn
		}
		out = append(out, j)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"donations": out})
}

// POST /api/admin/donations/{id}/status
func (g *Gateway) handleDonationStatus(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveDonationStatus(w, r)
}

// serveDonationStatus toggles a donation between active/inactive; shared by
// the admin and the level-5 charity co-admin endpoints.
func (g *Gateway) serveDonationStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid donation id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	d, err := g.Store.GetDonation(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "donation not found")
		return
	}

	switch req.Status {
	case db.DonationActive, db.DonationInactive:
		if d.Status == db.DonationExpired {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				"已失效的捐赠条目不可更改状态")
			return
		}
		// beta.2: activating a donation requires pricing to exist.
		if req.Status == db.DonationActive {
			pricing, pErr := g.Store.GetPricing(d.Service, d.Model)
			if pErr != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", pErr.Error())
				return
			}
			if pricing == nil {
				g.writeError(w, http.StatusBadRequest, "invalid_request",
					"该模型尚未设定价格，请先在定价表中添加该 (service, model) 组合")
				return
			}
		}
	case db.DonationExpired:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"不能手动将捐赠条目设为失效")
		return
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "状态值必须是 'active' 或 'inactive'", "Status must be 'active' or 'inactive'"))
		return
	}

	if err := g.Store.SetDonationStatus(id, req.Status); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// DELETE /api/admin/donations/{id}
func (g *Gateway) handleDeleteDonation(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveDeleteDonation(w, r)
}

// serveDeleteDonation deletes one donation; shared by the admin and the
// level-5 charity co-admin endpoints.
func (g *Gateway) serveDeleteDonation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid donation id")
		return
	}

	d, err := g.Store.GetDonation(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if d == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "donation not found")
		return
	}

	if err := g.Store.DeleteDonation(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// PATCH /api/admin/donations/{id}
func (g *Gateway) handlePatchDonation(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.servePatchDonation(w, r, admin)
}

// servePatchDonation partially updates a donation; shared by the admin and
// the level-5 charity co-admin endpoints (operator drives the App probe).
func (g *Gateway) servePatchDonation(w http.ResponseWriter, r *http.Request, operator *db.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid donation id")
		return
	}

	var req struct {
		Service     *string `json:"service"`
		Model       *string `json:"model"`
		DifyBaseURL *string `json:"dify_base_url"`
		DifyAPIKey  *string `json:"dify_api_key"`
		RpmLimit    *int    `json:"rpm_limit"`
		Deadline    *int64  `json:"deadline"`
		TotalCount  *int    `json:"total_count"`
		Note        *string `json:"note"`
		ReviewNote  *string `json:"review_note"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	patch := db.DonationPatch{
		Service: req.Service, Model: req.Model, DifyBaseURL: req.DifyBaseURL,
		DifyAPIKey: req.DifyAPIKey, RpmLimit: req.RpmLimit, Deadline: req.Deadline,
		TotalCount: req.TotalCount, Note: req.Note, ReviewNote: req.ReviewNote, Status: req.Status,
	}
	if patch.Service != nil {
		normalized, inputErr := normalizeDonationService(*patch.Service)
		if inputErr != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
			return
		}
		patch.Service = &normalized
	}
	if patch.Model != nil {
		normalized, inputErr := normalizeDonationModel(*patch.Model)
		if inputErr != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
			return
		}
		patch.Model = &normalized
	}
	if patch.DifyBaseURL != nil {
		normalized, baseErr := g.difyPolicy.ValidateBaseURL(*patch.DifyBaseURL)
		if baseErr != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				t(g.resolveLang(r), "dify_base_url 不符合出站安全策略", "dify_base_url is not allowed by the egress policy")+": "+baseErr.Error())
			return
		}
		patch.DifyBaseURL = &normalized
	}
	if inputErr := validateDonationAPIKey(patch.DifyAPIKey, false); inputErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
		return
	}
	if patch.RpmLimit != nil && *patch.RpmLimit <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "rpm_limit must be positive")
		return
	}
	if patch.Deadline != nil && *patch.Deadline <= time.Now().Unix() {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "截止时间必须是将来的 Unix 时间戳", "Deadline must be a future Unix timestamp"))
		return
	}
	if patch.TotalCount != nil && *patch.TotalCount <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "total_count must be positive")
		return
	}
	if patch.Status != nil && *patch.Status != db.DonationActive && *patch.Status != db.DonationInactive {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "状态值必须是 'active' 或 'inactive'", "Status must be 'active' or 'inactive'"))
		return
	}
	if patch.Note != nil {
		note := strings.TrimSpace(*patch.Note)
		patch.Note = &note
	}
	if patch.ReviewNote != nil {
		note := strings.TrimSpace(*patch.ReviewNote)
		patch.ReviewNote = &note
	}

	result, err := g.Store.PatchDonation(id, patch)
	if err != nil {
		var patchErr *db.DonationPatchError
		switch {
		case errors.As(err, &patchErr) && patchErr.Kind == db.DonationPatchNotFound:
			g.writeError(w, http.StatusNotFound, "not_found", "donation not found")
		case errors.As(err, &patchErr) && patchErr.Kind == db.DonationPatchExpired:
			g.writeError(w, http.StatusBadRequest, "invalid_request", "已失效的捐赠条目不可修改")
		case errors.As(err, &patchErr) && patchErr.Kind == db.DonationPatchReviewRecordAbsent:
			g.writeError(w, http.StatusBadRequest, "invalid_request", "该捐赠没有关联的申请记录，不能修改审核备注")
		case errors.As(err, &patchErr) && patchErr.Kind == db.DonationPatchPricingAbsent:
			g.writeError(w, http.StatusBadRequest, "invalid_request", "激活状态要求对应模型存在且启用定价")
		case errors.As(err, &patchErr) && patchErr.Kind == db.DonationPatchInvalid:
			g.writeError(w, http.StatusBadRequest, "invalid_request", patchErr.Error())
		case db.IsUniqueViolation(err):
			g.writeError(w, http.StatusConflict, "conflict", "donation update conflicts with an existing record")
		default:
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}

	// Dify validation runs only after the transaction commits.
	validation := map[string]interface{}{"compatible": true, "message": "无校验需求"}
	if patch.Service != nil || patch.DifyBaseURL != nil || patch.DifyAPIKey != nil {
		keyPlain, decErr := g.Store.Decrypt(result.Donation.DifyAPIKeyEnc)
		if decErr != nil {
			validation = map[string]interface{}{
				"compatible": false,
				"message":    fmt.Sprintf("密钥解密失败: %v", decErr),
			}
		} else {
			validation = g.validateDonationApp(r.Context(), operator.ID, result.Donation.Service, result.Donation.DifyBaseURL, keyPlain)
		}
	}
	donation := g.enrichDonationJSON(result.Donation)
	donation["has_review_record"] = result.HasReviewRecord
	if result.HasReviewRecord {
		donation["review_note"] = result.ReviewNote
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"donation":   donation,
		"validation": validation,
	})
}

// donationJSON builds the API representation of a Donation without exposing
// its Dify API key. isDupKey is set by list callers after computing duplicates.
func donationJSON(d *db.Donation) map[string]interface{} {
	out := map[string]interface{}{
		"id":                   d.ID,
		"service":              d.Service,
		"model":                d.Model,
		"dify_base_url":        d.DifyBaseURL,
		"has_key":              d.DifyAPIKeyEnc != "",
		"is_dup_key":           false,
		"source_user_id":       nil,
		"source_discord_id":    d.SourceDiscordID,
		"source_username":      d.SourceUsername,
		"source_text":          d.SourceText,
		"deadline":             d.Deadline,
		"total_count":          d.TotalCount,
		"remaining_count":      d.RemainingCount,
		"success_count":        d.SuccessCount,
		"failure_count":        d.FailureCount,
		"consecutive_failures": d.ConsecutiveFailures,
		"rpm_limit":            d.RpmLimit,
		"status":               d.Status,
		"note":                 d.Note,
		"created_at":           d.CreatedAt,
		"updated_at":           d.UpdatedAt,
		"source_display":       "", // filled by caller with resolveSourceDisplay in list handler
	}
	if d.SourceUserID.Valid {
		out["source_user_id"] = d.SourceUserID.Int64
	}
	return out
}

// resolveSourceDisplay resolves the source display string with live DB access.
func (g *Gateway) resolveSourceDisplay(d *db.Donation) string {
	// 1. source_user_id live -> username
	if d.SourceUserID.Valid {
		u, err := g.Store.GetUserByID(d.SourceUserID.Int64)
		if err == nil && u != nil {
			return u.Username
		}
		// User deleted — try discord_id rematch
		if d.SourceDiscordID != "" {
			u2, err := g.Store.GetUserByDiscordID(d.SourceDiscordID)
			if err == nil && u2 != nil {
				return u2.Username
			}
		}
		// Deleted user with no rematch
		if d.SourceUsername != "" {
			return d.SourceUsername + "（已注销）"
		}
	}

	// 2. discord_id rematch without user_id
	if d.SourceDiscordID != "" {
		u, err := g.Store.GetUserByDiscordID(d.SourceDiscordID)
		if err == nil && u != nil {
			return u.Username
		}
		if d.SourceUsername != "" {
			return d.SourceUsername + "（已注销）"
		}
	}

	// 3. source_text fallback
	if d.SourceText != "" {
		return d.SourceText
	}

	return "（未知来源）"
}

// --- User charity API ---

// GET /api/me/charity
func (g *Gateway) handleGetCharity(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	// beta.2: include pricing info for models with enabled pricing + active donations.
	pricingList := []map[string]interface{}{}
	pricings, pErr := g.Store.ListEnabledPricing()
	if pErr == nil {
		for _, p := range pricings {
			has, _ := g.Store.HasDonationsForPair(p.Service, p.Model)
			if has {
				// available = at least one routable donation right now
				// (status active + deadline in the future + remaining > 0).
				// HasDonationsForPair alone counts any status, so a model whose
				// donations are all expired/inactive would otherwise look usable.
				routable, _ := g.Store.ListRoutableDonations(p.Service, p.Model)
				pricingList = append(pricingList, map[string]interface{}{
					"service":   p.Service,
					"model":     p.Model,
					"price":     p.Price,
					"available": len(routable) > 0,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"charity_enabled": u.CharityEnabled,
		"pricing":         pricingList,
	})
}

// PUT /api/me/charity
func (g *Gateway) handlePutCharity(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	var req struct {
		Enabled   bool `json:"enabled"`
		Confirmed bool `json:"confirmed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	// false -> true requires confirmed:true
	if req.Enabled && !req.Confirmed {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"开启公益资源需要二次确认")
		return
	}

	if err := g.Store.SetUserCharityEnabled(u.ID, req.Enabled); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- Charity streaming/blocking handlers ---

// charityStreaming handles streaming charity calls with donation accounting.
func (g *Gateway) charityStreaming(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, lang, service string, startedAt time.Time, donation *db.Donation, reservation *db.CharityReservation, ctx context.Context) {
	wfReq.ResponseMode = "streaming"
	events, errCh := client.StreamWorkflowContext(ctx, wfReq)

	// Wait for first event or error
	var firstEvt *dify.StreamEvent
	select {
	case evt, ok := <-events:
		if ok {
			firstEvt = &evt
		}
	case err := <-errCh:
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
				"client disconnected before first upstream event", donation.ID, reservation.Price, "")
			return
		}
		if err != nil {
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
				difyErrorStatus(err), err.Error(), donation.ID, reservation.Price, "")
			g.writeDifyError(w, err, lang)
			return
		}
	case <-ctx.Done():
		g.charityCommitAccounting(reservation)
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
			ctx.Err().Error(), donation.ID, reservation.Price, "")
		return
	}
	if firstEvt == nil {
		select {
		case err := <-errCh:
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				detail := "client disconnected before first upstream event"
				if ctx.Err() != nil {
					detail = ctx.Err().Error()
				}
				g.charityCommitAccounting(reservation)
				g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
					detail, donation.ID, reservation.Price, "")
				return
			}
			if err != nil {
				g.charityCommitAccounting(reservation)
				g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
					difyErrorStatus(err), err.Error(), donation.ID, reservation.Price, "")
				g.writeDifyError(w, err, lang)
				return
			}
		default:
		}
		if ctx.Err() != nil {
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
				ctx.Err().Error(), donation.ID, reservation.Price, "")
			return
		}
		// HTTP 200 with no SSE event is an upstream/proxy anomaly.  The
		// response headers are not committed yet, so return a normal JSON
		// gateway error rather than a successful empty stream.  The
		// reservation was already dispatched, therefore settle it
		// conservatively; this path must not count class B or class A.
		g.charityCommitAccounting(reservation)
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_empty_stream", http.StatusBadGateway,
			"upstream returned an empty stream", donation.ID, reservation.Price, "")
		g.writeError(w, http.StatusBadGateway, "upstream_error",
			t(lang, "上游 Dify 服务返回空响应，请稍后重试", "Upstream Dify returned an empty response. Please try again later."))
		return
	}

	if ctx.Err() != nil {
		g.charityCommitAccounting(reservation)
		if firstEvt != nil {
			// We observed an upstream event before the downstream canceled;
			// this is still a class-B success.
			g.limiter.record(rpmClassB, userID, time.Now())
		}
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
			ctx.Err().Error(), donation.ID, reservation.Price, "")
		return
	}

	// Stream started (Dify HTTP 200): this is a successful charity call per
	// the contract. Commit the reservation and record class-B RPM immediately;
	// a later truncation must not turn this consumed call into a donation
	// failure or trigger auto-inactivation.
	donationID := donation.ID
	g.charityCommitAccounting(reservation)
	g.limiter.record(rpmClassB, userID, time.Now())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "stream_unsupported",
			http.StatusInternalServerError, "response writer does not support streaming", donationID, reservation.Price, "")
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
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest, detail, donationID, reservation.Price, "")
		return
	}

	status, code, detail := "success", "", ""
	var streamErr error
	select {
	case err := <-errCh:
		streamErr = err
	default:
	}

	switch {
	case conv.Failed():
		// Dify returned HTTP 200 and the SSE relay started, but the
		// workflow ended with status=failed.  Per the sixth-round
		// ruling this is NOT a donation failure (the endpoint was
		// reachable — this is a logical workflow error surfaced to
		// the admin alert center in S5).
		status, code = "error", "upstream_error"
		detail = conv.FailMessage()
		if streamErr != nil {
			log.Printf("[ERROR] dify charity stream (user %d): %s", userID, boundedProcessError(streamErr))
			if detail == "" {
				detail = streamErr.Error()
			}
		}
		// The reservation was already committed when the stream started;
		// workflow failure does not indicate Dify-endpoint unavailability.
	case streamErr != nil:
		// The stream had already started, so the reservation was committed and
		// class B was recorded above. A transport truncation still fails the
		// user's transfer, but it is not a donation-endpoint failure: do not
		// refund, increment failure_count, or auto-inactivate the donation.
		log.Printf("[ERROR] dify charity stream (user %d): %s", userID, boundedProcessError(streamErr))
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] "+sanitizePublicUpstreamError(streamErr, streamErr.Error(), lang)))
		flusher.Flush()
		status, code = "error", "upstream_error"
		detail = streamErr.Error()
	case !conv.Done():
		// The upstream closed cleanly before workflow_finished.  Partial
		// content may already have been relayed, so surface an in-stream
		// error and never synthesize a stop chunk or [DONE].  The call was
		// already dispatched and class B was recorded above; only class A
		// is withheld for the incomplete transfer.
		log.Printf("[ERROR] dify charity stream (user %d): stream ended before workflow_finished", userID)
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] upstream stream ended unexpectedly"))
		flusher.Flush()
		status, code = "error", "upstream_stream_cut"
		detail = "stream ended before workflow_finished"
	default:
		for _, msg := range conv.Finalize() {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		g.limiter.record(rpmClassA, userID, time.Now())
	}

	g.logRequestDonation(userID, modelName, service, startedAt, status, code, http.StatusOK, detail, donationID, reservation.Price, "")
}

// charityBlocking handles blocking charity calls with donation accounting.
func (g *Gateway) charityBlocking(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, lang, service string, startedAt time.Time, donation *db.Donation, reservation *db.CharityReservation, ctx context.Context) {
	wfReq.ResponseMode = "blocking"
	text, err := client.BlockingWorkflowContext(ctx, wfReq)
	donationID := donation.ID

	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			detail := err.Error()
			if ctx.Err() != nil {
				detail = ctx.Err().Error()
			}
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled",
				statusClientClosedRequest, detail, donationID, reservation.Price, "")
			return
		}
		var de *dify.DifyError
		if errors.As(err, &de) && de.Status == http.StatusOK {
			// 200-but-failed: success per §1.2, but admin alert
			g.limiter.record(rpmClassB, userID, time.Now())
			g.charityCommitAccounting(reservation)

			// Log first so the alert can link to this request log.
			logID := g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error", http.StatusOK, de.Error(), donationID, reservation.Price, "")
			g.maybeRecordBlockingFailedAlert(userID, modelName, service, de, &donationID, logID)
			g.writeDifyError(w, err, lang)
			return
		}
		// Transport-level truncation (Cloudflare 100s timeout, connection
		// reset, etc.): Dify App has consumed its quota even though the
		// response was cut short.  Charge the user normally but surface a
		// clear timeout diagnosis so they know the request was counted.
		if dify.IsTimeoutError(err) {
			g.limiter.record(rpmClassB, userID, time.Now())
			g.charityCommitAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_timeout", http.StatusGatewayTimeout, err.Error(), donationID, reservation.Price, "")
			g.writeError(w, http.StatusGatewayTimeout, "upstream_timeout",
				t(lang, "上游 Dify 服务响应超时：请求可能因 Cloudflare 100 秒限制被截断。建议使用流式传输（stream: true）或拆分任务后重试。", "Upstream Dify service timeout: the request may have been truncated by Cloudflare's 100-second limit. Consider using streaming (stream: true) or splitting the task."))
			return
		}
		// The workflow request was already dispatched. Even a transport or HTTP
		// failure before a usable response is uncertain: Dify may have consumed
		// the donor call, so settle conservatively and do not penalize the
		// donation endpoint.
		g.charityCommitAccounting(reservation)
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
			difyErrorStatus(err), err.Error(), donationID, reservation.Price, "")
		g.writeDifyError(w, err, lang)
		return
	}

	// Success
	g.limiter.record(rpmClassB, userID, time.Now())
	g.charityCommitAccounting(reservation)

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
	g.limiter.record(rpmClassA, userID, time.Now())
	g.logRequestDonation(userID, modelName, service, startedAt, "success", "", http.StatusOK, "", donationID, reservation.Price, "")
}

// charityCommitAccounting commits a durable dispatched reservation. The
// donation use and consumer debit were already claimed atomically before
// dispatch. Both confirmed consumption and uncertain upstream outcomes use
// this conservative settlement.
func (g *Gateway) charityCommitAccounting(reservation *db.CharityReservation) {
	if err := g.settlement.settleNow(reservation.ID); err != nil {
		log.Printf("[ERROR] commit charity reservation %s: %v", reservation.ID, err)
		g.settlement.wake(reservation.ID)
	}
}

func (g *Gateway) releaseCharitySetup(reservation *db.CharityReservation) {
	if err := g.settlement.settleNow(reservation.ID); err != nil {
		log.Printf("[ERROR] release charity setup reservation %s: %v", reservation.ID, err)
		g.settlement.wake(reservation.ID)
	}
}

// maybeRecordBlockingFailedAlert records an admin alert for a blocking call
// that returned HTTP 200 but workflow status==failed. The alert is linked to
// the request log row (when one was written) so the alert center can jump to
// the affected request; bound alerts are purged with the log's 30-day
// retention.
func (g *Gateway) maybeRecordBlockingFailedAlert(userID int64, modelName, service string, de *dify.DifyError, donationID *int64, requestLogID int64) {
	msg := fmt.Sprintf("阻塞调用返回 HTTP 200 但状态为失败：服务 %s，模型 %s，用户 %d，原始错误：%s",
		service, modelName, userID, de.Error())
	alert := &db.AdminAlert{
		Type:       db.AlertBlockingFailed200,
		Message:    msg,
		DonationID: donationID,
	}
	if requestLogID > 0 {
		alert.RequestLogID = &requestLogID
	}
	if err := g.Store.AddAdminAlert(alert); err != nil {
		log.Printf("[ERROR] write blocking failed 200 alert: %v", err)
	}
}

// logRequestDonation is like logRequest but includes a donation_id for charity calls.
func (g *Gateway) logRequestDonation(userID int64, model, service string, startedAt time.Time, status, errorCode string, httpStatus int, detail string, donationID int64, creditsConsumed int, antiAbuseInfo string) int64 {
	if maxChars := g.Config.LogDetailMaxChars; maxChars > 0 && maxChars < diagnostic.MaxBytes {
		detail = diagnostic.BoundTo(detail, maxChars)
	} else {
		detail = diagnostic.Bound(detail)
	}
	id, err := g.Store.AddRequestLogFull(userID, model, service, startedAt, time.Now(), status, errorCode, httpStatus, detail, donationID, creditsConsumed, antiAbuseInfo)
	if err != nil {
		log.Printf("[WARN] write request log: %v", err)
		return 0
	}
	return id
}

// resolveDonationSourceDisplay resolves source_display for a donation and
// sets it on the returned JSON map.
func (g *Gateway) enrichDonationJSON(d *db.Donation) map[string]interface{} {
	j := donationJSON(d)
	j["source_display"] = g.resolveSourceDisplay(d)
	return j
}

// --- User self-service donation application ---

// POST /api/me/donations — user submits a donation application.
func (g *Gateway) handleCreateDonationApp(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	// Gate: donation_enabled must be true.
	if g.Store.GetSettingString(db.SettingDonationEnabled, "") != "true" {
		g.writeError(w, http.StatusForbidden, "donation_disabled",
			"捐赠系统尚未被管理员启用")
		return
	}

	// The pending cap is enforced by the same SQLite statement that inserts
	// the application, avoiding a concurrent count-then-insert race.
	limit := g.Store.GetSettingIntAllowZero(db.SettingDonationReviewLimit, db.DefaultDonationReviewLimit)

	var req struct {
		Service     string `json:"service"`
		Model       string `json:"model"`
		DifyBaseURL string `json:"dify_base_url"`
		DifyAPIKey  string `json:"dify_api_key"`
		Deadline    int64  `json:"deadline"`
		TotalCount  int    `json:"total_count"`
		RpmLimit    int    `json:"rpm_limit"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	service, model, inputErr := normalizeDonationTarget(req.Service, req.Model)
	if inputErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
		return
	}
	req.Service, req.Model = service, model

	// The admin may disable a service for self-service donations
	// (anti-abuse tab switch); the dropdown is filtered client-side, this
	// check is defense in depth for direct API calls.
	if !g.Store.IsServiceDonationSelectable(req.Service) {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("服务 %q 当前不接受自助捐赠申请", req.Service))
		return
	}

	// Validate dify_base_url.
	normalizedBaseURL, baseErr := g.difyPolicy.ValidateBaseURL(req.DifyBaseURL)
	if baseErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "dify_base_url 不符合出站安全策略", "dify_base_url is not allowed by the egress policy"))
		return
	}
	req.DifyBaseURL = normalizedBaseURL

	if err := validateDonationAPIKey(&req.DifyAPIKey, true); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Validate deadline.
	if req.Deadline <= time.Now().Unix() {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "截止时间必须是将来的 Unix 时间戳", "Deadline must be a future Unix timestamp"))
		return
	}

	// Validate total_count.
	if req.TotalCount <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "捐赠次数必须为正整数", "Donation count must be a positive integer"))
		return
	}

	// Template services (v1): the donation App must come from a prior
	// donation-purpose download; its mapping snapshot is attached to the
	// application and later copied into the approved donation row.
	var mappingSnapshot map[string]string
	if serviceHasTemplate(req.Service) {
		var mapErr error
		mappingSnapshot, mapErr = g.Store.LatestGenerationMapping(u.ID, req.Service, "donation")
		if mapErr != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", mapErr.Error())
			return
		}
		if len(mappingSnapshot) == 0 {
			g.writeError(w, http.StatusBadRequest, "template_not_downloaded",
				t(g.resolveLang(r), "请先下载捐赠版 App 模板（下载时选择「捐出」）再提交申请", "Download the donation App template first (choose \"Donate\" when downloading) before submitting"))
			return
		}
	}

	app, err := g.Store.CreateDonationApplicationWithLimit(u.ID, req.Service, req.Model, req.DifyBaseURL, req.DifyAPIKey, req.TotalCount, req.Deadline, req.RpmLimit, strings.TrimSpace(req.Note), limit)
	if err != nil {
		if errors.Is(err, db.ErrPendingApplicationLimit) {
			pending, countErr := g.Store.CountPendingByUser(u.ID)
			if countErr != nil {
				pending = limit
			}
			g.writeError(w, http.StatusBadRequest, "too_many_pending",
				fmt.Sprintf("您已有 %d 条待审核申请（上限 %d），请等待审核完成后再提交", pending, limit))
			return
		}
		if db.IsUniqueViolation(err) {
			g.writeError(w, http.StatusConflict, "conflict", "donation application conflicts with an existing record")
			return
		}
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	g.recordConsoleActivity(u.ID)

	// Attach the template mapping snapshot (B'): the approved donation row
	// will carry it for routing and validation.
	if len(mappingSnapshot) > 0 {
		if raw, marshalErr := json.Marshal(mappingSnapshot); marshalErr == nil {
			if err := g.Store.SetApplicationMapping(app.ID, string(raw)); err != nil {
				log.Printf("[ERROR] attach donation mapping snapshot (app %d): %v", app.ID, err)
			}
		}
	}

	resp := map[string]interface{}{
		"ok":          true,
		"application": applicationJSON(app),
		"validation":  g.validatePublicDonationApp(r.Context(), u.ID, req.Service, req.DifyBaseURL, req.DifyAPIKey, g.resolveLang(r)),
	}
	if n := g.selfSiteNotice(r, req.DifyBaseURL); n != "" {
		resp["notice"] = n
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /api/me/donations — returns the user's own applications.
func (g *Gateway) handleListMyApplications(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	apps, err := g.Store.ListApplicationsByUser(u.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Pre-compute is_dup_key for all approved applications with linked donations.
	// Collect all linked donation IDs, batch-fetch, compute dup counts.
	type donationRef struct {
		appIdx int
		donID  int64
	}
	var refs []donationRef
	for i, a := range apps {
		if a.Status == db.AppStatusApproved && a.DonationID.Valid {
			refs = append(refs, donationRef{appIdx: i, donID: a.DonationID.Int64})
		}
	}

	dupSet := make(map[int64]bool) // donation ID -> is_dup_key
	if len(refs) > 0 {
		// Fetch all linked donations and compute SHA-256 dup counts.
		allDons, _ := g.Store.ListDonations()
		shaCounts := make(map[string]int)
		for _, d := range allDons {
			if d.DifyAPIKeySHA256 != "" {
				shaCounts[d.DifyAPIKeySHA256]++
			}
		}
		for _, d := range allDons {
			if d.DifyAPIKeySHA256 != "" && shaCounts[d.DifyAPIKeySHA256] >= 2 {
				dupSet[d.ID] = true
			}
		}
	}

	out := make([]map[string]interface{}, 0, len(apps))
	for _, a := range apps {
		aj := applicationJSON(a)
		// Enrich approved applications with linked donation info.
		if a.Status == db.AppStatusApproved && a.DonationID.Valid {
			d, err := g.Store.GetDonation(a.DonationID.Int64)
			if err == nil && d != nil {
				aj["donation_status"] = d.Status
				aj["donation_remaining"] = d.RemainingCount
				aj["donation_total"] = d.TotalCount
				aj["donation_deadline"] = d.Deadline
				aj["is_dup_key"] = dupSet[d.ID]
			}
		}
		out = append(out, aj)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"applications": out})
}

// applicationJSON builds the API representation of a DonationApplication.
func applicationJSON(a *db.DonationApplication) map[string]interface{} {
	out := map[string]interface{}{
		"id":            a.ID,
		"user_id":       a.UserID,
		"service":       a.Service,
		"model":         a.Model,
		"dify_base_url": a.DifyBaseURL,
		"has_key":       a.DifyAPIKeyEnc != "",
		"total_count":   a.TotalCount,
		"deadline":      a.Deadline,
		"rpm_limit":     a.RpmLimit,
		"note":          a.Note,
		"status":        a.Status,
		"review_note":   a.ReviewNote,
		"created_at":    a.CreatedAt,
	}
	if a.ReviewerID.Valid {
		out["reviewer_id"] = a.ReviewerID.Int64
	}
	if a.DonationID.Valid {
		out["donation_id"] = a.DonationID.Int64
	}
	if a.Username != "" {
		out["username"] = a.Username
		out["discord_id"] = a.DiscordID
	}
	return out
}

// --- Admin application review endpoints ---

// GET /api/admin/donations/pending — list pending applications.
func (g *Gateway) handleListPendingApplications(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveListPendingApplications(w, r)
}

// serveListPendingApplications lists pending donation applications; shared
// by the admin and the level-4 co-admin review endpoints.
func (g *Gateway) serveListPendingApplications(w http.ResponseWriter, r *http.Request) {

	apps, err := g.Store.ListPendingApplications()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out := make([]map[string]interface{}, 0, len(apps))
	for _, a := range apps {
		aj := applicationJSON(a)
		out = append(out, aj)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"applications": out})
}

// GET /api/admin/donations/applications — list all applications with optional filters.
func (g *Gateway) handleAdminListApplications(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	q := r.URL.Query()
	status := q.Get("status")
	userID, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)
	service := q.Get("service")
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	until, _ := strconv.ParseInt(q.Get("until"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	apps, total, err := g.Store.ListApplicationsFiltered(status, userID, service, since, until, limit, offset)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out := make([]map[string]interface{}, 0, len(apps))
	for _, a := range apps {
		out = append(out, applicationJSON(a))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"applications": out,
		"total":        total,
	})
}

// POST /api/admin/donations/{id}/approve — approve an application.
func (g *Gateway) handleApproveApplication(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveApproveApplication(w, r, admin)
}

// serveApproveApplication approves one pending application. operator is
// recorded as reviewer_id (administrator or level-4 co-admin share the
// users table, so the audit trail stays distinguishable).
func (g *Gateway) serveApproveApplication(w http.ResponseWriter, r *http.Request, operator *db.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid application id")
		return
	}

	var req struct {
		Service     *string `json:"service"`
		Model       *string `json:"model"`
		DifyBaseURL *string `json:"dify_base_url"`
		DifyAPIKey  *string `json:"dify_api_key"`
		TotalCount  *int    `json:"total_count"`
		Deadline    *int64  `json:"deadline"`
		RpmLimit    *int    `json:"rpm_limit"`
		ReviewNote  string  `json:"review_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	existingApp, err := g.Store.GetApplication(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if existingApp == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	effectiveService, effectiveModel := existingApp.Service, existingApp.Model
	if req.Service != nil {
		effectiveService = *req.Service
	}
	if req.Model != nil {
		effectiveModel = *req.Model
	}
	effectiveService, effectiveModel, inputErr := normalizeDonationTarget(effectiveService, effectiveModel)
	if inputErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", inputErr.Error())
		return
	}
	if err := validateDonationAPIKey(req.DifyAPIKey, false); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	effectiveDeadline := existingApp.Deadline
	if req.Deadline != nil {
		effectiveDeadline = *req.Deadline
	}
	if effectiveDeadline <= time.Now().Unix() {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "application deadline has expired")
		return
	}
	effectiveBaseURL := existingApp.DifyBaseURL
	if req.DifyBaseURL != nil {
		effectiveBaseURL = *req.DifyBaseURL
	}
	normalized, baseErr := g.difyPolicy.ValidateBaseURL(effectiveBaseURL)
	if baseErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "dify_base_url 不符合出站安全策略: "+baseErr.Error())
		return
	}
	if req.TotalCount != nil && *req.TotalCount <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "total_count must be positive")
		return
	}
	if req.RpmLimit != nil && *req.RpmLimit <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "rpm_limit must be positive")
		return
	}

	modified := &db.ApproveApplicationFields{
		DifyBaseURL: normalized,
	}
	if req.Service != nil {
		modified.Service = effectiveService
	}
	if req.Model != nil {
		modified.Model = effectiveModel
	}
	if req.DifyAPIKey != nil {
		modified.DifyAPIKey = strings.TrimSpace(*req.DifyAPIKey)
	}
	if req.TotalCount != nil {
		modified.TotalCount = *req.TotalCount
	}
	if req.Deadline != nil {
		modified.Deadline = *req.Deadline
	}
	if req.RpmLimit != nil {
		modified.RpmLimit = *req.RpmLimit
	}

	app, donation, err := g.Store.ApproveApplication(id, operator.ID, modified, strings.TrimSpace(req.ReviewNote))
	if err != nil {
		if db.IsUniqueViolation(err) {
			g.writeError(w, http.StatusConflict, "conflict", "approved donation conflicts with an existing record")
			return
		}
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// beta.2: if the (service, model) pair has no pricing, auto-set to inactive.
	pricingWarning := ""
	pricing, pErr := g.Store.GetPricing(donation.Service, donation.Model)
	if pErr == nil && pricing == nil {
		if sErr := g.Store.SetDonationStatus(donation.ID, db.DonationInactive); sErr != nil {
			log.Printf("[ERROR] auto-inactive donation %d (from approval): %v", donation.ID, sErr)
		} else {
			donation.Status = db.DonationInactive
			pricingWarning = "该模型尚未设定价格，捐赠已创建但自动设为未激活状态。请先在定价表中添加该组合后再激活。"
		}
	}

	resp := map[string]interface{}{
		"ok":          true,
		"application": applicationJSON(app),
		"donation":    donationJSON(donation),
	}
	if pricingWarning != "" {
		resp["warning"] = pricingWarning
	}
	// beta.2: validate Dify App parameters.
	keyPlain, decErr := g.Store.Decrypt(donation.DifyAPIKeyEnc)
	if decErr == nil {
		resp["validation"] = g.validateDonationApp(r.Context(), operator.ID, donation.Service, donation.DifyBaseURL, keyPlain)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/admin/donations/{id}/reject — reject an application.
func (g *Gateway) handleRejectApplication(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveRejectApplication(w, r, admin)
}

// serveRejectApplication rejects one pending application; operator is
// recorded as reviewer_id (same audit semantics as approval).
func (g *Gateway) serveRejectApplication(w http.ResponseWriter, r *http.Request, operator *db.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid application id")
		return
	}

	var req struct {
		ReviewNote string `json:"review_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	app, err := g.Store.RejectApplication(id, operator.ID, strings.TrimSpace(req.ReviewNote))
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"application": applicationJSON(app),
	})
}

// --- Batch operation helpers ---

// writeBatchDonationError writes a batch error response with failed_id.
func writeBatchDonationError(w http.ResponseWriter, msg string, id int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": false,
		"error": map[string]interface{}{
			"message": "[Dify2API] " + msg,
			"type":    "invalid_request",
			"code":    "invalid_request",
		},
		"failed_id": id,
	})
}

// writeBatchPairError writes a batch error response with failed_pair.
func writeBatchPairError(w http.ResponseWriter, msg string, service, model string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": false,
		"error": map[string]interface{}{
			"message": "[Dify2API] " + msg,
			"type":    "invalid_request",
			"code":    "invalid_request",
		},
		"failed_pair": map[string]string{"service": service, "model": model},
	})
}

// --- Batch donation application endpoints (7.1) ---

// POST /api/admin/donations/approve/batch — batch approve pending applications.
func (g *Gateway) handleBatchApproveApplications(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveBatchApproveApplications(w, r, admin)
}

// serveBatchApproveApplications atomically approves multiple pending
// applications; shared by the admin and the level-4 co-admin endpoints.
func (g *Gateway) serveBatchApproveApplications(w http.ResponseWriter, r *http.Request, operator *db.User) {

	var req struct {
		IDs        []int64 `json:"ids"`
		ReviewNote string  `json:"review_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.IDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "ids must be a non-empty array")
		return
	}

	// Atomic all-or-nothing: validate all first.
	for _, id := range req.IDs {
		app, err := g.Store.GetApplication(id)
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if app == nil {
			writeBatchDonationError(w, fmt.Sprintf("申请 %d 不存在", id), id)
			return
		}
		if app.Status != db.AppStatusPending {
			writeBatchDonationError(w,
				fmt.Sprintf("申请 %d 状态不是 pending（当前：%s）", id, app.Status), id)
			return
		}
		if app.Deadline <= time.Now().Unix() {
			writeBatchDonationError(w, fmt.Sprintf("申请 %d 的截止时间已过期", id), id)
			return
		}
		if _, baseErr := g.difyPolicy.ValidateBaseURL(app.DifyBaseURL); baseErr != nil {
			writeBatchDonationError(w, fmt.Sprintf("申请 %d 的 Dify 地址不符合出站安全策略", id), id)
			return
		}
	}

	if err := g.Store.ApproveApplications(req.IDs, operator.ID, strings.TrimSpace(req.ReviewNote)); err != nil {
		var stateErr *db.ApplicationReviewError
		if errors.As(err, &stateErr) {
			writeBatchDonationError(w, err.Error(), stateErr.ApplicationID)
			return
		}
		var deadlineErr *db.ApplicationDeadlineError
		if errors.As(err, &deadlineErr) {
			writeBatchDonationError(w, err.Error(), deadlineErr.ApplicationID)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", fmt.Sprintf("批量批准申请失败: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}

// POST /api/admin/donations/reject/batch — batch reject pending applications.
func (g *Gateway) handleBatchRejectApplications(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveBatchRejectApplications(w, r, admin)
}

// serveBatchRejectApplications atomically rejects multiple pending
// applications; shared by the admin and the level-4 co-admin endpoints.
func (g *Gateway) serveBatchRejectApplications(w http.ResponseWriter, r *http.Request, operator *db.User) {

	var req struct {
		IDs        []int64 `json:"ids"`
		ReviewNote string  `json:"review_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.IDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "ids must be a non-empty array")
		return
	}

	// Atomic all-or-nothing: validate all first.
	for _, id := range req.IDs {
		app, err := g.Store.GetApplication(id)
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if app == nil {
			writeBatchDonationError(w, fmt.Sprintf("申请 %d 不存在", id), id)
			return
		}
		if app.Status != db.AppStatusPending {
			writeBatchDonationError(w,
				fmt.Sprintf("申请 %d 状态不是 pending（当前：%s）", id, app.Status), id)
			return
		}
	}

	if err := g.Store.RejectApplications(req.IDs, operator.ID, strings.TrimSpace(req.ReviewNote)); err != nil {
		var stateErr *db.ApplicationReviewError
		if errors.As(err, &stateErr) {
			writeBatchDonationError(w, err.Error(), stateErr.ApplicationID)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", fmt.Sprintf("批量拒绝申请失败: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}

// --- Batch donation resource endpoints (7.2) ---

// POST /api/admin/donations/status/batch — batch set donation status.
func (g *Gateway) handleBatchDonationStatus(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveBatchDonationStatus(w, r)
}

// serveBatchDonationStatus atomically toggles donation status; shared by
// the admin and the level-5 charity co-admin endpoints.
func (g *Gateway) serveBatchDonationStatus(w http.ResponseWriter, r *http.Request) {

	var req struct {
		IDs    []int64 `json:"ids"`
		Status string  `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.IDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "ids must be a non-empty array")
		return
	}

	// Validate target status.
	switch req.Status {
	case db.DonationActive, db.DonationInactive:
		// OK
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "状态值必须是 'active' 或 'inactive'", "Status must be 'active' or 'inactive'"))
		return
	}

	if err := g.Store.BatchSetDonationStatus(req.IDs, req.Status); err != nil {
		var statusErr *db.DonationStatusError
		if errors.As(err, &statusErr) {
			writeBatchDonationError(w, statusErr.Error(), statusErr.DonationID)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}

// POST /api/admin/donations/delete/batch — batch delete donations.
func (g *Gateway) handleBatchDeleteDonations(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveBatchDeleteDonations(w, r)
}

// serveBatchDeleteDonations atomically deletes multiple donations; shared
// by the admin and the level-5 charity co-admin endpoints.
func (g *Gateway) serveBatchDeleteDonations(w http.ResponseWriter, r *http.Request) {

	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.IDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "ids must be a non-empty array")
		return
	}

	// Atomic all-or-nothing: validate all first.
	for _, id := range req.IDs {
		d, err := g.Store.GetDonation(id)
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if d == nil {
			writeBatchDonationError(w, fmt.Sprintf("捐赠条目 %d 不存在", id), id)
			return
		}
	}

	if err := g.Store.DeleteDonations(req.IDs); err != nil {
		var deleteErr *db.DonationDeleteError
		if errors.As(err, &deleteErr) {
			writeBatchDonationError(w, err.Error(), deleteErr.DonationID)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", fmt.Sprintf("批量删除捐赠条目失败: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}
