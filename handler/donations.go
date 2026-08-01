package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// --- Donation CRUD ---

// POST /api/admin/donations
func (g *Gateway) handleCreateDonation(w http.ResponseWriter, r *http.Request) {
	admin := g.requireAdmin(r)
	if admin == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

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

	// Validate service
	if !translator.IsSupportedService(req.Service) {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("不支持的服务 %q", req.Service))
		return
	}

	// Validate model: no brackets
	if strings.ContainsAny(req.Model, "[]") {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"模型名不得包含方括号")
		return
	}

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

	// Validate dify_api_key
	if req.DifyAPIKey == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"dify_api_key 为必填")
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
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// beta.2: if the (service, model) pair has no pricing, warn admin.
	pricingWarning := ""
	pricing, pErr := g.Store.GetPricing(created.Service, created.Model)
	if pErr == nil && pricing == nil {
		pricingWarning = "该模型尚未设定价格，捐赠已创建但自动设为未激活状态。请先在定价表中添加该组合后再激活。"
	}

	// Decrypt key for the creation response only
	keyPlain, decErr := g.Store.Decrypt(created.DifyAPIKeyEnc)
	if decErr != nil {
		log.Printf("[ERROR] decrypt donation key for creation response: %v", decErr)
		keyPlain = "(decrypt error)"
	}

	validation := g.validateDonationApp(r.Context(), admin.ID, created.Service, created.DifyBaseURL, keyPlain)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":         true,
		"donation":   donationJSON(created, &keyPlain),
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
		j := g.enrichDonationJSON(d, nil)
		if d.DifyAPIKeySHA256 != "" && shaCounts[d.DifyAPIKeySHA256] >= 2 {
			j["is_dup_key"] = true
		}
		if rn, ok := reviewNotes[d.ID]; ok {
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid donation id")
		return
	}

	var req struct {
		Service     string `json:"service"`
		Model       string `json:"model"`
		DifyBaseURL string `json:"dify_base_url"`
		DifyAPIKey  string `json:"dify_api_key"`
		RpmLimit    int    `json:"rpm_limit"`
		Deadline    int64  `json:"deadline"`
		TotalCount  int    `json:"total_count"`
		Note        string `json:"note"`
		ReviewNote  string `json:"review_note"`
		Status      string `json:"status"`
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
	if d.Status == db.DonationExpired {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"已失效的捐赠条目不可修改")
		return
	}

	// Build SET clause dynamically.
	now := time.Now().Unix()
	var sets []string
	var args []interface{}

	// Track fields that may need Dify validation.
	newBaseURL := d.DifyBaseURL
	newAPIKeyEnc := d.DifyAPIKeyEnc
	apiKeyChanged := false
	validateDify := false

	if req.Service != "" && req.Service != d.Service {
		if !translator.IsSupportedService(req.Service) {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("不支持的服务 %q", req.Service))
			return
		}
		sets = append(sets, "service=?")
		args = append(args, req.Service)
		validateDify = true
	}

	if req.Model != "" && req.Model != d.Model {
		if strings.ContainsAny(req.Model, "[]") {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				"模型名不得包含方括号")
			return
		}
		sets = append(sets, "model=?")
		args = append(args, req.Model)
	}

	if req.DifyBaseURL != "" {
		normalized, baseErr := g.difyPolicy.ValidateBaseURL(req.DifyBaseURL)
		if baseErr != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				t(g.resolveLang(r), "dify_base_url 不符合出站安全策略", "dify_base_url is not allowed by the egress policy")+": "+baseErr.Error())
			return
		}
		if normalized != d.DifyBaseURL {
			sets = append(sets, "dify_base_url=?")
			args = append(args, normalized)
			newBaseURL = normalized
			validateDify = true
		}
	}

	if req.DifyAPIKey != "" {
		enc, encErr := g.Store.Encrypt(req.DifyAPIKey)
		if encErr != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", encErr.Error())
			return
		}
		// Recompute SHA-256 for duplicate detection.
		sum := sha256.Sum256([]byte(req.DifyAPIKey))
		keySHA256 := hex.EncodeToString(sum[:])
		sets = append(sets, "dify_api_key_enc=?", "dify_api_key_sha256=?")
		args = append(args, enc, keySHA256)
		newAPIKeyEnc = enc
		apiKeyChanged = true
		validateDify = true
	}

	if req.RpmLimit > 0 && req.RpmLimit != d.RpmLimit {
		sets = append(sets, "rpm_limit=?")
		args = append(args, req.RpmLimit)
	}

	if req.Deadline > 0 && req.Deadline != d.Deadline {
		if req.Deadline <= time.Now().Unix() {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				t(g.resolveLang(r), "截止时间必须是将来的 Unix 时间戳", "Deadline must be a future Unix timestamp"))
			return
		}
		sets = append(sets, "deadline=?")
		args = append(args, req.Deadline)
	}

	if req.TotalCount > 0 && req.TotalCount != d.TotalCount {
		// Lowering total_count must never drive remaining_count below zero
		// (a negative value would be unroutable and confusing in the UI).
		sets = append(sets, "total_count=?, remaining_count=MAX(0, remaining_count + (? - ?))")
		args = append(args, req.TotalCount, req.TotalCount, d.TotalCount)
	}

	if req.Note != "" && req.Note != d.Note {
		sets = append(sets, "note=?")
		args = append(args, strings.TrimSpace(req.Note))
	}

	if req.Status != "" && req.Status != d.Status {
		switch req.Status {
		case db.DonationActive, db.DonationInactive:
			if d.Status == db.DonationExpired {
				g.writeError(w, http.StatusBadRequest, "invalid_request",
					"已失效的捐赠条目不可更改状态")
				return
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
		sets = append(sets, "status=?")
		args = append(args, req.Status)
		// Reset consecutive_failures on re-activation.
		if req.Status == db.DonationActive {
			sets = append(sets, "consecutive_failures=0")
		}
	}

	// Handle review_note separately (stored in donation_applications, not donations table).
	// Must be checked before the "no changes" guard to allow review_note-only updates.
	reviewNoteUpdated := false
	if req.ReviewNote != "" {
		if err := g.Store.UpdateDonationReviewNote(id, strings.TrimSpace(req.ReviewNote)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		reviewNoteUpdated = true
	}

	if len(sets) == 0 && !reviewNoteUpdated {
		// No changes requested.
		validation := map[string]interface{}{"compatible": true, "message": "无字段变更"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":         true,
			"donation":   g.enrichDonationJSON(d, nil),
			"validation": validation,
		})
		return
	}

	sets = append(sets, "updated_at=?")
	args = append(args, now)
	args = append(args, id)

	query := "UPDATE donations SET " + strings.Join(sets, ", ") + " WHERE id=?"
	if _, err := g.Store.Exec(query, args...); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Refresh from DB.
	updated, err := g.Store.GetDonation(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Dify validation (only when service/base_url/api_key changed).
	validation := map[string]interface{}{"compatible": true, "message": "无校验需求"}
	if validateDify {
		validKeyEnc := newAPIKeyEnc
		if !apiKeyChanged {
			validKeyEnc = d.DifyAPIKeyEnc
		}
		keyPlain, decErr := g.Store.Decrypt(validKeyEnc)
		if decErr != nil {
			validation = map[string]interface{}{
				"compatible": false,
				"message":    fmt.Sprintf("密钥解密失败: %v", decErr),
			}
		} else {
			validation = g.validateDonationApp(r.Context(), admin.ID, updated.Service, newBaseURL, keyPlain)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"donation":   g.enrichDonationJSON(updated, nil),
		"validation": validation,
	})
}

// donationJSON builds the API representation of a Donation.
// If keyPlain is non-nil, the Dify API key is included in plaintext
// (for creation response only). Otherwise, has_key is returned.
// isDupKey is set by the caller after computing duplicates across the list.
func donationJSON(d *db.Donation, keyPlain *string) map[string]interface{} {
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
	if keyPlain != nil {
		out["dify_api_key"] = *keyPlain
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
				pricingList = append(pricingList, map[string]interface{}{
					"service": p.Service,
					"model":   p.Model,
					"price":   p.Price,
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
func (g *Gateway) charityStreaming(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, service string, startedAt time.Time, donation *db.Donation, reservation *db.CharityReservation, ctx context.Context) {
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
			g.charitySuccessAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
				"client disconnected before first upstream event", donation.ID, reservation.Price, "")
			return
		}
		if err != nil {
			g.charityFailAccounting(userID, donation, reservation, err)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
				difyErrorStatus(err), err.Error(), donation.ID, 0, "")
			g.writeDifyError(w, err)
			return
		}
	case <-ctx.Done():
		g.charitySuccessAccounting(reservation)
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
				g.charitySuccessAccounting(reservation)
				g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled", statusClientClosedRequest,
					detail, donation.ID, reservation.Price, "")
				return
			}
			if err != nil {
				g.charityFailAccounting(userID, donation, reservation, err)
				g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
					difyErrorStatus(err), err.Error(), donation.ID, 0, "")
				g.writeDifyError(w, err)
				return
			}
		default:
		}
	}

	if ctx.Err() != nil {
		g.charitySuccessAccounting(reservation)
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
	g.charitySuccessAccounting(reservation)
	g.limiter.record(rpmClassB, userID, time.Now())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "stream_unsupported",
			http.StatusInternalServerError, "response writer does not support streaming", donationID, reservation.Price, "")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	conv := translator.NewStreamConverter(modelName)

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
		// the admin alert centre in S5).
		status, code = "error", "upstream_error"
		detail = conv.FailMessage()
		if streamErr != nil {
			log.Printf("[ERROR] dify charity stream (user %d): %v", userID, streamErr)
			if detail == "" {
				detail = streamErr.Error()
			}
		}
		// No charityFailAccounting here — the success was already
		// recorded when the stream started, and workflow failure
		// does not indicate Dify-endpoint unavailability.
	case streamErr != nil:
		// The stream had already started, so the reservation was committed and
		// class B was recorded above. A transport truncation still fails the
		// user's transfer, but it is not a donation-endpoint failure: do not
		// refund, increment failure_count, or auto-inactivate the donation.
		log.Printf("[ERROR] dify charity stream (user %d): %v", userID, streamErr)
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] "+streamErr.Error()))
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
			g.charitySuccessAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "client_canceled",
				statusClientClosedRequest, detail, donationID, reservation.Price, "")
			return
		}
		var de *dify.DifyError
		if errors.As(err, &de) && de.Status == http.StatusOK {
			// 200-but-failed: success per §1.2, but admin alert
			g.limiter.record(rpmClassB, userID, time.Now())
			g.charitySuccessAccounting(reservation)

			// Log first so the alert can link to this request log.
			logID := g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error", http.StatusOK, de.Error(), donationID, reservation.Price, "")
			g.maybeRecordBlockingFailedAlert(userID, modelName, service, de, &donationID, logID)
			g.writeDifyError(w, err)
			return
		}
		// Transport-level truncation (Cloudflare 100s timeout, connection
		// reset, etc.): Dify App has consumed its quota even though the
		// response was cut short.  Charge the user normally but surface a
		// clear timeout diagnosis so they know the request was counted.
		if dify.IsTimeoutError(err) {
			g.limiter.record(rpmClassB, userID, time.Now())
			g.charitySuccessAccounting(reservation)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_timeout", http.StatusGatewayTimeout, err.Error(), donationID, reservation.Price, "")
			g.writeError(w, http.StatusGatewayTimeout, "upstream_timeout",
				t(lang, "上游 Dify 服务响应超时：请求可能因 Cloudflare 100 秒限制被截断。建议使用流式传输（stream: true）或拆分任务后重试。", "Upstream Dify service timeout: the request may have been truncated by Cloudflare's 100-second limit. Consider using streaming (stream: true) or splitting the task."))
			return
		}
		// Real upstream failure — donation failure. Keep the selected donation
		// ID on the request log so the admin view can resolve its source.
		g.charityFailAccounting(userID, donation, reservation, err)
		g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error",
			difyErrorStatus(err), err.Error(), donationID, 0, "")
		g.writeDifyError(w, err)
		return
	}

	// Success
	g.limiter.record(rpmClassB, userID, time.Now())
	g.charitySuccessAccounting(reservation)

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

