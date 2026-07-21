package db

import (
	"database/sql"
	"fmt"
	"time"
)

// AppConfig is one user-owned mapping: model full name -> Dify App credentials.
// The API key is stored encrypted (dify_api_key_enc).
type AppConfig struct {
	ID            int64
	UserID        int64
	Model         string
	DifyBaseURL   string
	DifyAPIKeyEnc string
	Note          string
	Enabled       bool
	CreatedAt     int64
	UpdatedAt     int64
}

func scanAppConfig(row interface{ Scan(...interface{}) error }) (*AppConfig, error) {
	var c AppConfig
	var enabled int
	err := row.Scan(&c.ID, &c.UserID, &c.Model, &c.DifyBaseURL, &c.DifyAPIKeyEnc, &c.Note, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}

const appConfigCols = "id, user_id, model, dify_base_url, dify_api_key_enc, note, enabled, created_at, updated_at"

// CreateAppConfig inserts a model->App mapping for a user (model unique per user).
func (s *Store) CreateAppConfig(userID int64, model, baseURL, apiKey, note string) (*AppConfig, error) {
	enc, err := s.Encrypt(apiKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO app_configs (user_id, model, dify_base_url, dify_api_key_enc, note, enabled, created_at, updated_at) VALUES (?,?,?,?,?,1,?,?)`,
		userID, model, baseURL, enc, note, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create app config: %w", err)
	}
	id, _ := res.LastInsertId()
	return s.GetAppConfig(id)
}

// GetAppConfig fetches by primary key.
func (s *Store) GetAppConfig(id int64) (*AppConfig, error) {
	c, err := scanAppConfig(s.db.QueryRow(`SELECT `+appConfigCols+` FROM app_configs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// GetEnabledAppConfigByModel resolves a user's ENABLED config for a model full
// name (routing path). Returns (nil, nil) when absent or disabled.
func (s *Store) GetEnabledAppConfigByModel(userID int64, model string) (*AppConfig, error) {
	c, err := scanAppConfig(s.db.QueryRow(`SELECT `+appConfigCols+` FROM app_configs WHERE user_id=? AND model=? AND enabled=1`, userID, model))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// ListAppConfigs returns all configs of a user (including disabled), newest first.
func (s *Store) ListAppConfigs(userID int64) ([]*AppConfig, error) {
	rows, err := s.db.Query(`SELECT `+appConfigCols+` FROM app_configs WHERE user_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AppConfig
	for rows.Next() {
		c, err := scanAppConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateAppConfig replaces mutable fields of a user-owned config.
func (s *Store) UpdateAppConfig(id, userID int64, model, baseURL, apiKey, note string) error {
	enc, err := s.Encrypt(apiKey)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE app_configs SET model=?, dify_base_url=?, dify_api_key_enc=?, note=?, updated_at=? WHERE id=? AND user_id=?`,
		model, baseURL, enc, note, time.Now().Unix(), id, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app config not found")
	}
	return nil
}

// SetAppConfigEnabled toggles a user-owned config.
func (s *Store) SetAppConfigEnabled(id, userID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE app_configs SET enabled=?, updated_at=? WHERE id=? AND user_id=?`, v, time.Now().Unix(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app config not found")
	}
	return nil
}

// DeleteAppConfig permanently removes a user-owned config.
func (s *Store) DeleteAppConfig(id, userID int64) error {
	res, err := s.db.Exec(`DELETE FROM app_configs WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("app config not found")
	}
	return nil
}
