package db

import (
	"database/sql"
	"fmt"
	"time"
)

// AntiAbuseConfig holds the per-service anti-abuse configuration.
type AntiAbuseConfig struct {
	Service              string
	Mode                 int // 0=off, 1=charity-only, 2=global
	MinChars             int
	PenaltyDeductCredits int
	PenaltyBanHours      int
	CreatedAt            int64
	UpdatedAt            int64
}

func scanAntiAbuse(row interface{ Scan(...interface{}) error }) (*AntiAbuseConfig, error) {
	var c AntiAbuseConfig
	if err := row.Scan(&c.Service, &c.Mode, &c.MinChars, &c.PenaltyDeductCredits, &c.PenaltyBanHours, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// GetAntiAbuseConfigs loads all anti-abuse configs from the database.
// Services not present in the database are automatically seeded with
// default values (mode=2, min_chars=20, penalties=0).
func (s *Store) GetAntiAbuseConfigs(services []string) (map[string]*AntiAbuseConfig, error) {
	rows, err := s.db.Query(`SELECT service, mode, min_chars, penalty_deduct_credits, penalty_ban_hours, created_at, updated_at FROM service_anti_abuse`)
	if err != nil {
		return nil, fmt.Errorf("query anti_abuse: %w", err)
	}
	defer rows.Close()

	configs := make(map[string]*AntiAbuseConfig)
	for rows.Next() {
		c, err := scanAntiAbuse(rows)
		if err != nil {
			return nil, err
		}
		configs[c.Service] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Auto-seed any service not in the database.
	now := time.Now().Unix()
	for _, svc := range services {
		if _, ok := configs[svc]; !ok {
			_, err := s.db.Exec(
				`INSERT INTO service_anti_abuse (service, mode, min_chars, penalty_deduct_credits, penalty_ban_hours, created_at, updated_at)
				 VALUES (?, 2, 20, 0, 0, ?, ?)`,
				svc, now, now,
			)
			if err != nil {
				return nil, fmt.Errorf("seed anti_abuse for %q: %w", svc, err)
			}
			configs[svc] = &AntiAbuseConfig{
				Service:              svc,
				Mode:                 2,
				MinChars:             20,
				PenaltyDeductCredits: 0,
				PenaltyBanHours:      0,
				CreatedAt:            now,
				UpdatedAt:            now,
			}
		}
	}

	return configs, nil
}

// GetAntiAbuseConfig returns a single service's config, or nil when no row exists.
func (s *Store) GetAntiAbuseConfig(service string) (*AntiAbuseConfig, error) {
	c, err := scanAntiAbuse(s.db.QueryRow(
		`SELECT service, mode, min_chars, penalty_deduct_credits, penalty_ban_hours, created_at, updated_at
		 FROM service_anti_abuse WHERE service=?`,
		service,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// UpsertAntiAbuseConfig inserts or updates an anti-abuse config row.
func (s *Store) UpsertAntiAbuseConfig(service string, mode, minChars, penaltyDeductCredits, penaltyBanHours int) (*AntiAbuseConfig, error) {
	if mode < 0 || mode > 2 {
		return nil, fmt.Errorf("mode must be 0, 1, or 2, got %d", mode)
	}
	if minChars < 0 {
		return nil, fmt.Errorf("min_chars must be >= 0, got %d", minChars)
	}
	if penaltyDeductCredits < 0 {
		return nil, fmt.Errorf("penalty_deduct_credits must be >= 0, got %d", penaltyDeductCredits)
	}
	if penaltyBanHours < 0 {
		return nil, fmt.Errorf("penalty_ban_hours must be >= 0, got %d", penaltyBanHours)
	}

	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO service_anti_abuse (service, mode, min_chars, penalty_deduct_credits, penalty_ban_hours, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(service) DO UPDATE SET
		     mode=excluded.mode,
		     min_chars=excluded.min_chars,
		     penalty_deduct_credits=excluded.penalty_deduct_credits,
		     penalty_ban_hours=excluded.penalty_ban_hours,
		     updated_at=excluded.updated_at`,
		service, mode, minChars, penaltyDeductCredits, penaltyBanHours, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert anti_abuse: %w", err)
	}
	return s.GetAntiAbuseConfig(service)
}
