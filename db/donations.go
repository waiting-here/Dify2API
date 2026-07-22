package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Donation status constants.
const (
	DonationActive   = "active"
	DonationInactive = "inactive"
	DonationExpired  = "expired"
)

// Donation represents one donated Dify App entry in the public-resource pool.
type Donation struct {
	ID                  int64
	Service             string
	Model               string
	DifyBaseURL         string
	DifyAPIKeyEnc       string
	SourceUserID        sql.NullInt64
	SourceDiscordID     string
	SourceUsername      string
	SourceText          string
	Deadline            int64
	TotalCount          int
	RemainingCount      int
	SuccessCount        int
	FailureCount        int
	ConsecutiveFailures int
	Status              string
	Note                string
	CreatedAt           int64
	UpdatedAt           int64
}

func scanDonation(row interface{ Scan(...interface{}) error }) (*Donation, error) {
	var d Donation
	if err := row.Scan(
		&d.ID, &d.Service, &d.Model, &d.DifyBaseURL, &d.DifyAPIKeyEnc,
		&d.SourceUserID, &d.SourceDiscordID, &d.SourceUsername, &d.SourceText,
		&d.Deadline, &d.TotalCount, &d.RemainingCount,
		&d.SuccessCount, &d.FailureCount, &d.ConsecutiveFailures,
		&d.Status, &d.Note, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDonation inserts a new donation entry, encrypting the API key.
// Validates totalCount > 0 and deadline > 0. remainingCount starts at totalCount.
func (s *Store) CreateDonation(d *Donation, apiKeyPlain string) (*Donation, error) {
	if d.TotalCount <= 0 {
		return nil, fmt.Errorf("total_count must be positive, got %d", d.TotalCount)
	}
	if d.Deadline <= 0 {
		return nil, fmt.Errorf("deadline must be a positive unix timestamp, got %d", d.Deadline)
	}

	enc, err := s.Encrypt(apiKeyPlain)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO donations (service, model, dify_base_url, dify_api_key_enc,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count, status, note,
		 created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.Service, d.Model, d.DifyBaseURL, enc,
		d.SourceUserID, d.SourceDiscordID, d.SourceUsername, d.SourceText,
		d.Deadline, d.TotalCount, d.TotalCount, DonationActive, d.Note,
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create donation: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetDonation(id)
}

// GetDonation fetches a donation by primary key. Returns (nil, nil) when absent.
func (s *Store) GetDonation(id int64) (*Donation, error) {
	d, err := scanDonation(s.db.QueryRow(
		`SELECT id, service, model, dify_base_url, dify_api_key_enc,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 status, note, created_at, updated_at
		 FROM donations WHERE id=?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// ListDonations returns all donations, newest first.
func (s *Store) ListDonations() ([]*Donation, error) {
	rows, err := s.db.Query(
		`SELECT id, service, model, dify_base_url, dify_api_key_enc,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 status, note, created_at, updated_at
		 FROM donations ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Donation
	for rows.Next() {
		d, err := scanDonation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListRoutableDonationModels returns distinct (service, model) pairs that
// have at least one routable donation (active, not expired, remaining>0).
// Used to synthesise charity model entries for /v1/models.
func (s *Store) ListRoutableDonationModels() ([]struct{ Service, Model string }, error) {
	now := time.Now().Unix()
	rows, err := s.db.Query(
		`SELECT DISTINCT service, model FROM donations
		 WHERE status=? AND deadline > ? AND remaining_count > 0
		 ORDER BY service, model`,
		DonationActive, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct{ Service, Model string }
	for rows.Next() {
		var d struct{ Service, Model string }
		if err := rows.Scan(&d.Service, &d.Model); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListRoutableDonations returns active donations for a service+model pair
// that have not expired and still have remaining capacity.
func (s *Store) ListRoutableDonations(service, model string) ([]*Donation, error) {
	now := time.Now().Unix()
	rows, err := s.db.Query(
		`SELECT id, service, model, dify_base_url, dify_api_key_enc,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 status, note, created_at, updated_at
		 FROM donations
		 WHERE service=? AND model=? AND status=? AND deadline > ? AND remaining_count > 0
		 ORDER BY deadline ASC`,
		service, model, DonationActive, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Donation
	for rows.Next() {
		d, err := scanDonation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDonationStatus updates the status of a donation. When setting to active,
// consecutive_failures is reset to 0 (re-activation clears the failure counter).
func (s *Store) SetDonationStatus(id int64, status string) error {
	now := time.Now().Unix()
	if status == DonationActive {
		_, err := s.db.Exec(
			`UPDATE donations SET status=?, consecutive_failures=0, updated_at=? WHERE id=?`,
			status, now, id,
		)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE donations SET status=?, updated_at=? WHERE id=?`,
		status, now, id,
	)
	return err
}

// DeleteDonation removes a donation entry. Related request_logs and
// admin_alerts rows are intentionally left intact (orphan retention policy
// §8.4#1 — the foreign donation_id becomes dangling).
func (s *Store) DeleteDonation(id int64) error {
	_, err := s.db.Exec(`DELETE FROM donations WHERE id=?`, id)
	return err
}

// RecordDonationSuccess atomically decrements remaining_count, increments
// success_count, resets consecutive_failures, and flips status to expired
// when remaining_count reaches 0. Returns an error when the donation is
// not in a routable state (inactive/expired/no remaining).
func (s *Store) RecordDonationSuccess(id int64) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE donations
		 SET remaining_count = remaining_count - 1,
		     success_count = success_count + 1,
		     consecutive_failures = 0,
		     status = CASE WHEN remaining_count - 1 <= 0 THEN 'expired' ELSE status END,
		     updated_at = ?
		 WHERE id = ? AND status = 'active' AND remaining_count > 0`,
		now, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("donation %d is not routable (inactive, expired, or exhausted)", id)
	}
	return nil
}

// RecordDonationFailure increments failure_count and consecutive_failures.
// Returns the new consecutive_failures value for the caller to decide
// whether to auto-inactivate (≥10).
func (s *Store) RecordDonationFailure(id int64) (consecutive int, err error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE donations
		 SET failure_count = failure_count + 1,
		     consecutive_failures = consecutive_failures + 1,
		     updated_at = ?
		 WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, fmt.Errorf("donation %d not found", id)
	}
	if err := s.db.QueryRow(
		`SELECT consecutive_failures FROM donations WHERE id=?`, id,
	).Scan(&consecutive); err != nil {
		return 0, err
	}
	return consecutive, nil
}

// ExpireOverdueDonations flips any donation whose deadline has passed and
// that is not already expired to 'expired'. Returns the number of rows
// changed.
func (s *Store) ExpireOverdueDonations(now int64) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE donations SET status='expired', updated_at=? WHERE deadline <= ? AND status != 'expired'`,
		now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
