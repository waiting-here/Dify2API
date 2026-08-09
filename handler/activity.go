package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"dify2api/db"
)

const activityDateLayout = "2006-01-02"

func parseActivityRange(r *http.Request, now time.Time) (int64, int64, error) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	sinceRaw, untilRaw := r.URL.Query().Get("since"), r.URL.Query().Get("until")
	if sinceRaw == "" && untilRaw == "" {
		return today.AddDate(0, 0, -27).Unix(), today.Unix(), nil
	}
	if sinceRaw == "" || untilRaw == "" {
		return 0, 0, errInvalidActivityRange
	}
	since, err := time.Parse(activityDateLayout, sinceRaw)
	if err != nil || since.Format(activityDateLayout) != sinceRaw {
		return 0, 0, errInvalidActivityRange
	}
	until, err := time.Parse(activityDateLayout, untilRaw)
	if err != nil || until.Format(activityDateLayout) != untilRaw || since.After(until) {
		return 0, 0, errInvalidActivityRange
	}
	days := int(until.Sub(since)/(24*time.Hour)) + 1
	if days < 1 || days > db.ActivityRetentionDays {
		return 0, 0, errInvalidActivityRange
	}
	return since.Unix(), until.Unix(), nil
}

type activityRangeError struct{}

func (activityRangeError) Error() string { return "invalid activity date range" }

var errInvalidActivityRange error = activityRangeError{}

func (g *Gateway) handleAdminActivityStats(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	now := time.Now()
	since, until, err := parseActivityRange(r, now)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid activity date range")
		return
	}
	stats, err := g.Store.ActivityStats(since, until, now)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}
