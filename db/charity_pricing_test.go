package db

import (
	"database/sql"
	"testing"
	"time"
)

func TestCharityPricing_CRUD(t *testing.T) {
	st, _ := openTemp(t)

	// Upsert creates.
	cp, err := st.UpsertPricing("general", "claude-opus", 20, 0)
	if err != nil {
		t.Fatalf("UpsertPricing: %v", err)
	}
	if cp.Service != "general" || cp.Model != "claude-opus" {
		t.Errorf("pricing = %+v", cp)
	}
	// reward auto-filled: ceil(20*0.5) = 10
	if cp.Reward != 10 {
		t.Errorf("reward = %d, want 10", cp.Reward)
	}
	if cp.Price != 20 {
		t.Errorf("price = %d, want 20", cp.Price)
	}
	if cp.Enabled {
		t.Error("enabled should default to false")
	}

	// Upsert with explicit reward.
	cp2, err := st.UpsertPricing("general", "claude-opus", 30, 15)
	if err != nil {
		t.Fatalf("UpsertPricing update: %v", err)
	}
	if cp2.Price != 30 || cp2.Reward != 15 {
		t.Errorf("updated pricing price=%d reward=%d, want 30/15", cp2.Price, cp2.Reward)
	}

	// GetPricing found.
	got, err := st.GetPricing("general", "claude-opus")
	if err != nil || got == nil {
		t.Fatalf("GetPricing: %v %v", got, err)
	}
	if got.Price != 30 {
		t.Errorf("price = %d", got.Price)
	}

	// GetPricing missing.
	missing, err := st.GetPricing("nonexistent", "model")
	if err != nil || missing != nil {
		t.Errorf("GetPricing missing: %v %v", missing, err)
	}

	// ListPricing.
	list, err := st.ListPricing()
	if err != nil || len(list) != 1 {
		t.Errorf("ListPricing: len=%d err=%v", len(list), err)
	}

	// ListEnabledPricing — not enabled yet.
	list2, err := st.ListEnabledPricing()
	if err != nil || len(list2) != 0 {
		t.Errorf("ListEnabledPricing: len=%d (want 0, not enabled)", len(list2))
	}

	// SetPricingEnabled.
	if err := st.SetPricingEnabled("general", "claude-opus", true); err != nil {
		t.Fatalf("SetPricingEnabled true: %v", err)
	}
	got, _ = st.GetPricing("general", "claude-opus")
	if !got.Enabled {
		t.Error("enabled should be true after SetPricingEnabled")
	}
	list3, _ := st.ListEnabledPricing()
	if len(list3) != 1 {
		t.Errorf("ListEnabledPricing after enable: len=%d, want 1", len(list3))
	}

	// SetPricingEnabled false.
	if err := st.SetPricingEnabled("general", "claude-opus", false); err != nil {
		t.Fatalf("SetPricingEnabled false: %v", err)
	}
	got, _ = st.GetPricing("general", "claude-opus")
	if got.Enabled {
		t.Error("enabled should be false")
	}

	// SetPricingEnabled on non-existent.
	err = st.SetPricingEnabled("ghost", "model", true)
	if err == nil {
		t.Error("expected error for non-existent pricing")
	}
}

func TestCharityPricing_DeleteWithDonations(t *testing.T) {
	st, _ := openTemp(t)

	// Create pricing + donation.
	_, err := st.UpsertPricing("general", "test-model", 10, 5)
	if err != nil {
		t.Fatalf("UpsertPricing: %v", err)
	}

	// Create a donation for this pair.
	d := &Donation{
		Service:     "general",
		Model:       "test-model",
		DifyBaseURL: "https://d.example.com",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
	}
	_, err = st.CreateDonation(d, "secret-key")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}

	// DeletePricing should fail.
	err = st.DeletePricing("general", "test-model")
	if err == nil {
		t.Fatal("DeletePricing should fail when donations exist")
	}

	// Delete the donation, then DeletePricing should succeed.
	d2, _ := st.GetDonation(1)
	st.DeleteDonation(d2.ID)

	err = st.DeletePricing("general", "test-model")
	if err != nil {
		t.Fatalf("DeletePricing after donation removal: %v", err)
	}
	got, _ := st.GetPricing("general", "test-model")
	if got != nil {
		t.Error("pricing should be deleted")
	}
}

func TestCharityPricing_HasDonationsForPair(t *testing.T) {
	st, _ := openTemp(t)

	has, err := st.HasDonationsForPair("general", "nonexistent")
	if err != nil || has {
		t.Errorf("HasDonationsForPair empty: has=%v err=%v", has, err)
	}

	d := &Donation{
		Service:     "general",
		Model:       "real-model",
		DifyBaseURL: "https://d.example.com",
		Deadline:    time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:  10,
	}
	_, err = st.CreateDonation(d, "secret-key")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}

	has, err = st.HasDonationsForPair("general", "real-model")
	if err != nil || !has {
		t.Errorf("HasDonationsForPair should be true: has=%v err=%v", has, err)
	}

	// Different model.
	has, err = st.HasDonationsForPair("general", "other-model")
	if err != nil || has {
		t.Errorf("HasDonationsForPair other: has=%v err=%v", has, err)
	}
}

