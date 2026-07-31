package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dify2api/db"
	"dify2api/dify"
)

func TestRemoteContentOriginGate(t *testing.T) {
	gw := setupTestGateway(t)
	if err := gw.requireRemoteOrigin("https://example.com/path?q=1"); err != nil {
		t.Fatalf("allowlisted origin rejected: %v", err)
	}
	for _, raw := range []string{
		"https://sub.example.com/path",
		"http://example.com/path",
		"https://user:pass@example.com/path",
		"https://example.com/path#fragment",
		"http://127.0.0.1/internal",
	} {
		if err := gw.requireRemoteOrigin(raw); err == nil {
			t.Errorf("remote URL %q should be rejected", raw)
		}
	}
}

func TestNewDifyClient_PrivateOriginRequiresOperatorAllowlist(t *testing.T) {
	gw := setupTestGateway(t)
	policy, err := dify.NewEgressPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	gw.difyPolicy = policy
	if _, err := gw.newDifyClient(7, "http://127.0.0.1:8080", "key", 0); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private origin error = %v", err)
	}
}

func TestDifyProbeSemaphoreHonorsCancellation(t *testing.T) {
	gw := setupTestGateway(t)
	gw.difyProbeSem = make(chan struct{}, 1)
	release, err := gw.acquireDifyProbe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gw.acquireDifyProbe(ctx); err == nil {
		t.Fatal("probe acquire should stop when request context is canceled")
	}
}

func TestProbeLimiter_SlidingWindow(t *testing.T) {
	l := newProbeLimiter()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 5; i++ {
		if !l.allow(1, 5, now) {
			t.Fatalf("hit %d should be allowed", i+1)
		}
	}
	if l.allow(1, 5, now) {
		t.Fatal("6th hit in the same window should be denied")
	}
	// Denied attempts are not recorded.
	l.mu.Lock()
	n := len(l.hits[1])
	l.mu.Unlock()
	if n != 5 {
		t.Fatalf("denied attempt must not be recorded, got %d hits", n)
	}
	// A different user has its own budget.
	if !l.allow(2, 5, now) {
		t.Fatal("per-user budgets must be isolated")
	}
	// The window slides: 60s later the budget refills.
	if !l.allow(1, 5, now.Add(60*time.Second)) {
		t.Fatal("budget should refill after the window")
	}
	// The limit is taken per call: a raised cap applies immediately.
	for i := 0; i < 5; i++ {
		if !l.allow(1, 10, now.Add(120*time.Second)) {
			t.Fatalf("raised-cap hit %d should be allowed", i+1)
		}
	}
	if l.allow(1, 5, now.Add(120*time.Second)) {
		t.Fatal("original cap must still apply for a lowered limit")
	}
	// A non-positive limit falls back to the default.
	l2 := newProbeLimiter()
	for i := 0; i < defaultProbeLimitPerUser; i++ {
		if !l2.allow(3, 0, now) {
			t.Fatalf("default-cap hit %d should be allowed", i+1)
		}
	}
	if l2.allow(3, 0, now) {
		t.Fatal("default cap should deny over-limit attempts")
	}
}

func TestProbeLimiter_SweepDropsExpiredUsers(t *testing.T) {
	l := newProbeLimiter()
	now := time.Unix(1_700_000_000, 0)
	for uid := int64(1); uid <= 1200; uid++ {
		if !l.allow(uid, 5, now) {
			t.Fatalf("user %d should be allowed", uid)
		}
	}
	// All entries are expired 61s later; the next allow triggers the sweep
	// (map > 1024 entries, sweep cooldown elapsed).
	if !l.allow(1, 5, now.Add(61*time.Second)) {
		t.Fatal("refreshed user should be allowed")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) != 1 {
		t.Fatalf("sweep should drop expired users, got %d entries", len(l.hits))
	}
}

func TestCheckAppBinding_RateLimitFollowsSetting(t *testing.T) {
	gw := setupTestGateway(t)
	allowDifyTestOrigin(t, gw, "http://127.0.0.1:1")
	// Admin lowers the per-user probe cap to 2; the limiter must pick it up
	// on the next call (no restart).
	if err := gw.Store.SetSetting(db.SettingProbeLimitPerUser, "2"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		check := gw.checkAppBinding(ctx, 43, "[general]x", "http://127.0.0.1:1", "k")
		msg, _ := check["error"].(string)
		if msg == "rate limited, try again later" {
			t.Fatalf("probe %d should pass the lowered cap, got %v", i+1, check)
		}
	}
	check := gw.checkAppBinding(ctx, 43, "[general]x", "http://127.0.0.1:1", "k")
	if check["error"] != "rate limited, try again later" {
		t.Fatalf("3rd probe should be rate limited by the setting, got %v", check)
	}
}

