package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	filter.Model = strings.TrimSpace(q.Get("model"))

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

	// Enrich logs with donation source_display.
	type enrichedLog struct {
		ID              int64  `json:"id"`
		UserID          int64  `json:"user_id"`
		Username        string `json:"username"`
		Model           string `json:"model"`
		Service         string `json:"service"`
		StartedAt       int64  `json:"started_at"`
		EndedAt         int64  `json:"ended_at"`
		Status          string `json:"status"`
		ErrorCode       string `json:"error_code"`
		HTTPStatus      int    `json:"http_status"`
		ErrorDetail     string `json:"error_detail"`
		DonationID      *int64 `json:"donation_id"`
		CreditsConsumed int    `json:"credits_consumed"`
		AntiAbuseInfo   string `json:"anti_abuse_info"`
		SourceDisplay   string `json:"source_display,omitempty"`
	}

	// Build a cache of donation source displays for this batch.
	donCache := make(map[int64]string)
	for _, l := range logs {
		if l.DonationID != nil {
			if _, ok := donCache[*l.DonationID]; !ok {
				donCache[*l.DonationID] = g.resolveDonationSourceForLog(*l.DonationID)
			}
		}
	}

	out := make([]enrichedLog, 0, len(logs))
	for _, l := range logs {
		el := enrichedLog{
			ID:              l.ID,
			UserID:          l.UserID,
			Username:        l.Username,
			Model:           l.Model,
			Service:         l.Service,
			StartedAt:       l.StartedAt,
			EndedAt:         l.EndedAt,
			Status:          l.Status,
			ErrorCode:       l.ErrorCode,
			HTTPStatus:      l.HTTPStatus,
			ErrorDetail:     l.ErrorDetail,
			DonationID:      l.DonationID,
			CreditsConsumed: l.CreditsConsumed,
			AntiAbuseInfo:   l.AntiAbuseInfo,
		}
		if l.DonationID != nil {
			el.SourceDisplay = donCache[*l.DonationID]
		}
		out = append(out, el)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total": total,
		"logs":  out,
	})
}

// resolveDonationSourceForLog resolves the source_display string for a
// donation id in the context of admin log display.  If the donation has
// been deleted (GetDonation returns nil), it returns "（条目已删除）".
func (g *Gateway) resolveDonationSourceForLog(donationID int64) string {
	d, err := g.Store.GetDonation(donationID)
	if err != nil || d == nil {
		return "（条目已删除）"
	}
	return g.resolveSourceDisplay(d)
}
