package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
	"dify2api/difyapp"
)

// v1TestMux builds a fresh mux with the gateway routes.
func v1TestMux(gw *Gateway) *http.ServeMux {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	return mux
}

func userReq(mux *http.ServeMux, method, path string, cookies []*http.Cookie, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func loginUserCookie(t *testing.T, gw *Gateway, store *db.Store, discordID, username string) *http.Cookie {
	t.Helper()
	u, err := store.CreateUser(discordID, username, "")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: auth.SessionCookieName, Value: token}
}

func TestV1DownloadFlow(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)
	cookie := loginUserCookie(t, gw, store, "v1-user", "tester")

	// Unauthenticated -> 401.
	rec := userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=gpt-5.6-sol", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth download = %d", rec.Code)
	}

	// Unknown service -> 404.
	rec = userReq(mux, http.MethodGet, "/api/me/services/unknown/download?model=gpt-5.6-sol", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown service = %d", rec.Code)
	}

	// Missing model -> 404.
	rec = userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=does-not-exist", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad model = %d", rec.Code)
	}

	// Success: the selected model config is applied to the YAML attachment
	// before obfuscation (dependency pin, model/provider and params).
	rec = userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=claude-opus-4-6", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download = %d body=%s", rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "yaml") {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Disposition") == "" {
		t.Fatal("missing content-disposition")
	}
	body := rec.Body.Bytes()
	bodyText := string(body)
	if !strings.Contains(bodyText, "kind: app") {
		t.Fatal("download body is not a Dify app DSL")
	}
	for _, want := range []string{
		"langgenius/anthropic:0.3.26@e4580f78789aec59eabdafcd85ca75358ae4895134de28dbae5e38e9b307eb70",
		"name: claude-opus-4-6",
		"provider: langgenius/anthropic/anthropic",
		"context_1m: true",
		"temperature: 0.9",
	} {
		if !strings.Contains(bodyText, want) {
			t.Fatalf("downloaded Claude DSL missing %q", want)
		}
	}
	vars, err := difyapp.ExtractStartVariables(body)
	if err != nil || len(vars) < 403 || len(vars) > 418 {
		t.Fatalf("downloaded vars = %d err=%v, want 403+dummies", len(vars), err)
	}

	// Models list endpoint for the download modal.
	rec = userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/models", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("models = %d", rec.Code)
	}
	var ml struct {
		Models []db.ModelConfig `json:"models"`
	}
	json.Unmarshal(rec.Body.Bytes(), &ml)
	if len(ml.Models) != 2 {
		t.Fatalf("model list = %d, want 2", len(ml.Models))
	}

	// Mapping now exists -> chat-path remap works.
	u, _ := store.GetUserByDiscordID("v1-user")
	inputs, mapped, err := gw.remapInputsForService(u.ID, "sillytavern-main-v1", map[string]string{"system_prompt": "sys", "user_0": "hi"}, "personal")
	if err != nil || !mapped {
		t.Fatalf("remap after download: mapped=%v err=%v", mapped, err)
	}
	if len(inputs) != 2 {
		t.Fatalf("remap size = %d, want 2: %+v", len(inputs), inputs)
	}
	var sawSys, sawHi bool
	for k, v := range inputs {
		if v == "sys" {
			sawSys = true
		}
		if v == "hi" {
			sawHi = true
		}
		if k == "system_prompt" || strings.HasPrefix(k, "user_") {
			t.Fatalf("canonical key leaked through remap: %q", k)
		}
	}
	if !sawSys || !sawHi {
		t.Fatalf("remap lost values: %+v", inputs)
	}

	// Rate limit: 3 more downloads hit the cap (limit 3/min).
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=gpt-5.6-sol", []*http.Cookie{cookie}, "")
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit not enforced: %d", last.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestV1ChatRequiresDownload(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)
	u, err := store.CreateUser("v1-chat", "tester", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateAppConfig(u.ID, "[sillytavern-main-v1]backend", "http://example.com", "app-key", "note"); err != nil {
		t.Fatal(err)
	}
	plain, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Without a download, chat refuses with template_not_downloaded.
	body := `{"model":"[sillytavern-main-v1]backend","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plain)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("chat without download = %d body=%s", rec.Code, rec.Body.String()[:300])
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Error.Code != "template_not_downloaded" {
		t.Fatalf("error code = %q", errResp.Error.Code)
	}
}

func TestV1DeprecatedServicesAppearInAdminAntiAbuse(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	rec := userReq(mux, http.MethodGet, "/api/admin/anti-abuse", []*http.Cookie{adminCookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("anti-abuse list = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Configs []struct {
			Service    string `json:"service"`
			Deprecated bool   `json:"deprecated"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, config := range body.Configs {
		seen[config.Service] = config.Deprecated
	}
	if !seen["sillytavern-main-200"] || !seen["sillytavern-main-trimmed"] || seen["sillytavern-main-v1"] {
		t.Fatalf("admin deprecated flags = %+v", seen)
	}
}

