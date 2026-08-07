package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dify2api/auth"
	"dify2api/db"
)

// levelSettingsBody builds a complete valid PUT body for
// /api/admin/level-settings.
func levelSettingsBody(t2, t3, t4 int, names []string, banner string) string {
	return fmt.Sprintf(
		`{"threshold_2":%d,"threshold_3":%d,"threshold_4":%d,`+
			`"name_1":%q,"name_2":%q,"name_3":%q,"name_4":%q,"name_5":%q,`+
			`"banner_text":%q}`,
		t2, t3, t4, names[0], names[1], names[2], names[3], names[4], banner)
}

func TestAdminLevelSettings_GetDefaultsAndPutRoundtrip(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// GET returns built-in defaults.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/level-settings", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET defaults: status %d, body %s", rec.Code, rec.Body.String())
	}
	var got map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["threshold_2"].(float64) != 1 || got["threshold_3"].(float64) != 100 || got["threshold_4"].(float64) != 500 {
		t.Errorf("default thresholds = %v/%v/%v, want 1/100/500", got["threshold_2"], got["threshold_3"], got["threshold_4"])
	}
	for i := 1; i <= 5; i++ {
		if got[fmt.Sprintf("name_%d", i)].(string) != "" {
			t.Errorf("default name_%d = %v, want empty", i, got[fmt.Sprintf("name_%d", i)])
		}
	}
	if got["banner_text"].(string) != "" {
		t.Errorf("default banner_text = %v, want empty", got["banner_text"])
	}

	// PUT a complete valid set.
	body := levelSettingsBody(10, 20, 30, []string{"一级", "二级", "三级", "四级", "五级"}, "横幅内容")
	req = httptest.NewRequest(http.MethodPut, "/api/admin/level-settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: status %d, body %s", rec.Code, rec.Body.String())
	}

	// All nine keys landed in the store.
	if v := store.GetSettingString(db.SettingLevelThreshold2, ""); v != "10" {
		t.Errorf("threshold_2 = %q, want 10", v)
	}
	if v := store.GetSettingString(db.SettingLevelThreshold3, ""); v != "20" {
		t.Errorf("threshold_3 = %q, want 20", v)
	}
	if v := store.GetSettingString(db.SettingLevelThreshold4, ""); v != "30" {
		t.Errorf("threshold_4 = %q, want 30", v)
	}
	for i, key := range db.LevelNameKeys {
		want := []string{"一级", "二级", "三级", "四级", "五级"}[i]
		if v := store.GetSettingString(key, ""); v != want {
			t.Errorf("%s = %q, want %q", key, v, want)
		}
	}
	if v := store.GetSettingString(db.SettingLevelBannerText, ""); v != "横幅内容" {
		t.Errorf("banner_text = %q, want 横幅内容", v)
	}

	// GET reflects the stored values (including threshold_2 = 0 case handled
	// in the validation test below).
	req = httptest.NewRequest(http.MethodGet, "/api/admin/level-settings", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	got = map[string]interface{}{}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["threshold_2"].(float64) != 10 || got["name_1"].(string) != "一级" || got["banner_text"].(string) != "横幅内容" {
		t.Errorf("GET after PUT = %v", got)
	}
}

func TestAdminLevelSettings_Validation(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/level-settings", strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct {
		name string
		body string
	}{
		{"negative threshold", `{"threshold_2":-1,"threshold_3":100,"threshold_4":500,"name_1":"","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":""}`},
		{"t2 >= t3", `{"threshold_2":100,"threshold_3":100,"threshold_4":500,"name_1":"","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":""}`},
		{"t3 >= t4", `{"threshold_2":1,"threshold_3":500,"threshold_4":500,"name_1":"","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":""}`},
		{"name control char", `{"threshold_2":1,"threshold_3":100,"threshold_4":500,"name_1":"a\u0001","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":""}`},
		{"name too long", fmt.Sprintf(`{"threshold_2":1,"threshold_3":100,"threshold_4":500,"name_1":%q,"name_2":"","name_3":"","name_4":"","name_5":"","banner_text":""}`, strings.Repeat("名", maxLevelNameLen+1))},
		{"banner control char", `{"threshold_2":1,"threshold_3":100,"threshold_4":500,"name_1":"","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":"x\u0007"}`},
		{"banner too long", fmt.Sprintf(`{"threshold_2":1,"threshold_3":100,"threshold_4":500,"name_1":"","name_2":"","name_3":"","name_4":"","name_5":"","banner_text":%q}`, strings.Repeat("b", maxLevelBannerLen+1))},
		{"missing field", `{"threshold_2":1,"threshold_3":100,"threshold_4":500,"name_1":"a","name_2":"","name_3":"","name_4":"","name_5":""}`},
		{"garbage json", `{`},
	}
	for _, c := range cases {
		if rec := put(c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (body %s)", c.name, rec.Code, rec.Body.String())
		}
	}

	// A rejected request must not write anything: the store still has no
	// level settings (all defaults).
	th := store.LevelThresholds()
	if th.T2 != db.DefaultLevelThreshold2 || th.T3 != db.DefaultLevelThreshold3 || th.T4 != db.DefaultLevelThreshold4 {
		t.Errorf("thresholds after failed PUTs = %+v, want defaults (no partial writes)", th)
	}
	for _, key := range db.LevelNameKeys {
		if v := store.GetSettingString(key, ""); v != "" {
			t.Errorf("%s after failed PUTs = %q, want empty", key, v)
		}
	}

	// t2 = 0 is legal (all non-negative credits are level 2+).
	if rec := put(levelSettingsBody(0, 100, 500, []string{"", "", "", "", ""}, "")); rec.Code != http.StatusOK {
		t.Errorf("t2=0: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if th := store.LevelThresholds(); th.T2 != 0 {
		t.Errorf("threshold_2 = %d, want 0", th.T2)
	}
}

