package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dify2api/db"

	"github.com/yuin/goldmark"
)

// --- System bulletin IDs ---
// Negative IDs distinguish system bulletins from DB-stored ones.
const (
	sysBulletinCheckinDisabled  = -1
	sysBulletinDonationDisabled = -2
	sysBulletinCharityDisabled  = -3
)

var md = goldmark.New()

// RenderBulletinContent converts raw bulletin content to HTML for display.
// content_type == "markdown" uses goldmark; other values pass through unchanged.
func RenderBulletinContent(contentType, raw string) string {
	if contentType == "markdown" {
		var buf bytes.Buffer
		if err := md.Convert([]byte(raw), &buf); err != nil {
			return raw // fallback: return raw on error
		}
		return buf.String()
	}
	return raw
}

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
		Title       string `json:"title"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Type        string `json:"type"`
		SortOrder   int    `json:"sort_order"`
		Closable    bool   `json:"closable"`
		ExpiresAt   *int64 `json:"expires_at"`
		Lang        string `json:"lang"`
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
			t(g.resolveLang(r), "公告类型必须是 info、warning 或 important", "Bulletin type must be info, warning, or important"))
		return
	}

	// Validate content_type.
	switch req.ContentType {
	case "html", "markdown":
		// OK
	case "":
		req.ContentType = "html"
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "content_type 必须是 html 或 markdown", "content_type must be 'html' or 'markdown'"))
		return
	}

	// Validate lang.
	switch req.Lang {
	case "zh", "en":
		// OK
	case "":
		req.Lang = "zh"
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "lang 必须是 zh 或 en", "lang must be 'zh' or 'en'"))
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "标题为必填", "Title is required"))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "正文为必填", "Content is required"))
		return
	}

	var expiresAt sql.NullInt64
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}

	b := &db.Bulletin{
		Title:       strings.TrimSpace(req.Title),
		Content:     req.Content,
		ContentType: req.ContentType,
		Type:        req.Type,
		SortOrder:   req.SortOrder,
		Closable:    req.Closable,
		ExpiresAt:   expiresAt,
		Lang:        req.Lang,
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
		g.writeError(w, http.StatusNotFound, "not_found", t(g.resolveLang(r), "公告不存在", "Bulletin not found"))
		return
	}
	if existing.IsSystem {
		g.writeError(w, http.StatusForbidden, "forbidden", t(g.resolveLang(r), "系统公告不可编辑", "System bulletins cannot be edited"))
		return
	}

	var req struct {
		Title       string `json:"title"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Type        string `json:"type"`
		SortOrder   int    `json:"sort_order"`
		Closable    bool   `json:"closable"`
		ExpiresAt   *int64 `json:"expires_at"`
		Lang        string `json:"lang"`
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
			t(g.resolveLang(r), "公告类型必须是 info、warning 或 important", "Bulletin type must be info, warning, or important"))
		return
	}

	// Validate content_type.
	switch req.ContentType {
	case "html", "markdown":
		// OK
	case "":
		req.ContentType = "html"
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "content_type 必须是 html 或 markdown", "content_type must be 'html' or 'markdown'"))
		return
	}

	// Validate lang.
	switch req.Lang {
	case "zh", "en":
		// OK
	case "":
		req.Lang = "zh"
	default:
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			t(g.resolveLang(r), "lang 必须是 zh 或 en", "lang must be 'zh' or 'en'"))
		return
	}

	if strings.TrimSpace(req.Title) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "标题为必填", "Title is required"))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "正文为必填", "Content is required"))
		return
	}

	var expiresAt sql.NullInt64
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		expiresAt = sql.NullInt64{Int64: *req.ExpiresAt, Valid: true}
	}

	update := &db.Bulletin{
		Title:       strings.TrimSpace(req.Title),
		Content:     req.Content,
		ContentType: req.ContentType,
		Type:        req.Type,
		SortOrder:   req.SortOrder,
		Closable:    req.Closable,
		ExpiresAt:   expiresAt,
		Lang:        req.Lang,
	}

	updated, err := g.Store.UpdateBulletin(id, update)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if updated == nil {
		g.writeError(w, http.StatusNotFound, "not_found", t(g.resolveLang(r), "公告不存在", "Bulletin not found"))
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
		g.writeError(w, http.StatusNotFound, "not_found", t(g.resolveLang(r), "公告不存在", "Bulletin not found"))
		return
	}
	if existing.IsSystem {
		g.writeError(w, http.StatusForbidden, "forbidden", t(g.resolveLang(r), "系统公告不可删除", "System bulletins cannot be deleted"))
		return
	}

	if err := g.Store.DeleteBulletin(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// POST /api/admin/bulletins/delete/batch — batch delete bulletins (7.4.1).
func (g *Gateway) handleBatchDeleteBulletins(w http.ResponseWriter, r *http.Request) {
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

	if err := g.Store.DeleteBulletins(req.IDs); err != nil {
		if conflict, ok := err.(*db.BulletinDeleteError); ok {
			writeBatchDonationError(w, conflict.Error(), conflict.ID)
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
	out = append(out, sysBulletins...)

	// 2. DB bulletins (admin-created, active).
	// Render Markdown content to HTML before sending to user clients.
	for _, b := range dbList {
		bj := bulletinJSON(b)
		bj["content"] = RenderBulletinContent(b.ContentType, b.Content)
		out = append(out, bj)
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
		"id":           b.ID,
		"title":        b.Title,
		"content":      b.Content,
		"content_type": b.ContentType,
		"type":         b.Type,
		"sort_order":   b.SortOrder,
		"closable":     b.Closable,
		"created_at":   b.CreatedAt,
		"is_system":    b.IsSystem,
		"lang":         b.Lang,
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
