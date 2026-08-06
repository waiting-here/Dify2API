package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"dify2api/auth"
	"dify2api/db"
	"dify2api/dify"
)

func assertPublicTextOmits(t *testing.T, text string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("public text leaked %q: %s", secret, text)
		}
	}
}

func TestSanitizePublicUpstreamError_ClassifiesWithoutLocators(t *testing.T) {
	tests := []struct {
		name string
		err  error
		raw  string
		lang string
		want string
	}{
		{
			name: "refused zh",
			err: &url.Error{Op: "Post", URL: "https://private.example.com/v1/workflows/run", Err: &net.OpError{
				Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 443}, Err: fmt.Errorf("connection refused"),
			}},
			lang: "zh",
			want: "无法连接上游 Dify 服务",
		},
		{
			name: "timeout en",
			err:  fmt.Errorf("read https://timeout.example.net: unexpected EOF"),
			lang: "en",
			want: "upstream Dify service timed out",
		},
		{
			name: "dns en",
			err:  &net.DNSError{Err: "no such host", Name: "secret.example.org"},
			lang: "en",
			want: "Could not resolve the upstream Dify service address",
		},
		{
			name: "failed 200 zh",
			err:  &dify.DifyError{Status: http.StatusOK, Code: "app-secret", Message: "https://secret.example.org 2001:db8::1 app-secret"},
			lang: "zh",
			want: "上游 Dify 工作流执行失败",
		},
		{
			name: "html 5xx en",
			err:  &dify.DifyError{Status: http.StatusBadGateway, Message: `<html>proxy at http://10.0.0.8 token=d2a_secret</html>`},
			lang: "en",
			want: "temporarily unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizePublicUpstreamError(tc.err, tc.raw, tc.lang)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("message = %q, want fragment %q", got, tc.want)
			}
			assertPublicTextOmits(t, got,
				"private.example.com", "timeout.example.net", "secret.example.org",
				"203.0.113.9", "2001:db8::1", "10.0.0.8", "app-secret", "d2a_secret", "http://", "https://")
		})
	}

	if got := publicDifyErrorCode("invalid_param"); got != "invalid_param" {
		t.Fatalf("stable code = %q, want invalid_param", got)
	}
	for _, unsafe := range []string{"app-secret", "d2a_supersecret", "shortsecret", "https://secret.example"} {
		if got := publicDifyErrorCode(unsafe); got != "upstream_error" {
			t.Errorf("unsafe code %q survived as %q", unsafe, got)
		}
	}
}