func TestAdminLevelSettings_RequiresAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	u, _ := store.CreateUser("1001", "normal", "")
	sess, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}

	for _, m := range []string{http.MethodGet, http.MethodPut} {
		var body *strings.Reader
		if m == http.MethodPut {
			body = strings.NewReader(levelSettingsBody(1, 100, 500, []string{"", "", "", "", ""}, ""))
		} else {
			body = strings.NewReader("")
		}
		req := httptest.NewRequest(m, "/api/admin/level-settings", body)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s by normal user: status %d, want 403", m, rec.Code)
		}
	}
}

func TestAdminSetUserLevel(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	u, _ := store.CreateUser("1002", "leveller", "")
	store.SetUserDonationCredit(u.ID, 250) // automatic level 3

	put := func(userID int64, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/users/%d/level", userID), strings.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Set manual level 5.
	if rec := put(u.ID, `{"level":5}`); rec.Code != http.StatusOK {
		t.Fatalf("set level: status %d, body %s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUserByID(u.ID)
	if got.Level == nil || *got.Level != 5 {
		t.Fatalf("Level = %v, want 5", got.Level)
	}

	// Restore automatic with explicit null.
	if rec := put(u.ID, `{"level":null}`); rec.Code != http.StatusOK {
		t.Fatalf("clear level: status %d, body %s", rec.Code, rec.Body.String())
	}
	got, _ = store.GetUserByID(u.ID)
	if got.Level != nil {
		t.Fatalf("Level after clear = %v, want nil", *got.Level)
	}

	// Validation: out-of-range, non-integer, missing field.
	for _, body := range []string{`{"level":0}`, `{"level":6}`, `{"level":-1}`, `{"level":"x"}`, `{}`} {
		if rec := put(u.ID, body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status %d, want 400", body, rec.Code)
		}
	}

	// Unknown user -> 404; admin user target -> 404.
	if rec := put(999999, `{"level":3}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status %d, want 404", rec.Code)
	}
	admin, _ := store.EnsureAdminUser("root")
	if rec := put(admin.ID, `{"level":3}`); rec.Code != http.StatusNotFound {
		t.Errorf("admin target: status %d, want 404", rec.Code)
	}
}

func TestAdminListUsers_LevelFields(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	cookie := loginCookie(t, gw, "root", "s3cret")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	auto, _ := store.CreateUser("1003", "auto_user", "")
	store.SetUserDonationCredit(auto.ID, 250) // automatic level 3
	manual, _ := store.CreateUser("1004", "manual_user", "")
	five := 5
	store.SetUserLevel(manual.ID, &five) // manual level 5, 0 credits

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users: status %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Users []map[string]interface{} `json:"users"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	found := map[int64]map[string]interface{}{}
	for _, u := range resp.Users {
		found[int64(u["id"].(float64))] = u
	}
	a, ok := found[auto.ID]
	if !ok {
		t.Fatalf("auto user missing from list: %v", found)
	}
	if a["level"].(float64) != 3 || a["level_manual"].(bool) {
		t.Errorf("auto user level = %v manual=%v, want 3/false", a["level"], a["level_manual"])
	}
	m, ok := found[manual.ID]
	if !ok {
		t.Fatalf("manual user missing from list")
	}
	if m["level"].(float64) != 5 || !m["level_manual"].(bool) {
		t.Errorf("manual user level = %v manual=%v, want 5/true", m["level"], m["level_manual"])
	}
}

func TestMe_LevelFields(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	store.SetSetting(db.SettingLevelThreshold2, "1")
	store.SetSetting(db.SettingLevelThreshold3, "100")
	store.SetSetting(db.SettingLevelThreshold4, "500")
	store.SetSetting(db.SettingLevelName2, "青铜")
	store.SetSetting(db.SettingLevelBannerText, "全站横幅")
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	me := func(u *db.User) map[string]interface{} {
		sess, _, _ := store.CreateSession(u.ID)
		cookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("me: status %d, body %s", rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	// Automatic level 2 with a custom name and a banner.
	u, _ := store.CreateUser("1005", "bronze", "")
	store.SetUserDonationCredit(u.ID, 50)
	out := me(u)
	if out["level"].(float64) != 2 || out["level_name"].(string) != "青铜" || out["level_manual"].(bool) {
		t.Errorf("auto user: %v", out)
	}
	if out["banner_text"].(string) != "全站横幅" {
		t.Errorf("banner_text = %v", out["banner_text"])
	}

	// Manual level 5 with an empty custom name -> numeric fallback.
	u2, _ := store.CreateUser("1006", "platinum", "")
	five := 5
	store.SetUserLevel(u2.ID, &five)
	out = me(u2)
	if out["level"].(float64) != 5 || out["level_name"].(string) != "5" || !out["level_manual"].(bool) {
		t.Errorf("manual user: %v", out)
	}

	// Unconfigured banner -> empty string (client renders nothing).
	store.SetSetting(db.SettingLevelBannerText, "")
	out = me(u)
	if out["banner_text"].(string) != "" {
		t.Errorf("banner_text after clearing = %v, want empty", out["banner_text"])
	}
}