func TestV1ModelConfigSeedBackfillsExtractedParams(t *testing.T) {
	_, store := setupAuthGateway(t, "s3cret")
	if _, err := store.Exec(`UPDATE dify_model_configs SET params_json='', manual=0 WHERE model_key='claude-opus-4-6'`); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedModelConfigs(); err != nil {
		t.Fatal(err)
	}
	mc, err := store.GetModelConfig("claude-opus-4-6")
	if err != nil || mc == nil {
		t.Fatal(err)
	}
	if !strings.Contains(mc.ParamsJSON, `"context_1m":true`) || mc.DependencyHash != "e4580f78789aec59eabdafcd85ca75358ae4895134de28dbae5e38e9b307eb70" {
		t.Fatalf("backfilled config = %+v", mc)
	}
}

func TestV1ModelConfigsAdmin(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)

	// Unauthenticated -> 401.
	rec := userReq(mux, http.MethodGet, "/api/admin/model-configs", nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d", rec.Code)
	}

	// Admin login.
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	rec = userReq(mux, http.MethodGet, "/api/admin/model-configs", []*http.Cookie{adminCookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var list struct {
		Models []db.ModelConfig `json:"models"`
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Models) != 2 {
		t.Fatalf("seeded configs = %d, want 2", len(list.Models))
	}

	// Upsert + manual flag protection.
	rec = userReq(mux, http.MethodPut, "/api/admin/model-configs", []*http.Cookie{adminCookie},
		`{"model_key":"claude-opus-4-6","display_name":"Claude Opus 4.6","provider":"anthropic","dependency_plugin":"langgenius/anthropic","dependency_version":"9.9.9","dependency_hash":"deadbeef","manual":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put manual = %d %s", rec.Code, rec.Body.String())
	}
	// A non-manual PUT must not silently strip the manual protection.
	rec = userReq(mux, http.MethodPut, "/api/admin/model-configs", []*http.Cookie{adminCookie},
		`{"model_key":"claude-opus-4-6","display_name":"Claude Opus 4.6","provider":"anthropic","dependency_plugin":"langgenius/anthropic","dependency_version":"9.9.9","dependency_hash":"deadbeef","manual":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put non-manual = %d", rec.Code)
	}
	mc, _ := gw.Store.GetModelConfig("claude-opus-4-6")
	if !mc.Manual {
		t.Fatal("manual protection was stripped")
	}

	// Invalid params_json is rejected at the API boundary.
	rec = userReq(mux, http.MethodPut, "/api/admin/model-configs", []*http.Cookie{adminCookie},
		`{"model_key":"bad","display_name":"Bad","provider":"openai","dependency_plugin":"langgenius/openai","dependency_version":"1","dependency_hash":"hash","params_json":"[]","enabled":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid params_json = %d %s", rec.Code, rec.Body.String())
	}

	// Delete.
	rec = userReq(mux, http.MethodDelete, "/api/admin/model-configs/claude-opus-4-6", []*http.Cookie{adminCookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if mc2, _ := gw.Store.GetModelConfig("claude-opus-4-6"); mc2 != nil {
		t.Fatal("config not deleted")
	}
}

func TestV1DonationRegenerationGate(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)
	cookie := loginUserCookie(t, gw, store, "v1-donor", "donor")
	u, _ := store.GetUserByDiscordID("v1-donor")

	// No entries -> allowed.
	gate, err := gw.checkDonationRegeneration(u.ID, "sillytavern-main-v1", false)
	if err != nil || gate != "allow" {
		t.Fatalf("no entries gate = %q err=%v", gate, err)
	}

	// Pending application -> confirm required; confirm invalidates.
	app, err := store.CreateDonationApplication(u.ID, "sillytavern-main-v1", "gpt-5.6-sol",
		"http://example.com", "key", 10, time.Now().Add(24*time.Hour).Unix(), 10, "note")
	if err != nil {
		t.Fatal(err)
	}
	gate, _ = gw.checkDonationRegeneration(u.ID, "sillytavern-main-v1", false)
	if gate != "need_confirm" {
		t.Fatalf("pending gate = %q, want need_confirm", gate)
	}
	gate, _ = gw.checkDonationRegeneration(u.ID, "sillytavern-main-v1", true)
	if gate != "allow" {
		t.Fatalf("confirmed gate = %q, want allow", gate)
	}
	app2, _ := store.GetApplication(app.ID)
	if app2.Status != db.AppStatusInvalidated {
		t.Fatalf("application status = %q, want invalidated", app2.Status)
	}

	// Active donation -> blocked.
	if _, err := store.CreateDonation(&db.Donation{
		Service: "sillytavern-main-v1", Model: "gpt-5.6-sol",
		DifyBaseURL: "http://example.com", SourceUserID: sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: "v1-donor", SourceUsername: "donor",
		Deadline:   time.Now().Add(24 * time.Hour).Unix(),
		TotalCount: 10, RemainingCount: 10, RpmLimit: 10,
		Status: db.DonationActive,
	}, "key"); err != nil {
		t.Fatal(err)
	}
	gate, _ = gw.checkDonationRegeneration(u.ID, "sillytavern-main-v1", true)
	if gate != "blocked" {
		t.Fatalf("active gate = %q, want blocked", gate)
	}
	// Endpoint also refuses with donation_locked.
	rec := userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=gpt-5.6-sol&purpose=donation&confirm=true", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("donation download with active = %d", rec.Code)
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Error.Code != "donation_locked" {
		t.Fatalf("error code = %q", errResp.Error.Code)
	}
}

func TestV1DonationSnapshotFlow(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := v1TestMux(gw)
	cookie := loginUserCookie(t, gw, store, "v1-snapshot", "donor")
	u, _ := store.GetUserByDiscordID("v1-snapshot")

	// Enable self-service donations for the test.
	if err := store.SetSetting(db.SettingDonationEnabled, "true"); err != nil {
		t.Fatal(err)
	}

	// Download the donation-purpose template (creates the donation mapping).
	rec := userReq(mux, http.MethodGet, "/api/me/services/sillytavern-main-v1/download?model=gpt-5.6-sol&purpose=donation", []*http.Cookie{cookie}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("donation download = %d", rec.Code)
	}

	// Submit a donation application — must succeed with a snapshot attached.
	body := `{"service":"sillytavern-main-v1","model":"gpt-5.6-sol","dify_base_url":"http://example.com","dify_api_key":"app-key","deadline":` +
		fmt.Sprintf("%d", time.Now().Add(24*time.Hour).Unix()) + `,"total_count":10,"rpm_limit":10,"note":"donate"}`
	rec = userReq(mux, http.MethodPost, "/api/me/donations", []*http.Cookie{cookie}, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("donation app = %d %s", rec.Code, rec.Body.String()[:300])
	}
	apps, err := store.ListApplicationsByUser(u.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("applications = %d err=%v", len(apps), err)
	}
	app := apps[0]
	if app.MappingJSON == "" {
		t.Fatal("mapping snapshot not attached to application")
	}

	// Without a donation download, the application is refused.
	u2, _ := store.CreateUser("v1-snapshot-2", "nodownload", "")
	sess2, _, _ := store.CreateSession(u2.ID)
	cookie2 := &http.Cookie{Name: auth.SessionCookieName, Value: sess2}
	rec = userReq(mux, http.MethodPost, "/api/me/donations", []*http.Cookie{cookie2}, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no-download donation app = %d", rec.Code)
	}

	// Admin approval copies the snapshot into the donation row.
	admin := loginCookie(t, gw, "root", "s3cret")
	reviewer, _ := store.GetUserByDiscordID("__admin__")
	if reviewer == nil {
		reviewer, _ = store.EnsureAdminUser("root")
	}
	_, donation, err := store.ApproveApplication(app.ID, reviewer.ID, nil, "approved")
	if err != nil {
		t.Fatal(err)
	}
	if donation.MappingJSON == "" {
		t.Fatal("mapping snapshot not copied to donation row")
	}
	// The snapshot equals the latest donation-purpose generation mapping.
	mapping, err := store.LatestGenerationMapping(u.ID, "sillytavern-main-v1", "donation")
	if err != nil || len(mapping) == 0 {
		t.Fatalf("generation mapping missing: %v", err)
	}
	var snapshot map[string]string
	json.Unmarshal([]byte(donation.MappingJSON), &snapshot)
	if len(snapshot) != len(mapping) {
		t.Fatalf("snapshot size %d != mapping %d", len(snapshot), len(mapping))
	}

	// Export includes the snapshot.
	bundle, err := store.ExportUserData(u.ID)
	if err != nil || len(bundle.DonationApplications) != 1 {
		t.Fatalf("export apps = %d err=%v", len(bundle.DonationApplications), err)
	}
	if bundle.DonationApplications[0].MappingJSON == "" {
		t.Fatal("export lost the mapping snapshot")
	}
	if len(bundle.TemplateDownloads) == 0 || bundle.TemplateDownloads[0].Seed == "" || bundle.TemplateDownloads[0].MappingJSON == "" || bundle.TemplateDownloads[0].DummyJSON == "" {
		t.Fatal("export lost the template generation seed, mapping or dummy list")
	}
	var dummyKeys []string
	if err := json.Unmarshal([]byte(bundle.TemplateDownloads[0].DummyJSON), &dummyKeys); err != nil || len(dummyKeys) != bundle.TemplateDownloads[0].DummyCount {
		t.Fatalf("export dummy list = %v err=%v", dummyKeys, err)
	}
	_ = admin
}
