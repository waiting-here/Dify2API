package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dify2api/auth"
)

func TestBulletinPublicEndpoint(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// Public endpoint: no auth required.
	req := httptest.NewRequest(http.MethodGet, "/api/bulletins", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("public bulletins: status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Bulletins []map[string]interface{} `json:"bulletins"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Without any settings configured:
	// credits_cap defaults to 500 (>0), so checkin_disabled NOT generated.
	// donation_enabled defaults to "" (false) -> donation_disabled generated.
	// charity_enabled defaults to "" (false) -> charity_disabled generated.
	if len(resp.Bulletins) != 2 {
		t.Errorf("expected 2 system bulletins (donation+charity disabled), got %d: %v",
			len(resp.Bulletins), resp.Bulletins)
	}
}

func TestBulletinAdminCRUD(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// 1. List (empty initially).
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/bulletins", nil)
	listReq.AddCookie(adminCookie)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Bulletins []map[string]interface{} `json:"bulletins"`
	}
	json.NewDecoder(listRec.Body).Decode(&listResp)
	initialCount := len(listResp.Bulletins)

	// 2. Create.
	createBody := `{"title":"Test Bulletin","content":"<p>Hello</p>","type":"info","sort_order":10,"closable":true,"expires_at":null}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/bulletins", strings.NewReader(createBody))
	createReq.AddCookie(adminCookie)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body: %s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		OK       bool                   `json:"ok"`
		Bulletin map[string]interface{} `json:"bulletin"`
	}
	json.NewDecoder(createRec.Body).Decode(&createResp)
	if !createResp.OK {
		t.Fatal("create: ok = false")
	}
	bulletinID := int64(createResp.Bulletin["id"].(float64))
	if bulletinID == 0 {
		t.Fatal("create: id = 0")
	}

	// 3. List (should have +1).
	listReq2 := httptest.NewRequest(http.MethodGet, "/api/admin/bulletins", nil)
	listReq2.AddCookie(adminCookie)
	listRec2 := httptest.NewRecorder()
	mux.ServeHTTP(listRec2, listReq2)
	json.NewDecoder(listRec2.Body).Decode(&listResp)
	if len(listResp.Bulletins) != initialCount+1 {
		t.Errorf("list after create: got %d, want %d", len(listResp.Bulletins), initialCount+1)
	}

	// 4. Update.
	updateBody := `{"title":"Updated","content":"<p>Updated</p>","type":"warning","sort_order":20,"closable":false,"expires_at":null}`
	updateReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/admin/bulletins/%d", bulletinID), strings.NewReader(updateBody))
	updateReq.AddCookie(adminCookie)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	mux.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body: %s", updateRec.Code, updateRec.Body.String())
	}

	// 5. Delete.
	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/bulletins/%d", bulletinID), nil)
	delReq.AddCookie(adminCookie)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body: %s", delRec.Code, delRec.Body.String())
	}

	// 6. List (should be back to initial).
	listReq3 := httptest.NewRequest(http.MethodGet, "/api/admin/bulletins", nil)
	listReq3.AddCookie(adminCookie)
	listRec3 := httptest.NewRecorder()
	mux.ServeHTTP(listRec3, listReq3)
	json.NewDecoder(listRec3.Body).Decode(&listResp)
	if len(listResp.Bulletins) != initialCount {
		t.Errorf("list after delete: got %d, want %d", len(listResp.Bulletins), initialCount)
	}
}

func TestBulletinAdmin_NonAdmin(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, _ := store.CreateUser("42", "tester", "")
	token, _, _ := store.CreateSession(u.ID)
	cookie := &http.Cookie{Name: auth.SessionCookieName, Value: token}

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/admin/bulletins", ""},
		{http.MethodPost, "/api/admin/bulletins", `{"title":"x","content":"x"}`},
		{http.MethodPut, "/api/admin/bulletins/1", `{"title":"x","content":"x"}`},
		{http.MethodDelete, "/api/admin/bulletins/1", ""},
	}
	for _, tt := range tests {
		var req *http.Request
		if tt.body != "" {
			req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(tt.method, tt.path, nil)
		}
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", tt.method, tt.path, rec.Code)
		}
	}
}

func TestBulletinAdmin_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/bulletins"},
		{http.MethodPost, "/api/admin/bulletins"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", tt.method, tt.path, rec.Code)
		}
	}
}

func TestBulletinAdmin_RejectSystemEdit(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	// System bulletins have negative IDs. They don't exist in DB, so 404.
	updateBody := `{"title":"hack","content":"<p>hack</p>","type":"info","sort_order":0,"closable":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/bulletins/-1", strings.NewReader(updateBody))
	req.AddCookie(adminCookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("edit system bulletin: status = %d, want 404", rec.Code)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/admin/bulletins/-1", nil)
	delReq.AddCookie(adminCookie)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNotFound {
		t.Errorf("delete system bulletin: status = %d, want 404", delRec.Code)
	}
}

func TestBulletinHostSeparation_UserHost(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bulletins", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.SiteHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("user-host admin bulletins: status = %d, want 404", rec.Code)
	}
}

func TestBulletinHostSeparation_AdminHost(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	adminCookie := loginCookie(t, gw, "root", "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bulletins", nil)
	req.AddCookie(adminCookie)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin-host bulletins: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestBulletinPublicHostSeparation_AdminHost(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")

	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	wrapped := gw.Wrap(mux)

	// Public /api/bulletins should be accessible from admin host too.
	req := httptest.NewRequest(http.MethodGet, "/api/bulletins", nil)
	req.Host = gw.Config.Admin.AdminHost
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("admin-host public bulletins: status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}
