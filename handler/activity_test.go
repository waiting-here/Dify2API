package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dify2api/db"
)

func activityAdminRequest(gw *Gateway, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = gw.Config.Admin.AdminHost
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	gw.Wrap(mux).ServeHTTP(rec, req)
	return rec
}

func TestAdminActivityStatsContractRangeAndIsolation(t *testing.T) {
	gw, store := setupAuthGateway(t, "activity-secret")
	adminCookie := loginCookie(t, gw, "root", "activity-secret")
	now := time.Now().UTC()
	var ordinaryCookie *http.Cookie
	var ordinaryUserID int64
	for i := 0; i < 5; i++ {
		u, err := store.CreateUser(fmt.Sprintf("activity-api-%d", i), fmt.Sprintf("private-name-%d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			ordinaryCookie = meUserCookie(t, store, u)
			ordinaryUserID = u.ID
		}
		if err := store.AddRequestLog(u.ID, "m", "general", now, now, "success", ""); err != nil {
			t.Fatal(err)
		}
	}
	rec := activityAdminRequest(gw, adminCookie, "/api/admin/activity/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", rec.Code, rec.Body.String())
	}
	var stats db.ActivityStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Timezone != "UTC" || len(stats.ByDay) != 28 {
		t.Fatalf("timezone/by_day = %q/%d", stats.Timezone, len(stats.ByDay))
	}
	if stats.Summary.DAU == nil || *stats.Summary.DAU != 5 {
		t.Fatalf("DAU = %v, want 5", stats.Summary.DAU)
	}
	if strings.Contains(rec.Body.String(), "private-name") || strings.Contains(rec.Body.String(), "user_id") || strings.Contains(rec.Body.String(), "discord") {
		t.Fatalf("stats leaked a user dimension: %s", rec.Body.String())
	}

	if unauth := activityAdminRequest(gw, nil, "/api/admin/activity/stats"); unauth.Code != http.StatusForbidden {
		t.Fatalf("unauth status=%d, want 403", unauth.Code)
	}
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	userHostReq := httptest.NewRequest(http.MethodGet, "/api/admin/activity/stats", nil)
	userHostReq.Host = gw.Config.Admin.SiteHost
	userHostReq.AddCookie(ordinaryCookie)
	userHostRec := httptest.NewRecorder()
	gw.Wrap(mux).ServeHTTP(userHostRec, userHostReq)
	if userHostRec.Code != http.StatusNotFound {
		t.Fatalf("user-host status=%d, want 404", userHostRec.Code)
	}
	five := 5
	if err := store.SetUserLevel(ordinaryUserID, &five); err != nil {
		t.Fatal(err)
	}
	if levelUser := activityAdminRequest(gw, ordinaryCookie, "/api/admin/activity/stats"); levelUser.Code != http.StatusForbidden {
		t.Fatalf("non-admin/level user on admin host status=%d, want 403", levelUser.Code)
	}

	today := now.Format(activityDateLayout)
	valid400 := now.AddDate(0, 0, -399).Format(activityDateLayout)
	invalid401 := now.AddDate(0, 0, -400).Format(activityDateLayout)
	cases := []struct {
		query string
		want  int
	}{
		{"?since=" + valid400 + "&until=" + today, http.StatusOK},
		{"?since=" + invalid401 + "&until=" + today, http.StatusBadRequest},
		{"?since=" + today, http.StatusBadRequest},
		{"?since=2026-02-30&until=" + today, http.StatusBadRequest},
		{"?since=" + today + "&until=2026-01-01", http.StatusBadRequest},
	}
	for _, tc := range cases {
		got := activityAdminRequest(gw, adminCookie, "/api/admin/activity/stats"+tc.query)
		if got.Code != tc.want {
			t.Errorf("query %q status=%d want=%d body=%s", tc.query, got.Code, tc.want, got.Body.String())
		}
		if tc.want == http.StatusOK {
			var body db.ActivityStats
			_ = json.Unmarshal(got.Body.Bytes(), &body)
			if len(body.ByDay) != 400 {
				t.Errorf("400-day response has %d buckets", len(body.ByDay))
			}
		}
	}

	// Although the endpoint itself is GET-only, the wrapped mutating path is
	// still covered by the existing CSRF middleware before method dispatch.
	post := httptest.NewRequest(http.MethodPost, "/api/admin/activity/stats", nil)
	post.Host = gw.Config.Admin.AdminHost
	post.AddCookie(adminCookie)
	postRec := httptest.NewRecorder()
	gw.Wrap(mux).ServeHTTP(postRec, post)
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("cross-site/missing-origin POST status=%d, want 403", postRec.Code)
	}
}

