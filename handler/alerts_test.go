package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

func TestAlertList_NonAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403", rec.Code)
	}
}

func TestAlertList_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("no session: status = %d, want 403", rec.Code)
	}
}

func TestAlertList_Basic(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Add some alerts.
	for i := 0; i < 3; i++ {
		if err := store.AddAdminAlert(&db.AdminAlert{
			Type:    db.AlertBlockingFailed200,
			Message: "test alert",
		}); err != nil {
			t.Fatalf("AddAdminAlert: %v", err)
		}
	}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=10", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Alerts []db.AdminAlert `json:"alerts"`
		Total  int             `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 3 || len(resp.Alerts) != 3 {
		t.Errorf("total=%d len=%d, want 3/3", resp.Total, len(resp.Alerts))
	}
}

func TestAlertList_Pagination(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	for i := 0; i < 5; i++ {
		store.AddAdminAlert(&db.AdminAlert{
			Type:    db.AlertBlockingFailed200,
			Message: "test",
		})
	}

	// Page 1: 3 items.
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=3&offset=0", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp struct {
		Alerts []db.AdminAlert `json:"alerts"`
		Total  int             `json:"total"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 5 || len(resp.Alerts) != 3 {
		t.Errorf("page1: total=%d len=%d, want 5/3", resp.Total, len(resp.Alerts))
	}

	// Page 2: 2 items.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=3&offset=3", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 5 || len(resp.Alerts) != 2 {
		t.Errorf("page2: total=%d len=%d, want 5/2", resp.Total, len(resp.Alerts))
	}
}

func TestAlertDelete_EmptySlice(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/alerts",
		strings.NewReader(`{"ids":[]}`))
	req.AddCookie(adminCookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("empty ids: status = %d, want 200", rec.Code)
	}
}

func TestAlertDelete_Batch(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Add 3 alerts.
	for i := 0; i < 3; i++ {
		a := &db.AdminAlert{
			Type:    db.AlertBlockingFailed200,
			Message: "to delete",
		}
		if err := store.AddAdminAlert(a); err != nil {
			t.Fatalf("AddAdminAlert: %v", err)
		}
	}

	// Get alert ids.
	list, total, _ := store.ListAdminAlerts(10, 0)
	if total != 3 {
		t.Fatalf("expected 3 alerts, got %d", total)
	}
	ids := make([]int64, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}

	body, _ := json.Marshal(map[string]interface{}{"ids": ids})
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/alerts", strings.NewReader(string(body)))
	req.AddCookie(adminCookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK      bool  `json:"ok"`
		Deleted int64 `json:"deleted"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Deleted != 3 {
		t.Errorf("deleted = %d, want 3", resp.Deleted)
	}

	// Verify alerts are gone.
	list2, total2, _ := store.ListAdminAlerts(10, 0)
	if total2 != 0 || len(list2) != 0 {
		t.Errorf("remaining: total=%d len=%d, want 0", total2, len(list2))
	}
}

func TestAlertDelete_NonAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/alerts",
		strings.NewReader(`{"ids":[1]}`))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin delete: status = %d, want 403", rec.Code)
	}
}

func TestAlertList_HostSeparation_UserHost(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.SiteHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("user-host alerts: status = %d, want 404", rec.Code)
	}
}

func TestAlertList_HostSeparation_AdminHost(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Seed one alert.
	store.AddAdminAlert(&db.AdminAlert{
		Type:    db.AlertBlockingFailed200,
		Message: "host test",
	})

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=1", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin-host alerts: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAlertPrefs_HostSeparation(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	// Admin host: allowed.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alert-prefs", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin-host alert-prefs: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// User host: must be hidden.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/alert-prefs", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.SiteHost
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("user-host alert-prefs: status = %d, want 404", rec.Code)
	}
}

func TestAlertList_IncludeRequestLogID(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	// Create a user and a log entry.
	u, _ := store.CreateUser("42", "alertuser", "")
	store.AddRequestLog(u.ID, "[general]x", "general", time.Now().Add(-10*time.Minute), time.Now().Add(-9*time.Minute), "success", "")
	logs, _ := store.ListRequestLogs(u.ID, 10)
	logID := logs[0].ID

	// Add an alert with request_log_id.
	store.AddAdminAlert(&db.AdminAlert{
		Type:         db.AlertBlockingFailed200,
		Message:      "linked alert",
		RequestLogID: &logID,
	})

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alerts?limit=10", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp struct {
		Alerts []db.AdminAlert `json:"alerts"`
		Total  int             `json:"total"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1", resp.Total)
	}
	if resp.Alerts[0].RequestLogID == nil || *resp.Alerts[0].RequestLogID != logID {
		t.Errorf("request_log_id = %v, want %d", resp.Alerts[0].RequestLogID, logID)
	}
}

