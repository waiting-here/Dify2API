package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// TestV120ToV121MigrationCompatibility verifies that a production database
// created by the v1.2.0 binary opens cleanly with the current code and that
// the v1.2.1 schema additions (alert_prefs table, donation_selectable
// column) are applied idempotently at startup — no manual migration needed.
func TestV120ToV121MigrationCompatibility(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")
	keyPath := filepath.Join(dir, "old.key")

	// 1. Build a database with the EXACT v1.2.0 schema: take the current
	// schema and remove the two v1.2.1 additions (alert_prefs table and the
	// donation_selectable column). Guards below ensure the removals matched.
	oldSchema := schema
	alertPrefsBlock := `
CREATE TABLE IF NOT EXISTS alert_prefs (
	event_type     TEXT PRIMARY KEY,
	show_in_center INTEGER NOT NULL DEFAULT 1,
	email_enabled  INTEGER NOT NULL DEFAULT 1,
	updated_at     INTEGER NOT NULL DEFAULT 0
);
`
	donSelLine := "    donation_selectable    INTEGER NOT NULL DEFAULT 1,\n"
	if !strings.Contains(oldSchema, alertPrefsBlock) {
		t.Fatal("fixture broken: alert_prefs block not found in current schema")
	}
	if !strings.Contains(oldSchema, donSelLine) {
		t.Fatal("fixture broken: donation_selectable line not found in current schema")
	}
	oldSchema = strings.Replace(oldSchema, alertPrefsBlock, "", 1)
	oldSchema = strings.Replace(oldSchema, donSelLine, "", 1)

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("apply v1.2.0 schema: %v", err)
	}
	// Note: v1.2.0's CREATE TABLE for bulletins already includes
	// content_type (the Open() ALTER only targets pre-alpha3 databases), so
	// the derived v1.2.0 schema above is complete as-is.
	// Seed a v1.2.0-style anti-abuse row (no donation_selectable column),
	// a user and an alert — all must survive the upgrade.
	if _, err := raw.Exec(
		`INSERT INTO service_anti_abuse (service, mode, min_chars, penalty_deduct_credits, penalty_ban_hours, created_at, updated_at)
		 VALUES ('general', 2, 20, 0, 0, 1, 1)`); err != nil {
		t.Fatalf("seed old anti-abuse row: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (discord_id, username, credits, created_at, updated_at) VALUES ('disc1','alice',100,1,1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO admin_alerts (type, message, created_at) VALUES ('blocking_failed_200','old alert',1)`); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	// 2. Open the old database with the current (v1.2.1) code path.
	st, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}

	// 3. Old anti-abuse row readable; missing flag defaults to selectable.
	c, err := st.GetAntiAbuseConfig("general")
	if err != nil || c == nil {
		t.Fatalf("GetAntiAbuseConfig: %v / %v", c, err)
	}
	if c.Mode != 2 {
		t.Errorf("mode = %d, want 2", c.Mode)
	}
	if !st.IsServiceDonationSelectable("general") {
		t.Error("old row must default to donation_selectable=1")
	}
	// Upsert through the new column works and persists.
	if _, err := st.UpsertAntiAbuseConfig("general", 1, 10, 5, 3, 0); err != nil {
		t.Fatalf("upsert with new column: %v", err)
	}
	if st.IsServiceDonationSelectable("general") {
		t.Error("upsert with donation_selectable=0 must be honoured")
	}

	// 4. alert_prefs is created by Open(); prefs default to on.
	if err := st.EnsureAlertPrefs([]string{"user_auto_banned"}); err != nil {
		t.Fatalf("EnsureAlertPrefs: %v", err)
	}
	if !st.IsAlertEmailEnabled("user_auto_banned") || !st.IsAlertShownInCenter("user_auto_banned") {
		t.Error("seeded pref should default to both on")
	}

	// 5. v1.2.0 data survives.
	if _, total, err := st.ListAdminAlerts(100, 0); err != nil || total != 1 {
		t.Errorf("old alert lost: total=%d err=%v", total, err)
	}
	if u, err := st.GetUserByDiscordID("disc1"); err != nil || u == nil || u.Credits != 100 {
		t.Errorf("old user lost: %v / %v", u, err)
	}

	// 6. Re-opening is idempotent (no duplicate column errors, no data loss).
	if err := st.Close(); err != nil {
		t.Fatalf("close migrated db: %v", err)
	}
	st2, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("re-open migrated db: %v", err)
	}
	defer st2.Close()
	if st2.IsServiceDonationSelectable("general") {
		t.Error("donation_selectable=0 must persist across reopen")
	}
	if _, total, _ := st2.ListAdminAlerts(100, 0); total != 1 {
		t.Errorf("old alert lost after reopen: total=%d", total)
	}
	if err := st2.Close(); err != nil {
		t.Fatalf("close re-opened db: %v", err)
	}
}
