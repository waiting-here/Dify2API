package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockDifyApp is a minimal Dify Workflow endpoint for routing tests.
func mockDifyApp(t *testing.T, captured *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workflows/run" {
			w.WriteHeader(404)
			return
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		*captured = body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"task_id":"t","workflow_run_id":"w","data":{"id":"x","status":"succeeded","outputs":{"text":"MOCK_REPLY"}}}`)
	}))
}

// setupRoutedUser creates a user with a caller key and an App config pointing
// at the mock Dify App; returns the gateway, the plaintext key, and the user id.
func setupRoutedUser(t *testing.T, difyURL, model string) (*Gateway, string, int64) {
	t.Helper()
	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, difyURL)
	u, err := store.CreateUser("42", "tester", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("set caller key: %v", err)
	}
	if _, err := store.CreateAppConfig(u.ID, model, difyURL, "app-secret", ""); err != nil {
		t.Fatalf("create app config: %v", err)
	}
	return gw, key, u.ID
}

func chatRequest(gw *Gateway, key, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRouting_General_OK(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[general]claude-opus-4-6","messages":[{"role":"user","content":"hello"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	inputs := captured["inputs"].(map[string]interface{})
	if inputs["user_0"] != "hello" {
		t.Errorf("user_0 = %v", inputs["user_0"])
	}
	if len(inputs) != 1 {
		t.Errorf("general should send exactly 1 input, got %v", inputs)
	}
	// Request log written.
	logs, _ := gw.Store.ListRequestLogs(uid, 10)
	found := false
	for _, l := range logs {
		if l.Model == "[general]claude-opus-4-6" && l.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Error("request log (success) not recorded")
	}
}

func TestRouting_General_RejectsSystem(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[general]claude-opus-4-6","messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_message_sequence") {
		t.Errorf("body should be invalid_message_sequence: %s", rec.Body.String())
	}
	if captured != nil {
		t.Error("gateway must not forward invalid requests to Dify")
	}
}

func TestRouting_Custom_WithAndWithoutSystem(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[custom]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[custom]claude-opus-4-6","messages":[{"role":"system","content":"S"},{"role":"user","content":"U"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("with system: status = %d, body: %s", rec.Code, rec.Body.String())
	}
	inputs := captured["inputs"].(map[string]interface{})
	if inputs["system_prompt"] != "S" || inputs["user_0"] != "U" {
		t.Errorf("inputs = %v", inputs)
	}

	captured = nil
	rec = chatRequest(gw, key, `{"model":"[custom]claude-opus-4-6","messages":[{"role":"user","content":"U2"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("user only: status = %d", rec.Code)
	}
	inputs = captured["inputs"].(map[string]interface{})
	if inputs["system_prompt"] != "" || inputs["user_0"] != "U2" {
		t.Errorf("inputs = %v", inputs)
	}
}

func TestRouting_WebsiteSummary(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[website-summary]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[website-summary]claude-opus-4-6","messages":[{"role":"system","content":"要点式总结"},{"role":"user","content":"https://example.com/a"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	inputs := captured["inputs"].(map[string]interface{})
	if inputs["request_url"] != "https://example.com/a" || inputs["request_instruction"] != "要点式总结" {
		t.Errorf("inputs = %v", inputs)
	}

	// A syntactically valid URL is still rejected unless the deployment
	// operator explicitly trusts its origin; it must never reach Dify.
	gw.remoteContentOrigins = map[string]struct{}{}
	captured = nil
	rec = chatRequest(gw, key, `{"model":"[website-summary]claude-opus-4-6","messages":[{"role":"user","content":"https://example.com/private"}]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "remote_url_not_allowed") {
		t.Fatalf("unallowlisted URL: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if captured != nil {
		t.Error("unallowlisted remote URL must not be forwarded")
	}

	// Missing URL -> 400, not forwarded.
	captured = nil
	rec = chatRequest(gw, key, `{"model":"[website-summary]claude-opus-4-6","messages":[{"role":"user","content":"not-a-url"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad url: status = %d, want 400", rec.Code)
	}
	if captured != nil {
		t.Error("bad url must not be forwarded")
	}
}

func TestRouting_SillyTavern(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[sillytavern-main-trimmed]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[sillytavern-main-trimmed]claude-opus-4-6","messages":[{"role":"system","content":"S"},{"role":"user","content":"U0"},{"role":"assistant","content":"A1"},{"role":"user","content":"U1"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	inputs := captured["inputs"].(map[string]interface{})
	if inputs["system_prompt"] != "S" || inputs["user_0"] != "U0" || inputs["assistant_1"] != "A1" || inputs["user_1"] != "U1" {
		t.Errorf("inputs = %v", inputs)
	}
}

func TestRouting_ShujukuFilling(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[sillytavern-SP·数据库-填表]gemini-3.1-pro-preview")

	body := `{"model":"[sillytavern-SP·数据库-填表]gemini-3.1-pro-preview","messages":[` +
		`{"role":"system","content":"S"},` +
		`{"role":"assistant","content":"A0"},` +
		`{"role":"user","content":"U1"},` +
		`{"role":"assistant","content":"A1"},` +
		`{"role":"user","content":"U2"},` +
		`{"role":"assistant","content":"A2"},` +
		`{"role":"user","content":"U3"},` +
		`{"role":"assistant","content":"P"}` +
		`]}`
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	inputs := captured["inputs"].(map[string]interface{})
	want := map[string]string{
		"system_prompt": "S", "assistant_0": "A0", "user_1": "U1", "assistant_1": "A1",
		"user_2": "U2", "assistant_2": "A2", "user_3": "U3", "assistant_prefill": "P",
	}
	for k, v := range want {
		if inputs[k] != v {
			t.Errorf("inputs[%q] = %v, want %q", k, inputs[k], v)
		}
	}

	// Wrong alternation (user first) -> 400, not forwarded.
	captured = nil
	bad := `{"model":"[sillytavern-SP·数据库-填表]gemini-3.1-pro-preview","messages":[{"role":"user","content":"U"}]}`
	rec = chatRequest(gw, key, bad)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("user-first: status = %d, want 400", rec.Code)
	}
	if captured != nil {
		t.Error("invalid alternation must not be forwarded")
	}
}

func TestRouting_AntiAbuseInfo(t *testing.T) {
	tests := []struct {
		name          string
		role          string
		content       string
		deductCredits int
		banHours      int
		wantCode      string
		wantInfo      string
		wantCredits   int
		wantBanned    bool
	}{
		{
			name:          "invalid role has no penalties",
			role:          "tool",
			content:       "this content is long enough",
			deductCredits: 5,
			banHours:      24,
			wantCode:      "invalid_role",
			wantInfo:      `{"triggered":"invalid_role","penalties":[]}`,
			wantCredits:   10,
		},
		{
			name:        "short content without penalties",
			role:        "user",
			content:     "short",
			wantCode:    "content_too_short",
			wantInfo:    `{"triggered":"content_too_short","penalties":[]}`,
			wantCredits: 10,
		},
		{
			name:          "short content with both penalties",
			role:          "user",
			content:       "short",
			deductCredits: 5,
			banHours:      24,
			wantCode:      "content_too_short",
			wantInfo:      `{"triggered":"content_too_short","penalties":["credits_deducted:5","banned:24h"]}`,
			wantCredits:   5,
			wantBanned:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]interface{}
			srv := mockDifyApp(t, &captured)
			defer srv.Close()
			gw, key, uid := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")
			if err := gw.Store.SetUserCredits(uid, 10); err != nil {
				t.Fatalf("SetUserCredits: %v", err)
			}
			if _, err := gw.Store.UpsertAntiAbuseConfig("general", 2, 20, tt.deductCredits, tt.banHours, 1); err != nil {
				t.Fatalf("UpsertAntiAbuseConfig: %v", err)
			}
			gw.refreshAntiAbuseCache()

			body := fmt.Sprintf(`{"model":"[general]claude-opus-4-6","messages":[{"role":%q,"content":%q}]}`, tt.role, tt.content)
			rec := chatRequest(gw, key, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantCode) {
				t.Errorf("response body should contain %q: %s", tt.wantCode, rec.Body.String())
			}
			if captured != nil {
				t.Error("anti-abuse rejection must not be forwarded to Dify")
			}

			logs, err := gw.Store.ListRequestLogs(uid, 10)
			if err != nil {
				t.Fatalf("ListRequestLogs: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("request logs = %d, want 1", len(logs))
			}
			if logs[0].ErrorCode != tt.wantCode {
				t.Errorf("error_code = %q, want %q", logs[0].ErrorCode, tt.wantCode)
			}
			if logs[0].AntiAbuseInfo != tt.wantInfo {
				t.Errorf("anti_abuse_info = %q, want %q", logs[0].AntiAbuseInfo, tt.wantInfo)
			}

			u, err := gw.Store.GetUserByID(uid)
			if err != nil {
				t.Fatalf("GetUserByID: %v", err)
			}
			if u.Credits != tt.wantCredits {
				t.Errorf("credits = %d, want %d", u.Credits, tt.wantCredits)
			}
			if tt.wantBanned {
				if u.BannedUntil < time.Now().Add(23*time.Hour).Unix() {
					t.Errorf("banned_until = %d, want roughly 24 hours in the future", u.BannedUntil)
				}
			} else if u.BannedUntil != 0 {
				t.Errorf("banned_until = %d, want 0", u.BannedUntil)
			}
		})
	}
}

func TestRouting_ModelNotFound(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, uid := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")

	rec := chatRequest(gw, key, `{"model":"[nope]whatever","messages":[{"role":"user","content":"this is a long enough message to pass anti-abuse"}]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model_not_found") {
		t.Errorf("body should contain model_not_found: %s", rec.Body.String())
	}
	logs, _ := gw.Store.ListRequestLogs(uid, 10)
	found := false
	for _, l := range logs {
		if l.ErrorCode == "model_not_found" {
			found = true
		}
	}
	if !found {
		t.Error("model_not_found should be logged")
	}
}

func TestRouting_DisabledConfigNotRouted(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")
	cfgs, _ := gw.Store.ListAppConfigs(1)
	for _, c := range cfgs {
		gw.Store.SetAppConfigEnabled(c.ID, c.UserID, false)
	}

	rec := chatRequest(gw, key, `{"model":"[general]claude-opus-4-6","messages":[{"role":"user","content":"u"}]}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("disabled config: status = %d, want 404", rec.Code)
	}
}

func TestModels_PerUserFiltered(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]claude-opus-4-6")

	// Another user's config must not appear.
	gw.Store.CreateUser("99", "other", "")
	other, _ := gw.Store.GetUserByDiscordID("99")
	gw.Store.CreateAppConfig(other.ID, "[secret]model", srv.URL, "k", "")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Data) != 1 || resp.Data[0].ID != "[general]claude-opus-4-6" {
		t.Errorf("models = %+v, want exactly the user's own model", resp.Data)
	}
}
