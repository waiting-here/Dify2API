package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"dify2api/db"
)

func TestResolveLang_DefaultZh(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gw := NewGateway(testConfig(), store)

	req := httptest.NewRequest("GET", "/api/test", nil)
	lang := gw.resolveLang(req)
	if lang != "zh" {
		t.Errorf("expected default 'zh', got %q", lang)
	}
}

func TestResolveLang_QueryParam(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gw := NewGateway(testConfig(), store)

	req := httptest.NewRequest("GET", "/api/test?lang=en", nil)
	lang := gw.resolveLang(req)
	if lang != "en" {
		t.Errorf("expected 'en' from query param, got %q", lang)
	}

	req2 := httptest.NewRequest("GET", "/api/test?lang=zh", nil)
	lang2 := gw.resolveLang(req2)
	if lang2 != "zh" {
		t.Errorf("expected 'zh' from query param, got %q", lang2)
	}
}

func TestResolveLang_UserPreference(t *testing.T) {
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "test.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gw := NewGateway(testConfig(), store)

	u, err := store.CreateUser("123", "testuser", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserLang(u.ID, "en"); err != nil {
		t.Fatal(err)
	}

	token, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "dify2api_session", Value: token})

	lang := gw.resolveLang(req)
	if lang != "en" {
		t.Errorf("expected 'en' from user preference, got %q", lang)
	}
}

func TestHelperT(tt *testing.T) {
	if got := t("zh", "中文", "English"); got != "中文" {
		tt.Errorf("expected '中文' for lang=zh, got %q", got)
	}
	if got := t("", "中文", "English"); got != "中文" {
		tt.Errorf("expected '中文' for empty lang, got %q", got)
	}
	if got := t("en", "中文", "English"); got != "English" {
		tt.Errorf("expected 'English' for lang=en, got %q", got)
	}
	if got := t("fr", "中文", "English"); got != "中文" {
		tt.Errorf("expected '中文' for unsupported lang=fr, got %q", got)
	}
}
