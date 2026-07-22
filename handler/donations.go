package handler

import (
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

// pickWeightedDonation selects a donation from candidates using weighted
// random sampling. Weight_i = 1 / max(deadline_i - now, 60). Returns nil
// when candidates is empty.
func pickWeightedDonation(candidates []*db.Donation) *db.Donation {
	if len(candidates) == 0 {
		return nil
	}
	now := time.Now().Unix()
	type wd struct {
		d *db.Donation
		w float64
	}
	weights := make([]wd, 0, len(candidates))
	total := 0.0
	for _, d := range candidates {
		remaining := d.Deadline - now
		if remaining < 60 {
			remaining = 60
		}
		w := 1.0 / float64(remaining)
		weights = append(weights, wd{d: d, w: w})
		total += w
	}
	if total <= 0 {
		return candidates[0]
	}
	r := rand.Float64() * total
	for _, wd := range weights {
		r -= wd.w
		if r <= 0 {
			return wd.d
		}
	}
	return weights[len(weights)-1].d
}

// --- Donation CRUD ---

// POST /api/admin/donations
func (g *Gateway) handleCreateDonation(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
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
			"截止时间必须是将来的 Unix 时间戳")
		return
	}

	// Validate total_count
	if req.TotalCount <= 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"捐赠次数必须为正整数")
		return
	}

	// Validate dify_base_url
	req.DifyBaseURL = strings.TrimRight(strings.TrimSpace(req.DifyBaseURL), "/")
	if req.DifyBaseURL == "" || !(strings.HasPrefix(req.DifyBaseURL, "http://") || strings.HasPrefix(req.DifyBaseURL, "https://")) {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"dify_base_url 必须为合法的 http(s) URL")
		return
	}

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
		Status:          db.DonationActive,
		Note:            req.Note,
	}

	created, err := g.Store.CreateDonation(d, req.DifyAPIKey)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Decrypt key for the creation response only
	keyPlain, decErr := g.Store.Decrypt(created.DifyAPIKeyEnc)
	if decErr != nil {
		log.Printf("[ERROR] decrypt donation key for creation response: %v", decErr)
		keyPlain = "(decrypt error)"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"donation": donationJSON(created, &keyPlain),
	})
}

// GET /api/admin/donations
func (g *Gateway) handleListDonations(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	list, err := g.Store.ListDonations()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out := make([]map[string]interface{}, 0, len(list))
	for _, d := range list {
		out = append(out, donationJSON(d, nil))
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
	case db.DonationExpired:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"不能手动将捐赠条目设为失效")
		return
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"状态值必须是 'active' 或 'inactive'")
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

