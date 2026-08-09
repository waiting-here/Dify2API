package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dify2api/db"
)

func seedHandlerS2Donation(t *testing.T, store *db.Store, model, status, note string) *db.Donation {
	t.Helper()
	donation, err := store.CreateDonation(&db.Donation{
		Service: "general", Model: model, DifyBaseURL: "https://dify.example.com/v1",
		Deadline: time.Now().Add(48 * time.Hour).Unix(), TotalCount: 10,
		RpmLimit: 5, Status: status, Note: note,
	}, "handler-s2-secret")
	if err != nil {
		t.Fatalf("seed donation: %v", err)
	}
	return donation
}

func seedHandlerS2LinkedDonation(t *testing.T, store *db.Store, model, reviewNote string) (*db.DonationApplication, *db.Donation) {
	t.Helper()
	user, err := store.CreateUser("s2-applicant-"+model, "s2-applicant-"+model, "")
	if err != nil {
		t.Fatalf("create applicant: %v", err)
	}
	reviewer, err := store.CreateUser("s2-reviewer-"+model, "s2-reviewer-"+model, "")
	if err != nil {
		t.Fatalf("create reviewer: %v", err)
	}
	app, err := store.CreateDonationApplication(
		user.ID, "general", model, "https://dify.example.com/v1", "handler-s2-secret",
		10, time.Now().Add(48*time.Hour).Unix(), 5, "source note",
	)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	app, donation, err := store.ApproveApplication(app.ID, reviewer.ID, &db.ApproveApplicationFields{}, reviewNote)
	if err != nil {
		t.Fatalf("approve application: %v", err)
	}
	return app, donation
}

func enableHandlerS2Pricing(t *testing.T, store *db.Store, model string) {
	t.Helper()
	reward := 5
	if _, err := store.UpsertPricing("general", model, 10, &reward); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	if err := store.SetPricingEnabled("general", model, true); err != nil {
		t.Fatalf("enable pricing: %v", err)
	}
}

