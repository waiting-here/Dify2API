package db

import (
	"errors"
	"testing"
	"time"
)

func s2String(v string) *string { return &v }
func s2Int(v int) *int          { return &v }

func seedS2Donation(t *testing.T, st *Store, model, status, note string) *Donation {
	t.Helper()
	d, err := st.CreateDonation(&Donation{
		Service: "general", Model: model, DifyBaseURL: "https://dify.example.com/v1",
		Deadline: time.Now().Add(24 * time.Hour).Unix(), TotalCount: 10,
		RpmLimit: 5, Status: status, Note: note,
	}, "app-s2-secret")
	if err != nil {
		t.Fatalf("seed donation: %v", err)
	}
	return d
}

func seedS2LinkedDonation(t *testing.T, st *Store, model string) (*DonationApplication, *Donation) {
	t.Helper()
	user, err := st.CreateUser("s2-user-"+model, "s2-user-"+model, "")
	if err != nil {
		t.Fatalf("create applicant: %v", err)
	}
	reviewer, err := st.CreateUser("s2-reviewer-"+model, "s2-reviewer-"+model, "")
	if err != nil {
		t.Fatalf("create reviewer: %v", err)
	}
	app, err := st.CreateDonationApplication(
		user.ID, "general", model, "https://dify.example.com/v1", "app-s2-secret",
		10, time.Now().Add(24*time.Hour).Unix(), 5, "source note",
	)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	app, donation, err := st.ApproveApplication(app.ID, reviewer.ID, &ApproveApplicationFields{}, "review note")
	if err != nil {
		t.Fatalf("approve application: %v", err)
	}
	return app, donation
}

func enableS2Pricing(t *testing.T, st *Store, model string) {
	t.Helper()
	if _, err := st.UpsertPricing("general", model, 10, rewardPtr(5)); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	if err := st.SetPricingEnabled("general", model, true); err != nil {
		t.Fatalf("enable pricing: %v", err)
	}
}

func TestPatchDonation_ThreeStateCountsAndActivation(t *testing.T) {
	st, _ := openTemp(t)
	app, donation := seedS2LinkedDonation(t, st, "patch-three-state")

	result, err := st.PatchDonation(donation.ID, DonationPatch{})
	if err != nil {
		t.Fatalf("omitted patch: %v", err)
	}
	if result.Donation.Note != "source note" || !result.HasReviewRecord || result.ReviewNote != "review note" {
		t.Fatalf("omitted notes changed: %+v", result)
	}

	result, err = st.PatchDonation(donation.ID, DonationPatch{Note: s2String(""), ReviewNote: s2String("")})
	if err != nil {
		t.Fatalf("clear notes: %v", err)
	}
	if result.Donation.Note != "" || result.ReviewNote != "" || !result.HasReviewRecord {
		t.Fatalf("notes not cleared: %+v", result)
	}
	storedApp, _ := st.GetApplication(app.ID)
	if storedApp.ReviewNote != "" {
		t.Fatalf("stored review note = %q", storedApp.ReviewNote)
	}

	result, err = st.PatchDonation(donation.ID, DonationPatch{
		Note: s2String("  replacement  "), ReviewNote: s2String("  reviewed again  "),
	})
	if err != nil {
		t.Fatalf("replace notes: %v", err)
	}
	if result.Donation.Note != "replacement" || result.ReviewNote != "reviewed again" {
		t.Fatalf("notes not trimmed/replaced: %+v", result)
	}

	rawKey := "  app-s2-replacement  "
	result, err = st.PatchDonation(donation.ID, DonationPatch{DifyAPIKey: &rawKey})
	if err != nil {
		t.Fatalf("replace key: %v", err)
	}
	plainKey, err := st.Decrypt(result.Donation.DifyAPIKeyEnc)
	if err != nil || plainKey != rawKey {
		t.Fatalf("stored key = %q, err=%v; want original value %q", plainKey, err, rawKey)
	}

	if _, err := st.RawExec(`UPDATE donations SET remaining_count=6 WHERE id=?`, donation.ID); err != nil {
		t.Fatalf("seed consumed count: %v", err)
	}
	result, err = st.PatchDonation(donation.ID, DonationPatch{TotalCount: s2Int(7)})
	if err != nil {
		t.Fatalf("lower total: %v", err)
	}
	if result.Donation.TotalCount != 7 || result.Donation.RemainingCount != 3 {
		t.Fatalf("counts after delta = %d/%d, want 3/7", result.Donation.RemainingCount, result.Donation.TotalCount)
	}
	result, err = st.PatchDonation(donation.ID, DonationPatch{TotalCount: s2Int(2)})
	if err != nil {
		t.Fatalf("clamp total: %v", err)
	}
	if result.Donation.RemainingCount != 0 {
		t.Fatalf("remaining = %d, want 0", result.Donation.RemainingCount)
	}

	if _, err := st.RawExec(`UPDATE donations SET consecutive_failures=4 WHERE id=?`, donation.ID); err != nil {
		t.Fatalf("seed failures: %v", err)
	}
	enableS2Pricing(t, st, donation.Model)
	result, err = st.PatchDonation(donation.ID, DonationPatch{Status: s2String(DonationActive)})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if result.Donation.ConsecutiveFailures != 0 {
		t.Fatalf("consecutive failures = %d, want 0", result.Donation.ConsecutiveFailures)
	}
}

