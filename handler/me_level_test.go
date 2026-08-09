package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

// meUserCookie creates a session cookie for an existing user.
func meUserCookie(t *testing.T, store *db.Store, u *db.User) *http.Cookie {
	t.Helper()
	sess, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: sess}
}

// TestMeLevelEndpoints_LevelMatrix exercises the full R-A permission matrix
// over every new /api/me/... level-gated endpoint: anonymous 401, levels
// below the requirement 403, at-level users pass (mutating endpoints then
// fail on their own validation — the guard must have already passed),
// and the admin passes everything via requireLevel.
func TestMeLevelEndpoints_LevelMatrix(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	lv1, _ := store.CreateUser("2001", "lv1", "")
	lv2, _ := store.CreateUser("2002", "lv2", "")
	store.SetUserDonationCredit(lv2.ID, 50) // level 2
	lv3, _ := store.CreateUser("2003", "lv3", "")
	store.SetUserDonationCredit(lv3.ID, 250) // level 3
	lv4, _ := store.CreateUser("2004", "lv4", "")
	store.SetUserDonationCredit(lv4.ID, 600) // level 4
	lv5, _ := store.CreateUser("2005", "lv5", "")
	five := 5
	store.SetUserLevel(lv5.ID, &five) // manual level 5

	cookies := map[string]*http.Cookie{
		"anon":  nil,
		"lv1":   meUserCookie(t, store, lv1),
		"lv2":   meUserCookie(t, store, lv2),
		"lv3":   meUserCookie(t, store, lv3),
		"lv4":   meUserCookie(t, store, lv4),
		"lv5":   meUserCookie(t, store, lv5),
		"admin": adminCookie(t, gw),
	}
	levelOf := map[string]int{"anon": 0, "lv1": 1, "lv2": 2, "lv3": 3, "lv4": 4, "lv5": 5, "admin": 99}

	// GET rows assert the exact status; mutating rows only assert that the
	// guard was passed (body/state errors downstream are expected).
	rows := []struct {
		name   string
		method string
		path   string
		min    int
		body   string
		exact  bool
	}{
		{name: "review pending", method: http.MethodGet, path: "/api/me/review/pending", min: 4, exact: true},
		{name: "review approve single", method: http.MethodPost, path: "/api/me/review/1/approve", min: 4, body: `{}`},
		{name: "review reject single", method: http.MethodPost, path: "/api/me/review/1/reject", min: 4, body: `{}`},
		{name: "review approve batch", method: http.MethodPost, path: "/api/me/review/approve/batch", min: 4, body: `{"ids":[1]}`},
		{name: "review reject batch", method: http.MethodPost, path: "/api/me/review/reject/batch", min: 4, body: `{"ids":[1]}`},
		{name: "charity donations list", method: http.MethodGet, path: "/api/me/charity-admin/donations", min: 5, exact: true},
		{name: "charity donation create", method: http.MethodPost, path: "/api/me/charity-admin/donations", min: 5, body: `{}`},
		{name: "charity donation patch", method: http.MethodPatch, path: "/api/me/charity-admin/donations/1", min: 5, body: `{}`},
		{name: "charity donation status", method: http.MethodPost, path: "/api/me/charity-admin/donations/1/status", min: 5, body: `{"status":"active"}`},
		{name: "charity donation delete", method: http.MethodDelete, path: "/api/me/charity-admin/donations/1", min: 5},
		{name: "charity donation status batch", method: http.MethodPost, path: "/api/me/charity-admin/donations/status/batch", min: 5, body: `{"ids":[1],"status":"active"}`},
		{name: "charity donation delete batch", method: http.MethodPost, path: "/api/me/charity-admin/donations/delete/batch", min: 5, body: `{"ids":[1]}`},
		{name: "pricing list", method: http.MethodGet, path: "/api/me/charity-admin/pricing", min: 5, exact: true},
		{name: "pricing upsert", method: http.MethodPut, path: "/api/me/charity-admin/pricing", min: 5, body: `{}`},
		{name: "pricing patch", method: http.MethodPatch, path: "/api/me/charity-admin/pricing", min: 5, body: `{}`},
		{name: "pricing delete", method: http.MethodDelete, path: "/api/me/charity-admin/pricing", min: 5, body: `{}`},
		{name: "pricing delete batch", method: http.MethodPost, path: "/api/me/charity-admin/pricing/delete/batch", min: 5, body: `{"pairs":[{"service":"general","model":"x"}]}`},
		{name: "all-logs list", method: http.MethodGet, path: "/api/me/all-logs", min: 5, exact: true},
		{name: "all-logs stats", method: http.MethodGet, path: "/api/me/all-logs/stats", min: 5, exact: true},
	}

	for _, row := range rows {
		for who, cookie := range cookies {
			t.Run(row.name+"/"+who, func(t *testing.T) {
				rec := donationRequest(gw, cookie, row.method, row.path, nil)
				if row.body != "" {
					// donationRequest marshals a map; pass a raw string body
					// via a dedicated recorder for the mutating rows.
					req := httptest.NewRequest(row.method, row.path, strings.NewReader(row.body))
					req.Header.Set("Content-Type", "application/json")
					if cookie != nil {
						req.AddCookie(cookie)
					}
					rec = httptest.NewRecorder()
					mux.ServeHTTP(rec, req)
				}
				level := levelOf[who]
				switch {
				case level == 0:
					if rec.Code != http.StatusUnauthorized {
						t.Fatalf("status=%d want 401, body=%s", rec.Code, rec.Body.String())
					}
				case level < row.min:
					if rec.Code != http.StatusForbidden {
						t.Fatalf("status=%d want 403, body=%s", rec.Code, rec.Body.String())
					}
				case row.exact:
					if rec.Code != http.StatusOK {
						t.Fatalf("status=%d want 200, body=%s", rec.Code, rec.Body.String())
					}
				default:
					if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
						t.Fatalf("guard rejected level %d (want pass), status=%d body=%s", level, rec.Code, rec.Body.String())
					}
				}
			})
		}
	}
}

