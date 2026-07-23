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
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Model       string `json:"model"`
	Service     string `json:"service"`
	StartedAt   int64  `json:"started_at"`
	EndedAt     int64  `json:"ended_at"`
	Status      string `json:"status"`       // "success" | "error"
	ErrorCode   string `json:"error_code"`   // "" on success; gateway error code or upstream status otherwise
	HTTPStatus  int    `json:"http_status"`  // HTTP status returned to the caller (0 = unrecorded legacy row)
	ErrorDetail string `json:"error_detail"` // short error message (never request/response content)
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
func (s *Store) AddRequestLogFull(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string, httpStatus int, errorDetail string, donationID int64) error {
	var don interface{}
	if donationID > 0 {
		don = donationID
	}
	_, err := s.db.Exec(
		`INSERT INTO request_logs (user_id, model, service, started_at, ended_at, status, error_code, http_status, error_detail, donation_id) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		userID, model, service, startedAt.Unix(), endedAt.Unix(), status, errorCode, httpStatus, errorDetail, don,
	)
	return err
}

// AddRequestLogDonation records a completed call with a donation routing
// annotation. This is a separate method (not an overload) to avoid
// breaking all existing callers.
func (s *Store) AddRequestLogDonation(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string, donationID int64) error {
	return s.AddRequestLogFull(userID, model, service, startedAt, endedAt, status, errorCode, 0, "", donationID)
}

// ListRequestLogs returns a user's recent logs, newest first (bounded).
func (s *Store) ListRequestLogs(userID int64, limit int) ([]*RequestLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, user_id, model, service, started_at, ended_at, status, error_code, http_status, error_detail
		 FROM request_logs WHERE user_id=? ORDER BY started_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Service, &l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode, &l.HTTPStatus, &l.ErrorDetail); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// AdminRequestLog extends RequestLog with the username obtained via LEFT JOIN
// and the optional donation tracking column (admin-only).
type AdminRequestLog struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Model       string `json:"model"`
	Service     string `json:"service"`
	StartedAt   int64  `json:"started_at"`
	EndedAt     int64  `json:"ended_at"`
	Status      string `json:"status"`
	ErrorCode   string `json:"error_code"`
	HTTPStatus  int    `json:"http_status"`  // HTTP status returned to the caller (0 = unrecorded legacy row)
	ErrorDetail string `json:"error_detail"` // short error message (never request/response content)
	DonationID  *int64 `json:"donation_id"`  // null when not a charity request
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

// ListAllRequestLogs returns request logs across all users with optional
// filters and offset-based pagination.  All WHERE conditions are parameterized.
func (s *Store) ListAllRequestLogs(f LogFilter, limit, offset int) ([]*AdminRequestLog, int, error) {
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

	query := `SELECT l.id, l.user_id, COALESCE(u.username, ''), l.model, l.service,
		l.started_at, l.ended_at, l.status, l.error_code, l.http_status, l.error_detail, l.donation_id
		FROM request_logs l
		LEFT JOIN users u ON l.user_id = u.id` +
		where + ` ORDER BY l.started_at DESC LIMIT ? OFFSET ?`

	allArgs := append(args, limit, offset)
	rows, err := s.db.Query(query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*AdminRequestLog
	for rows.Next() {
		var l AdminRequestLog
		var donationID sql.NullInt64
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Model, &l.Service,
			&l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode, &l.HTTPStatus, &l.ErrorDetail, &donationID); err != nil {
			return nil, 0, err
		}
		if donationID.Valid {
			l.DonationID = &donationID.Int64
		}
		out = append(out, &l)
	}
	return out, total, rows.Err()
}

// PurgeOldRequestLogs deletes logs older than the retention window that are
// NOT bound to a donation (donation_id IS NULL). Donation-bound logs are
// cleaned by PurgeExpiredDonationLogs which uses a per-donation gate.
func (s *Store) PurgeOldRequestLogs() (int64, error) {
	cutoff := time.Now().Add(-RequestLogRetention).Unix()
	res, err := s.db.Exec(`DELETE FROM request_logs WHERE started_at < ? AND donation_id IS NULL`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeExpiredDonationLogs deletes ALL request_logs bound to a donation when
// that donation's most recent request is older than the retention window.
// Only non-expired donations are considered (expired ones have their own
// cleanup path). Returns the total number of deleted rows.
func (s *Store) PurgeExpiredDonationLogs(now int64) (int64, error) {
	cutoff := now - int64(RequestLogRetention.Seconds())

	// Step 1: find donation ids whose latest request_log is old enough.
	rows, err := s.db.Query(
		`SELECT d.id FROM donations d
		 WHERE (SELECT MAX(rl.started_at) FROM request_logs rl
		        WHERE rl.donation_id = d.id) <= ?`, cutoff,
	)
	if err != nil {
		return 0, err
	}
	var ids []interface{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	if len(ids) == 0 {
		return 0, nil
	}

	// Step 2: delete logs and cascade-delete alerts bound to those logs.
	// Build a parameterized IN clause for the donation ids.
	placeholders := make([]string, len(ids))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	inClause := strings.Join(placeholders, ",")

	// Cascade: delete alerts whose request_log_id points to a log we're about to delete.
	_, err = s.db.Exec(
		`DELETE FROM admin_alerts WHERE request_log_id IN (
			SELECT id FROM request_logs WHERE donation_id IN (`+inClause+`)
		)`, ids...,
	)
	if err != nil {
		return 0, err
	}

	// Delete the logs.
	res, err := s.db.Exec(
		`DELETE FROM request_logs WHERE donation_id IN (`+inClause+`) AND started_at <= ?`,
		append(ids, cutoff)...,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
