package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"dify2api/db"
	"dify2api/mailer"
)

// alertPrefEventTypes lists every category with alert-center/email switches:
// the five mailer event types plus the blocking-failed-200 record type.
func alertPrefEventTypes() []string {
	types := make([]string, 0, 6)
	for _, et := range mailer.AllEventTypes() {
		types = append(types, string(et))
	}
	types = append(types, db.AlertBlockingFailed200)
	return types
}

// handleListAlertPrefs serves GET /api/admin/alert-prefs — the per-category
// switches shown in the alert center.
func (g *Gateway) handleListAlertPrefs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	prefs, err := g.Store.ListAlertPrefs()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"prefs": prefs})
}

// handlePutAlertPrefs serves PUT /api/admin/alert-prefs — batch update of
// the per-category switches. Unknown event types are rejected.
func (g *Gateway) handlePutAlertPrefs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		Prefs []struct {
			EventType    string `json:"event_type"`
			ShowInCenter *bool  `json:"show_in_center"`
			EmailEnabled *bool  `json:"email_enabled"`
		} `json:"prefs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	known := make(map[string]bool, len(alertPrefEventTypes()))
	for _, et := range alertPrefEventTypes() {
		known[et] = true
	}
	updates := make([]db.AlertPrefUpdate, 0, len(req.Prefs))
	for _, p := range req.Prefs {
		if !known[p.EventType] {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				"unknown alert event type: "+p.EventType)
			return
		}
		updates = append(updates, db.AlertPrefUpdate{
			EventType: p.EventType, ShowInCenter: p.ShowInCenter, EmailEnabled: p.EmailEnabled,
		})
	}
	if err := g.Store.SetAlertPrefs(updates); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	prefs, err := g.Store.ListAlertPrefs()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "prefs": prefs})
}

// handleListAlerts serves GET /api/admin/alerts with pagination.
func (g *Gateway) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	q := r.URL.Query()
	limit := 20
	if s := q.Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a non-negative integer")
			return
		}
		limit = v
	}
	offset := 0
	if s := q.Get("offset"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "offset must be a non-negative integer")
			return
		}
		offset = v
	}

	alerts, total, err := g.Store.ListAdminAlerts(limit, offset)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": alerts,
		"total":  total,
	})
}

// handleDeleteAlerts serves DELETE /api/admin/alerts with a JSON body
// of alert ids to delete in batch.
func (g *Gateway) handleDeleteAlerts(w http.ResponseWriter, r *http.Request) {
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

	n, err := g.Store.DeleteAdminAlerts(req.IDs)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"deleted": n,
	})
}