func TestPatchDonation_ValidationAndMissingReviewRollback(t *testing.T) {
	st, _ := openTemp(t)
	donation := seedS2Donation(t, st, "patch-validation", DonationInactive, "original")

	_, err := st.PatchDonation(donation.ID, DonationPatch{Note: s2String("changed"), ReviewNote: s2String("")})
	var patchErr *DonationPatchError
	if !errors.As(err, &patchErr) || patchErr.Kind != DonationPatchReviewRecordAbsent {
		t.Fatalf("missing review error = %v", err)
	}
	got, _ := st.GetDonation(donation.ID)
	if got.Note != "original" {
		t.Fatalf("donation update was not rolled back: note=%q", got.Note)
	}

	_, err = st.PatchDonation(donation.ID, DonationPatch{Status: s2String(DonationActive)})
	if !errors.As(err, &patchErr) || patchErr.Kind != DonationPatchPricingAbsent {
		t.Fatalf("missing enabled pricing error = %v", err)
	}
	if err := st.SetDonationStatus(donation.ID, DonationExpired); err != nil {
		t.Fatalf("expire donation: %v", err)
	}
	_, err = st.PatchDonation(donation.ID, DonationPatch{Note: s2String("changed")})
	if !errors.As(err, &patchErr) || patchErr.Kind != DonationPatchExpired {
		t.Fatalf("expired error = %v", err)
	}
}

