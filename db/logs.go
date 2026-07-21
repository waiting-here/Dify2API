package db

import (
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
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Model     string `json:"model"`
	Service   string `json:"service"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at"`
	Status    string `json:"status"`     // "success" | "error"
	ErrorCode string `json:"error_code"` // "" on success; gateway error code or upstream status otherwise
}

// AddRequestLog records one completed call.
func (s *Store) AddRequestLog(userID int64, model, service string, startedAt, endedAt time.Time, status, errorCode string) error {
	_, err := s.db.Exec(
		`INSERT INTO request_logs (user_id, model, service, started_at, ended_at, status, error_code) VALUES (?,?,?,?,?,?,?)`,
		userID, model, service, startedAt.Unix(), endedAt.Unix(), status, errorCode,
	)
	return err
}

// ListRequestLogs returns a user's recent logs, newest first (bounded).
func (s *Store) ListRequestLogs(userID int64, limit int) ([]*RequestLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, user_id, model, service, started_at, ended_at, status, error_code
		 FROM request_logs WHERE user_id=? ORDER BY started_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RequestLog
	for rows.Next() {
		var l RequestLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Model, &l.Service, &l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// AdminRequestLog extends RequestLog with the username obtained via LEFT JOIN.
type AdminRequestLog struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Model     string `json:"model"`
	Service   string `json:"service"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
}

// LogFilter collects optional AND-combined filters for ListAllRequestLogs.
type LogFilter struct {
	UserID  *int64
	Service string
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
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT l.id, l.user_id, COALESCE(u.username, ''), l.model, l.service,
		l.started_at, l.ended_at, l.status, l.error_code
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
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Model, &l.Service,
			&l.StartedAt, &l.EndedAt, &l.Status, &l.ErrorCode); err != nil {
			return nil, 0, err
		}
		out = append(out, &l)
	}
	return out, total, rows.Err()
}

// PurgeOldRequestLogs deletes logs older than the retention window.
func (s *Store) PurgeOldRequestLogs() (int64, error) {
	cutoff := time.Now().Add(-RequestLogRetention).Unix()
	res, err := s.db.Exec(`DELETE FROM request_logs WHERE started_at < ?`, cutoff)
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
