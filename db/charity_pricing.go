package db

import (
	"database/sql"
	"fmt"
	"math"
)

// CharityPricing defines the price and reward for one (service, model)
// combination in the charity routing system.
type CharityPricing struct {
	Service string
	Model   string
	Price   int
	Reward  int
	Enabled bool
}

func scanPricing(row interface{ Scan(...interface{}) error }) (*CharityPricing, error) {
	var p CharityPricing
	var enabled int
	if err := row.Scan(&p.Service, &p.Model, &p.Price, &p.Reward, &enabled); err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	return &p, nil
}

// GetPricing returns the pricing for a (service, model) pair.
// Returns (nil, nil) when no row exists.
func (s *Store) GetPricing(service, model string) (*CharityPricing, error) {
	p, err := scanPricing(s.db.QueryRow(
		`SELECT service, model, price, reward, enabled FROM charity_pricing WHERE service=? AND model=?`,
		service, model,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ListPricing returns all pricing entries ordered by service, model.
func (s *Store) ListPricing() ([]*CharityPricing, error) {
	rows, err := s.db.Query(
		`SELECT service, model, price, reward, enabled FROM charity_pricing ORDER BY service, model`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CharityPricing
	for rows.Next() {
		p, err := scanPricing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListEnabledPricing returns pricing entries where enabled=1.
// Used to synthesise charity model entries for /v1/models.
func (s *Store) ListEnabledPricing() ([]*CharityPricing, error) {
	rows, err := s.db.Query(
		`SELECT service, model, price, reward, enabled FROM charity_pricing WHERE enabled=1 ORDER BY service, model`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CharityPricing
	for rows.Next() {
		p, err := scanPricing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPricing inserts or updates a pricing row.
// Validates price >= 0, reward >= 0.
// If reward == 0, auto-fills ceil(price * 0.5).
// enabled is left unchanged (defaults to 0 on insert, preserved on update
// unless explicitly changed via SetPricingEnabled).
func (s *Store) UpsertPricing(service, model string, price int, reward *int) (*CharityPricing, error) {
	if price < 0 {
		return nil, fmt.Errorf("price must be >= 0, got %d", price)
	}
	r := 0
	if reward != nil {
		if *reward < 0 {
			return nil, fmt.Errorf("reward must be >= 0, got %d", *reward)
		}
		r = *reward
	} else {
		r = int(math.Ceil(float64(price) * 0.5))
	}

	_, err := s.db.Exec(
		`INSERT INTO charity_pricing (service, model, price, reward, enabled)
		 VALUES (?,?,?,?,0)
		 ON CONFLICT(service, model) DO UPDATE SET price=excluded.price, reward=excluded.reward`,
		service, model, price, r,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert pricing: %w", err)
	}
	return s.GetPricing(service, model)
}

// DeletePricing removes a pricing row. Returns an error when donations exist
// for this (service, model) pair.
func (s *Store) DeletePricing(service, model string) error {
	has, err := s.HasDonationsForPair(service, model)
	if err != nil {
		return err
	}
	if has {
		return fmt.Errorf("该 (service, model) 组合下存在捐赠条目，无法删除定价")
	}
	_, err = s.db.Exec(`DELETE FROM charity_pricing WHERE service=? AND model=?`, service, model)
	return err
}

// HasDonationsForPair checks whether the donations table contains any entry
// (regardless of status) for the given (service, model) pair.
func (s *Store) HasDonationsForPair(service, model string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM donations WHERE service=? AND model=?`,
		service, model,
	).Scan(&n)
	return n > 0, err
}

// SetPricingEnabled sets the enabled flag for a pricing row.
func (s *Store) SetPricingEnabled(service, model string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	res, err := s.db.Exec(
		`UPDATE charity_pricing SET enabled=? WHERE service=? AND model=?`,
		val, service, model,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pricing (%s, %s) not found", service, model)
	}
	return nil
}
