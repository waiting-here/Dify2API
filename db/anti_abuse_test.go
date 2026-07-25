package db

import (
	"testing"
)

func TestAntiAbuse_GetAllAndSeed(t *testing.T) {
	st, _ := openTemp(t)

	services := []string{"general", "custom", "website-summary"}

	// First call: no rows exist, should auto-seed all services.
	configs, err := st.GetAntiAbuseConfigs(services)
	if err != nil {
		t.Fatalf("GetAntiAbuseConfigs: %v", err)
	}
	if len(configs) != len(services) {
		t.Fatalf("expected %d configs, got %d", len(services), len(configs))
	}
	for _, svc := range services {
		c, ok := configs[svc]
		if !ok {
			t.Fatalf("missing config for service %q", svc)
		}
		if c.Mode != 2 || c.MinChars != 20 || c.PenaltyDeductCredits != 0 || c.PenaltyBanHours != 0 {
			t.Errorf("service %q: expected defaults (2,20,0,0), got (%d,%d,%d,%d)",
				svc, c.Mode, c.MinChars, c.PenaltyDeductCredits, c.PenaltyBanHours)
		}
	}

	// Second call: all rows exist, should return existing values (no re-seed).
	configs2, err := st.GetAntiAbuseConfigs(services)
	if err != nil {
		t.Fatalf("GetAntiAbuseConfigs (2): %v", err)
	}
	if len(configs2) != len(services) {
		t.Fatalf("expected %d configs, got %d", len(services), len(configs2))
	}

	// Verify row count in DB.
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(1) FROM service_anti_abuse`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(services) {
		t.Errorf("expected %d rows in table, got %d", len(services), n)
	}
}

func TestAntiAbuse_UpsertAndGet(t *testing.T) {
	st, _ := openTemp(t)

	svc := "general"

	// Seed the row first.
	_, err := st.GetAntiAbuseConfigs([]string{svc})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Upsert: update mode and penalties.
	c, err := st.UpsertAntiAbuseConfig(svc, 1, 15, 10, 24)
	if err != nil {
		t.Fatalf("UpsertAntiAbuseConfig: %v", err)
	}
	if c.Mode != 1 || c.MinChars != 15 || c.PenaltyDeductCredits != 10 || c.PenaltyBanHours != 24 {
		t.Errorf("expected (1,15,10,24), got (%d,%d,%d,%d)",
			c.Mode, c.MinChars, c.PenaltyDeductCredits, c.PenaltyBanHours)
	}

	// Get single config.
	c2, err := st.GetAntiAbuseConfig(svc)
	if err != nil {
		t.Fatalf("GetAntiAbuseConfig: %v", err)
	}
	if c2 == nil {
		t.Fatal("expected non-nil config")
	}
	if c2.Mode != 1 {
		t.Errorf("mode: expected 1, got %d", c2.Mode)
	}

	// Get non-existent service.
	c3, err := st.GetAntiAbuseConfig("nonexistent")
	if err != nil {
		t.Fatalf("GetAntiAbuseConfig(nonexistent): %v", err)
	}
	if c3 != nil {
		t.Error("expected nil for non-existent service")
	}
}

func TestAntiAbuse_UpsertValidation(t *testing.T) {
	st, _ := openTemp(t)

	svc := "custom"

	// Seed.
	_, err := st.GetAntiAbuseConfigs([]string{svc})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Invalid mode.
	_, err = st.UpsertAntiAbuseConfig(svc, 3, 20, 0, 0)
	if err == nil {
		t.Error("expected error for mode=3")
	}

	// Negative min_chars.
	_, err = st.UpsertAntiAbuseConfig(svc, 2, -1, 0, 0)
	if err == nil {
		t.Error("expected error for min_chars=-1")
	}

	// Negative penalty.
	_, err = st.UpsertAntiAbuseConfig(svc, 2, 20, -5, 0)
	if err == nil {
		t.Error("expected error for penalty_deduct_credits=-5")
	}

	// Valid mode=0 (off).
	c, err := st.UpsertAntiAbuseConfig(svc, 0, 20, 0, 0)
	if err != nil {
		t.Fatalf("UpsertAntiAbuseConfig mode=0: %v", err)
	}
	if c.Mode != 0 {
		t.Errorf("mode: expected 0, got %d", c.Mode)
	}
}

func TestAntiAbuse_SeedNewService(t *testing.T) {
	st, _ := openTemp(t)

	// Seed only "general" first.
	_, err := st.GetAntiAbuseConfigs([]string{"general"})
	if err != nil {
		t.Fatalf("seed general: %v", err)
	}

	// Now call with an additional service; it should be auto-seeded.
	configs, err := st.GetAntiAbuseConfigs([]string{"general", "custom"})
	if err != nil {
		t.Fatalf("GetAntiAbuseConfigs: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	c, ok := configs["custom"]
	if !ok {
		t.Fatal("missing config for custom")
	}
	if c.Mode != 2 || c.MinChars != 20 {
		t.Errorf("custom defaults: expected (2,20), got (%d,%d)", c.Mode, c.MinChars)
	}
}