// charitySuccessAccounting commits a durable reservation. The donation use
// and consumer debit were already claimed atomically before dispatch.
func (g *Gateway) charitySuccessAccounting(reservation *db.CharityReservation) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := g.Store.CommitCharityReservation(ctx, reservation.ID); err != nil {
		log.Printf("[ERROR] commit charity reservation %s: %v", reservation.ID, err)
	}
}

// charityFailAccounting refunds a pre-dispatch debit/use and records a Dify
// endpoint failure in the same transaction.
func (g *Gateway) charityFailAccounting(userID int64, donation *db.Donation, reservation *db.CharityReservation, err error) {
	log.Printf("[DONATION] donation %d failure (user %d, reservation %s): %v", donation.ID, userID, reservation.ID, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	consecutive, recErr := g.Store.ReleaseCharityReservation(ctx, reservation.ID, true)
	if recErr != nil {
		log.Printf("[ERROR] release charity reservation %s: %v", reservation.ID, recErr)
		return
	}
	g.maybeInactivateDonation(donation, consecutive)
}

func (g *Gateway) maybeInactivateDonation(donation *db.Donation, consecutive int) {
	limit := g.Store.GetSettingInt(db.SettingDonationFailLimit, db.DefaultDonationFailLimit)
	if consecutive < limit {
		return
	}
	if err := g.Store.SetDonationStatus(donation.ID, db.DonationInactive); err != nil {
		log.Printf("[ERROR] auto-inactivate donation %d after %d failures: %v", donation.ID, consecutive, err)
		return
	}
	log.Printf("[DONATION] donation %d auto-inactivated after %d consecutive failures", donation.ID, consecutive)
	if g.mailer != nil {
		g.mailer.DonationInactive(donation.Service, donation.Model, donation.ID, consecutive)
	}
}

func (g *Gateway) releaseCharitySetup(reservation *db.CharityReservation) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := g.Store.ReleaseCharityReservation(ctx, reservation.ID, false); err != nil {
		log.Printf("[ERROR] release charity setup reservation %s: %v", reservation.ID, err)
	}
}

