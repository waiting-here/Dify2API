package db

import (
	"time"
)

// HasActiveDonationForService reports whether the user has an active
// donation entry for a template service (B' regeneration gate).
func (s *Store) HasActiveDonationForService(userID int64, service string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM donations
		WHERE source_user_id=? AND service=? AND status=?`,
		userID, service, DonationActive).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// HasInactiveDonationForService reports whether the user has any inactive
// donation entries or pending applications for a template service. These
// become invalid when the user regenerates the donation App.
func (s *Store) HasInactiveDonationForService(userID int64, service string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT
		(SELECT COUNT(1) FROM donations WHERE source_user_id=? AND service=? AND status IN (?,?) )
		+ (SELECT COUNT(1) FROM donation_applications WHERE user_id=? AND service=? AND status IN (?,?) )`,
		userID, service, DonationInactive, DonationExpired,
		userID, service, AppStatusPending, AppStatusApproved).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InvalidateDonationsForService atomically invalidates pending applications
// and expired donations for the user-side B' recovery path. Active donations
// are never touched here — they remain admin-only.
func (s *Store) InvalidateDonationsForService(userID int64, service string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE donation_applications SET status=?, review_note='auto-invalidated by template regeneration'
		WHERE user_id=? AND service=? AND status=?`, AppStatusInvalidated, userID, service, AppStatusPending); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE donations SET status=?, updated_at=? WHERE source_user_id=? AND service=? AND status=?`,
		DonationInactive, now.Unix(), userID, service, DonationExpired); err != nil {
		return err
	}
	return tx.Commit()
}
