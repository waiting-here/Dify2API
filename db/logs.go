package db

import (
	"database/sql"
	"strings"
	"time"
)

// RequestLogRetention is how long request logs are kept (rolling 30 days).
const RequestLogRetention = 30 * 24 * time.Hour

// RequestLog is one recorded gateway call (metadata only — never the request
// or response content).
//
// DESIGN INTENT (compliance): request/response content is intentionally
// excluded from logs. This ensures the gateway operator has no actual
// knowledge of user-generated content passing through the service, which
// is a deliberate architectural safeguard against secondary liability for
// illegal content transmitted by users.
type RequestLog struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	Model           string `json:"model"`
	Service         string `json:"service"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at"`
	Status          string `json:"status"`           // "success" | "error"
	ErrorCode       string `json:"error_code"`       // "" on success; gateway error code or upstream status otherwise
	HTTPStatus      int    `json:"http_status"`      // HTTP status returned to the caller (0 = unrecorded legacy row)
	ErrorDetail     string `json:"error_detail"`     // short error message (never request/response content)
	CreditsConsumed int    `json:"credits_consumed"` // credits deducted for this call (0 for non-charity)
	AntiAbuseInfo   string `json:"anti_abuse_info"`  // JSON describing anti-abuse trigger & penalties (empty if none)
}

// AddRequestLog records one completed call (no HTTP status / error detail;
// prefer AddRequestLogFull on new call sites).
func (s *Store) AddRequestLog(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string) error {
	_, err := s.db.Exec(
		`INSERT INTO request_logs (user_id, model, service, started_at, ended_at, status, error_code) VALUES (?,?,?,?,?,?,?)`,
		userID, model, service, startedAt.Unix(), endedAt.Unix(), status, errorCode,
	)
	return err
}

