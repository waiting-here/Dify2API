package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ModelConfig is one admin-managed model configuration used by
// downloadable-template services (v1.4.0: sillytavern-main-v1).
type ModelConfig struct {
	ModelKey         string `json:"model_key"`
	DisplayName      string `json:"display_name"`
	Provider         string `json:"provider"`
	DependencyPlugin string `json:"dependency_plugin"`
	DependencyVer    string `json:"dependency_version"`
	DependencyHash   string `json:"dependency_hash"`
	ParamsJSON       string `json:"params_json"`
	Enabled          bool   `json:"enabled"`
	SortOrder        int    `json:"sort_order"`
	Manual           bool   `json:"manual"`
	UpdatedAt        int64  `json:"updated_at"`
}

// DefaultModelConfigs are seeded idempotently on startup (extracted from
// the base template's dependency pins).
var DefaultModelConfigs = []ModelConfig{
	{
		ModelKey: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "anthropic",
		DependencyPlugin: "langgenius/anthropic", DependencyVer: "0.3.26",
		DependencyHash: "e4580f78789aec59eabdafcd85ca75358ae4895134de28dbae5e38e9b307eb70",
		ParamsJSON:     `{"context_1m":true,"max_tokens":128000,"temperature":0.9}`,
		Enabled:        true, SortOrder: 1,
	},
	{
		ModelKey: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", Provider: "openai",
		DependencyPlugin: "langgenius/openai", DependencyVer: "1.0.3",
		DependencyHash: "cc37544750d72ca3782bdaa81ab0e2facbf5bb74105a169a7ff974a27c6a5f29",
		ParamsJSON:     `{"max_tokens":128000,"reasoning_effort":"high"}`,
		Enabled:        true, SortOrder: 2,
	},
}

const modelConfigCols = `model_key, display_name, provider, dependency_plugin, dependency_version,
	dependency_hash, params_json, enabled, sort_order, manual, updated_at`

func scanModelConfig(row interface{ Scan(...interface{}) error }) (ModelConfig, error) {
	var m ModelConfig
	var enabled, manual int
	err := row.Scan(&m.ModelKey, &m.DisplayName, &m.Provider, &m.DependencyPlugin,
		&m.DependencyVer, &m.DependencyHash, &m.ParamsJSON, &enabled, &m.SortOrder, &manual, &m.UpdatedAt)
	m.Enabled = enabled != 0
	m.Manual = manual != 0
	return m, err
}