// TestMeReview_Level4FullFlowAndReviewerID verifies that a level-4 user can
// list/approve/reject/batch-review pending applications with the exact same
// store semantics as the admin path (field validation, idempotency, batch
// atomicity), and that reviewer_id records the operator's users.id so the
// audit trail distinguishes administrators from co-admins.
func TestMeReview_Level4FullFlowAndReviewerID(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	store.SetSetting(db.SettingDonationEnabled, "true")
	applicant := appUserCookie(t, gw, store)
	lv4, _ := store.CreateUser("2009", "reviewer4", "")
	store.SetUserDonationCredit(lv4.ID, 600) // automatic level 4
	lv4C := meUserCookie(t, store, lv4)

	deadline := time.Now().Add(48 * time.Hour).Unix()
	submit := func(model string) int64 {
		rec := donationRequest(gw, applicant, "POST", "/api/me/donations", map[string]interface{}{
			"service":       "general",
			"model":         model,
			"dify_base_url": "https://dify.example.com/v1",
			"dify_api_key":  "k-" + model,
			"deadline":      deadline,
			"total_count":   100,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("submit %s: status=%d body=%s", model, rec.Code, rec.Body.String())
		}
		var resp struct {
			Application map[string]interface{} `json:"application"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return int64(resp.Application["id"].(float64))
	}
	reviewerOf := func(appID int64) int64 {
		app, err := store.GetApplication(appID)
		if err != nil {
			t.Fatalf("get application: %v", err)
		}
		if !app.ReviewerID.Valid {
			t.Fatalf("application %d has no reviewer_id", appID)
		}
		return app.ReviewerID.Int64
	}

	// Pending list is visible to the level-4 co-admin.
	appID := submit("claude-opus-4-6")
	rec := donationRequest(gw, lv4C, "GET", "/api/me/review/pending", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pending: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pending struct {
		Applications []map[string]interface{} `json:"applications"`
	}
	json.Unmarshal(rec.Body.Bytes(), &pending)
	if len(pending.Applications) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending.Applications))
	}

	// Approve: reviewer_id is the level-4 operator.
	rec = donationRequest(gw, lv4C, "POST", fmt.Sprintf("/api/me/review/%d/approve", appID), map[string]interface{}{"review_note": "密钥已更新，审核通过"})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := reviewerOf(appID); got != lv4.ID {
		t.Errorf("reviewer_id = %d, want level-4 user %d", got, lv4.ID)
	}

	// Idempotency: re-approving the same application is rejected (not pending).
	rec = donationRequest(gw, lv4C, "POST", fmt.Sprintf("/api/me/review/%d/approve", appID), map[string]interface{}{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("re-approve: status=%d want 400, body=%s", rec.Code, rec.Body.String())
	}

	// Reject records the same operator.
	appID2 := submit("claude-haiku")
	rec = donationRequest(gw, lv4C, "POST", fmt.Sprintf("/api/me/review/%d/reject", appID2), map[string]interface{}{"review_note": "模型名不符合规范"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reject: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := reviewerOf(appID2); got != lv4.ID {
		t.Errorf("reject reviewer_id = %d, want level-4 user %d", got, lv4.ID)
	}

	// Batch approve two pending applications atomically.
	appID3, appID4 := submit("gpt-5.6-sol"), submit("claude-sonnet-4-6")
	rec = donationRequest(gw, lv4C, "POST", "/api/me/review/approve/batch", map[string]interface{}{"ids": []int64{appID3, appID4}, "review_note": "batch"})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch approve: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reviewerOf(appID3) != lv4.ID || reviewerOf(appID4) != lv4.ID {
		t.Error("batch approve reviewer_id mismatch")
	}

	// Batch approve of an already-approved id fails with failed_id.
	rec = donationRequest(gw, lv4C, "POST", "/api/me/review/approve/batch", map[string]interface{}{"ids": []int64{appID3}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("re-batch: status=%d want 400, body=%s", rec.Code, rec.Body.String())
	}
	var batchErr struct {
		FailedID int64 `json:"failed_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &batchErr)
	if batchErr.FailedID != appID3 {
		t.Errorf("failed_id = %d, want %d", batchErr.FailedID, appID3)
	}

	// Batch reject.
	appID5 := submit("claude-3-7-sonnet")
	rec = donationRequest(gw, lv4C, "POST", "/api/me/review/reject/batch", map[string]interface{}{"ids": []int64{appID5}})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch reject: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reviewerOf(appID5) != lv4.ID {
		t.Error("batch reject reviewer_id mismatch")
	}
}

// TestMeCharityAdmin_Level5CRUD verifies the level-5 charity co-admin
// surface: donation CRUD/status/batch and pricing CRUD/batch-delete all
// delegate to the shared admin cores.
func TestMeCharityAdmin_Level5CRUD(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	lv5, _ := store.CreateUser("2010", "coadmin5", "")
	five := 5
	store.SetUserLevel(lv5.ID, &five)
	c := meUserCookie(t, store, lv5)

	deadline := time.Now().Add(48 * time.Hour).Unix()

	// Create a donation.
	rec := donationRequest(gw, c, "POST", "/api/me/charity-admin/donations", map[string]interface{}{
		"service":       "general",
		"model":         "coadmin-model",
		"dify_base_url": "https://dify.example.com/v1",
		"dify_api_key":  "don-key",
		"deadline":      deadline,
		"total_count":   10,
		"rpm_limit":     5,
		"source_text":   "co-admin entry",
		"note":          "co-admin",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create donation: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Donation map[string]interface{} `json:"donation"`
	}
	json.Unmarshal(rec.Body.Bytes(), &createResp)
	donID := int64(createResp.Donation["id"].(float64))

	// List shows the entry (has_key, never the plaintext key).
	rec = donationRequest(gw, c, "GET", "/api/me/charity-admin/donations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list donations: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Donations []map[string]interface{} `json:"donations"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Donations) != 1 || int64(listResp.Donations[0]["id"].(float64)) != donID {
		t.Fatalf("list donations = %v", listResp.Donations)
	}
	if listResp.Donations[0]["has_key"] != true {
		t.Error("has_key should be true")
	}
	if _, leaked := listResp.Donations[0]["dify_api_key"]; leaked {
		t.Error("dify_api_key leaked in list response")
	}

	// Activating without pricing is rejected (same gate as admin).
	rec = donationRequest(gw, c, "POST", fmt.Sprintf("/api/me/charity-admin/donations/%d/status", donID), map[string]interface{}{"status": "active"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("activate without pricing: status=%d want 400, body=%s", rec.Code, rec.Body.String())
	}

	// Pricing upsert, then activation succeeds.
	rec = donationRequest(gw, c, "PUT", "/api/me/charity-admin/pricing", map[string]interface{}{"service": "general", "model": "coadmin-model", "price": 100})
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = donationRequest(gw, c, "POST", fmt.Sprintf("/api/me/charity-admin/donations/%d/status", donID), map[string]interface{}{"status": "active"})
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Pricing list + patch (enable). Active donation PATCH requires the result
	// pair to have enabled pricing.
	rec = donationRequest(gw, c, "GET", "/api/me/charity-admin/pricing", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pricingResp struct {
		Pricing []map[string]interface{} `json:"pricing"`
	}
	json.Unmarshal(rec.Body.Bytes(), &pricingResp)
	if len(pricingResp.Pricing) != 1 {
		t.Fatalf("pricing = %v", pricingResp.Pricing)
	}
	// Upsert creates the row (default disabled); the enabled toggle is a
	// separate PATCH — same contract as the admin endpoint.
	if pricingResp.Pricing[0]["service"] != "general" || pricingResp.Pricing[0]["model"] != "coadmin-model" || pricingResp.Pricing[0]["price"].(float64) != 100 {
		t.Fatalf("pricing = %v", pricingResp.Pricing)
	}
	rec = donationRequest(gw, c, "PATCH", "/api/me/charity-admin/pricing", map[string]interface{}{"service": "general", "model": "coadmin-model", "enabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Donation patch (review note + note).
	rec = donationRequest(gw, c, "PATCH", fmt.Sprintf("/api/me/charity-admin/donations/%d", donID), map[string]interface{}{"note": "updated by co-admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch donation: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Batch delete donations first (pricing delete is blocked while a
	// donation references the pair), then batch delete pricing.
	rec = donationRequest(gw, c, "POST", "/api/me/charity-admin/donations/delete/batch", map[string]interface{}{"ids": []int64{donID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete donations: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = donationRequest(gw, c, "POST", "/api/me/charity-admin/pricing/delete/batch", map[string]interface{}{
		"pairs": []map[string]string{{"service": "general", "model": "coadmin-model"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Pricing delete via DELETE verb works too.
	rec = donationRequest(gw, c, "PUT", "/api/me/charity-admin/pricing", map[string]interface{}{"service": "general", "model": "coadmin-model", "price": 50})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-upsert pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = donationRequest(gw, c, "DELETE", "/api/me/charity-admin/pricing", map[string]interface{}{"service": "general", "model": "coadmin-model"})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete pricing: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestMeAllLogs_SanitizationBoundary is the R-B negative test for the
// level-5 all-logs view: error_detail must never contain URLs, domains, IPs
// or secrets, while the admin view keeps the raw stored text.
func TestMeAllLogs_SanitizationBoundary(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	lv5, _ := store.CreateUser("2006", "auditor", "")
	five := 5
	store.SetUserLevel(lv5.ID, &five)
	lv5C := meUserCookie(t, store, lv5)
	adminC := adminCookie(t, gw)

	evil := `Post "https://dify.internal.example.com/v1/workflows/run": dial tcp 10.0.0.5:443: connection refused (sk-secret-abc123)`
	now := time.Now()
	if _, err := store.AddRequestLogFull(100, "general", "[general]x", now, now.Add(time.Second), "error", "upstream_error", http.StatusBadGateway, evil, 0, 0, ""); err != nil {
		t.Fatalf("insert log: %v", err)
	}

	fetch := func(cookie *http.Cookie, path string) []map[string]interface{} {
		rec := donationRequest(gw, cookie, "GET", path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var resp struct {
			Logs []map[string]interface{} `json:"logs"`
		}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Logs
	}

	// Level-5 view is sanitized.
	meLogs := fetch(lv5C, "/api/me/all-logs")
	if len(meLogs) != 1 {
		t.Fatalf("all-logs rows = %d, want 1", len(meLogs))
	}
	detail, _ := meLogs[0]["error_detail"].(string)
	for _, needle := range []string{"dify.internal", "10.0.0.5", "sk-secret", "http", "connection refused"} {
		if strings.Contains(detail, needle) {
			t.Errorf("level-5 error_detail leaks %q: %q", needle, detail)
		}
	}
	if detail == "" {
		t.Error("level-5 error_detail is empty; want the generic localized diagnosis")
	}

	// Admin view keeps the raw stored text.
	adminLogs := fetch(adminC, "/api/admin/logs")
	if len(adminLogs) != 1 {
		t.Fatalf("admin logs rows = %d, want 1", len(adminLogs))
	}
	if adminLogs[0]["error_detail"] != evil {
		t.Errorf("admin error_detail = %q, want raw %q", adminLogs[0]["error_detail"], evil)
	}
}

// TestMeAllLogsStats_Level5 verifies the level-5 stats endpoint reuses the
// hourly contract (by_hour + empty by_service compatibility field).
func TestMeAllLogsStats_Level5(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	lv5, _ := store.CreateUser("2011", "statistician", "")
	five := 5
	store.SetUserLevel(lv5.ID, &five)
	now := time.Now()
	store.AddRequestLogFull(lv5.ID, "general", "[general]x", now, now.Add(time.Second), "success", "", http.StatusOK, "", 0, 0, "")

	rec := donationRequest(gw, meUserCookie(t, store, lv5), "GET", "/api/me/all-logs/stats", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ByHour    []map[string]interface{} `json:"by_hour"`
		ByService []interface{}            `json:"by_service"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.ByHour) == 0 {
		t.Error("by_hour empty; want at least one hourly bucket")
	}
	if resp.ByService == nil {
		t.Error("by_service missing; want empty compatibility array")
	}
}

// TestMeLevel_DowngradeTakesEffectImmediately verifies lazy level
// computation: a manual downgrade or a credit change below the threshold
// revokes access on the very next request.
func TestMeLevel_DowngradeTakesEffectImmediately(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	u, _ := store.CreateUser("2008", "switcher", "")
	five := 5
	store.SetUserLevel(u.ID, &five)
	c := meUserCookie(t, store, u)

	if rec := donationRequest(gw, c, "GET", "/api/me/all-logs", nil); rec.Code != http.StatusOK {
		t.Fatalf("level 5 all-logs: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Restore automatic (0 credits -> level 1): immediate 403.
	store.SetUserLevel(u.ID, nil)
	if rec := donationRequest(gw, c, "GET", "/api/me/all-logs", nil); rec.Code != http.StatusForbidden {
		t.Errorf("after downgrade: status=%d want 403, body=%s", rec.Code, rec.Body.String())
	}

	// Credits-based promotion to level 4 works lazily...
	store.SetUserDonationCredit(u.ID, 600)
	if rec := donationRequest(gw, c, "GET", "/api/me/review/pending", nil); rec.Code != http.StatusOK {
		t.Errorf("auto level 4 review: status=%d want 200, body=%s", rec.Code, rec.Body.String())
	}

	// ...and dropping below the threshold revokes review access at once.
	store.SetUserDonationCredit(u.ID, 10)
	if rec := donationRequest(gw, c, "GET", "/api/me/review/pending", nil); rec.Code != http.StatusForbidden {
		t.Errorf("after credit drop: status=%d want 403, body=%s", rec.Code, rec.Body.String())
	}
}

// TestMeLevelEndpoints_HostSeparation verifies the gw.Wrap isolation
// contract: the level-gated endpoints live on the user host only; the admin
// host's /api/me exact-match allowlist keeps them unreachable there, while
// /api/admin/* keeps working on the admin host and stays hidden on the user
// host.
func TestMeLevelEndpoints_HostSeparation(t *testing.T) {
	gw, store := setupAuthGateway(t, "x")
	lv5, _ := store.CreateUser("2007", "auditor", "")
	five := 5
	store.SetUserLevel(lv5.ID, &five)
	userC := meUserCookie(t, store, lv5)
	adminC := adminCookie(t, gw)

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	h := gw.Wrap(mux)

	get := func(host, path string, cookie *http.Cookie) int {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// User host: level-gated endpoints reachable, /api/admin/* hidden.
	if code := get("localhost:10086", "/api/me/review/pending", userC); code != http.StatusOK {
		t.Errorf("user host review pending: status=%d want 200", code)
	}
	if code := get("localhost:10086", "/api/me/all-logs", userC); code != http.StatusOK {
		t.Errorf("user host all-logs: status=%d want 200", code)
	}
	if code := get("localhost:10086", "/api/admin/donations/pending", userC); code != http.StatusNotFound {
		t.Errorf("user host admin endpoint: status=%d want 404", code)
	}

	// Admin host: /api/me/* is allowlisted exactly, so the new level-gated
	// paths 404 by design; /api/admin/* remains available.
	if code := get("admin.localhost", "/api/me/review/pending", adminC); code != http.StatusNotFound {
		t.Errorf("admin host me review: status=%d want 404", code)
	}
	if code := get("admin.localhost", "/api/me/all-logs", adminC); code != http.StatusNotFound {
		t.Errorf("admin host me all-logs: status=%d want 404", code)
	}
	if code := get("admin.localhost", "/api/admin/donations/pending", adminC); code != http.StatusOK {
		t.Errorf("admin host admin endpoint: status=%d want 200", code)
	}

	// Cookie-authenticated state changes on the new endpoints are CSRF-
	// guarded like every other /api/* mutation: a missing Origin is 403.
	req := httptest.NewRequest(http.MethodPost, "http://localhost:10086/api/me/review/1/approve", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userC)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("missing Origin on approve: status=%d want 403 (CSRF), body=%s", rec.Code, rec.Body.String())
	}
	req.Header.Set("Origin", "http://localhost:10086")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// With the correct Origin the guard passes (level 5 >= 4) and the
	// request reaches the core, which 404s on the nonexistent id.
	if rec.Code != http.StatusNotFound {
		t.Errorf("valid Origin on approve: status=%d want 404 (no such application), body=%s", rec.Code, rec.Body.String())
	}
}

// TestMeLevel_AdminPassesRequireLevel verifies the administrator passes the
// level channel unconditionally (is_admin retains every capability).
func TestMeLevel_AdminPassesRequireLevel(t *testing.T) {
	gw, _ := setupAuthGateway(t, "x")
	adminC := adminCookie(t, gw)

	for _, path := range []string{"/api/me/review/pending", "/api/me/charity-admin/donations", "/api/me/charity-admin/pricing", "/api/me/all-logs", "/api/me/all-logs/stats"} {
		if rec := donationRequest(gw, adminC, "GET", path, nil); rec.Code != http.StatusOK {
			t.Errorf("admin GET %s: status=%d want 200, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
