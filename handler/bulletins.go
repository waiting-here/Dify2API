package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
)

// --- System bulletin IDs ---
// Negative IDs distinguish system bulletins from DB-stored ones.
const (
	sysBulletinCheckinDisabled  = -1
	sysBulletinDonationDisabled = -2
	sysBulletinCharityDisabled  = -3
)

// --- Admin CRUD ---

// GET /api/admin/bulletins
func (g *Gateway) handleAdminListBulletins(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	list, err := g.Store.ListBulletins()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out := make([]map[string]interface{}, 0, len(list))
	for _, b := range list {
		out = append(out, bulletinJSON(b))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"bulletins": out})
}

// POST /api/admin/bulletins
func (g *Gateway) handleAdminCreateBulletin(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	var req struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		Type      string `json:"type"`
		SortOrder int    `json:"sort_order"`
		Closable  bool   `json:"closable"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	// Validate type.
	switch req.Type {
	case db.BulletinTypeInfo, db.BulletinTypeWarning, db.BulletinTypeImportant:
		// OK
	case "":
		req.Type = db.BulletinTypeInfo
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"公告类型必须是 info、warning 或 important")
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "标题为必填")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "正文为必填")
		return
	}

	var expiresAt sql.NullInt64
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}

	b := &db.Bulletin{
		Title:     strings.TrimSpace(req.Title),
		Content:   req.Content,
		Type:      req.Type,
		SortOrder: req.SortOrder,
		Closable:  req.Closable,
		ExpiresAt: expiresAt,
		Lang:      "zh",
	}

	created, err := g.Store.CreateBulletin(b)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"bulletin": bulletinJSON(created),
	})
}

// PUT /api/admin/bulletins/{id}
func (g *Gateway) handleAdminUpdateBulletin(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid bulletin id")
		return
	}

	existing, err := g.Store.GetBulletin(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if existing == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "公告不存在")
		return
	}
	if existing.IsSystem {
		g.writeError(w, http.StatusForbidden, "forbidden", "系统公告不可编辑")
		return
	}

	var req struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		Type      string `json:"type"`
		SortOrder int    `json:"sort_order"`
		Closable  bool   `json:"closable"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	switch req.Type {
	case db.BulletinTypeInfo, db.BulletinTypeWarning, db.BulletinTypeImportant:
		// OK
	case "":
		req.Type = db.BulletinTypeInfo
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"公告类型必须是 info、warning 或 important")
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "标题为必填")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "正文为必填")
		return
	}

	var expiresAt sql.NullInt64
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}

	update := &db.Bulletin{
		Title:     strings.TrimSpace(req.Title),
		Content:   req.Content,
		Type:      req.Type,
		SortOrder: req.SortOrder,
		Closable:  req.Closable,
		ExpiresAt: expiresAt,
	}

	updated, err := g.Store.UpdateBulletin(id, update)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if updated == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "公告不存在")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"bulletin": bulletinJSON(updated),
	})
}

// DELETE /api/admin/bulletins/{id}
func (g *Gateway) handleAdminDeleteBulletin(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid bulletin id")
		return
	}

	existing, err := g.Store.GetBulletin(id)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if existing == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "公告不存在")
		return
	}
	if existing.IsSystem {
		g.writeError(w, http.StatusForbidden, "forbidden", "系统公告不可删除")
		return
	}

	if err := g.Store.DeleteBulletin(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- Public user endpoint ---

// GET /api/bulletins
// Returns merged list: DB active bulletins + system bulletins (injected).
// Public endpoint, no auth required.
func (g *Gateway) handleListBulletins(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	dbList, err := g.Store.ListActiveBulletins(now)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	out := make([]map[string]interface{}, 0, len(dbList)+3)

	// 1. System bulletins first (injected by handler based on settings).
	// Determine language from the logged-in user; fall back to "zh".
	lang := "zh"
	if u := g.currentUser(r); u != nil && u.Lang != "" {
		lang = u.Lang
	}
	sysBulletins := g.buildSystemBulletins(now, lang)
	for _, sb := range sysBulletins {
		out = append(out, sb)
	}

	// 2. DB bulletins (admin-created, active).
	for _, b := range dbList {
		out = append(out, bulletinJSON(b))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"bulletins": out})
}

// buildSystemBulletins creates system bulletin entries based on current settings.
// lang is the preferred UI language ("zh" or "en"); falls back to Chinese when unrecognised.
func (g *Gateway) buildSystemBulletins(now int64, lang string) []map[string]interface{} {
	var out []map[string]interface{}

	// Checkin disabled (credits_cap == 0).
	creditsCap := g.Store.GetSettingIntAllowZero(db.SettingCreditsCap, db.DefaultCreditsCap)
	if creditsCap == 0 {
		title := "Check-in System Unavailable"
		if lang == "zh" {
			title = "签到系统当前未开放"
		}
		out = append(out, map[string]interface{}{
			"id":         sysBulletinCheckinDisabled,
			"title":      title,
			"content":    "",
			"type":       db.BulletinTypeWarning,
			"sort_order": 100,
			"closable":   false,
			"created_at": now,
			"expires_at": nil,
			"is_system":  true,
			"system_key": "checkin_disabled",
			"lang":       lang,
		})
	}

	// Donation disabled.
	donationEnabled := g.Store.GetSettingString(db.SettingDonationEnabled, "") == "true"
	if !donationEnabled {
		title := "Donation System Unavailable"
		if lang == "zh" {
			title = "捐赠系统当前未开放"
		}
		out = append(out, map[string]interface{}{
			"id":         sysBulletinDonationDisabled,
			"title":      title,
			"content":    "",
			"type":       db.BulletinTypeWarning,
			"sort_order": 99,
			"closable":   false,
			"created_at": now,
			"expires_at": nil,
			"is_system":  true,
			"system_key": "donation_disabled",
			"lang":       lang,
		})
	}

	// Charity disabled.
	charityEnabled := g.Store.GetSettingString(db.SettingCharityEnabled, "") == "true"
	if !charityEnabled {
		title := "Charity System Unavailable"
		if lang == "zh" {
			title = "公益系统当前未开放"
		}
		out = append(out, map[string]interface{}{
			"id":         sysBulletinCharityDisabled,
			"title":      title,
			"content":    "",
			"type":       db.BulletinTypeWarning,
			"sort_order": 98,
			"closable":   false,
			"created_at": now,
			"expires_at": nil,
			"is_system":  true,
			"system_key": "charity_disabled",
			"lang":       lang,
		})
	}

	return out
}

// bulletinJSON builds the API representation of a Bulletin.
func bulletinJSON(b *db.Bulletin) map[string]interface{} {
	out := map[string]interface{}{
		"id":         b.ID,
		"title":      b.Title,
		"content":    b.Content,
		"type":       b.Type,
		"sort_order": b.SortOrder,
		"closable":   b.Closable,
		"created_at": b.CreatedAt,
		"is_system":  b.IsSystem,
		"lang":       b.Lang,
	}
	if b.ExpiresAt.Valid {
		out["expires_at"] = b.ExpiresAt.Int64
	} else {
		out["expires_at"] = nil
	}
	if b.SystemKey.Valid {
		out["system_key"] = b.SystemKey.String
	} else {
		out["system_key"] = nil
	}
	return out
}