// SeedModelConfigs inserts the default model configs when absent. For rows
// created by the initial v1.4 development build (empty params_json and
// manual=false), it also backfills the extracted template parameters/hash;
// operator-managed/manual rows are never overwritten.
func (s *Store) SeedModelConfigs() error {
	now := time.Now().Unix()
	for _, m := range DefaultModelConfigs {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO dify_model_configs
			(model_key, display_name, provider, dependency_plugin, dependency_version,
			 dependency_hash, params_json, enabled, sort_order, manual, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			m.ModelKey, m.DisplayName, m.Provider, m.DependencyPlugin, m.DependencyVer,
			m.DependencyHash, m.ParamsJSON, boolInt(m.Enabled), m.SortOrder, boolInt(m.Manual), now); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE dify_model_configs SET params_json=?, dependency_hash=?, updated_at=?
			WHERE model_key=? AND manual=0 AND params_json=''`,
			m.ParamsJSON, m.DependencyHash, now, m.ModelKey); err != nil {
			return err
		}
	}
	return nil
}

// ListModelConfigs returns all model configs ordered by sort_order, model_key.
func (s *Store) ListModelConfigs() ([]ModelConfig, error) {
	rows, err := s.db.Query(`SELECT ` + modelConfigCols + ` FROM dify_model_configs ORDER BY sort_order, model_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelConfig{}
	for rows.Next() {
		m, err := scanModelConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListEnabledModelConfigs returns enabled configs (user-facing model list).
func (s *Store) ListEnabledModelConfigs() ([]ModelConfig, error) {
	rows, err := s.db.Query(`SELECT ` + modelConfigCols + ` FROM dify_model_configs WHERE enabled=1 ORDER BY sort_order, model_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelConfig{}
	for rows.Next() {
		m, err := scanModelConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetModelConfig fetches one config (nil when absent).
func (s *Store) GetModelConfig(modelKey string) (*ModelConfig, error) {
	m, err := scanModelConfig(s.db.QueryRow(`SELECT `+modelConfigCols+` FROM dify_model_configs WHERE model_key=?`, modelKey))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PutModelConfig upserts a model config. manual=true marks it so the daily
// marketplace task never overrides it.
func (s *Store) PutModelConfig(m ModelConfig) error {
	_, err := s.db.Exec(`INSERT INTO dify_model_configs
		(model_key, display_name, provider, dependency_plugin, dependency_version,
		 dependency_hash, params_json, enabled, sort_order, manual, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(model_key) DO UPDATE SET
			display_name=excluded.display_name, provider=excluded.provider,
			dependency_plugin=excluded.dependency_plugin, dependency_version=excluded.dependency_version,
			dependency_hash=excluded.dependency_hash, params_json=excluded.params_json,
			enabled=excluded.enabled, sort_order=excluded.sort_order, manual=excluded.manual,
			updated_at=excluded.updated_at`,
		m.ModelKey, m.DisplayName, m.Provider, m.DependencyPlugin, m.DependencyVer,
		m.DependencyHash, m.ParamsJSON, boolInt(m.Enabled), m.SortOrder, boolInt(m.Manual), time.Now().Unix())
	return err
}

// DeleteModelConfig removes a config.
func (s *Store) DeleteModelConfig(modelKey string) error {
	_, err := s.db.Exec(`DELETE FROM dify_model_configs WHERE model_key=?`, modelKey)
	return err
}

// UpdateModelDependency applies a marketplace-sourced dependency version to
// every non-manual config using the same plugin. Returns the number updated.
func (s *Store) UpdateModelDependency(plugin, version, hash string, now time.Time) (int, error) {
	res, err := s.db.Exec(`UPDATE dify_model_configs
		SET dependency_version=?, dependency_hash=?, updated_at=?
		WHERE dependency_plugin=? AND manual=0`,
		version, hash, now.Unix(), plugin)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ServiceGeneration is one template download record.
type ServiceGeneration struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	Service       string `json:"service"`
	ModelKey      string `json:"model_key"`
	Purpose       string `json:"purpose"` // personal | donation
	Seed          string `json:"-"`
	MappingJSON   string `json:"-"`
	DummyJSON     string `json:"-"`
	DummyCount    int    `json:"dummy_count"`
	DownloadCount int    `json:"download_count"`
	CreatedAt     int64  `json:"created_at"`
}

// AddServiceGeneration records a template download and returns its id.
func (s *Store) AddServiceGeneration(userID int64, service, modelKey, purpose string, seed []byte, mapping map[string]string, dummyKeys []string, now time.Time) (int64, error) {
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return 0, err
	}
	dummyJSON, err := json.Marshal(dummyKeys)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO service_generations
		(user_id, service, model_key, purpose, seed, mapping_json, dummy_json, dummy_count, download_count, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		userID, service, modelKey, purpose, string(seed), string(mappingJSON), string(dummyJSON), len(dummyKeys), 1, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LatestGenerationMapping returns the most recent generation's mapping for
// (user, service, purpose) — nil when none exists.
func (s *Store) LatestGenerationMapping(userID int64, service, purpose string) (map[string]string, error) {
	var raw string
	err := s.db.QueryRow(`SELECT mapping_json FROM service_generations
		WHERE user_id=? AND service=? AND purpose=?
		ORDER BY id DESC LIMIT 1`, userID, service, purpose).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("decode generation mapping: %w", err)
	}
	return m, nil
}

// ListServiceGenerations returns a user's generation records (export).
func (s *Store) ListServiceGenerations(userID int64) ([]ServiceGeneration, error) {
	rows, err := s.db.Query(`SELECT id, user_id, service, model_key, purpose, seed, mapping_json, dummy_json,
		dummy_count, download_count, created_at FROM service_generations WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceGeneration{}
	for rows.Next() {
		var g ServiceGeneration
		if err := rows.Scan(&g.ID, &g.UserID, &g.Service, &g.ModelKey, &g.Purpose, &g.Seed,
			&g.MappingJSON, &g.DummyJSON, &g.DummyCount, &g.DownloadCount, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PurgeExpiredServiceGenerations enforces the rolling 30-day retention.
func (s *Store) PurgeExpiredServiceGenerations(now int64) (int64, error) {
	cutoff := now - int64(RequestLogRetention.Seconds())
	res, err := s.db.Exec(`DELETE FROM service_generations WHERE created_at<?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
