package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"dify2api/db"
)

// handleAdminLogs serves GET /api/admin/logs with optional filters and pagination.
func (g *Gateway) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveAllLogs(w, r, false)
}

// serveAllLogs serves the site-wide request-log listing with optional
// filters and pagination. public=true applies the R-B user-view sanitizer
// to error_detail (level-5 all-logs); the admin view keeps the raw text.
func (g *Gateway) serveAllLogs(w http.ResponseWriter, r *http.Request, public bool) {

	filter, err := parseLogFilter(r)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	q := r.URL.Query()

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
	if public {
		// Never reuse the admin export or the raw stored error text: the
		// level-5 user view goes through the same display boundary as the
		// user-site /api/logs (R-B).
		logs = sanitizePublicAdminRequestLogs(logs, g.resolveLang(r))
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

// handleAdminExportLogs serves GET /api/admin/logs/export with the same
// filters as the list endpoint.  format=csv|json (default json).
func (g *Gateway) handleAdminExportLogs(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	f, err := parseLogFilter(r)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format != "csv" {
		format = "json"
	}

	// Fetch all matching logs (no pagination, never truncated).
	logs, err := g.Store.ExportAllRequestLogs(f)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Build donation source cache.
	donCache := make(map[int64]string)
	for _, l := range logs {
		if l.DonationID != nil {
			if _, ok := donCache[*l.DonationID]; !ok {
				donCache[*l.DonationID] = g.resolveDonationSourceForLog(*l.DonationID)
			}
		}
	}

	ts := time.Now().Format("20060102-150405")

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dify2api-logs-%s.csv"`, ts))
		csvWriter := csv.NewWriter(w)
		_ = csvWriter.Write([]string{"ID", "User", "Username", "Model", "Service", "StartedAt", "EndedAt", "Status", "ErrorCode", "HTTPStatus", "ErrorDetail", "DonationID", "CreditsConsumed", "AntiAbuseInfo", "DonationSource"})
		for _, l := range logs {
			var donID, donSrc string
			if l.DonationID != nil {
				donID = strconv.FormatInt(*l.DonationID, 10)
				donSrc = donCache[*l.DonationID]
			}
			row := []string{
				strconv.FormatInt(l.ID, 10), strconv.FormatInt(l.UserID, 10), l.Username,
				l.Model, l.Service, strconv.FormatInt(l.StartedAt, 10), strconv.FormatInt(l.EndedAt, 10),
				l.Status, l.ErrorCode, strconv.Itoa(l.HTTPStatus), l.ErrorDetail, donID,
				strconv.Itoa(l.CreditsConsumed), l.AntiAbuseInfo, donSrc,
			}
			for i := range row {
				row[i] = safeCSVCell(row[i])
			}
			_ = csvWriter.Write(row)
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			log.Printf("[ERROR] write admin log CSV export: %v", err)
		}
		return
	}

	// JSON.
	type exportRow struct {
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
	out := make([]exportRow, 0, len(logs))
	for _, l := range logs {
		er := exportRow{
			ID: l.ID, UserID: l.UserID, Username: l.Username,
			Model: l.Model, Service: l.Service,
			StartedAt: l.StartedAt, EndedAt: l.EndedAt,
			Status: l.Status, ErrorCode: l.ErrorCode,
			HTTPStatus: l.HTTPStatus, ErrorDetail: l.ErrorDetail,
			DonationID: l.DonationID, CreditsConsumed: l.CreditsConsumed,
			AntiAbuseInfo: l.AntiAbuseInfo,
		}
		if l.DonationID != nil {
			er.SourceDisplay = donCache[*l.DonationID]
		}
		out = append(out, er)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dify2api-logs-%s.json"`, ts))
	json.NewEncoder(w).Encode(out)
}

// safeCSVCell prevents spreadsheet programs from interpreting exported data
// as a formula. Spreadsheet applications also recognize a formula marker after
// leading whitespace or control characters, so inspect the first non-prefix
// rune. A leading apostrophe is the broadly compatible text marker used by
// Excel, LibreOffice, and similar applications; encoding/csv still handles
// commas, quotes, and newlines independently.
func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	for _, r := range value {
		if r == '\ufeff' || unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		switch r {
		case '=', '+', '-', '@':
			return "'" + value
		default:
			return value
		}
	}
	return value
}

// handleAdminLogStats serves GET /api/admin/logs/stats with hourly aggregates
// for chart rendering. Frontend merges hours into local days.
// The endpoint accepts the same filter parameters as the list/export endpoints.
// The "days" parameter is accepted for compatibility but ignored.
// When "by_service=1" is passed, returns 501 Not Implemented.
func (g *Gateway) handleAdminLogStats(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	g.serveLogStats(w, r)
}

// serveLogStats serves the hourly log-stats contract; shared by the admin
// and the level-5 all-logs endpoints. The payload contains only counts, no
// per-request fields, so no sanitization is needed.
func (g *Gateway) serveLogStats(w http.ResponseWriter, r *http.Request) {

	q := r.URL.Query()

	// Check for explicit by_service request.
	if q.Get("by_service") == "1" {
		g.writeError(w, http.StatusNotImplemented, "not_implemented", "service statistics are not implemented")
		return
	}

	// Accept "days" parameter for compatibility but ignore it.
	// Stats now return all history within the 30-day retention window (or filtered range).
	_ = q.Get("days")

	// Parse and validate filter with strict semantics.
	filter, err := parseLogFilter(r)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	byHour, err := g.Store.LogStatsByHour(filter)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"by_hour":    byHour,
		"by_service": []interface{}{}, // Compatibility field, always empty
	})
}

// parseLogFilter extracts and validates the common log filter params from query string.
// Returns an error if any parameter is invalid, ensuring strict validation semantics.
// This is shared between list, export, and stats endpoints to guarantee consistent behavior.
func parseLogFilter(r *http.Request) (db.LogFilter, error) {
	q := r.URL.Query()
	var f db.LogFilter

	if s := q.Get("user_id"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			return db.LogFilter{}, fmt.Errorf("user_id must be a positive integer")
		}
		f.UserID = &v
	}

	f.Service = q.Get("service")

	f.Model = strings.TrimSpace(q.Get("model"))

	if s := q.Get("status"); s != "" {
		if s != "success" && s != "error" {
			return db.LogFilter{}, fmt.Errorf("status must be 'success' or 'error'")
		}
		f.Status = s
	}

	if s := q.Get("since"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			return db.LogFilter{}, fmt.Errorf("since must be a positive unix timestamp")
		}
		f.Since = v
	}

	if s := q.Get("until"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			return db.LogFilter{}, fmt.Errorf("until must be a positive unix timestamp")
		}
		f.Until = v
	}

	if f.Since > 0 && f.Until > 0 && f.Since > f.Until {
		return db.LogFilter{}, fmt.Errorf("since must be <= until")
	}

	return f, nil
}