func TestAlertPrefsAPI(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// Seeded at gateway construction: every category present with defaults on.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/alert-prefs", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET prefs: status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Prefs []db.AlertPref `json:"prefs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	seen := map[string]db.AlertPref{}
	for _, p := range resp.Prefs {
		seen[p.EventType] = p
		if !p.ShowInCenter || !p.EmailEnabled {
			t.Errorf("%s: seeded defaults should be on, got %+v", p.EventType, p)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("want 6 categories, got %d: %v", len(seen), seen)
	}
	for _, et := range []string{"user_auto_banned", "donation_inactive", "admin_login_locked", "pricing_missing", "debug_abuse", "blocking_failed_200"} {
		if _, ok := seen[et]; !ok {
			t.Errorf("missing category %s", et)
		}
	}

	// Turn off both switches for one category.
	body := `{"prefs":[{"event_type":"user_auto_banned","show_in_center":false,"email_enabled":false}]}`
	req = httptest.NewRequest(http.MethodPut, "/api/admin/alert-prefs", strings.NewReader(body))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT prefs: status %d body %s", rec.Code, rec.Body.String())
	}
	if store.IsAlertShownInCenter("user_auto_banned") || store.IsAlertEmailEnabled("user_auto_banned") {
		t.Error("expected show off, email off after PUT")
	}
	// Omitted switches retain their current value.
	body = `{"prefs":[{"event_type":"user_auto_banned","email_enabled":true}]}`
	req = httptest.NewRequest(http.MethodPut, "/api/admin/alert-prefs", strings.NewReader(body))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || store.IsAlertShownInCenter("user_auto_banned") || !store.IsAlertEmailEnabled("user_auto_banned") {
		t.Fatalf("partial preference update did not preserve center gate: status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Other categories untouched.
	if !store.IsAlertEmailEnabled("donation_inactive") {
		t.Error("donation_inactive must stay enabled")
	}

	// The gate takes effect: no record written for the disabled category.
	if err := store.AddAdminAlert(&db.AdminAlert{Type: "user_auto_banned", Message: "x"}); err != nil {
		t.Fatalf("AddAdminAlert: %v", err)
	}
	if _, total, _ := store.ListAdminAlerts(100, 0); total != 0 {
		t.Fatalf("gated alert should not be recorded, total=%d", total)
	}

	// Unknown event type rejected.
	body = `{"prefs":[{"event_type":"bogus","email_enabled":false}]}`
	req = httptest.NewRequest(http.MethodPut, "/api/admin/alert-prefs", strings.NewReader(body))
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown type: status %d, want 400", rec.Code)
	}

	// Non-admin rejected.
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/alert-prefs", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-admin: status = %d, want 403", rec.Code)
	}
}

func TestAlertPrefsPut_AtomicRollbackThroughGateway(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")
	if _, err := store.RawExec(`CREATE TRIGGER fail_alert_pref_update BEFORE UPDATE ON alert_prefs
		WHEN NEW.event_type='donation_inactive' BEGIN SELECT RAISE(ABORT, 'injected alert pref failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)
	body := `{"prefs":[
		{"event_type":"user_auto_banned","show_in_center":false},
		{"event_type":"donation_inactive","email_enabled":false}
	]}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/alert-prefs", strings.NewReader(body))
	req.Host = gw.Config.Admin.AdminHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://admin.localhost")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !store.IsAlertShownInCenter("user_auto_banned") {
		t.Error("first preference update survived failed batch")
	}
	if !store.IsAlertEmailEnabled("donation_inactive") {
		t.Error("failing preference changed despite rollback")
	}
}
