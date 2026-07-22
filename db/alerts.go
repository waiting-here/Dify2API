package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Admin alert type constants.
const (
	AlertBlockingFailed200     = "blocking_failed_200"
	AlertDonationExhaustedRace = "donation_exhausted_race"
)

// AdminAlert records an operational event that requires administrator
// attention (F6: admin alert centre).
type AdminAlert struct {
	ID           int64  `json:"id"`
	Type         string `json:"type"`
	Message      string `json:"message"`
	RequestLogID *int64 `json:"request_log_id"`
	DonationID   *int64 `json:"donation_id"`
	CreatedAt    int64  `json:"created_at"`
}

func scanAdminAlert(row interface{ Scan(...interface{}) error }) (*AdminAlert, error) {
	var a AdminAlert
	var reqLogID, donationID sql.NullInt64
	if err := row.Scan(&a.ID, &a.Type, &a.Message, &reqLogID, &donationID, &a.CreatedAt); err != nil {
		return nil, err
	}
	if reqLogID.Valid {
		a.RequestLogID = &reqLogID.Int64
	}
	if donationID.Valid {
		a.DonationID = &donationID.Int64
	}
	return &a, nil
}

// AddAdminAlert inserts a new alert row.
func (s *Store) AddAdminAlert(a *AdminAlert) error {
	var reqLogID, donationID interface{}
	if a.RequestLogID != nil {
		reqLogID = *a.RequestLogID
	}
	if a.DonationID != nil {
		donationID = *a.DonationID
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO admin_alerts (type, message, request_log_id, donation_id, created_at) VALUES (?,?,?,?,?)`,
		a.Type, a.Message, reqLogID, donationID, now,
	)
	return err
}

// ListAdminAlerts returns alerts newest-first with offset-based pagination.
func (s *Store) ListAdminAlerts(limit, offset int) ([]*AdminAlert, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(
		`SELECT id, type, message, request_log_id, donation_id, created_at
		 FROM admin_alerts ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*AdminAlert
	for rows.Next() {
		a, err := scanAdminAlert(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// PurgeAlertsForExpiredLogs deletes alerts whose bound request_log is older
// than the retention window AND has no donation_id (regular logs cleaned by
// PurgeOldRequestLogs).  Alerts with request_log_id IS NULL (unbound alerts)
// are intentionally NOT cleaned — per the third-round ruling they require
// manual deletion.
func (s *Store) PurgeAlertsForExpiredLogs(now int64) (int64, error) {
	cutoff := now - int64(RequestLogRetention.Seconds())
	res, err := s.db.Exec(
		`DELETE FROM admin_alerts WHERE request_log_id IS NOT NULL
		 AND request_log_id IN (
			SELECT id FROM request_logs WHERE started_at <= ? AND donation_id IS NULL
		)`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteAdminAlerts deletes alerts by primary key (multi-select batch).
// Returns the number of rows actually deleted. An empty slice is a no-op.
func (s *Store) DeleteAdminAlerts(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`DELETE FROM admin_alerts WHERE id IN (%s)`, strings.Join(placeholders, ","))
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
