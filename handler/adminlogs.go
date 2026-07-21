package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"dify2api/db"
)

// handleAdminLogs serves GET /api/admin/logs with optional filters and pagination.
func (g *Gateway) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	q := r.URL.Query()

	var filter db.LogFilter

	if s := q.Get("user_id"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "user_id must be an integer")
			return
		}
		filter.UserID = &v
	}
	filter.Service = q.Get("service")

	if s := q.Get("status"); s != "" {
		if s != "success" && s != "error" {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "status must be 'success' or 'error'")
			return
		}
		filter.Status = s
	}

	if s := q.Get("since"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "since must be a unix timestamp (integer)")
			return
		}
		filter.Since = v
	}
	if s := q.Get("until"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "until must be a unix timestamp (integer)")
			return
		}
		filter.Until = v
	}

	limit := 100
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

	logs, total, err := g.Store.ListAllRequestLogs(filter, limit, offset)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total": total,
		"logs":  logs,
	})
}