func TestPatchDonation_FailureInjectionRollsBack(t *testing.T) {
	t.Run("donation write", func(t *testing.T) {
		st, _ := openTemp(t)
		app, donation := seedS2LinkedDonation(t, st, "patch-donation-failure")
		if _, err := st.RawExec(`CREATE TRIGGER fail_s2_donation_update BEFORE UPDATE ON donations
			BEGIN SELECT RAISE(ABORT, 'injected donation update failure'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		_, err := st.PatchDonation(donation.ID, DonationPatch{
			Note: s2String("changed"), ReviewNote: s2String("changed review"),
		})
		if err == nil {
			t.Fatal("expected injected donation failure")
		}
		got, _ := st.GetDonation(donation.ID)
		gotApp, _ := st.GetApplication(app.ID)
		if got.Note != "source note" || gotApp.ReviewNote != "review note" {
			t.Fatalf("partial update after donation failure: note=%q review=%q", got.Note, gotApp.ReviewNote)
		}
	})

	t.Run("review write", func(t *testing.T) {
		st, _ := openTemp(t)
		app, donation := seedS2LinkedDonation(t, st, "patch-review-failure")
		if _, err := st.RawExec(`CREATE TRIGGER fail_s2_review_update BEFORE UPDATE OF review_note ON donation_applications
			BEGIN SELECT RAISE(ABORT, 'injected review update failure'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		_, err := st.PatchDonation(donation.ID, DonationPatch{
			Note: s2String("changed"), ReviewNote: s2String("changed review"),
		})
		if err == nil {
			t.Fatal("expected injected review failure")
		}
		got, _ := st.GetDonation(donation.ID)
		gotApp, _ := st.GetApplication(app.ID)
		if got.Note != "source note" || gotApp.ReviewNote != "review note" {
			t.Fatalf("partial update after review failure: note=%q review=%q", got.Note, gotApp.ReviewNote)
		}
	})
}

func TestPatchDonation_ConcurrentDeleteReturnsNotFound(t *testing.T) {
	st, _ := openTemp(t)
	donation := seedS2Donation(t, st, "patch-delete-race", DonationInactive, "original")
	if _, err := st.RawExec(`CREATE TRIGGER delete_s2_donation_before_update BEFORE UPDATE ON donations
		BEGIN DELETE FROM donations WHERE id=OLD.id; END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	_, err := st.PatchDonation(donation.ID, DonationPatch{Note: s2String("changed")})
	var patchErr *DonationPatchError
	if !errors.As(err, &patchErr) || patchErr.Kind != DonationPatchNotFound {
		t.Fatalf("concurrent delete error = %v", err)
	}
	got, getErr := st.GetDonation(donation.ID)
	if getErr != nil || got == nil || got.Note != "original" {
		t.Fatalf("rollback did not restore target: got=%+v err=%v", got, getErr)
	}
}

func TestPatchPricing_AtomicValidationFailureAndDelete(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		st, _ := openTemp(t)
		if _, err := st.UpsertPricing("general", "pricing-invalid", 10, rewardPtr(5)); err != nil {
			t.Fatalf("seed pricing: %v", err)
		}
		enabled := true
		invalid := []PricingPatch{
			{Price: s2Int(-1), Reward: s2Int(8), Enabled: &enabled},
			{Price: s2Int(20), Reward: s2Int(-1), Enabled: &enabled},
		}
		for i, patch := range invalid {
			if _, err := st.PatchPricing("general", "pricing-invalid", patch); err == nil {
				t.Fatalf("expected invalid pricing patch %d", i)
			}
			got, _ := st.GetPricing("general", "pricing-invalid")
			if got.Price != 10 || got.Reward != 5 || got.Enabled {
				t.Fatalf("partial pricing update %d: %+v", i, got)
			}
		}
	})

	t.Run("write failure", func(t *testing.T) {
		st, _ := openTemp(t)
		if _, err := st.UpsertPricing("general", "pricing-write-failure", 10, rewardPtr(5)); err != nil {
			t.Fatalf("seed pricing: %v", err)
		}
		if _, err := st.RawExec(`CREATE TRIGGER fail_s2_pricing_update BEFORE UPDATE ON charity_pricing
			BEGIN SELECT RAISE(ABORT, 'injected pricing update failure'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		enabled := true
		_, err := st.PatchPricing("general", "pricing-write-failure", PricingPatch{
			Price: s2Int(20), Reward: s2Int(8), Enabled: &enabled,
		})
		if err == nil {
			t.Fatal("expected injected pricing failure")
		}
		got, _ := st.GetPricing("general", "pricing-write-failure")
		if got.Price != 10 || got.Reward != 5 || got.Enabled {
			t.Fatalf("partial pricing update: %+v", got)
		}
	})

	t.Run("concurrent delete", func(t *testing.T) {
		st, _ := openTemp(t)
		if _, err := st.UpsertPricing("general", "pricing-delete-race", 10, rewardPtr(5)); err != nil {
			t.Fatalf("seed pricing: %v", err)
		}
		if _, err := st.RawExec(`CREATE TRIGGER delete_s2_pricing_before_update BEFORE UPDATE ON charity_pricing
			BEGIN DELETE FROM charity_pricing WHERE service=OLD.service AND model=OLD.model; END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		_, err := st.PatchPricing("general", "pricing-delete-race", PricingPatch{Price: s2Int(20)})
		var patchErr *PricingPatchError
		if !errors.As(err, &patchErr) {
			t.Fatalf("concurrent delete error = %v", err)
		}
		got, getErr := st.GetPricing("general", "pricing-delete-race")
		if getErr != nil || got == nil || got.Price != 10 {
			t.Fatalf("rollback did not restore pricing: got=%+v err=%v", got, getErr)
		}
	})
}