// donationJSON builds the API representation of a Donation.
// If keyPlain is non-nil, the Dify API key is included in plaintext
// (for creation response only). Otherwise, has_key is returned.
func donationJSON(d *db.Donation, keyPlain *string) map[string]interface{} {
	out := map[string]interface{}{
		"id":                   d.ID,
		"service":              d.Service,
		"model":                d.Model,
		"dify_base_url":        d.DifyBaseURL,
		"has_key":              d.DifyAPIKeyEnc != "",
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"charity_enabled": u.CharityEnabled,
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
func (g *Gateway) charityStreaming(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, service string, startedAt time.Time, donation *db.Donation) {
	wfReq.ResponseMode = "streaming"
	events, errCh := client.StreamWorkflow(wfReq)

	// Wait for first event or error
	var firstEvt *dify.StreamEvent
	select {
	case evt, ok := <-events:
		if ok {
			firstEvt = &evt
		}
	case err := <-errCh:
		if err != nil {
			g.charityFailAccounting(userID, donation, err)
			g.writeDifyError(w, err)
			return
		}
	}
	if firstEvt == nil {
		select {
		case err := <-errCh:
			if err != nil {
				g.charityFailAccounting(userID, donation, err)
				g.writeDifyError(w, err)
				return
			}
		default:
		}
	}

	// Stream started (Dify HTTP 200): record success per §1.2
	donationID := donation.ID
	g.charitySuccessAccounting(userID, donation, modelName, service, startedAt)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.logRequest(userID, modelName, service, startedAt, "error", "stream_unsupported",
			http.StatusInternalServerError, "response writer does not support streaming")
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
		// Transport-level failure mid-stream (connection reset,
		// timeout, …).  Per the sixth-round ruling this IS a
		// "Dify 端不可用" failure and counts toward the 10-strike
		// auto-inactivate rule.
		log.Printf("[ERROR] dify charity stream (user %d): %v", userID, streamErr)
		fmt.Fprint(w, translator.FormatSSEErrorFrame("[Dify] "+streamErr.Error()))
		flusher.Flush()
		status, code = "error", "upstream_error"
		detail = streamErr.Error()
		g.charityFailAccounting(userID, donation, streamErr)
	default:
		for _, msg := range conv.Finalize() {
			fmt.Fprint(w, msg.Data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		g.limiter.record(rpmClassA, userID, time.Now())
	}

	g.logRequestDonation(userID, modelName, service, startedAt, status, code, http.StatusOK, detail, donationID)
}

// charityBlocking handles blocking charity calls with donation accounting.
func (g *Gateway) charityBlocking(w http.ResponseWriter, client *dify.Client, wfReq *dify.WorkflowRequest, modelName string, userID int64, service string, startedAt time.Time, donation *db.Donation) {
	wfReq.ResponseMode = "blocking"
	text, err := client.BlockingWorkflow(wfReq)
	donationID := donation.ID

	if err != nil {
		var de *dify.DifyError
		if errors.As(err, &de) && de.Status == http.StatusOK {
			// 200-but-failed: success per §1.2, but admin alert
			g.limiter.record(rpmClassB, userID, time.Now())
			g.charitySuccessAccounting(userID, donation, modelName, service, startedAt)
			g.maybeRecordBlockingFailedAlert(userID, modelName, service, de, &donationID)

			// Log first (we need the log ID for the alert)
			g.logRequestDonation(userID, modelName, service, startedAt, "error", "upstream_error", http.StatusOK, de.Error(), donationID)
			g.writeDifyError(w, err)
			return
		}
		// Real upstream failure — donation failure
		g.charityFailAccounting(userID, donation, err)
		g.logRequest(userID, modelName, service, startedAt, "error", "upstream_error", difyErrorStatus(err), err.Error())
		g.writeDifyError(w, err)
		return
	}

	// Success
	g.limiter.record(rpmClassB, userID, time.Now())
	g.charitySuccessAccounting(userID, donation, modelName, service, startedAt)

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
	g.logRequestDonation(userID, modelName, service, startedAt, "success", "", http.StatusOK, "", donationID)
}

// charitySuccessAccounting records the success side of donation accounting.
func (g *Gateway) charitySuccessAccounting(userID int64, donation *db.Donation, modelName, service string, startedAt time.Time) {
	// 1. Record donation success (remaining_count--, success_count++, may expire)
	if err := g.Store.RecordDonationSuccess(donation.ID); err != nil {
		log.Printf("[ERROR] charity success accounting race (donation %d): %v", donation.ID, err)
		alert := &db.AdminAlert{
			Type:    db.AlertDonationExhaustedRace,
			Message: fmt.Sprintf("公益资源竞争：捐赠条目 %d 在成功调用时已被消耗完", donation.ID),
		}
		alert.DonationID = &donation.ID
		if err := g.Store.AddAdminAlert(alert); err != nil {
			log.Printf("[ERROR] write donation exhausted alert: %v", err)
		}
		return
	}

	// 2. Adjust donation credit for source user (if applicable)
	if donation.SourceUserID.Valid {
		if _, err := g.Store.AdjustUserDonationCredit(donation.SourceUserID.Int64, 1); err != nil {
			log.Printf("[ERROR] adjust donation credit (user %d): %v", donation.SourceUserID.Int64, err)
		}
	}

	// 3. Deduct charity cost from the calling user.
	cost := g.Store.GetSettingInt(db.SettingCharityCost, db.DefaultCharityCost)
	if _, err := g.Store.AdjustUserCredits(userID, -cost); err != nil {
		log.Printf("[ERROR] deduct charity credit (user %d): %v", userID, err)
	}
}

// charityFailAccounting records a donation failure (Dify-side error only).
func (g *Gateway) charityFailAccounting(userID int64, donation *db.Donation, err error) {
	log.Printf("[DONATION] donation %d failure (user %d): %v", donation.ID, userID, err)
	consecutive, recErr := g.Store.RecordDonationFailure(donation.ID)
	if recErr != nil {
		log.Printf("[ERROR] record donation failure (donation %d): %v", donation.ID, recErr)
		return
	}

	// If consecutive >= limit, auto-inactivate.
	limit := g.Store.GetSettingInt(db.SettingDonationFailLimit, db.DefaultDonationFailLimit)
	if consecutive >= limit {
		if err2 := g.Store.SetDonationStatus(donation.ID, db.DonationInactive); err2 != nil {
			log.Printf("[ERROR] auto-inactivate donation %d after %d failures: %v", donation.ID, consecutive, err2)
		} else {
			log.Printf("[DONATION] donation %d auto-inactivated after %d consecutive failures", donation.ID, consecutive)
			if g.mailer != nil {
				g.mailer.DonationInactive(donation.Service, donation.Model, donation.ID, consecutive)
			}
		}
	}
}

// maybeRecordBlockingFailedAlert records an admin alert for a blocking call
// that returned HTTP 200 but workflow status==failed.
func (g *Gateway) maybeRecordBlockingFailedAlert(userID int64, modelName, service string, de *dify.DifyError, donationID *int64) {
	msg := fmt.Sprintf("阻塞调用返回 HTTP 200 但状态为失败：服务 %s，模型 %s，用户 %d，原始错误：%s",
		service, modelName, userID, de.Error())
	alert := &db.AdminAlert{
		Type:       db.AlertBlockingFailed200,
		Message:    msg,
		DonationID: donationID,
	}
	if err := g.Store.AddAdminAlert(alert); err != nil {
		log.Printf("[ERROR] write blocking failed 200 alert: %v", err)
	}
}

// logRequestDonation is like logRequest but includes a donation_id for charity calls.
func (g *Gateway) logRequestDonation(userID int64, model, service string, startedAt time.Time, status, errorCode string, httpStatus int, detail string, donationID int64) {
	if len(detail) > g.Config.LogDetailMaxChars {
		detail = detail[:g.Config.LogDetailMaxChars] + "…"
	}
	if err := g.Store.AddRequestLogFull(userID, model, service, startedAt, time.Now(), status, errorCode, httpStatus, detail, donationID); err != nil {
		log.Printf("[WARN] write request log: %v", err)
	}
}

// resolveDonationSourceDisplay resolves source_display for a donation and
// sets it on the returned JSON map.
func (g *Gateway) enrichDonationJSON(d *db.Donation, keyPlain *string) map[string]interface{} {
	j := donationJSON(d, keyPlain)
	j["source_display"] = g.resolveSourceDisplay(d)
	return j
}