func TestPublicErrorViewsSanitizeWhileAdminRetainsRaw(t *testing.T) {
	raw := "failure at https://private.example.com/v1 from 203.0.113.9 and [2001:db8::9] using app-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"status":"failed","error":%q,"outputs":{}}}`, raw)
	}))
	defer srv.Close()

	gw, key, userID := setupRoutedUser(t, srv.URL, "[general]x")
	rec := chatRequest(gw, key, `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("chat status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "上游 Dify 工作流执行失败") {
		t.Fatalf("chat body missing friendly category: %s", rec.Body.String())
	}
	assertPublicTextOmits(t, rec.Body.String(), raw, "private.example.com", "203.0.113.9", "2001:db8::9", "app-secret")

	stored, err := gw.Store.ListRequestLogs(userID, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored logs = %d, err=%v", len(stored), err)
	}
	if !strings.Contains(stored[0].ErrorDetail, raw) {
		t.Fatalf("raw request log was not preserved: %q", stored[0].ErrorDetail)
	}

	token, _, err := gw.Store.CreateSession(userID)
	if err != nil {
		t.Fatal(err)
	}
	userCookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	userLogsReq := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	userLogsReq.AddCookie(userCookie)
	userLogsRec := httptest.NewRecorder()
	mux.ServeHTTP(userLogsRec, userLogsReq)
	var userLogs struct {
		Logs []*db.RequestLog `json:"logs"`
	}
	if err := json.NewDecoder(userLogsRec.Body).Decode(&userLogs); err != nil || len(userLogs.Logs) != 1 {
		t.Fatalf("decode user logs: len=%d err=%v body=%s", len(userLogs.Logs), err, userLogsRec.Body.String())
	}
	assertPublicTextOmits(t, userLogs.Logs[0].ErrorDetail, raw, "private.example.com", "203.0.113.9", "2001:db8::9", "app-secret")

	selfExportReq := httptest.NewRequest(http.MethodGet, "/api/me/export", nil)
	selfExportReq.AddCookie(userCookie)
	selfExportRec := httptest.NewRecorder()
	mux.ServeHTTP(selfExportRec, selfExportReq)
	var selfExport db.ExportBundle
	if err := json.NewDecoder(selfExportRec.Body).Decode(&selfExport); err != nil || len(selfExport.Logs) != 1 {
		t.Fatalf("decode self export: len=%d err=%v body=%s", len(selfExport.Logs), err, selfExportRec.Body.String())
	}
	assertPublicTextOmits(t, selfExport.Logs[0].ErrorDetail, raw, "private.example.com", "203.0.113.9", "2001:db8::9", "app-secret")

	adminCookie := loginCookie(t, gw, "root", "x")
	adminLogsReq := httptest.NewRequest(http.MethodGet, "/api/admin/logs?limit=10", nil)
	adminLogsReq.AddCookie(adminCookie)
	adminLogsRec := httptest.NewRecorder()
	mux.ServeHTTP(adminLogsRec, adminLogsReq)
	if !strings.Contains(adminLogsRec.Body.String(), raw) {
		t.Fatalf("admin logs lost raw detail: %s", adminLogsRec.Body.String())
	}

	adminExportReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/admin/users/%d/export", userID), nil)
	adminExportReq.SetPathValue("id", fmt.Sprint(userID))
	adminExportReq.AddCookie(adminCookie)
	adminExportRec := httptest.NewRecorder()
	mux.ServeHTTP(adminExportRec, adminExportReq)
	var adminExport db.ExportBundle
	if err := json.NewDecoder(adminExportRec.Body).Decode(&adminExport); err != nil || len(adminExport.Logs) != 1 {
		t.Fatalf("decode admin export: len=%d err=%v body=%s", len(adminExport.Logs), err, adminExportRec.Body.String())
	}
	if !strings.Contains(adminExport.Logs[0].ErrorDetail, raw) {
		t.Fatalf("admin export lost raw detail: %q", adminExport.Logs[0].ErrorDetail)
	}

	alertsReq := httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=10", nil)
	alertsReq.AddCookie(adminCookie)
	alertsRec := httptest.NewRecorder()
	mux.ServeHTTP(alertsRec, alertsReq)
	if !strings.Contains(alertsRec.Body.String(), raw) {
		t.Fatalf("admin alert lost raw detail: %s", alertsRec.Body.String())
	}
}

func TestPublicError_NonJSONBodyAndFakeSelfOrigin(t *testing.T) {
	t.Run("non-json body", func(t *testing.T) {
		raw := `<html>proxy https://edge.secret.example at 198.51.100.7 token app-secret</html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, raw)
		}))
		defer srv.Close()
		gw, key, userID := setupRoutedUser(t, srv.URL, "[general]x")
		rec := chatRequest(gw, key, `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`)
		assertPublicTextOmits(t, rec.Body.String(), raw, "edge.secret.example", "198.51.100.7", "app-secret")
		logs, _ := gw.Store.ListRequestLogs(userID, 10)
		if len(logs) != 1 || !strings.Contains(logs[0].ErrorDetail, raw) {
			t.Fatalf("raw non-JSON body not retained in admin log: %+v", logs)
		}
	})

	t.Run("fake self origin", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":{"status":"succeeded","outputs":{"text":"unexpected"}}}`)
		}))
		defer srv.Close()
		gw, key, userID := setupRoutedUser(t, srv.URL, "[general]x")
		parsed, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		gw.difyPolicy.AddSelfOrigins(srv.URL, "", net.JoinHostPort("127.0.0.1", parsed.Port()))
		rec := chatRequest(gw, key, `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "无法连接上游 Dify 服务") {
			t.Fatalf("self-origin response = %d %s", rec.Code, rec.Body.String())
		}
		assertPublicTextOmits(t, rec.Body.String(), srv.URL, "127.0.0.1", parsed.Port())
		logs, _ := gw.Store.ListRequestLogs(userID, 10)
		if len(logs) != 1 || !strings.Contains(logs[0].ErrorDetail, "127.0.0.1") {
			t.Fatalf("fake self-origin raw diagnostic missing: %+v", logs)
		}
	})
}

func TestPublicProbeAndDonationValidationSanitize(t *testing.T) {
	raw := "probe failed at https://probe.secret.example 192.0.2.44 with app-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, raw)
	}))
	defer srv.Close()
	gw, store := setupAuthGateway(t, "x")
	allowDifyTestOrigin(t, gw, srv.URL)

	public := gw.validatePublicDonationApp(context.Background(), 10, "general", srv.URL, "app-secret", "en")
	publicMessage, _ := public["message"].(string)
	if publicMessage == "" {
		t.Fatalf("public validation missing message: %v", public)
	}
	assertPublicTextOmits(t, publicMessage, raw, "probe.secret.example", "192.0.2.44", "app-secret")

	admin := gw.validateDonationApp(context.Background(), 10, "general", srv.URL, "app-secret")
	if message, _ := admin["message"].(string); !strings.Contains(message, raw) {
		t.Fatalf("admin validation lost raw diagnostic: %v", admin)
	}

	u, err := store.CreateUser("probe-user", "probe-user", "")
	if err != nil {
		t.Fatal(err)
	}
	token, _, _ := store.CreateSession(u.ID)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/configs", strings.NewReader(fmt.Sprintf(
		`{"model":"[general]probe","dify_base_url":%q,"dify_api_key":"app-secret"}`, srv.URL)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var response struct {
		AppCheck struct {
			Error string `json:"error"`
		} `json:"app_check"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil || response.AppCheck.Error == "" {
		t.Fatalf("decode app_check: err=%v body=%s", err, rec.Body.String())
	}
	assertPublicTextOmits(t, response.AppCheck.Error, raw, "probe.secret.example", "192.0.2.44", "app-secret")

	badConfig := configRequest{
		Model:   "[general]bad",
		BaseURL: "https://config.secret.example/\napp-secret",
		APIKey:  "app-secret",
	}
	validationMessage := badConfig.validate(gw)
	if validationMessage == "" {
		t.Fatal("malformed config URL should be rejected")
	}
	assertPublicTextOmits(t, validationMessage, "config.secret.example", "app-secret", "https://")
}

