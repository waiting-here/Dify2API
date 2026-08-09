package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DonationApplication status constants.
const (
	AppStatusPending  = "pending"
	AppStatusApproved = "approved"
	AppStatusRejected = "rejected"
)

// ErrPendingApplicationLimit is returned when an atomic application insert
// would exceed the user's configured pending-review limit.
var ErrPendingApplicationLimit = errors.New("pending donation application limit reached")

// ApplicationDeadlineError reports an approval whose effective deadline has
// already elapsed. The check lives inside the approval transaction so batch
// and future callers cannot bypass the HTTP-layer validation.
type ApplicationDeadlineError struct {
	ApplicationID int64
	Deadline      int64
}

func (e *ApplicationDeadlineError) Error() string {
	return fmt.Sprintf("application %d deadline has expired", e.ApplicationID)
}

// ApplicationReviewError identifies an application that could not be claimed
// for review because it is absent or no longer pending. Batch callers use the
// ID to report the item that caused the whole transaction to roll back.
type ApplicationReviewError struct {
	ApplicationID int64
	Status        string
	NotFound      bool
}

func (e *ApplicationReviewError) Error() string {
	if e.NotFound {
		return fmt.Sprintf("application %d not found", e.ApplicationID)
	}
	return fmt.Sprintf("application %d is not pending (current: %s)", e.ApplicationID, e.Status)
}

// DonationApplication represents a user-submitted donation application.
type DonationApplication struct {
	ID            int64
	UserID        int64
	Service       string
	Model         string
	DifyBaseURL   string
	DifyAPIKeyEnc string
	TotalCount    int
	Deadline      int64
	RpmLimit      int
	Note          string
	Status        string
	ReviewerID    sql.NullInt64
	ReviewNote    string
	DonationID    sql.NullInt64
	CreatedAt     int64
	// Joined fields (populated by list queries).
	Username       string // applicant username
	DiscordID      string // applicant discord_id
	RemainingCount *int   // donation remaining count (when approved)
}