func TestCharityPricing_UpsertValidates(t *testing.T) {
	st, _ := openTemp(t)

	// Negative price.
	_, err := st.UpsertPricing("general", "x", -1, 0)
	if err == nil {
		t.Error("expected error for negative price")
	}

	// Negative reward.
	_, err = st.UpsertPricing("general", "x", 10, -1)
	if err == nil {
		t.Error("expected error for negative reward")
	}
}

func TestCharityPricing_ListEnabledMultiple(t *testing.T) {
	st, _ := openTemp(t)

	// Create 3 pricing entries, enable 2.
	st.UpsertPricing("general", "m1", 10, 0)
	st.UpsertPricing("general", "m2", 20, 0)
	st.UpsertPricing("general", "m3", 30, 0)

	st.SetPricingEnabled("general", "m1", true)
	st.SetPricingEnabled("general", "m3", true)

	list, err := st.ListEnabledPricing()
	if err != nil {
		t.Fatalf("ListEnabledPricing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 enabled, got %d", len(list))
	}
	seen := make(map[string]bool)
	for _, p := range list {
		seen[p.Model] = true
	}
	if !seen["m1"] || !seen["m3"] || seen["m2"] {
		t.Errorf("unexpected enabled set: %v", seen)
	}
}

func TestCharityPricing_RewardAutoFillEdge(t *testing.T) {
	st, _ := openTemp(t)

	// price=0, reward=0 → ceil(0) = 0
	cp, err := st.UpsertPricing("general", "zero", 0, 0)
	if err != nil {
		t.Fatalf("UpsertPricing zero: %v", err)
	}
	if cp.Reward != 0 {
		t.Errorf("reward for price 0 = %d, want 0", cp.Reward)
	}

	// price=1, reward=0 → ceil(0.5) = 1
	cp, err = st.UpsertPricing("general", "one", 1, 0)
	if err != nil {
		t.Fatalf("UpsertPricing one: %v", err)
	}
	if cp.Reward != 1 {
		t.Errorf("reward for price 1 = %d, want 1", cp.Reward)
	}

	// price=5, reward=0 → ceil(2.5) = 3
	cp, err = st.UpsertPricing("general", "five", 5, 0)
	if err != nil {
		t.Fatalf("UpsertPricing five: %v", err)
	}
	if cp.Reward != 3 {
		t.Errorf("reward for price 5 = %d, want 3", cp.Reward)
	}

	// Explicit reward takes precedence (not auto-filled)
	cp, err = st.UpsertPricing("general", "explicit", 100, 1)
	if err != nil {
		t.Fatalf("UpsertPricing explicit: %v", err)
	}
	if cp.Reward != 1 {
		t.Errorf("reward = %d, want 1 (explicit)", cp.Reward)
	}
}

func TestCharityPricing_ExportBundle(t *testing.T) {
	st, _ := openTemp(t)

	// Create user + donation for the pair, then verify pricing exists via HasDonationsForPair.
	// ExportBundle doesn't include charity_pricing (it's admin data, not user data),
	// but we confirm the pricing table works alongside donations.
	u, err := st.CreateUser("999", "pricing_user", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	d := &Donation{
		Service:         "general",
		Model:           "export-test",
		DifyBaseURL:     "https://d.example.com",
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: u.DiscordID,
		SourceUsername:  u.Username,
		Deadline:        time.Now().Add(24 * time.Hour).Unix(),
		TotalCount:      10,
	}
	created, err := st.CreateDonation(d, "secret-key")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}

	// Verify we can query HasDonationsForPair.
	has, err := st.HasDonationsForPair("general", "export-test")
	if err != nil || !has {
		t.Errorf("HasDonationsForPair: has=%v err=%v", has, err)
	}

	// Upsert pricing and verify.
	cp, err := st.UpsertPricing("general", "export-test", 50, 0)
	if err != nil {
		t.Fatalf("UpsertPricing: %v", err)
	}
	if cp == nil || cp.Price != 50 {
		t.Errorf("pricing = %+v", cp)
	}

	// DeletePricing should be blocked (donation exists).
	if err := st.DeletePricing("general", "export-test"); err == nil {
		t.Error("DeletePricing should fail when donation exists")
	}

	// Delete donation first, then delete pricing succeeds.
	st.DeleteDonation(created.ID)
	if err := st.DeletePricing("general", "export-test"); err != nil {
		t.Errorf("DeletePricing after donation deleted: %v", err)
	}
}
