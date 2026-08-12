package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dify2api/db"
)

func TestMarketplacePluginDependencyPin(t *testing.T) {
	cases := []struct {
		name            string
		plugin, version string
		hash            string
		ok              bool
	}{
		{name: "live manifest", plugin: "langgenius/openai", version: "2.0.0", hash: "abc", ok: true},
		{name: "missing checksum", ok: false},
	}
	plugins := []marketplacePlugin{
		{Org: "langgenius", Name: "openai", LatestVersion: "2.0.0", LatestPackageIdentifier: "langgenius/openai:2.0.0@abc"},
		{Org: "langgenius", Name: "openai", LatestVersion: "2.0.0", LatestPackageIdentifier: "langgenius/openai:2.0.0"},
	}
	for i, tc := range cases {
		plugin, version, hash, ok := plugins[i].dependencyPin()
		if plugin != tc.plugin || version != tc.version || hash != tc.hash || ok != tc.ok {
			t.Errorf("%s = (%q,%q,%q,%v)", tc.name, plugin, version, hash, ok)
		}
	}
}

func TestMarketplaceSyncUpdatesNonManual(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	oldURL := marketplaceManifestURL
	defer func() { marketplaceManifestURL = oldURL }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/api/v1/dist/plugins/manifest.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"plugins": [
				{"org":"langgenius","name":"anthropic","latest_version":"0.4.1","latest_package_identifier":"langgenius/anthropic:0.4.1@abc123"},
				{"org":"langgenius","name":"openai","latest_version":"2.0.0","latest_package_identifier":"langgenius/openai:2.0.0@def456"},
				{"org":"langgenius","name":"openai","latest_version":"2.0.0","latest_package_identifier":"langgenius/openai:2.0.0@def456"}
			]
		}`))
	}))
	defer srv.Close()
	marketplaceManifestURL = srv.URL + "/api/v1/dist/plugins/manifest.json"

	// Mark one config manual — it must be left untouched.
	manual := db.ModelConfig{
		ModelKey: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "anthropic",
		DependencyPlugin: "langgenius/anthropic", DependencyVer: "0.3.26",
		DependencyHash: "cc37544750d72ca3782bdaa81ab0e2facbf5bb74105a169a7ff974a27c6a5f29",
		Manual:         true, Enabled: true, SortOrder: 1,
	}
	if err := store.PutModelConfig(manual); err != nil {
		t.Fatal(err)
	}

	gw.marketplaceSyncOnce(time.Now())

	mc, err := store.GetModelConfig("gpt-5.6-sol")
	if err != nil || mc == nil {
		t.Fatal(err)
	}
	if mc.DependencyVer != "2.0.0" || mc.DependencyHash != "def456" {
		t.Fatalf("non-manual not updated: %+v", mc)
	}
	mc2, _ := store.GetModelConfig("claude-opus-4-6")
	if mc2.DependencyVer != "0.3.26" {
		t.Fatalf("manual row was overridden: %+v", mc2)
	}

	// Failure path records an alert-center entry.
	marketplaceManifestURL = srv.URL + "/broken"
	gw.marketplaceSyncOnce(time.Now())
	alerts, total, err := store.ListAdminAlerts(50, 0)
	if err != nil || total == 0 {
		t.Fatalf("no alert recorded on failure: %v total=%d", err, total)
	}
	failedAlert := false
	for _, a := range alerts {
		if a.Type == "marketplace_sync" && a.Message != "" {
			failedAlert = true
		}
	}
	if !failedAlert {
		t.Fatalf("marketplace failure alert missing: %+v", alerts)
	}
}
