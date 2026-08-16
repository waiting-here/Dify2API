package db

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func adminAlertColumnNames(t *testing.T, st *Store) map[string]bool {
	t.Helper()
	rows, err := st.db.Query(`PRAGMA table_info(admin_alerts)`)
	if err != nil {
		t.Fatalf("admin_alerts table_info: %v", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan admin_alerts table_info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("admin_alerts table_info rows: %v", err)
	}
	return columns
}

func assertSubjectAlertSchema(t *testing.T, st *Store) {
	t.Helper()
	if !adminAlertColumnNames(t, st)["subject_user_id"] {
		t.Fatal("admin_alerts.subject_user_id column is missing")
	}
	var indexSQL string
	if err := st.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_admin_alerts_subject_user'`,
	).Scan(&indexSQL); err != nil {
		t.Fatalf("subject alert index missing: %v", err)
	}
	if !strings.Contains(indexSQL, "subject_user_id") {
		t.Fatalf("subject alert index SQL = %q, want subject_user_id", indexSQL)
	}
}

func TestR05SubjectAlertSchema_FreshAndOldMigration(t *testing.T) {
	t.Run("fresh", func(t *testing.T) {
		st, _ := openTemp(t)
		assertSubjectAlertSchema(t, st)
	})

	t.Run("old", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "old.db")
		keyPath := filepath.Join(dir, "old.key")
		oldSchema := strings.Replace(schema, "\tsubject_user_id INTEGER,\n", "", 1)
		if oldSchema == schema {
			t.Fatal("fixture did not remove subject_user_id from old schema")
		}
		raw, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open old database: %v", err)
		}
		if _, err := raw.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL;`); err != nil {
			raw.Close()
			t.Fatalf("old database pragmas: %v", err)
		}
		if _, err := raw.Exec(oldSchema); err != nil {
			raw.Close()
			t.Fatalf("apply old schema: %v", err)
		}
		if err := raw.Close(); err != nil {
			t.Fatalf("close old database: %v", err)
		}

		st, err := Open(dbPath, keyPath)
		if err != nil {
			t.Fatalf("open migrated database: %v", err)
		}
		assertSubjectAlertSchema(t, st)
		if err := st.Close(); err != nil {
			t.Fatalf("close migrated database: %v", err)
		}
		st, err = Open(dbPath, keyPath)
		if err != nil {
			t.Fatalf("reopen migrated database: %v", err)
		}
		defer st.Close()
		assertSubjectAlertSchema(t, st)
	})
}

func TestR05LegacyAlertSubjectParser(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		message string
		want    int64
		ok      bool
	}{
		{
			name:    "auto uses final token",
			kind:    "user_auto_banned",
			message: "name（ID：42）因 （ID：142）因 3 次超限",
			want:    142,
			ok:      true,
		},
		{
			name:    "debug uses final token",
			kind:    "debug_abuse",
			message: "name（ID：142）在 （ID：42）在 10 分钟内开启了 6 次",
			want:    42,
			ok:      true,
		},
		{
			name:    "zero rejected",
			kind:    "user_auto_banned",
			message: "name（ID：0）因 x",
		},
		{
			name:    "overflow rejected",
			kind:    "debug_abuse",
			message: "name（ID：9223372036854775808）在 x",
		},
		{
			name:    "missing separator rejected",
			kind:    "user_auto_banned",
			message: "name（ID：42）因x",
		},
		{
			name:    "other event rejected",
			kind:    "blocking_failed_200",
			message: "name（ID：42）",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := legacyAlertSubjectID(tc.kind, tc.message)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("legacyAlertSubjectID(%q, %q)=(%d,%v), want (%d,%v)", tc.kind, tc.message, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestR05DeleteUserSubjectAndLegacyAlerts(t *testing.T) {
	st, _ := openTemp(t)
	if _, err := st.RawExec(`INSERT INTO users (id, discord_id, username, created_at, updated_at)
		VALUES (42, 'r05-target', 'target', 1, 1),
		       (142, 'r05-142', 'other-142', 1, 1),
		       (420, 'r05-420', 'other-420', 1, 1)`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	targetID := int64(42)
	add := func(alert *AdminAlert) {
		t.Helper()
		if err := st.AddAdminAlert(alert); err != nil {
			t.Fatalf("AddAdminAlert(%+v): %v", alert, err)
		}
	}
	add(&AdminAlert{Type: "user_auto_banned", Message: "new auto", SubjectUserID: &targetID})
	add(&AdminAlert{Type: "debug_abuse", Message: "new debug", SubjectUserID: &targetID})
	add(&AdminAlert{Type: "user_auto_banned", Message: "legacy target（ID：42）因 3 次超限被自动封禁"})
	add(&AdminAlert{Type: "debug_abuse", Message: "legacy target debug（ID：42）在 10 分钟内开启了 6 次 Debug session"})
	add(&AdminAlert{Type: "user_auto_banned", Message: "adjacent（ID：142）因 3 次超限被自动封禁"})
	add(&AdminAlert{Type: "debug_abuse", Message: "adjacent（ID：420）在 10 分钟内开启了 6 次 Debug session"})
	add(&AdminAlert{Type: "blocking_failed_200", Message: "ordinary op（ID：42）"})
	add(&AdminAlert{Type: "user_auto_banned", Message: "legacy without an ID）因 x"})
	add(&AdminAlert{Type: "user_auto_banned", Message: "attacker（ID：42）因 （ID：142）因 3 次超限被自动封禁"})
	add(&AdminAlert{Type: "debug_abuse", Message: "target（ID：142）在 （ID：42）在 10 分钟内开启了 6 次 Debug session"})
	add(&AdminAlert{Type: "user_auto_banned", Message: "zero（ID：0）因 x"})
	add(&AdminAlert{Type: "debug_abuse", Message: "overflow（ID：9223372036854775808）在 x"})
	add(&AdminAlert{Type: "user_auto_banned", Message: "broken（ID：42）因x"})

	alerts, total, err := st.ListAdminAlerts(100, 0)
	if err != nil || total != 13 || len(alerts) != 13 {
		t.Fatalf("seeded alerts total=%d len=%d err=%v", total, len(alerts), err)
	}
	seenSubject := false
	for _, alert := range alerts {
		if alert.SubjectUserID != nil && *alert.SubjectUserID == targetID {
			seenSubject = true
		}
	}
	if !seenSubject {
		t.Fatal("subject_user_id was not returned by ListAdminAlerts")
	}

	if err := st.DeleteUser(targetID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	var remaining int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining alerts: %v", err)
	}
	if remaining != 8 {
		t.Fatalf("remaining alerts=%d, want 8", remaining)
	}
	alerts, _, err = st.ListAdminAlerts(100, 0)
	if err != nil {
		t.Fatalf("list remaining alerts: %v", err)
	}
	deleted := map[string]bool{
		"new auto":  true,
		"new debug": true,
		"legacy target（ID：42）因 3 次超限被自动封禁":                        true,
		"legacy target debug（ID：42）在 10 分钟内开启了 6 次 Debug session": true,
		"target（ID：142）在 （ID：42）在 10 分钟内开启了 6 次 Debug session":    true,
	}
	for _, alert := range alerts {
		if deleted[alert.Message] {
			t.Errorf("target alert survived: %+v", alert)
		}
		if alert.SubjectUserID != nil && *alert.SubjectUserID == targetID {
			t.Errorf("target subject alert survived: %+v", alert)
		}
	}
	for _, fragment := range []string{
		"（ID：142）因 3 次超限被自动封禁",
		"（ID：420）在 10 分钟内开启了 6 次 Debug session",
		"ordinary op（ID：42）",
		"legacy without an ID）因 x",
		"attacker（ID：42）因 （ID：142）因 3 次超限被自动封禁",
		"zero（ID：0）因 x",
		"overflow（ID：9223372036854775808）在 x",
		"broken（ID：42）因x",
	} {
		found := false
		for _, alert := range alerts {
			if strings.Contains(alert.Message, fragment) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("retained alert fragment %q is missing", fragment)
		}
	}
}

func TestR05DeleteUserSubjectAlertsRollBack(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("r05-rollback", "rollback", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	subjectID := u.ID
	legacyMessage := "legacy rollback（ID：" + strconv.FormatInt(subjectID, 10) + "）在 rollback"
	if err := st.AddAdminAlert(&AdminAlert{Type: "user_auto_banned", Message: "new rollback", SubjectUserID: &subjectID}); err != nil {
		t.Fatalf("add subject alert: %v", err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: "debug_abuse", Message: legacyMessage}); err != nil {
		t.Fatalf("add legacy alert: %v", err)
	}
	trigger := `CREATE TRIGGER r05_fail_delete_user
		BEFORE DELETE ON users WHEN OLD.id=` + strconv.FormatInt(u.ID, 10) + `
		BEGIN SELECT RAISE(ABORT, 'forced R05 delete failure'); END`
	if _, err := st.RawExec(trigger); err != nil {
		t.Fatalf("create rollback trigger: %v", err)
	}
	if err := st.DeleteUser(u.ID); err == nil {
		t.Fatal("DeleteUser succeeded despite injected failure")
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts WHERE subject_user_id=?`, u.ID).Scan(&count); err != nil {
		t.Fatalf("count subject alerts after rollback: %v", err)
	}
	if count != 1 {
		t.Fatalf("subject alerts after rollback=%d, want 1", count)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM admin_alerts WHERE type='debug_abuse' AND message=?`, legacyMessage).Scan(&count); err != nil {
		t.Fatalf("count legacy alerts after rollback: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy alerts after rollback=%d, want 1", count)
	}
	if got, err := st.GetUserByID(u.ID); err != nil || got == nil {
		t.Fatalf("user after rollback=%+v err=%v", got, err)
	}
}

func TestR05SubjectAlertInsertRequiresExistingUser(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("r05m1-subject", "subject", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	subjectID := u.ID
	if err := st.AddAdminAlert(&AdminAlert{
		Type:          "debug_abuse",
		Message:       "subject exists",
		SubjectUserID: &subjectID,
	}); err != nil {
		t.Fatalf("insert existing subject: %v", err)
	}
	alerts, total, err := st.ListAdminAlerts(100, 0)
	if err != nil || total != 1 || len(alerts) != 1 || alerts[0].SubjectUserID == nil || *alerts[0].SubjectUserID != subjectID {
		t.Fatalf("existing subject alert total=%d alerts=%+v err=%v", total, alerts, err)
	}
	if err := st.DeleteUser(subjectID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if err := st.AddAdminAlert(&AdminAlert{
		Type:          "debug_abuse",
		Message:       "late subject suppressed",
		SubjectUserID: &subjectID,
	}); err != nil {
		t.Fatalf("insert missing subject: %v", err)
	}
	if _, total, err := st.ListAdminAlerts(100, 0); err != nil || total != 0 {
		t.Fatalf("late subject total=%d err=%v, want 0", total, err)
	}
	if err := st.AddAdminAlert(&AdminAlert{Type: "operator_notice", Message: "null subject retained"}); err != nil {
		t.Fatalf("insert null subject: %v", err)
	}
	alerts, total, err = st.ListAdminAlerts(100, 0)
	if err != nil || total != 1 || len(alerts) != 1 || alerts[0].SubjectUserID != nil {
		t.Fatalf("null subject alert total=%d alerts=%+v err=%v", total, alerts, err)
	}

	other, err := st.CreateUser("r05m1-gated", "gated", "")
	if err != nil {
		t.Fatalf("CreateUser gated: %v", err)
	}
	if err := st.EnsureAlertPrefs([]string{"gated_subject"}); err != nil {
		t.Fatalf("EnsureAlertPrefs: %v", err)
	}
	if err := st.SetAlertPref("gated_subject", false, true); err != nil {
		t.Fatalf("SetAlertPref: %v", err)
	}
	gatedID := other.ID
	if err := st.AddAdminAlert(&AdminAlert{
		Type:          "gated_subject",
		Message:       "subject gate",
		SubjectUserID: &gatedID,
	}); err != nil {
		t.Fatalf("insert gated subject: %v", err)
	}
	if _, total, err := st.ListAdminAlerts(100, 0); err != nil || total != 1 {
		t.Fatalf("gated subject total=%d err=%v, want 1", total, err)
	}
}
