package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminActivityFrontendContract(t *testing.T) {
	gw, _ := setupAuthGateway(t, "activity-frontend")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	index := get("/")
	helperAt := strings.Index(index, `/static/admin_activity.js`)
	adminAt := strings.Index(index, `/static/admin.js`)
	if helperAt < 0 || adminAt < 0 || helperAt > adminAt {
		t.Fatalf("activity helpers must load before admin.js: helper=%d admin=%d", helperAt, adminAt)
	}
	for _, contract := range []string{
		`.activity-summary-cards`,
		`.activity-chart-grid`,
		`@media (max-width: 768px)`,
	} {
		if !strings.Contains(index, contract) {
			t.Errorf("SPA shell missing activity layout contract %q", contract)
		}
	}

	adminJS := get("/static/admin.js")
	if got := strings.Count(adminJS, "/api/admin/activity/stats"); got != 1 {
		t.Errorf("admin activity UI endpoint references=%d, want exactly one", got)
	}
	for _, contract := range []string{
		`activity: initAdminActivityTab`,
		`resizeAdminActivityCharts();`,
		`_adminActivityRequestGate.isCurrent(requestToken)`,
		`destroyAdminActivityCharts();`,
		`row.api_attempts`,
		`row.api_successes`,
		`stats?.timezone !== "UTC"`,
		`id="activity-retry"`,
		`retry.id = "activity-chart-retry"`,
	} {
		if !strings.Contains(adminJS, contract) {
			t.Errorf("admin.js missing activity contract %q", contract)
		}
	}

	i18nJS := get("/static/i18n.js")
	for _, contract := range []string{
		`adminTabActivity: "活跃度"`,
		`adminTabActivity: "Activity"`,
		`activitySuppressed: "<5"`,
		`activityTimezoneInvalid`,
	} {
		if !strings.Contains(i18nJS, contract) {
			t.Errorf("i18n.js missing activity contract %q", contract)
		}
	}
}

func TestActivityLegalPolicyContract(t *testing.T) {
	gw, _ := setupAuthGateway(t, "activity-policy")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	for path, phrases := range map[string][]string{
		"/privacy": {
			"严格滚动保留 30 天",
			"用户级活跃度日聚合最多保留 <strong>400 天</strong>",
			"不可逆全站匿名统计快照",
			"至少等待 <strong>7 天</strong>后再启用活跃度采集",
		},
		"/privacy?lang=en": {
			"strict rolling 30-day",
			"User-level daily activity aggregates are retained for up to <strong>400 days</strong>",
			"Irreversible Site-Wide Anonymous Statistical Snapshots",
			"wait at least <strong>7 days</strong> before enabling activity collection",
		},
		"/terms": {
			"用户级活跃度日聚合",
			"无法回连个人的全站匿名统计快照最多保留 400 天",
		},
		"/terms?lang=en": {
			"user-level daily activity aggregates",
			"may remain after account deletion for up to 400 days",
		},
	} {
		body := get(path)
		for _, phrase := range phrases {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s missing frozen policy phrase %q", path, phrase)
			}
		}
	}
}