func TestDonationPatch_AdminAndLevel5ShareAtomicContract(t *testing.T) {
	tests := []struct {
		name   string
		path   func(int64) string
		cookie func(*testing.T, *Gateway, *db.Store) *http.Cookie
	}{
		{
			name:   "admin",
			path:   func(id int64) string { return fmt.Sprintf("/api/admin/donations/%d", id) },
			cookie: func(t *testing.T, gw *Gateway, _ *db.Store) *http.Cookie { return adminCookie(t, gw) },
		},
		{
			name: "level5",
			path: func(id int64) string { return fmt.Sprintf("/api/me/charity-admin/donations/%d", id) },
			cookie: func(t *testing.T, _ *Gateway, store *db.Store) *http.Cookie {
				u, err := store.CreateUser("s2-level5", "s2-level5", "")
				if err != nil {
					t.Fatalf("create level5: %v", err)
				}
				level := 5
				if err := store.SetUserLevel(u.ID, &level); err != nil {
					t.Fatalf("set level5: %v", err)
				}
				return meUserCookie(t, store, u)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw, store := setupAuthGateway(t, "x")
			cookie := tc.cookie(t, gw, store)
			app, donation := seedHandlerS2LinkedDonation(t, store, "shared-"+tc.name, "initial review")
			enableHandlerS2Pricing(t, store, "shared-updated-"+tc.name)
			deadline := time.Now().Add(72 * time.Hour).Unix()
			body := map[string]interface{}{
				"service": "general", "model": " shared-updated-" + tc.name + " ",
				"dify_base_url": "https://dify2.example.com/v1", "dify_api_key": "replacement-secret",
				"rpm_limit": 9, "deadline": deadline, "total_count": 15,
				"note": "  updated note  ", "review_note": "  updated review  ", "status": "active",
			}
			rec := donationRequest(gw, cookie, http.MethodPatch, tc.path(donation.ID), body)
			if rec.Code != http.StatusOK {
				t.Fatalf("full patch: status=%d body=%s", rec.Code, rec.Body.String())
			}
			var response struct {
				Donation map[string]interface{} `json:"donation"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if _, leaked := response.Donation["dify_api_key"]; leaked {
				t.Fatal("PATCH response leaked dify_api_key")
			}
			if response.Donation["has_review_record"] != true || response.Donation["review_note"] != "updated review" {
				t.Fatalf("review contract = %v", response.Donation)
			}
			stored, _ := store.GetDonation(donation.ID)
			storedApp, _ := store.GetApplication(app.ID)
			plain, decErr := store.Decrypt(stored.DifyAPIKeyEnc)
			if decErr != nil || plain != "replacement-secret" {
				t.Fatalf("stored key mismatch: plain=%q err=%v", plain, decErr)
			}
			if stored.Model != "shared-updated-"+tc.name || stored.Note != "updated note" || storedApp.ReviewNote != "updated review" || stored.Status != db.DonationActive {
				t.Fatalf("stored patch mismatch: donation=%+v app=%+v", stored, storedApp)
			}

			rec = donationRequest(gw, cookie, http.MethodPatch, tc.path(donation.ID), map[string]interface{}{"status": "inactive"})
			if rec.Code != http.StatusOK {
				t.Fatalf("minimal status patch: status=%d body=%s", rec.Code, rec.Body.String())
			}
			stored, _ = store.GetDonation(donation.ID)
			storedApp, _ = store.GetApplication(app.ID)
			if stored.Note != "updated note" || storedApp.ReviewNote != "updated review" {
				t.Fatalf("status-only patch changed omitted notes: note=%q review=%q", stored.Note, storedApp.ReviewNote)
			}

			rec = donationRequest(gw, cookie, http.MethodPatch, tc.path(donation.ID), map[string]interface{}{"note": "", "review_note": ""})
			if rec.Code != http.StatusOK {
				t.Fatalf("clear notes patch: status=%d body=%s", rec.Code, rec.Body.String())
			}
			stored, _ = store.GetDonation(donation.ID)
			storedApp, _ = store.GetApplication(app.ID)
			if stored.Note != "" || storedApp.ReviewNote != "" {
				t.Fatalf("explicit clear failed: note=%q review=%q", stored.Note, storedApp.ReviewNote)
			}
		})
	}
}

func TestDonationPatch_InvalidValuesConflictsAndConcurrentDelete(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)
	donation := seedHandlerS2Donation(t, store, "patch-invalid", db.DonationInactive, "original")
	path := fmt.Sprintf("/api/admin/donations/%d", donation.ID)
	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"zero rpm", map[string]interface{}{"rpm_limit": 0}},
		{"zero deadline", map[string]interface{}{"deadline": 0}},
		{"zero total", map[string]interface{}{"total_count": 0}},
		{"blank service", map[string]interface{}{"service": " "}},
		{"unknown service", map[string]interface{}{"service": "unknown"}},
		{"blank model", map[string]interface{}{"model": " "}},
		{"bracket model", map[string]interface{}{"model": "bad[model]"}},
		{"blank key", map[string]interface{}{"dify_api_key": "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := donationRequest(gw, admin, http.MethodPatch, path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 body=%s", rec.Code, rec.Body.String())
			}
			stored, _ := store.GetDonation(donation.ID)
			if stored.Note != "original" || stored.Model != "patch-invalid" {
				t.Fatalf("invalid patch changed row: %+v", stored)
			}
		})
	}

	other := seedHandlerS2Donation(t, store, "patch-conflict", db.DonationInactive, "other")
	if _, err := store.RawExec(`CREATE UNIQUE INDEX s2_unique_donation_pair ON donations(service, model)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	rec := donationRequest(gw, admin, http.MethodPatch, path, map[string]interface{}{"model": other.Model})
	if rec.Code != http.StatusConflict {
		t.Fatalf("unique conflict status=%d body=%s", rec.Code, rec.Body.String())
	}

	trigger := fmt.Sprintf(`CREATE TRIGGER s2_delete_donation_before_update BEFORE UPDATE ON donations
		WHEN OLD.id=%d BEGIN DELETE FROM donations WHERE id=OLD.id; END`, donation.ID)
	if _, err := store.RawExec(trigger); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}
	rec = donationRequest(gw, admin, http.MethodPatch, path, map[string]interface{}{"note": "changed"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("concurrent delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDonationList_HasReviewRecordDistinguishesEmptyAndAbsent(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)
	_, linked := seedHandlerS2LinkedDonation(t, store, "list-linked-empty", "")
	direct := seedHandlerS2Donation(t, store, "list-direct", db.DonationInactive, "direct")
	rec := donationRequest(gw, admin, http.MethodGet, "/api/admin/donations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Donations []map[string]interface{} `json:"donations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	byID := make(map[int64]map[string]interface{})
	for _, donation := range response.Donations {
		byID[int64(donation["id"].(float64))] = donation
	}
	if byID[linked.ID]["has_review_record"] != true || byID[linked.ID]["review_note"] != "" {
		t.Fatalf("linked empty review record = %v", byID[linked.ID])
	}
	if byID[direct.ID]["has_review_record"] != false {
		t.Fatalf("direct review record = %v", byID[direct.ID])
	}
	if _, ok := byID[direct.ID]["review_note"]; ok {
		t.Fatalf("direct donation unexpectedly has review_note: %v", byID[direct.ID])
	}
}

func TestPricingPatch_AtomicAndSharedErrorContract(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		cookie func(*testing.T, *Gateway, *db.Store) *http.Cookie
	}{
		{"admin", "/api/admin/pricing", func(t *testing.T, gw *Gateway, _ *db.Store) *http.Cookie { return adminCookie(t, gw) }},
		{"level5", "/api/me/charity-admin/pricing", func(t *testing.T, _ *Gateway, store *db.Store) *http.Cookie {
			u, _ := store.CreateUser("pricing-level5", "pricing-level5", "")
			level := 5
			store.SetUserLevel(u.ID, &level)
			return meUserCookie(t, store, u)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw, store := setupAuthGateway(t, "x")
			cookie := tc.cookie(t, gw, store)
			reward := 5
			if _, err := store.UpsertPricing("general", "pricing-"+tc.name, 10, &reward); err != nil {
				t.Fatal(err)
			}
			invalidCases := []map[string]interface{}{
				{"enabled": true, "price": -1, "reward": 8},
				{"enabled": true, "price": 20, "reward": -1},
				{"enabled": "yes", "price": 20, "reward": 8},
			}
			for i, invalid := range invalidCases {
				invalid["service"] = "general"
				invalid["model"] = "pricing-" + tc.name
				rec := donationRequest(gw, cookie, http.MethodPatch, tc.path, invalid)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("invalid patch %d status=%d body=%s", i, rec.Code, rec.Body.String())
				}
				stored, _ := store.GetPricing("general", "pricing-"+tc.name)
				if stored.Price != 10 || stored.Reward != 5 || stored.Enabled {
					t.Fatalf("invalid patch %d partially persisted: %+v", i, stored)
				}
			}
			rec := donationRequest(gw, cookie, http.MethodPatch, tc.path, map[string]interface{}{
				"service": "general", "model": "missing-" + tc.name, "enabled": true,
			})
			if rec.Code != http.StatusNotFound {
				t.Fatalf("missing patch status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)
	reward := 5
	if _, err := store.UpsertPricing("general", "pricing-trigger", 10, &reward); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RawExec(`CREATE TRIGGER s2_fail_pricing_update BEFORE UPDATE ON charity_pricing
		BEGIN SELECT RAISE(ABORT, 'injected pricing update failure'); END`); err != nil {
		t.Fatal(err)
	}
	rec := donationRequest(gw, admin, http.MethodPatch, "/api/admin/pricing", map[string]interface{}{
		"service": "general", "model": "pricing-trigger", "enabled": true, "price": 20, "reward": 8,
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("injected failure status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, _ := store.GetPricing("general", "pricing-trigger")
	if stored.Price != 10 || stored.Reward != 5 || stored.Enabled {
		t.Fatalf("trigger failure partially persisted: %+v", stored)
	}

	gw2, store2 := setupAuthGateway(t, "x")
	admin2 := adminCookie(t, gw2)
	if _, err := store2.UpsertPricing("general", "pricing-delete-race", 10, &reward); err != nil {
		t.Fatal(err)
	}
	if _, err := store2.RawExec(`CREATE TRIGGER s2_delete_pricing_before_update BEFORE UPDATE ON charity_pricing
		BEGIN DELETE FROM charity_pricing WHERE service=OLD.service AND model=OLD.model; END`); err != nil {
		t.Fatal(err)
	}
	rec = donationRequest(gw2, admin2, http.MethodPatch, "/api/admin/pricing", map[string]interface{}{
		"service": "general", "model": "pricing-delete-race", "price": 20,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("concurrent pricing delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDonationInputValidation_AllEntryPoints(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	admin := adminCookie(t, gw)
	store.SetSetting(db.SettingDonationEnabled, "true")
	userCookie := appUserCookie(t, gw, store)
	deadline := time.Now().Add(48 * time.Hour).Unix()

	base := map[string]interface{}{
		"service": "general", "model": "valid-model", "dify_base_url": "https://dify.example.com/v1",
		"dify_api_key": "valid-secret", "deadline": deadline, "total_count": 10,
		"source_text": "source", "note": "source",
	}
	invalid := []struct {
		name  string
		field string
		value interface{}
	}{
		{"blank service", "service", "   "},
		{"unknown service", "service", "unknown"},
		{"blank model", "model", "   "},
		{"bracket model", "model", "bad[model]"},
		{"blank key", "dify_api_key", "   "},
	}
	for _, tc := range invalid {
		t.Run("create "+tc.name, func(t *testing.T) {
			body := make(map[string]interface{}, len(base))
			for k, v := range base {
				body[k] = v
			}
			body[tc.field] = tc.value
			rec := donationRequest(gw, admin, http.MethodPost, "/api/admin/donations", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
		t.Run("application "+tc.name, func(t *testing.T) {
			body := make(map[string]interface{}, len(base))
			for k, v := range base {
				body[k] = v
			}
			delete(body, "source_text")
			body[tc.field] = tc.value
			rec := donationRequest(gw, userCookie, http.MethodPost, "/api/me/donations", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	validCreate := make(map[string]interface{}, len(base))
	for k, v := range base {
		validCreate[k] = v
	}
	validCreate["model"] = "valid-create-response"
	rec := donationRequest(gw, admin, http.MethodPost, "/api/admin/donations", validCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("legal create regressed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createResponse struct {
		Donation map[string]interface{} `json:"donation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResponse); err != nil {
		t.Fatal(err)
	}
	if _, leaked := createResponse.Donation["dify_api_key"]; leaked {
		t.Fatal("create response leaked dify_api_key")
	}

	appUser, _ := store.CreateUser("approval-validation", "approval-validation", "")
	app, err := store.CreateDonationApplication(
		appUser.ID, "general", "approval-valid", "https://dify.example.com/v1", "valid-secret",
		10, deadline, 5, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	approvalCases := []map[string]interface{}{
		{"service": "unknown"}, {"model": " "}, {"model": "bad[model]"}, {"dify_api_key": "   "},
	}
	for i, body := range approvalCases {
		rec := donationRequest(gw, admin, http.MethodPost, fmt.Sprintf("/api/admin/donations/%d/approve", app.ID), body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("approval case %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
		stored, _ := store.GetApplication(app.ID)
		if stored.Status != db.AppStatusPending || stored.DonationID != (sql.NullInt64{}) {
			t.Fatalf("invalid approval changed application: %+v", stored)
		}
	}
	rec = donationRequest(gw, admin, http.MethodPost, fmt.Sprintf("/api/admin/donations/%d/approve", app.ID), map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("legal approval regressed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