// AddRequestLogFull records one completed call with the admin-facing
// diagnostic fields: the HTTP status returned to the caller and a short
// error detail (never request/response content — see the RequestLog
// design-intent comment). donationID <= 0 stores NULL (not a charity call).
// antiAbuseInfo is a JSON string describing anti-abuse trigger & penalties,
// or empty string if not triggered. Returns the new log row id (0 on error)
// so callers can link dependent rows (e.g. admin alerts).
func (s *Store) AddRequestLogFull(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string, httpStatus int, errorDetail string, donationID int64, creditsConsumed int, antiAbuseInfo string) (int64, error) {
	var don interface{}
	if donationID > 0 {
		don = donationID
	}
	res, err := s.db.Exec(
		`INSERT INTO request_logs (user_id, model, service, started_at, ended_at, status, error_code, http_status, error_detail, donation_id, credits_consumed, anti_abuse_info) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		userID, model, service, startedAt.Unix(), endedAt.Unix(), status, errorCode, httpStatus, errorDetail, don, creditsConsumed, antiAbuseInfo,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddRequestLogDonation records a completed call with a donation routing
// annotation. This is a separate method (not an overload) to avoid
// breaking all existing callers.
func (s *Store) AddRequestLogDonation(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string, donationID int64) error {
	_, err := s.AddRequestLogFull(userID, model, service, startedAt, endedAt, status, errorCode, 0, "", donationID, 0, "")
	return err
}

// ListRequestLogs returns a user's recent logs, newest first (bounded).
func (s *Store) ListRequestLogs(userID int64, limit int) ([]*RequestLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, user_id, model, service, started_at, ended_at, status, error_code, http_status, error_detail, credits_consumed, anti_abuse_info
		 FROM request_logs WHERE user_id=? ORDER BY started_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Service, &l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode, &l.HTTPStatus, &l.ErrorDetail, &l.CreditsConsumed, &l.AntiAbuseInfo); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ExportRequestLogs returns ALL of a user's request logs (newest first),
// without any LIMIT — intended only for the GDPR export path.
func (s *Store) ExportRequestLogs(userID int64) ([]*RequestLog, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, model, service, started_at, ended_at, status, error_code, http_status, error_detail, credits_consumed, anti_abuse_info
		 FROM request_logs WHERE user_id=? ORDER BY started_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Service, &l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode, &l.HTTPStatus, &l.ErrorDetail, &l.CreditsConsumed, &l.AntiAbuseInfo); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// AdminRequestLog extends RequestLog with the username obtained via LEFT JOIN
// and the optional donation tracking column (admin-only).
type AdminRequestLog struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	Username        string `json:"username"`
	Model           string `json:"model"`
	Service         string `json:"service"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code"`
	HTTPStatus      int    `json:"http_status"`      // HTTP status returned to the caller (0 = unrecorded legacy row)
	ErrorDetail     string `json:"error_detail"`     // short error message (never request/response content)
	DonationID      *int64 `json:"donation_id"`      // null when not a charity request
	CreditsConsumed int    `json:"credits_consumed"` // credits deducted for this call (0 for non-charity)
	AntiAbuseInfo   string `json:"anti_abuse_info"`  // JSON describing anti-abuse trigger & penalties (empty if none)
}

// LogFilter collects optional AND-combined filters for ListAllRequestLogs.
type LogFilter struct {
	UserID  *int64
	Service string
	Model   string
	Status  string
	Since   int64
	Until   int64
}

// requestLogSelect is the shared SELECT prefix for admin log queries.
const requestLogSelect = `SELECT l.id, l.user_id, COALESCE(u.username, ''), l.model, l.service,
	l.started_at, l.ended_at, l.status, l.error_code, l.http_status, l.error_detail, l.donation_id, l.credits_consumed, l.anti_abuse_info
	FROM request_logs l
	LEFT JOIN users u ON l.user_id = u.id`

// logFilterWhere builds the parameterized WHERE clause for a LogFilter.
// Returns "" and no args when the filter is empty.
func logFilterWhere(f LogFilter) (string, []interface{}) {
	var conds []string
	var args []interface{}

	if f.UserID != nil {
		conds = append(conds, "l.user_id = ?")
		args = append(args, *f.UserID)
	}
	if f.Service != "" {
		conds = append(conds, "l.service = ?")
		args = append(args, f.Service)
	}
	if f.Model != "" {
		conds = append(conds, "l.model = ?")
		args = append(args, f.Model)
	}
	if f.Status != "" {
		conds = append(conds, "l.status = ?")
		args = append(args, f.Status)
	}
	if f.Since > 0 {
		conds = append(conds, "l.started_at >= ?")
		args = append(args, f.Since)
	}
	if f.Until > 0 {
		conds = append(conds, "l.started_at <= ?")
		args = append(args, f.Until)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

// listRequestLogs runs the shared filtered admin-log query.
// A limit <= 0 returns every matching row without LIMIT/OFFSET (export path).
func (s *Store) listRequestLogs(where string, args []interface{}, limit, offset int) ([]*AdminRequestLog, error) {
	query := requestLogSelect + where + ` ORDER BY l.started_at DESC`
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(append([]interface{}{}, args...), limit, offset)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*AdminRequestLog
	for rows.Next() {
		var l AdminRequestLog
		var donationID sql.NullInt64
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Model, &l.Service,
			&l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode, &l.HTTPStatus, &l.ErrorDetail, &donationID, &l.CreditsConsumed, &l.AntiAbuseInfo); err != nil {
			return nil, err
		}
		if donationID.Valid {
			l.DonationID = &donationID.Int64
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ListAllRequestLogs returns request logs across all users with optional
// filters and offset-based pagination.  All WHERE conditions are parameterized.
func (s *Store) ListAllRequestLogs(f LogFilter, limit, offset int) ([]*AdminRequestLog, int, error) {
	where, args := logFilterWhere(f)

	// Total count (without limit/offset).
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM request_logs l`+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Clamp limit.
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	logs, err := s.listRequestLogs(where, args, limit, offset)
	return logs, total, err
}

// ExportAllRequestLogs returns every request log matching the filter, newest
// first, without pagination — the admin CSV/JSON export path. Unlike
// ListAllRequestLogs it never clamps to a page size, so exports are complete.
func (s *Store) ExportAllRequestLogs(f LogFilter) ([]*AdminRequestLog, error) {
	where, args := logFilterWhere(f)
	return s.listRequestLogs(where, args, 0, 0)
}

// LogDayStat is one day's aggregated log counts.
type LogDayStat struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Total   int    `json:"total"`
	Success int    `json:"success"`
	Error   int    `json:"error"`
}

// LogServiceStat is per-service aggregated log counts.
type LogServiceStat struct {
	Service string `json:"service"`
	Count   int    `json:"count"`
}

// LogHourStat is one hour's aggregated log counts (UTC hour bucket).
type LogHourStat struct {
	HourUnix int64 `json:"hour_unix"` // Unix timestamp of hour start (UTC)
	Total    int   `json:"total"`
	Success  int   `json:"success"`
	Error    int   `json:"error"`
}

// LogStats returns daily and per-service aggregates for the last N days.
// since/until narrow the window further (0 = no bound).
// Deprecated: Use LogStatsByHour for new code.
func (s *Store) LogStats(days int, since, until int64) ([]LogDayStat, []LogServiceStat, error) {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	// By day.
	var dayConds []string
	var dayArgs []interface{}
	dayConds = append(dayConds, "started_at >= ?")
	dayArgs = append(dayArgs, cutoff)
	if since > 0 {
		dayConds = append(dayConds, "started_at >= ?")
		dayArgs = append(dayArgs, since)
	}
	if until > 0 {
		dayConds = append(dayConds, "started_at <= ?")
		dayArgs = append(dayArgs, until)
	}
	dayWhere := "WHERE " + strings.Join(dayConds, " AND ")

	dayQuery := `SELECT date(started_at, 'unixepoch') AS day,
		COUNT(*) AS total,
		SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS success,
		SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) AS error
		FROM request_logs ` + dayWhere + `
		GROUP BY day ORDER BY day ASC`

	rows, err := s.db.Query(dayQuery, dayArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var days_ []LogDayStat
	for rows.Next() {
		var d LogDayStat
		if err := rows.Scan(&d.Date, &d.Total, &d.Success, &d.Error); err != nil {
			return nil, nil, err
		}
		days_ = append(days_, d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// By service.
	var svcConds []string
	var svcArgs []interface{}
	svcConds = append(svcConds, "started_at >= ?")
	svcArgs = append(svcArgs, cutoff)
	if since > 0 {
		svcConds = append(svcConds, "started_at >= ?")
		svcArgs = append(svcArgs, since)
	}
	if until > 0 {
		svcConds = append(svcConds, "started_at <= ?")
		svcArgs = append(svcArgs, until)
	}
	svcWhere := "WHERE " + strings.Join(svcConds, " AND ")

	svcQuery := `SELECT service, COUNT(*) AS cnt
		FROM request_logs ` + svcWhere + `
		GROUP BY service ORDER BY cnt DESC LIMIT 10`

	rows2, err := s.db.Query(svcQuery, svcArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows2.Close()

	var svcs []LogServiceStat
	for rows2.Next() {
		var s LogServiceStat
		if err := rows2.Scan(&s.Service, &s.Count); err != nil {
			return nil, nil, err
		}
		svcs = append(svcs, s)
	}
	return days_, svcs, rows2.Err()
}

// LogStatsByHour returns hourly aggregated log counts for stats API.
// The filter accepts user_id, service, model, status, and time bounds.
// An empty filter returns all history within the current 30-day retention window.
func (s *Store) LogStatsByHour(f LogFilter) ([]LogHourStat, error) {
	// If no explicit time bounds are provided, apply the 30-day retention cutoff.
	// This ensures "all history" stays within the rolling retention window.
	if f.Since <= 0 {
		f.Since = time.Now().Add(-RequestLogRetention).Unix()
	}

	where, args := logFilterWhere(f)

	// Hourly aggregation: use UTC hour buckets for consistent server-side grouping.
	// The frontend will merge these into local days.
	query := `SELECT strftime('%Y-%m-%d %H:00', l.started_at, 'unixepoch') AS hour_str,
			CAST(strftime('%s', strftime('%Y-%m-%d %H:00', l.started_at, 'unixepoch')) AS INTEGER) AS hour_unix,
			COUNT(*) AS total,
			SUM(CASE WHEN l.status = 'success' THEN 1 ELSE 0 END) AS success,
			SUM(CASE WHEN l.status = 'error' THEN 1 ELSE 0 END) AS error
			FROM request_logs l` + where + `
			GROUP BY hour_str
			ORDER BY hour_unix ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []LogHourStat
	for rows.Next() {
		var hourStr string
		var h LogHourStat
		if err := rows.Scan(&hourStr, &h.HourUnix, &h.Total, &h.Success, &h.Error); err != nil {
			return nil, err
		}
		stats = append(stats, h)
	}
	return stats, rows.Err()
}

// PurgeExpiredRequestLogs deletes every request log older than the rolling
// retention cutoff, independent of donation state. Alerts bound to those logs
// are deleted first in the same transaction. This covers active donations and
// orphan donation_id values as well as ordinary requests.
func (s *Store) PurgeExpiredRequestLogs(now int64) (logsDeleted, alertsDeleted int64, err error) {
	cutoff := now - int64(RequestLogRetention.Seconds())
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	alertResult, err := tx.Exec(
		`DELETE FROM admin_alerts WHERE request_log_id IN (
			SELECT id FROM request_logs WHERE started_at < ?
		)`, cutoff,
	)
	if err != nil {
		return 0, 0, err
	}
	alertsDeleted, err = alertResult.RowsAffected()
	if err != nil {
		return 0, 0, err
	}

	logResult, err := tx.Exec(`DELETE FROM request_logs WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0, 0, err
	}
	logsDeleted, err = logResult.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return logsDeleted, alertsDeleted, nil
}

// PurgeOldRequestLogs is the legacy one-count cleanup API.
// Deprecated: use PurgeExpiredRequestLogs so alert cleanup is observable.
func (s *Store) PurgeOldRequestLogs() (int64, error) {
	logs, _, err := s.PurgeExpiredRequestLogs(time.Now().Unix())
	return logs, err
}

// PurgeExpiredDonationLogs is retained for source compatibility. Retention is
// now per log, not per donation, so this cleans every expired request log.
// Deprecated: use PurgeExpiredRequestLogs.
func (s *Store) PurgeExpiredDonationLogs(now int64) (int64, error) {
	logs, _, err := s.PurgeExpiredRequestLogs(now)
	return logs, err
}

// RawExec exposes the underlying sql.DB Exec for tests (e.g. manipulating
// session expiry directly).  Application code should use the dedicated methods.
func (s *Store) RawExec(query string, args ...interface{}) (int64, error) {
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
