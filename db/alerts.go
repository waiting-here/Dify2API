package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Admin alert type constants.
const (
	AlertBlockingFailed200 = "blocking_failed_200"
)

// AdminAlert records an operational event that requires administrator
// attention (F6: admin alert center).
type AdminAlert struct {
	ID           int64  `json:"id"`
	Type         string `json:"type"`
	Message      string `json:"message"`
	RequestLogID *int64 `json:"request_log_id"`
	DonationID   *int64 `json:"donation_id"`
	// UserID is the owner of the linked request log, resolved via LEFT JOIN
	// in ListAdminAlerts (nil when the alert has no request_log_id or the
	// log/user no longer exists).
	UserID    *int64 `json:"user_id,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func scanAdminAlert(row interface{ Scan(...interface{}) error }) (*AdminAlert, error) {
	var a AdminAlert
	var reqLogID, donationID, userID sql.NullInt64
	if err := row.Scan(&a.ID, &a.Type, &a.Message, &reqLogID, &donationID, &a.CreatedAt, &userID); err != nil {
		return nil, err
	}
	if reqLogID.Valid {
		a.RequestLogID = &reqLogID.Int64
	}
	if donationID.Valid {
		a.DonationID = &donationID.Int64
	}
	if userID.Valid {
		a.UserID = &userID.Int64
	}
	return &a, nil
}

// AddAdminAlert inserts a new alert row. When the alert type has a pref row
// with show_in_center=0, the record is skipped entirely (the category is
// turned off in the alert center). Unknown types default to recorded.
func (s *Store) AddAdminAlert(a *AdminAlert) error {
	if !s.IsAlertShownInCenter(a.Type) {
		return nil
	}
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

// AlertPref is a per-category switch controlling whether events of the type
// are recorded in the alert center and whether they trigger email alerts.
type AlertPref struct {
	EventType    string `json:"event_type"`
	ShowInCenter bool   `json:"show_in_center"`
	EmailEnabled bool   `json:"email_enabled"`
	UpdatedAt    int64  `json:"updated_at"`
}

// EnsureAlertPrefs seeds pref rows for the given event types with default
// values (both switches on) and leaves existing rows untouched.
func (s *Store) EnsureAlertPrefs(types []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, et := range types {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO alert_prefs (event_type, show_in_center, email_enabled, updated_at)
			 VALUES (?, 1, 1, ?)`,
			et, now,
		); err != nil {
			return fmt.Errorf("seed alert_prefs %q: %w", et, err)
		}
	}
	return tx.Commit()
}

// SetAlertPref updates both switches of one category.
func (s *Store) SetAlertPref(eventType string, showInCenter, emailEnabled bool) error {
	sc, ec := 0, 0
	if showInCenter {
		sc = 1
	}
	if emailEnabled {
		ec = 1
	}
	res, err := s.db.Exec(
		`UPDATE alert_prefs SET show_in_center=?, email_enabled=?, updated_at=? WHERE event_type=?`,
		sc, ec, time.Now().Unix(), eventType,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown alert event type %q", eventType)
	}
	return nil
}

// AlertPrefUpdate contains only the preference fields explicitly supplied by
// a batch request. Nil fields retain their current values.
type AlertPrefUpdate struct {
	EventType    string
	ShowInCenter *bool
	EmailEnabled *bool
}

// SetAlertPrefs applies a preference batch atomically. Missing rows are
// rejected so a typo cannot silently create a category with surprising
// defaults. Repeated event types retain the historical request-order behavior.
func (s *Store) SetAlertPrefs(updates []AlertPrefUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, update := range updates {
		var show, email interface{}
		if update.ShowInCenter != nil {
			show = boolToInt(*update.ShowInCenter)
		}
		if update.EmailEnabled != nil {
			email = boolToInt(*update.EmailEnabled)
		}
		res, err := tx.Exec(
			`UPDATE alert_prefs
			 SET show_in_center=COALESCE(?, show_in_center),
			     email_enabled=COALESCE(?, email_enabled), updated_at=?
			 WHERE event_type=?`,
			show, email, now, update.EventType,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("unknown alert event type %q", update.EventType)
		}
	}
	return tx.Commit()
}

// IsAlertShownInCenter reports whether events of the type are recorded in the
// alert center. Missing rows default to true.
func (s *Store) IsAlertShownInCenter(eventType string) bool {
	return s.alertPrefFlag(eventType, "show_in_center", true)
}

// IsAlertEmailEnabled reports whether events of the type trigger emails.
// Missing rows default to true.
func (s *Store) IsAlertEmailEnabled(eventType string) bool {
	return s.alertPrefFlag(eventType, "email_enabled", true)
}

func (s *Store) alertPrefFlag(eventType, column string, def bool) bool {
	var v int
	err := s.db.QueryRow(
		`SELECT `+column+` FROM alert_prefs WHERE event_type=?`,
		eventType,
	).Scan(&v)
	if err != nil {
		return def
	}
	return v != 0
}

// ListAlertPrefs returns all pref rows (empty when nothing was seeded yet).
func (s *Store) ListAlertPrefs() ([]*AlertPref, error) {
	rows, err := s.db.Query(
		`SELECT event_type, show_in_center, email_enabled, updated_at FROM alert_prefs ORDER BY event_type`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AlertPref
	for rows.Next() {
		var p AlertPref
		var sc, ec int
		if err := rows.Scan(&p.EventType, &sc, &ec, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.ShowInCenter = sc != 0
		p.EmailEnabled = ec != 0
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ListAdminAlerts returns alerts newest-first with offset-based pagination.
func (s *Store) ListAdminAlerts(limit, offset int) ([]*AdminAlert, int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(
		`SELECT a.id, a.type, a.message, a.request_log_id, a.donation_id, a.created_at, rl.user_id
		 FROM admin_alerts a
		 LEFT JOIN request_logs rl ON rl.id = a.request_log_id
		 ORDER BY a.created_at DESC LIMIT ? OFFSET ?`,
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

// PurgeAlertsForExpiredLogs is the legacy alert-only cleanup API. It deletes
// alerts bound to any expired request log, including donation logs. Unbound
// alerts are intentionally retained for manual review.
// Deprecated: use PurgeExpiredRequestLogs for atomic alert and log cleanup.
func (s *Store) PurgeAlertsForExpiredLogs(now int64) (int64, error) {
	cutoff := now - int64(RequestLogRetention.Seconds())
	res, err := s.db.Exec(
		`DELETE FROM admin_alerts WHERE request_log_id IN (
			SELECT id FROM request_logs WHERE started_at < ?
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
