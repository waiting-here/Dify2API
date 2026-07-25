package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// DonationApplication status constants.
const (
	AppStatusPending  = "pending"
	AppStatusApproved = "approved"
	AppStatusRejected = "rejected"
)

// DonationApplication represents a user-submitted donation application.
type DonationApplication struct {
	ID              int64
	UserID          int64
	Service         string
	Model           string
	DifyBaseURL     string
	DifyAPIKeyEnc   string
	TotalCount      int
	Deadline        int64
	RpmLimit        int
	Note            string
	Status          string
	ReviewerID      sql.NullInt64
	ReviewNote      string
	DonationID      sql.NullInt64
	CreatedAt       int64
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
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		userID, service, model, difyBaseURL, enc,
		totalCount, deadline, rpmLimit, note, AppStatusPending, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create application: %w", err)
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
	now := time.Now().Unix()

	// Load application.
	app, err := s.GetApplication(id)
	if err != nil {
		return nil, nil, err
	}
	if app == nil {
		return nil, nil, fmt.Errorf("application %d not found", id)
	}
	if app.Status != AppStatusPending {
		return nil, nil, fmt.Errorf("application %d is not pending (current: %s)", id, app.Status)
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

	// Compute SHA-256 of the plaintext API key for duplicate detection.
	// We need the plaintext key at this point; it's stored in the application
	// or may have been modified by the reviewer.
	plainKey := m.DifyAPIKey
	if plainKey == "" {
		// Reviewer did not modify the key; decrypt the original.
		plainKey, _ = s.Decrypt(app.DifyAPIKeyEnc)
	}
	keySHA256 := ""
	if plainKey != "" {
		sum := sha256.Sum256([]byte(plainKey))
		keySHA256 = hex.EncodeToString(sum[:])
	}

	// Create donation entry (inactive).
	res, err := s.db.Exec(
		`INSERT INTO donations (service, model, dify_base_url, dify_api_key_enc, dify_api_key_sha256,
		 source_user_id, source_discord_id, source_username, source_text,
		 deadline, total_count, remaining_count, rpm_limit, status, note,
		 created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		service, model, baseURL, apiKeyEnc, keySHA256,
		sql.NullInt64{Int64: app.UserID, Valid: true}, "", "", "",
		deadline, totalCount, totalCount, rpmLimit, DonationInactive, app.Note,
		now, now,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create donation from application: %w", err)
	}
	donationID, _ := res.LastInsertId()

	// Update application status.
	_, err = s.db.Exec(
		`UPDATE donation_applications
		 SET status=?, reviewer_id=?, review_note=?, donation_id=?
		 WHERE id=?`,
		AppStatusApproved, reviewerID, reviewNote, donationID, id,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("update application status: %w", err)
	}

	// Refresh application.
	app, err = s.GetApplication(id)
	if err != nil {
		return nil, nil, err
	}

	// Fetch created donation.
	donation, err := s.GetDonation(donationID)
	if err != nil {
		return app, nil, err
	}

	return app, donation, nil
}

// RejectApplication rejects a pending application with an optional review note.
func (s *Store) RejectApplication(id int64, reviewerID int64, reviewNote string) (*DonationApplication, error) {
	// Load application.
	app, err := s.GetApplication(id)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, fmt.Errorf("application %d not found", id)
	}
	if app.Status != AppStatusPending {
		return nil, fmt.Errorf("application %d is not pending (current: %s)", id, app.Status)
	}

	_, err = s.db.Exec(
		`UPDATE donation_applications
		 SET status=?, reviewer_id=?, review_note=?
		 WHERE id=?`,
		AppStatusRejected, reviewerID, reviewNote, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update application status: %w", err)
	}

	return s.GetApplication(id)
}