func TestPublicErrorEnglishBlocking(t *testing.T) {
	raw := "failure https://english.secret.example 2001:db8::5 app-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, raw)
	}))
	defer srv.Close()
	gw, key, userID := setupRoutedUser(t, srv.URL, "[general]x")
	if err := gw.Store.SetUserLang(userID, "en"); err != nil {
		t.Fatal(err)
	}
	rec := chatRequest(gw, key, `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`)
	if !strings.Contains(rec.Body.String(), "temporarily unavailable") {
		t.Fatalf("English friendly message missing: %s", rec.Body.String())
	}
	assertPublicTextOmits(t, rec.Body.String(), "english.secret.example", "2001:db8::5", "app-secret")
}

func TestSanitizePublicRequestLogsDoesNotMutateRawRows(t *testing.T) {
	raw := "https://future.secret.example 192.0.2.9 d2a_secret"
	rows := []*db.RequestLog{{ErrorDetail: raw}}
	public := sanitizePublicRequestLogs(rows, "zh")
	if rows[0].ErrorDetail != raw {
		t.Fatal("public log adapter mutated the raw admin row")
	}
	assertPublicTextOmits(t, public[0].ErrorDetail, "future.secret.example", "192.0.2.9", "d2a_secret")

	adminRows := []*db.AdminRequestLog{{ErrorDetail: raw}}
	publicAdminRows := sanitizePublicAdminRequestLogs(adminRows, "en")
	if adminRows[0].ErrorDetail != raw {
		t.Fatal("future public all-logs adapter mutated the admin row")
	}
	assertPublicTextOmits(t, publicAdminRows[0].ErrorDetail, "future.secret.example", "192.0.2.9", "d2a_secret")
}

func TestProbeTimeoutEnglish(t *testing.T) {
	got := probeError(context.DeadlineExceeded, "en")
	if !strings.Contains(got, "timed out") {
		t.Fatalf("probe timeout = %q", got)
	}
}

func TestPublicDonationValidationKeepsLocalContractMessage(t *testing.T) {
	const local = "App 参数与契约不兼容"
	if got := sanitizePublicDonationValidationMessage(local, "en"); got != local {
		t.Fatalf("local validation message = %q, want %q", got, local)
	}

	got := sanitizePublicDonationValidationMessage("probe failed at https://secret.example 192.0.2.10", "en")
	assertPublicTextOmits(t, got, "secret.example", "192.0.2.10", "https://")
}