func TestCheckAppBinding_RateLimited(t *testing.T) {
	gw := setupTestGateway(t)
	allowDifyTestOrigin(t, gw, "http://127.0.0.1:1")
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		check := gw.checkAppBinding(ctx, 42, "[general]x", "http://127.0.0.1:1", "k")
		msg, _ := check["error"].(string)
		if msg == "" || msg == "rate limited, try again later" {
			t.Fatalf("probe %d should fail with a network error, got %v", i+1, check)
		}
	}
	// Hold the only semaphore slot: the rate-limited call must not touch it
	// (it is rejected before acquiring).
	gw.difyProbeSem = make(chan struct{}, 1)
	release, err := gw.acquireDifyProbe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	check := gw.checkAppBinding(ctx, 42, "[general]x", "http://127.0.0.1:1", "k")
	if check["error"] != "rate limited, try again later" {
		t.Fatalf("6th probe should be rate limited, got %v", check)
	}
	if _, ok := check["compatible"]; ok {
		t.Error("rate-limited probe must not carry a compatible verdict")
	}
}

func TestCheckAppBinding_TimeoutWhileQueued(t *testing.T) {
	gw := setupTestGateway(t)
	gw.difyProbeSem = make(chan struct{}, 1)
	release, err := gw.acquireDifyProbe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	check := gw.checkAppBinding(ctx, 7, "[general]x", "http://127.0.0.1:1", "k")
	if check["error"] != "probe timeout" {
		t.Fatalf("queued probe should time out, got %v", check)
	}
	if _, ok := check["compatible"]; ok {
		t.Error("timed-out probe must not carry a compatible verdict")
	}
}

func TestCheckAppBinding_TimeoutUpstream(t *testing.T) {
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never answer before the deadline fires
		fmt.Fprint(w, `{"user_input_form":[]}`)
	}))
	defer func() { close(block); slow.Close() }()

	gw := setupTestGateway(t)
	allowDifyTestOrigin(t, gw, slow.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	check := gw.checkAppBinding(ctx, 8, "[general]x", slow.URL, "k")
	if check["error"] != "probe timeout" {
		t.Fatalf("slow upstream should time out, got %v", check)
	}
}

func TestUpdateConfig_SkipsProbeWhenBindingUnchanged(t *testing.T) {
	var probes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/parameters" {
			probes.Add(1)
		}
		fmt.Fprint(w, `{"user_input_form":[{"paragraph":{"variable":"user_0","required":true}}]}`)
	}))
	defer srv.Close()

	gw, store := setupAuthGateway(t, "s3cret")
	allowDifyTestOrigin(t, gw, srv.URL, "http://127.0.0.1:1")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: "dify2api_session", Value: token}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	post := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	decode := func(rec *httptest.ResponseRecorder) map[string]json.RawMessage {
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body
	}

	// Create (always probes).
	rec := post(http.MethodPost, "/api/configs",
		fmt.Sprintf(`{"model":"[general]x","dify_base_url":%q,"dify_api_key":"app-k","note":"first"}`, srv.URL))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d, body %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Config struct {
			ID int64 `json:"id"`
		} `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 {
		t.Fatalf("create should probe once, got %d", probes.Load())
	}

	// 1. Same model/base URL/key, only note changed -> no probe, no app_check.
	rec = post(http.MethodPut, fmt.Sprintf("/api/configs/%d", created.Config.ID),
		fmt.Sprintf(`{"model":"[general]x","dify_base_url":%q,"dify_api_key":"app-k","note":"renamed"}`, srv.URL))
	if rec.Code != http.StatusOK {
		t.Fatalf("unchanged update: status %d, body %s", rec.Code, rec.Body.String())
	}
	body := decode(rec)
	if _, ok := body["app_check"]; ok {
		t.Errorf("unchanged update should omit app_check, got %s", body["app_check"])
	}
	if probes.Load() != 1 {
		t.Errorf("unchanged update must not probe, got %d", probes.Load())
	}
	var cfg struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(body["config"], &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Note != "renamed" {
		t.Errorf("note should still be saved, got %q", cfg.Note)
	}

	// 2. Key changed -> probe runs again (4 probes total stays under the cap).
	rec = post(http.MethodPut, fmt.Sprintf("/api/configs/%d", created.Config.ID),
		fmt.Sprintf(`{"model":"[general]x","dify_base_url":%q,"dify_api_key":"new-key","note":"renamed"}`, srv.URL))
	if _, ok := decode(rec)["app_check"]; !ok {
		t.Error("key change should probe (app_check present)")
	}
	if probes.Load() != 2 {
		t.Errorf("key change should probe, got %d", probes.Load())
	}

	// 3. Model changed -> probe runs.
	rec = post(http.MethodPut, fmt.Sprintf("/api/configs/%d", created.Config.ID),
		fmt.Sprintf(`{"model":"[general]y","dify_base_url":%q,"dify_api_key":"new-key","note":"renamed"}`, srv.URL))
	if _, ok := decode(rec)["app_check"]; !ok {
		t.Error("model change should probe")
	}
	if probes.Load() != 3 {
		t.Errorf("model change should probe, got %d", probes.Load())
	}

	// 4. Base URL changed -> probe runs (against the new origin, which fails
	// fast; the mock counter cannot observe it).
	rec = post(http.MethodPut, fmt.Sprintf("/api/configs/%d", created.Config.ID),
		`{"model":"[general]y","dify_base_url":"http://127.0.0.1:1","dify_api_key":"new-key","note":"renamed"}`)
	if _, ok := decode(rec)["app_check"]; !ok {
		t.Error("base URL change should probe")
	}
}
