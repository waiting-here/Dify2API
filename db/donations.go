package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	DifyAPIKeySHA256    string
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
	RpmLimit            int
	Status              string
	Note                string
	CreatedAt           int64
	UpdatedAt           int64
}

// DonationDeleteError identifies an expected delete conflict. Batch callers
// use DonationID to report the item that caused the transaction to roll back.
type DonationDeleteError struct {
	DonationID int64
	InFlight   int
	NotFound   bool
}

func (e *DonationDeleteError) Error() string {
	if e.NotFound {
		return fmt.Sprintf("donation %d not found", e.DonationID)
	}
	return fmt.Sprintf("donation %d has %d in-flight charity reservation(s)", e.DonationID, e.InFlight)
}

func scanDonation(row interface{ Scan(...interface{}) error }) (*Donation, error) {
	var d Donation
	if err := row.Scan(
		&d.ID, &d.Service, &d.Model, &d.DifyBaseURL, &d.DifyAPIKeyEnc,
		&d.DifyAPIKeySHA256,
		&d.SourceUserID, &d.SourceDiscordID, &d.SourceUsername, &d.SourceText,
		&d.Deadline, &d.TotalCount, &d.RemainingCount,
		&d.SuccessCount, &d.FailureCount, &d.ConsecutiveFailures,
		&d.RpmLimit,
		&d.Status, &d.Note, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &d, nil
}

// CreateDonation inserts a new donation entry, encrypting the API key.
// Validates totalCount > 0 and deadline > 0. remainingCount starts at totalCount.
// rpmLimit defaults to 10 when <= 0.
func (s *Store) CreateDonation(d *Donation, apiKeyPlain string) (*Donation, error) {
	if d.TotalCount <= 0 {
		return nil, fmt.Errorf("total_count must be positive, got %d", d.TotalCount)
	}
	if d.Deadline <= 0 {
		return nil, fmt.Errorf("deadline must be a positive unix timestamp, got %d", d.Deadline)
	}
	rpmLimit := d.RpmLimit
	if rpmLimit <= 0 {
		rpmLimit = 10
	}
	status := d.Status
	if status == "" {
		status = DonationActive
	}

	enc, err := s.Encrypt(apiKeyPlain)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	// Compute SHA-256 of the plaintext API key for duplicate detection.
	sum := sha256.Sum256([]byte(apiKeyPlain))
	keySHA256 := hex.EncodeToString(sum[:])

	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO donations (service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count, rpm_limit, status, note,
		 created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.Service, d.Model, d.DifyBaseURL, enc, keySHA256,
		d.SourceUserID, d.SourceDiscordID, d.SourceUsername, d.SourceText,
		d.Deadline, d.TotalCount, d.TotalCount, rpmLimit, status, d.Note,
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
		`SELECT id, service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 rpm_limit,
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
		`SELECT id, service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 rpm_limit,
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

// ListRoutableDonations returns active donations for a service+model pair
// that have not expired and still have remaining capacity.
func (s *Store) ListRoutableDonations(service, model string) ([]*Donation, error) {
	now := time.Now().Unix()
	rows, err := s.db.Query(
		`SELECT id, service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count,
		 success_count, failure_count, consecutive_failures,
		 rpm_limit,
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

// DonationStatusError identifies an expected failed_id validation error in a
// batch status update.
type DonationStatusError struct {
	DonationID    int64
	NotFound      bool
	Expired       bool
	PricingAbsent bool
	Service       string
	Model         string
}

func (e *DonationStatusError) Error() string {
	switch {
	case e.NotFound:
		return fmt.Sprintf("捐赠条目 %d 不存在", e.DonationID)
	case e.Expired:
		return fmt.Sprintf("已失效的捐赠条目 %d 不可更改状态", e.DonationID)
	case e.PricingAbsent:
		return fmt.Sprintf("捐赠条目 %d 的模型 (%s, %s) 尚未设定价格，请先在定价表中添加该组合后再激活",
			e.DonationID, e.Service, e.Model)
	default:
		return fmt.Sprintf("捐赠条目 %d 状态不可更改", e.DonationID)
	}
}

// BatchSetDonationStatus validates and updates a complete selection in one
// transaction. Duplicate IDs retain request-order/count semantics; missing,
// expired, or unpriced activation targets roll back the entire batch.
func (s *Store) BatchSetDonationStatus(ids []int64, status string) error {
	if status != DonationActive && status != DonationInactive {
		return fmt.Errorf("status must be active or inactive")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var current, service, model string
		if err := tx.QueryRow(
			`SELECT status, service, model FROM donations WHERE id=?`, id,
		).Scan(&current, &service, &model); err != nil {
			if err == sql.ErrNoRows {
				return &DonationStatusError{DonationID: id, NotFound: true}
			}
			return err
		}
		if current == DonationExpired {
			return &DonationStatusError{DonationID: id, Expired: true}
		}
		if status == DonationActive {
			var pricingCount int
			if err := tx.QueryRow(
				`SELECT COUNT(1) FROM charity_pricing WHERE service=? AND model=?`, service, model,
			).Scan(&pricingCount); err != nil {
				return err
			}
			if pricingCount == 0 {
				return &DonationStatusError{
					DonationID: id, PricingAbsent: true, Service: service, Model: model,
				}
			}
		}
	}
	now := time.Now().Unix()
	for _, id := range ids {
		var err error
		if status == DonationActive {
			_, err = tx.Exec(
				`UPDATE donations SET status=?, consecutive_failures=0, updated_at=? WHERE id=?`, status, now, id,
			)
		} else {
			_, err = tx.Exec(`UPDATE donations SET status=?, updated_at=? WHERE id=?`, status, now, id)
		}
		if err != nil {
			return fmt.Errorf("set donation %d status: %w", id, err)
		}
	}
	return tx.Commit()
}

// DeleteDonation removes a donation entry. Related request_logs and alerts
// remain available until the same per-row 30-day retention cutoff as all
// other request metadata; donation_id may therefore become temporarily orphaned.
func (s *Store) DeleteDonation(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteDonationTx(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteDonations removes all IDs in one transaction. Missing/duplicate IDs
// and active reservations roll back every deletion.
func (s *Store) DeleteDonations(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if err := deleteDonationTx(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func deleteDonationTx(tx *sql.Tx, id int64) error {
	var active int
	if err := tx.QueryRow(
		`SELECT COUNT(1) FROM charity_reservations WHERE donation_id=? AND status IN (?,?)`,
		id, ReservationReserved, ReservationDispatched,
	).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return &DonationDeleteError{DonationID: id, InFlight: active}
	}
	res, err := tx.Exec(`DELETE FROM donations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if n != 1 {
		return &DonationDeleteError{DonationID: id, NotFound: true}
	}
	return nil
}

// RecordDonationSuccess is retained for low-level compatibility tests.
// Deprecated: live charity routing must use Reserve/CommitCharityReservation
// so donation capacity and consumer credits are claimed in one transaction.
// It atomically decrements remaining_count, increments success_count, resets
// consecutive_failures, and flips status to expired
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
