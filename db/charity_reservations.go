package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	ReservationReserved   = "reserved"
	ReservationDispatched = "dispatched"
	ReservationCommitted  = "committed"
	ReservationReleased   = "released"
)

var (
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrDonationUnavailable = errors.New("donation is no longer routable")
)

// CharityReservation is the durable accounting state for one charity call.
// Price/reward and donor identity are snapshots taken before the request is
// dispatched, so later pricing or donation edits cannot change settlement.
type CharityReservation struct {
	ID          string
	UserID      int64
	DonationID  int64
	DonorUserID sql.NullInt64
	Price       int
	Reward      int
	Status      string
	CreatedAt   int64
	UpdatedAt   int64
}

func scanCharityReservation(row interface{ Scan(...interface{}) error }) (*CharityReservation, error) {
	var r CharityReservation
	if err := row.Scan(&r.ID, &r.UserID, &r.DonationID, &r.DonorUserID, &r.Price, &r.Reward, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func reservationID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ReserveCharityCall atomically claims one donation use and debits the
// consumer. No upstream request may be sent before this succeeds.
func (s *Store) ReserveCharityCall(ctx context.Context, userID, donationID int64, price, reward int) (*CharityReservation, error) {
	if price < 0 || reward < 0 {
		return nil, fmt.Errorf("price and reward must be non-negative")
	}
	id, err := reservationID()
	if err != nil {
		return nil, fmt.Errorf("generate reservation id: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE donations
		 SET remaining_count=remaining_count-1,
		     status=CASE WHEN remaining_count-1<=0 THEN 'expired' ELSE status END,
		     updated_at=?
		 WHERE id=? AND status='active' AND deadline>? AND remaining_count>0`,
		now, donationID, now,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrDonationUnavailable
	}

	var donorUserID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT source_user_id FROM donations WHERE id=?`, donationID).Scan(&donorUserID); err != nil {
		return nil, err
	}
	res, err = tx.ExecContext(ctx,
		`UPDATE users SET credits=credits-?, updated_at=? WHERE id=? AND credits>=?`,
		price, now, userID, price,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrInsufficientCredits
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO charity_reservations
		 (id,user_id,donation_id,donor_user_id,price,reward,status,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?, ?,?)`,
		id, userID, donationID, donorUserID, price, reward, ReservationReserved, now, now,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CharityReservation{
		ID: id, UserID: userID, DonationID: donationID, DonorUserID: donorUserID,
		Price: price, Reward: reward, Status: ReservationReserved, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// MarkCharityDispatched durably records that the donated credential is about
// to be used. Startup recovery conservatively commits this state.
func (s *Store) MarkCharityDispatched(ctx context.Context, id string) error {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE charity_reservations SET status=?, updated_at=? WHERE id=? AND status=?`,
		ReservationDispatched, now, id, ReservationReserved,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	r, err := s.GetCharityReservation(id)
	if err != nil {
		return err
	}
	if r != nil && r.Status == ReservationDispatched {
		return nil
	}
	return fmt.Errorf("reservation %s cannot be dispatched", id)
}

func (s *Store) GetCharityReservation(id string) (*CharityReservation, error) {
	r, err := scanCharityReservation(s.db.QueryRow(
		`SELECT id,user_id,donation_id,donor_user_id,price,reward,status,created_at,updated_at
		 FROM charity_reservations WHERE id=?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func getCharityReservationTx(ctx context.Context, tx *sql.Tx, id string) (*CharityReservation, error) {
	return scanCharityReservation(tx.QueryRowContext(ctx,
		`SELECT id,user_id,donation_id,donor_user_id,price,reward,status,created_at,updated_at
		 FROM charity_reservations WHERE id=?`, id,
	))
}

// CommitCharityReservation atomically records successful consumption and
// grants the donor's contribution/reward. It is idempotent.
func (s *Store) CommitCharityReservation(ctx context.Context, id string) (*CharityReservation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := getCharityReservationTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	switch r.Status {
	case ReservationCommitted:
		return r, nil
	case ReservationReleased:
		return nil, fmt.Errorf("reservation %s was already released", id)
	case ReservationDispatched:
	default:
		return nil, fmt.Errorf("reservation %s has invalid status %q", id, r.Status)
	}

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE donations
		 SET success_count=success_count+1, consecutive_failures=0, updated_at=?
		 WHERE id=?`, now, r.DonationID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("reservation %s donation %d no longer exists", id, r.DonationID)
	}
	if r.DonorUserID.Valid {
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET donation_credit=donation_credit+1, credits=credits+?, updated_at=? WHERE id=?`,
			r.Reward, now, r.DonorUserID.Int64,
		); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE charity_reservations SET status=?, updated_at=? WHERE id=?`,
		ReservationCommitted, now, id,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.Status = ReservationCommitted
	r.UpdatedAt = now
	return r, nil
}

// ReleaseCharityReservation atomically refunds the consumer and restores the
// reserved donation use. When countFailure is true it also records a donor
// endpoint failure and returns the new consecutive-failure count.
func (s *Store) ReleaseCharityReservation(ctx context.Context, id string, countFailure bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	r, err := getCharityReservationTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	switch r.Status {
	case ReservationReleased:
		return 0, nil
	case ReservationCommitted:
		return 0, fmt.Errorf("reservation %s was already committed", id)
	case ReservationReserved, ReservationDispatched:
	default:
		return 0, fmt.Errorf("reservation %s has invalid status %q", id, r.Status)
	}

	now := time.Now().Unix()
	failureInc := 0
	if countFailure {
		failureInc = 1
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE donations
		 SET remaining_count=remaining_count+1,
		     failure_count=failure_count+?,
		     consecutive_failures=consecutive_failures+?,
		     status=CASE
		       WHEN status='expired' AND remaining_count=0 AND deadline>? THEN 'active'
		       ELSE status END,
		     updated_at=?
		 WHERE id=?`,
		failureInc, failureInc, now, now, r.DonationID,
	)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, fmt.Errorf("reservation %s donation %d no longer exists", id, r.DonationID)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET credits=credits+?, updated_at=? WHERE id=?`, r.Price, now, r.UserID,
	); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE charity_reservations SET status=?, updated_at=? WHERE id=?`,
		ReservationReleased, now, id,
	); err != nil {
		return 0, err
	}
	consecutive := 0
	if countFailure {
		if err := tx.QueryRowContext(ctx, `SELECT consecutive_failures FROM donations WHERE id=?`, r.DonationID).Scan(&consecutive); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return consecutive, nil
}

// RecoverCharityReservations resolves states left by an interrupted process:
// never-dispatched reservations are refunded, while dispatched calls are
// conservatively committed to prevent a consumed donor call becoming free.
func (s *Store) RecoverCharityReservations(ctx context.Context) (released, committed int, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,status FROM charity_reservations WHERE status IN (?,?) ORDER BY created_at`,
		ReservationReserved, ReservationDispatched,
	)
	if err != nil {
		return 0, 0, err
	}
	type pending struct{ id, status string }
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.status); err != nil {
			rows.Close()
			return 0, 0, err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	for _, p := range list {
		if p.status == ReservationReserved {
			if _, err := s.ReleaseCharityReservation(ctx, p.id, false); err != nil {
				return released, committed, err
			}
			released++
		} else {
			if _, err := s.CommitCharityReservation(ctx, p.id); err != nil {
				return released, committed, err
			}
			committed++
		}
	}
	return released, committed, nil
}

func (s *Store) ListUserCharityReservations(userID int64) ([]*CharityReservation, error) {
	rows, err := s.db.Query(
		`SELECT id,user_id,donation_id,donor_user_id,price,reward,status,created_at,updated_at
		 FROM charity_reservations WHERE user_id=? OR donor_user_id=? ORDER BY created_at DESC`,
		userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CharityReservation
	for rows.Next() {
		r, err := scanCharityReservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PurgeSettledCharityReservations(before int64) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM charity_reservations WHERE status IN (?,?) AND updated_at<?`,
		ReservationCommitted, ReservationReleased, before,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
