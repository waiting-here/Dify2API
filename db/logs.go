package db

import (
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

// PurgeOldRequestLogs deletes logs older than the retention window.
func (s *Store) PurgeOldRequestLogs() (int64, error) {
	cutoff := time.Now().Add(-RequestLogRetention).Unix()
	res, err := s.db.Exec(`DELETE FROM request_logs WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