func scanDonationApplication(row interface{ Scan(...interface{}) error }) (*DonationApplication, error) {
	var a DonationApplication
	if err := row.Scan(
		&a.ID, &a.UserID, &a.Service, &a.Model, &a.DifyBaseURL, &a.DifyAPIKeyEnc,
		&a.TotalCount, &a.Deadline, &a.RpmLimit, &a.Note, &a.Status,
		&a.ReviewerID, &a.ReviewNote, &a.DonationID, &a.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateDonationApplication inserts a new pending application with encrypted API key.
// rpmLimit defaults to 10 when <= 0.
func (s *Store) CreateDonationApplication(userID int64, service, model, difyBaseURL, difyAPIKey string, totalCount int, deadline int64, rpmLimit int, note string) (*DonationApplication, error) {
	return s.CreateDonationApplicationWithLimit(userID, service, model, difyBaseURL, difyAPIKey, totalCount, deadline, rpmLimit, note, -1)
}

// CreateDonationApplicationWithLimit inserts a pending application only when
// the user's current pending count is below pendingLimit. The count predicate
// and INSERT are one SQLite statement, so concurrent submissions cannot both
// pass a separate count-then-insert check. A negative limit disables the cap
// for trusted internal callers. A zero limit rejects all submissions,
// matching the existing administrator setting semantics.
func (s *Store) CreateDonationApplicationWithLimit(userID int64, service, model, difyBaseURL, difyAPIKey string, totalCount int, deadline int64, rpmLimit int, note string, pendingLimit int) (*DonationApplication, error) {
	enc, err := s.Encrypt(difyAPIKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}

	if rpmLimit <= 0 {
		rpmLimit = 10
	}

	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO donation_applications (user_id, service, model, dify_base_url, dify_api_key_enc,
		 total_count, deadline, rpm_limit, note, status, created_at)
		 SELECT ?,?,?,?,?,?,?,?,?,?,?
		 WHERE ? < 0 OR (
			SELECT COUNT(1) FROM donation_applications WHERE user_id=? AND status=?
		 ) < ?`,
		userID, service, model, difyBaseURL, enc,
		totalCount, deadline, rpmLimit, note, AppStatusPending, now,
		pendingLimit, userID, AppStatusPending, pendingLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return nil, fmt.Errorf("create application rows affected: %w", rowsErr)
	} else if n != 1 {
		return nil, ErrPendingApplicationLimit
	}
	id, _ := res.LastInsertId()
	return s.GetApplication(id)
}

// GetApplication fetches a donation application by ID. Returns (nil, nil) when absent.
func (s *Store) GetApplication(id int64) (*DonationApplication, error) {
	a, err := scanDonationApplication(s.db.QueryRow(
		`SELECT id, user_id, service, model, dify_base_url, dify_api_key_enc,
		 total_count, deadline, rpm_limit, note, status,
		 reviewer_id, review_note, donation_id, created_at
		 FROM donation_applications WHERE id=?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// ListApplicationsByUser returns all applications for a user, newest first.
func (s *Store) ListApplicationsByUser(userID int64) ([]*DonationApplication, error) {
	rows, err := s.db.Query(
		`SELECT da.id, da.user_id, da.service, da.model, da.dify_base_url, da.dify_api_key_enc,
		 da.total_count, da.deadline, da.rpm_limit, da.note, da.status,
		 da.reviewer_id, da.review_note, da.donation_id, da.created_at
		 FROM donation_applications da
		 WHERE da.user_id=?
		 ORDER BY da.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DonationApplication
	for rows.Next() {
		a, err := scanDonationApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListPendingApplications returns all pending applications with applicant username/discord_id, newest first.
func (s *Store) ListPendingApplications() ([]*DonationApplication, error) {
	rows, err := s.db.Query(
		`SELECT da.id, da.user_id, da.service, da.model, da.dify_base_url, da.dify_api_key_enc,
		 da.total_count, da.deadline, da.rpm_limit, da.note, da.status,
		 da.reviewer_id, da.review_note, da.donation_id, da.created_at,
		 u.username, u.discord_id
		 FROM donation_applications da
		 JOIN users u ON u.id = da.user_id
		 WHERE da.status=?
		 ORDER BY da.created_at DESC`,
		AppStatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DonationApplication
	for rows.Next() {
		var a DonationApplication
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Service, &a.Model, &a.DifyBaseURL, &a.DifyAPIKeyEnc,
			&a.TotalCount, &a.Deadline, &a.RpmLimit, &a.Note, &a.Status,
			&a.ReviewerID, &a.ReviewNote, &a.DonationID, &a.CreatedAt,
			&a.Username, &a.DiscordID,
		); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// CountPendingByUser returns the number of pending applications for a user.
func (s *Store) CountPendingByUser(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM donation_applications WHERE user_id=? AND status=?`,
		userID, AppStatusPending,
	).Scan(&n)
	return n, err
}

// ApproveApplicationFields holds optional modifications the reviewer can make.
// Zero/empty fields mean "keep the original value from the application".
type ApproveApplicationFields struct {
	Service     string `json:"service"`
	Model       string `json:"model"`
	DifyBaseURL string `json:"dify_base_url"`
	DifyAPIKey  string `json:"dify_api_key"` // plaintext, re-encrypted
	TotalCount  int    `json:"total_count"`
	Deadline    int64  `json:"deadline"`
	RpmLimit    int    `json:"rpm_limit"`
}

// ApproveApplication approves a pending application, creates a donation entry
// (inactive), and updates the application status to approved.
// modifiedFields may be partially populated; empty/zero fields retain the
// original application values.
func (s *Store) ApproveApplication(id int64, reviewerID int64, m *ApproveApplicationFields, reviewNote string) (*DonationApplication, *Donation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	app, donation, err := s.approveApplicationTx(tx, id, reviewerID, m, reviewNote)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return app, donation, nil
}

// ApproveApplications approves every ID in one transaction. Any missing,
// duplicate, or already-final application rolls back all prior work.
func (s *Store) ApproveApplications(ids []int64, reviewerID int64, reviewNote string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, _, err := s.approveApplicationTx(tx, id, reviewerID, &ApproveApplicationFields{}, reviewNote); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) approveApplicationTx(tx *sql.Tx, id int64, reviewerID int64, m *ApproveApplicationFields, reviewNote string) (*DonationApplication, *Donation, error) {
	if m == nil {
		m = &ApproveApplicationFields{}
	}
	now := time.Now().Unix()

	// Claim the pending row before reading or creating the donation. If any
	// later step fails, the transaction restores the pending state.
	claim, err := tx.Exec(
		`UPDATE donation_applications
		 SET status=?, reviewer_id=?, review_note=?
		 WHERE id=? AND status=?`,
		AppStatusApproved, reviewerID, reviewNote, id, AppStatusPending,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("claim application for approval: %w", err)
	}
	if n, rowsErr := claim.RowsAffected(); rowsErr != nil {
		return nil, nil, rowsErr
	} else if n != 1 {
		return nil, nil, applicationReviewStateError(tx, id)
	}

	app, err := scanDonationApplication(tx.QueryRow(
		`SELECT id, user_id, service, model, dify_base_url, dify_api_key_enc,
		 total_count, deadline, rpm_limit, note, status,
		 reviewer_id, review_note, donation_id, created_at
		 FROM donation_applications WHERE id=?`, id,
	))
	if err != nil {
		return nil, nil, err
	}

	// Merge modified fields with originals.
	service := app.Service
	model := app.Model
	baseURL := app.DifyBaseURL
	apiKeyEnc := app.DifyAPIKeyEnc
	totalCount := app.TotalCount
	deadline := app.Deadline
	rpmLimit := app.RpmLimit

	if m.Service != "" {
		service = m.Service
	}
	if m.Model != "" {
		model = m.Model
	}
	if m.DifyBaseURL != "" {
		baseURL = m.DifyBaseURL
	}
	if m.DifyAPIKey != "" {
		enc, encErr := s.Encrypt(m.DifyAPIKey)
		if encErr != nil {
			return nil, nil, fmt.Errorf("encrypt modified api key: %w", encErr)
		}
		apiKeyEnc = enc
	}
	if m.TotalCount > 0 {
		totalCount = m.TotalCount
	}
	if m.Deadline > 0 {
		deadline = m.Deadline
	}
	if m.RpmLimit > 0 {
		rpmLimit = m.RpmLimit
	}
	if deadline <= now {
		return nil, nil, &ApplicationDeadlineError{ApplicationID: id, Deadline: deadline}
	}

	// Compute SHA-256 of the plaintext API key for duplicate detection.
	// We need the plaintext key at this point; it's stored in the application
	// or may have been modified by the reviewer.
	plainKey := m.DifyAPIKey
	if plainKey == "" {
		// Reviewer did not modify the key; decrypt the original.
		plainKey, err = s.Decrypt(app.DifyAPIKeyEnc)
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt application api key: %w", err)
		}
	}
	keySHA256 := ""
	if plainKey != "" {
		sum := sha256.Sum256([]byte(plainKey))
		keySHA256 = hex.EncodeToString(sum[:])
	}

	// Create donation entry (inactive).
	// Look up the applicant's Discord ID and username for the source fields.
	var sourceDiscordID, sourceUsername string
	if err := tx.QueryRow(`SELECT discord_id, username FROM users WHERE id=?`, app.UserID).Scan(&sourceDiscordID, &sourceUsername); err != nil {
		return nil, nil, fmt.Errorf("load application user: %w", err)
	}

	res, err := tx.Exec(
		`INSERT INTO donations (service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count, rpm_limit, status, note,
		 created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		service, model, baseURL, apiKeyEnc, keySHA256,
		sql.NullInt64{Int64: app.UserID, Valid: true}, sourceDiscordID, sourceUsername, "",
		deadline, totalCount, totalCount, rpmLimit, DonationInactive, app.Note,
		now, now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create donation from application: %w", err)
	}
	donationID, _ := res.LastInsertId()

	// Link the claimed application to the donation. The condition protects
	// against accidental double-linking inside future callers.
	link, err := tx.Exec(
		`UPDATE donation_applications
		 SET donation_id=?
		 WHERE id=? AND status=? AND donation_id IS NULL`,
		donationID, id, AppStatusApproved,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("link approved application: %w", err)
	}
	if n, rowsErr := link.RowsAffected(); rowsErr != nil {
		return nil, nil, rowsErr
	} else if n != 1 {
		return nil, nil, fmt.Errorf("link approved application %d: row changed concurrently", id)
	}

	app.DonationID = sql.NullInt64{Int64: donationID, Valid: true}
	donation := &Donation{
		ID: donationID, Service: service, Model: model, DifyBaseURL: baseURL,
		DifyAPIKeyEnc: apiKeyEnc, DifyAPIKeySHA256: keySHA256,
		SourceUserID:    sql.NullInt64{Int64: app.UserID, Valid: true},
		SourceDiscordID: sourceDiscordID, SourceUsername: sourceUsername,
		Deadline: deadline, TotalCount: totalCount, RemainingCount: totalCount,
		RpmLimit: rpmLimit, Status: DonationInactive, Note: app.Note,
		CreatedAt: now, UpdatedAt: now,
	}
	return app, donation, nil
}

// RejectApplication rejects a pending application with an optional review note.
func (s *Store) RejectApplication(id int64, reviewerID int64, reviewNote string) (*DonationApplication, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	app, err := rejectApplicationTx(tx, id, reviewerID, reviewNote)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return app, nil
}

// RejectApplications rejects every ID in one transaction.
func (s *Store) RejectApplications(ids []int64, reviewerID int64, reviewNote string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := rejectApplicationTx(tx, id, reviewerID, reviewNote); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func rejectApplicationTx(tx *sql.Tx, id int64, reviewerID int64, reviewNote string) (*DonationApplication, error) {
	res, err := tx.Exec(
		`UPDATE donation_applications
		 SET status=?, reviewer_id=?, review_note=?
		 WHERE id=? AND status=?`,
		AppStatusRejected, reviewerID, reviewNote, id, AppStatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf("claim application for rejection: %w", err)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil {
		return nil, rowsErr
	} else if n != 1 {
		return nil, applicationReviewStateError(tx, id)
	}
	return scanDonationApplication(tx.QueryRow(
		`SELECT id, user_id, service, model, dify_base_url, dify_api_key_enc,
		 total_count, deadline, rpm_limit, note, status,
		 reviewer_id, review_note, donation_id, created_at
		 FROM donation_applications WHERE id=?`, id,
	))
}

func applicationReviewStateError(tx *sql.Tx, id int64) error {
	var status string
	err := tx.QueryRow(`SELECT status FROM donation_applications WHERE id=?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return &ApplicationReviewError{ApplicationID: id, NotFound: true}
	}
	if err != nil {
		return err
	}
	return &ApplicationReviewError{ApplicationID: id, Status: status}
}

// UpdateDonationReviewNote updates the review_note for a donation's originating application.
// Does nothing (no error) when the donation has no corresponding application record.
func (s *Store) UpdateDonationReviewNote(donationID int64, note string) error {
	_, err := s.db.Exec(
		`UPDATE donation_applications SET review_note=? WHERE donation_id=?`,
		note, donationID,
	)
	return err
}

// ListApplicationsFiltered returns applications matching optional filters with pagination.
// status empty = all (pending/approved/rejected); userID > 0 = exact match;
// service non-empty = exact match; since/until > 0 = created_at range.
// limit default 100, max 500. Returns the total matching count for pagination.
func (s *Store) ListApplicationsFiltered(status string, userID int64, service string, since, until int64, limit, offset int) ([]*DonationApplication, int, error) {
	var conds []string
	var args []interface{}

	if status != "" {
		conds = append(conds, "da.status = ?")
		args = append(args, status)
	}
	if userID > 0 {
		conds = append(conds, "da.user_id = ?")
		args = append(args, userID)
	}
	if service != "" {
		conds = append(conds, "da.service = ?")
		args = append(args, service)
	}
	if since > 0 {
		conds = append(conds, "da.created_at >= ?")
		args = append(args, since)
	}
	if until > 0 {
		conds = append(conds, "da.created_at <= ?")
		args = append(args, until)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	// Total count.
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM donation_applications da`+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Clamp limit.
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT da.id, da.user_id, da.service, da.model, da.dify_base_url, da.dify_api_key_enc,
		da.total_count, da.deadline, da.rpm_limit, da.note, da.status,
		da.reviewer_id, da.review_note, da.donation_id, da.created_at,
		u.username, u.discord_id
		FROM donation_applications da
		LEFT JOIN users u ON u.id = da.user_id` +
		where + ` ORDER BY da.created_at DESC LIMIT ? OFFSET ?`

	allArgs := append(args, limit, offset)
	rows, err := s.db.Query(query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*DonationApplication
	for rows.Next() {
		var a DonationApplication
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Service, &a.Model, &a.DifyBaseURL, &a.DifyAPIKeyEnc,
			&a.TotalCount, &a.Deadline, &a.RpmLimit, &a.Note, &a.Status,
			&a.ReviewerID, &a.ReviewNote, &a.DonationID, &a.CreatedAt,
			&a.Username, &a.DiscordID,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, &a)
	}
	return out, total, rows.Err()
}

// GetReviewNotesByDonationIDs returns a map of donation_id → review_note for
// every donation that originated from an application. Empty notes remain in
// the map so callers can distinguish an empty review record from no record.
func (s *Store) GetReviewNotesByDonationIDs(ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT donation_id, review_note FROM donation_applications
		WHERE donation_id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]string)
	for rows.Next() {
		var donID int64
		var note string
		if err := rows.Scan(&donID, &note); err != nil {
			return nil, err
		}
		out[donID] = note
	}
	return out, rows.Err()
}