// maybeRecordBlockingFailedAlert records an admin alert for a blocking call
// that returned HTTP 200 but workflow status==failed. The alert is linked to
// the request log row (when one was written) so the alert centre can jump to
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
	if len(detail) > g.Config.LogDetailMaxChars {
		detail = detail[:g.Config.LogDetailMaxChars] + "…"
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
func (g *Gateway) enrichDonationJSON(d *db.Donation, keyPlain *string) map[string]interface{} {
	j := donationJSON(d, keyPlain)
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

	// Check pending limit.
	limit := g.Store.GetSettingInt(db.SettingDonationReviewLimit, db.DefaultDonationReviewLimit)
	pending, err := g.Store.CountPendingByUser(u.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if pending >= limit {
		g.writeError(w, http.StatusBadRequest, "too_many_pending",
			fmt.Sprintf("您已有 %d 条待审核申请（上限 %d），请等待审核完成后再提交", pending, limit))
		return
	}

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

	// Validate service.
	if !translator.IsSupportedService(req.Service) {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("不支持的服务 %q", req.Service))
		return
	}

	// Validate model: no brackets.
	if strings.ContainsAny(req.Model, "[]") {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"模型名不得包含方括号")
		return
	}

	// Validate dify_base_url.
	normalizedBaseURL, baseErr := g.difyPolicy.ValidateBaseURL(req.DifyBaseURL)
	if baseErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "dify_base_url 不符合出站安全策略", "dify_base_url is not allowed by the egress policy")+": "+baseErr.Error())
		return
	}
	req.DifyBaseURL = normalizedBaseURL

	// Validate dify_api_key.
	if strings.TrimSpace(req.DifyAPIKey) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"dify_api_key 为必填")
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

	app, err := g.Store.CreateDonationApplication(u.ID, req.Service, req.Model, req.DifyBaseURL, req.DifyAPIKey, req.TotalCount, req.Deadline, req.RpmLimit, strings.TrimSpace(req.Note))
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	resp := map[string]interface{}{
		"ok":          true,
		"application": applicationJSON(app),
		"validation":  g.validateDonationApp(r.Context(), u.ID, req.Service, req.DifyBaseURL, req.DifyAPIKey),
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
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	adminUser := g.currentUser(r)

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid application id")
		return
	}

	var req struct {
		Service     string `json:"service"`
		Model       string `json:"model"`
		DifyBaseURL string `json:"dify_base_url"`
		DifyAPIKey  string `json:"dify_api_key"`
		TotalCount  int    `json:"total_count"`
		Deadline    int64  `json:"deadline"`
		RpmLimit    int    `json:"rpm_limit"`
		ReviewNote  string `json:"review_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	// Validate modified fields if provided.
	if req.Model != "" && strings.ContainsAny(req.Model, "[]") {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"模型名不得包含方括号")
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
	effectiveBaseURL := req.DifyBaseURL
	if effectiveBaseURL == "" {
		effectiveBaseURL = existingApp.DifyBaseURL
	}
	normalized, baseErr := g.difyPolicy.ValidateBaseURL(effectiveBaseURL)
	if baseErr != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "dify_base_url 不符合出站安全策略: "+baseErr.Error())
		return
	}
	req.DifyBaseURL = normalized

	modified := &db.ApproveApplicationFields{
		Service:     req.Service,
		Model:       req.Model,
		DifyBaseURL: req.DifyBaseURL,
		DifyAPIKey:  req.DifyAPIKey,
		TotalCount:  req.TotalCount,
		Deadline:    req.Deadline,
		RpmLimit:    req.RpmLimit,
	}

	app, donation, err := g.Store.ApproveApplication(id, adminUser.ID, modified, strings.TrimSpace(req.ReviewNote))
	if err != nil {
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
		"donation":    donationJSON(donation, nil),
	}
	if pricingWarning != "" {
		resp["warning"] = pricingWarning
	}
	// beta.2: validate Dify App parameters.
	keyPlain, decErr := g.Store.Decrypt(donation.DifyAPIKeyEnc)
	if decErr == nil {
		resp["validation"] = g.validateDonationApp(r.Context(), adminUser.ID, donation.Service, donation.DifyBaseURL, keyPlain)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// POST /api/admin/donations/{id}/reject — reject an application.
func (g *Gateway) handleRejectApplication(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	adminUser := g.currentUser(r)

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

	app, err := g.Store.RejectApplication(id, adminUser.ID, strings.TrimSpace(req.ReviewNote))
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
		"ok":        false,
		"error":     msg,
		"failed_id": id,
	})
}

// writeBatchPairError writes a batch error response with failed_pair.
func writeBatchPairError(w http.ResponseWriter, msg string, service, model string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          false,
		"error":       msg,
		"failed_pair": map[string]string{"service": service, "model": model},
	})
}

// --- Batch donation application endpoints (7.1) ---

// POST /api/admin/donations/approve/batch — batch approve pending applications.
func (g *Gateway) handleBatchApproveApplications(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	adminUser := g.currentUser(r)

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
		if _, baseErr := g.difyPolicy.ValidateBaseURL(app.DifyBaseURL); baseErr != nil {
			writeBatchDonationError(w, fmt.Sprintf("申请 %d 的 Dify 地址不符合出站安全策略", id), id)
			return
		}
	}

	// All passed: approve each.
	for _, id := range req.IDs {
		_, _, err := g.Store.ApproveApplication(id, adminUser.ID,
			&db.ApproveApplicationFields{}, strings.TrimSpace(req.ReviewNote))
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal",
				fmt.Sprintf("批准申请 %d 失败: %v", id, err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}

// POST /api/admin/donations/reject/batch — batch reject pending applications.
func (g *Gateway) handleBatchRejectApplications(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	adminUser := g.currentUser(r)

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

	// All passed: reject each.
	for _, id := range req.IDs {
		_, err := g.Store.RejectApplication(id, adminUser.ID, strings.TrimSpace(req.ReviewNote))
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal",
				fmt.Sprintf("拒绝申请 %d 失败: %v", id, err))
			return
		}
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
		if d.Status == db.DonationExpired {
			writeBatchDonationError(w,
				fmt.Sprintf("已失效的捐赠条目 %d 不可更改状态", id), id)
			return
		}
		// Switching to active: check pricing exists.
		if req.Status == db.DonationActive {
			pricing, pErr := g.Store.GetPricing(d.Service, d.Model)
			if pErr != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", pErr.Error())
				return
			}
			if pricing == nil {
				writeBatchDonationError(w,
					fmt.Sprintf("捐赠条目 %d 的模型 (%s, %s) 尚未设定价格，请先在定价表中添加该组合后再激活",
						id, d.Service, d.Model), id)
				return
			}
		}
	}

	// All passed: apply status.
	for _, id := range req.IDs {
		if err := g.Store.SetDonationStatus(id, req.Status); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal",
				fmt.Sprintf("设置捐赠条目 %d 状态失败: %v", id, err))
			return
		}
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

	// All passed: delete each.
	for _, id := range req.IDs {
		if err := g.Store.DeleteDonation(id); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal",
				fmt.Sprintf("删除捐赠条目 %d 失败: %v", id, err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.IDs),
	})
}