func TestConsoleActivityCallerKeyScope(t *testing.T) {
	gw, store := setupAuthGateway(t, "activity-console")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_input_form":[{"paragraph":{"variable":"user_0","required":true}}]}`))
	}))
	defer srv.Close()
	allowDifyTestOrigin(t, gw, srv.URL)
	store.SetSetting(db.SettingDonationEnabled, "true")
	u, _ := store.CreateUser("activity-console", "console", "")
	cookie := meUserCookie(t, store, u)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	get := httptest.NewRequest(http.MethodGet, "/api/caller-key", nil)
	get.AddCookie(cookie)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("caller-key GET=%d", getRec.Code)
	}
	activity, err := store.ListUserActivity(u.ID)
	if err != nil || len(activity) != 0 {
		t.Fatalf("GET counted console activity: len=%d err=%v", len(activity), err)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/caller-key/reset", nil)
	post.AddCookie(cookie)
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusOK {
		t.Fatalf("caller-key reset=%d body=%s", postRec.Code, postRec.Body.String())
	}
	activity, err = store.ListUserActivity(u.ID)
	if err != nil || len(activity) != 1 || activity[0].ConsoleActions != 1 {
		t.Fatalf("reset activity=%+v err=%v", activity, err)
	}

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(http.MethodGet, "/api/configs", ""); rec.Code != http.StatusOK {
		t.Fatalf("config GET=%d", rec.Code)
	}
	created := do(http.MethodPost, "/api/configs", fmt.Sprintf(
		`{"model":"[general]activity","dify_base_url":%q,"dify_api_key":"key","note":"one"}`, srv.URL))
	if created.Code != http.StatusOK {
		t.Fatalf("config create=%d body=%s", created.Code, created.Body.String())
	}
	var createBody struct {
		Config struct {
			ID int64 `json:"id"`
		} `json:"config"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil || createBody.Config.ID == 0 {
		t.Fatalf("decode config create: id=%d err=%v", createBody.Config.ID, err)
	}
	configPath := fmt.Sprintf("/api/configs/%d", createBody.Config.ID)
	if rec := do(http.MethodPut, configPath, fmt.Sprintf(
		`{"model":"[general]activity","dify_base_url":%q,"dify_api_key":"key","note":"two"}`, srv.URL)); rec.Code != http.StatusOK {
		t.Fatalf("config update=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, configPath+"/toggle", `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("config toggle=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodDelete, configPath, ""); rec.Code != http.StatusOK {
		t.Fatalf("config delete=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/me/donations", ""); rec.Code != http.StatusOK {
		t.Fatalf("donation GET=%d body=%s", rec.Code, rec.Body.String())
	}
	donation := do(http.MethodPost, "/api/me/donations", fmt.Sprintf(
		`{"service":"general","model":"activity-donation","dify_base_url":%q,"dify_api_key":"key","deadline":%d,"total_count":10}`, srv.URL, time.Now().Add(48*time.Hour).Unix()))
	if donation.Code != http.StatusOK {
		t.Fatalf("donation submit=%d body=%s", donation.Code, donation.Body.String())
	}
	activity, err = store.ListUserActivity(u.ID)
	if err != nil || len(activity) != 1 || activity[0].ConsoleActions != 6 {
		t.Fatalf("console scope activity=%+v err=%v, want six successful writes only", activity, err)
	}
}
