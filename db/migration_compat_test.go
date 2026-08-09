package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func stripV131RuntimeIndexes(t *testing.T, input string) string {
	t.Helper()
	for _, indexLine := range []string{
		"CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);\n",
		"CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at);\n",
		"CREATE INDEX IF NOT EXISTS idx_da_donation ON donation_applications(donation_id);\n",
	} {
		if !strings.Contains(input, indexLine) {
			t.Fatalf("fixture broken: missing %q", strings.TrimSpace(indexLine))
		}
		input = strings.Replace(input, indexLine, "", 1)
	}
	return input
}

func stripV131ActivitySchema(t *testing.T, input string) string {
	t.Helper()
	start := strings.Index(input, "CREATE TABLE IF NOT EXISTS user_activity_daily")
	end := strings.Index(input, "CREATE TABLE IF NOT EXISTS donations")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("fixture broken: activity schema block not found")
	}
	return input[:start] + input[end:]
}

// TestV120ToV121MigrationCompatibility verifies that a production database
// created by the v1.2.0 binary opens cleanly with the current code and that
// the v1.2.1 schema additions (alert_prefs table, donation_selectable
// column) are applied idempotently at startup — no manual migration needed.
func TestV120ToV121MigrationCompatibility(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")
	keyPath := filepath.Join(dir, "old.key")

	// 1. Build a database with the EXACT v1.2.0 schema: take the current
	// schema and remove the v1.2.1 additions (alert_prefs table and the
	// donation_selectable column) plus the v1.3.0 addition (users.level).
	// Guards below ensure the removals matched.
	oldSchema := stripV131ActivitySchema(t, stripV131RuntimeIndexes(t, schema))
	alertPrefsBlock := `
CREATE TABLE IF NOT EXISTS alert_prefs (
	event_type     TEXT PRIMARY KEY,
	show_in_center INTEGER NOT NULL DEFAULT 1,
	email_enabled  INTEGER NOT NULL DEFAULT 1,
	updated_at     INTEGER NOT NULL DEFAULT 0
);
`
	donSelLine := "    donation_selectable    INTEGER NOT NULL DEFAULT 1,\n"
	levelBlock := "updated_at      INTEGER NOT NULL,\n\tlevel           INTEGER\n"
	if !strings.Contains(oldSchema, alertPrefsBlock) {
		t.Fatal("fixture broken: alert_prefs block not found in current schema")
	}
	if !strings.Contains(oldSchema, donSelLine) {
		t.Fatal("fixture broken: donation_selectable line not found in current schema")
	}
	if !strings.Contains(oldSchema, levelBlock) {
		t.Fatal("fixture broken: users.level block not found in current schema")
	}
	oldSchema = strings.Replace(oldSchema, alertPrefsBlock, "", 1)
	oldSchema = strings.Replace(oldSchema, donSelLine, "", 1)
	oldSchema = strings.Replace(oldSchema, levelBlock, "updated_at      INTEGER NOT NULL\n", 1)

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

// TestV121ToV130MigrationCompatibility verifies that a production database
// created by the v1.2.1 binary opens cleanly with the current code: the
// users.level column is added idempotently with NULL (automatic) semantics,
// the nine level-settings keys fall back to their defaults when absent, and
// re-opening stays idempotent — no manual migration needed.
func TestV121ToV130MigrationCompatibility(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")
	keyPath := filepath.Join(dir, "old.key")

	// 1. Build a database with the EXACT v1.2.1 schema: take the current
	// schema and remove the v1.3.0 addition (users.level). The guard below
	// ensures the removal matched.
	oldSchema := stripV131ActivitySchema(t, stripV131RuntimeIndexes(t, schema))
	levelBlock := "updated_at      INTEGER NOT NULL,\n\tlevel           INTEGER\n"
	if !strings.Contains(oldSchema, levelBlock) {
		t.Fatal("fixture broken: users.level block not found in current schema")
	}
	oldSchema = strings.Replace(oldSchema, levelBlock, "updated_at      INTEGER NOT NULL\n", 1)

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("apply v1.2.1 schema: %v", err)
	}
	// Seed v1.2.1-style rows: a user (with donation credit), an app config,
	// an alert and a level-name-ish setting that must all survive the upgrade.
	if _, err := raw.Exec(
		`INSERT INTO users (discord_id, username, credits, donation_credit, created_at, updated_at)
		 VALUES ('disc1','alice',100,250,1,1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO admin_alerts (type, message, created_at) VALUES ('donation_outcome','old alert',1)`); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	// 2. Open the old database with the current (v1.3.0) code path.
	st, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("Open migrated db: %v", err)
	}

	// 3. users.level was added and defaults to NULL (automatic).
	u, err := st.GetUserByDiscordID("disc1")
	if err != nil || u == nil {
		t.Fatalf("old user lost: %v / %v", u, err)
	}
	if u.Level != nil {
		t.Errorf("migrated user Level = %v, want nil (automatic)", *u.Level)
	}
	// The automatic level is computed lazily from the v1.2.1-era credit.
	if level, manual := GetUserLevel(u, st.LevelThresholds()); level != 3 || manual {
		t.Errorf("effective level = %d manual=%v, want 3/false (250 credits, defaults 1/100/500)", level, manual)
	}

	// 4. The nine level-settings keys are absent: defaults apply.
	th := st.LevelThresholds()
	if th.T2 != DefaultLevelThreshold2 || th.T3 != DefaultLevelThreshold3 || th.T4 != DefaultLevelThreshold4 {
		t.Errorf("default thresholds = %+v, want %d/%d/%d", th, DefaultLevelThreshold2, DefaultLevelThreshold3, DefaultLevelThreshold4)
	}
	for _, key := range LevelNameKeys {
		if v := st.GetSettingString(key, ""); v != "" {
			t.Errorf("absent level name %s = %q, want empty", key, v)
		}
	}
	if v := st.GetSettingString(SettingLevelBannerText, ""); v != "" {
		t.Errorf("absent banner = %q, want empty", v)
	}

	// 5. Writing a manual level through the new column works and persists.
	five := 5
	if err := st.SetUserLevel(u.ID, &five); err != nil {
		t.Fatalf("SetUserLevel on migrated db: %v", err)
	}

	// 6. Old v1.2.1 data survives the migration.
	if _, total, err := st.ListAdminAlerts(100, 0); err != nil || total != 1 {
		t.Errorf("old alert lost: total=%d err=%v", total, err)
	}

	// 7. Re-opening is idempotent (no duplicate column errors, no data loss).
	if err := st.Close(); err != nil {
		t.Fatalf("close migrated db: %v", err)
	}
	st2, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("re-open migrated db: %v", err)
	}
	defer st2.Close()
	u2, err := st2.GetUserByDiscordID("disc1")
	if err != nil || u2 == nil {
		t.Fatalf("user lost after reopen: %v / %v", u2, err)
	}
	if u2.Level == nil || *u2.Level != 5 {
		t.Errorf("manual level after reopen = %v, want 5", u2.Level)
	}
	if u2.Credits != 100 || u2.DonationCredit != 250 {
		t.Errorf("user data after reopen: credits=%d donation_credit=%d, want 100/250", u2.Credits, u2.DonationCredit)
	}
	if _, total, _ := st2.ListAdminAlerts(100, 0); total != 1 {
		t.Errorf("old alert lost after reopen: total=%d", total)
	}
	if err := st2.Close(); err != nil {
		t.Fatalf("close re-opened db: %v", err)
	}
}
