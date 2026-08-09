package db

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivitySchemaContract(t *testing.T) {
	st, _ := openTemp(t)
	for table, want := range map[string][]string{
		"user_activity_daily": {"day", "user_id", "api_attempts", "api_successes", "console_actions", "checkins", "updated_at"},
		"site_activity_daily": {"day", "new_users", "product_active", "successful_api_active", "attempted_api_active", "console_active", "checkin_only_active", "api_attempts", "api_successes", "wau", "active_28d", "engaged_28d", "finalized_at"},
	} {
		rows, err := st.db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			got = append(got, name)
		}
		rows.Close()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s columns=%v, want %v", table, got, want)
		}
	}
	var indexName string
	if err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_uad_user_day'`).Scan(&indexName); err != nil {
		t.Fatalf("idx_uad_user_day missing: %v", err)
	}
}

func TestRequestActivityConcurrentAndEventUnion(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("activity-100", "activity", "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.AddRequestLogFull(u.ID, fmt.Sprintf("m-%d", i), "general", at, at.Add(time.Second), "success", "", 200, "", 0, 0, "")
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent log: %v", err)
		}
	}
	if err := st.RecordConsoleAction(u.ID, at); err != nil {
		t.Fatal(err)
	}
	var attempts, successes, console int64
	if err := st.db.QueryRow(`SELECT api_attempts,api_successes,console_actions FROM user_activity_daily WHERE day=? AND user_id=?`, utcDay(at), u.ID).
		Scan(&attempts, &successes, &console); err != nil {
		t.Fatal(err)
	}
	if attempts != 100 || successes != 100 || console != 1 {
		t.Fatalf("user aggregate = attempts %d successes %d console %d", attempts, successes, console)
	}
	var product, attempted, successful, consoleActive, siteAttempts, siteSuccesses int64
	if err := st.db.QueryRow(`SELECT product_active,attempted_api_active,successful_api_active,console_active,api_attempts,api_successes
		FROM site_activity_daily WHERE day=?`, utcDay(at)).Scan(&product, &attempted, &successful, &consoleActive, &siteAttempts, &siteSuccesses); err != nil {
		t.Fatal(err)
	}
	if product != 1 || attempted != 1 || successful != 1 || consoleActive != 1 || siteAttempts != 100 || siteSuccesses != 100 {
		t.Fatalf("site union/counts = %d/%d/%d/%d attempts=%d successes=%d", product, attempted, successful, consoleActive, siteAttempts, siteSuccesses)
	}

	failureOnly, _ := st.CreateUser("activity-failure", "failure", "")
	if err := st.AddRequestLog(failureOnly.ID, "m", "general", at, at, "error", "upstream"); err != nil {
		t.Fatal(err)
	}
	checkinOnly, _ := st.CreateUser("activity-checkin", "checkin", "")
	if status, _, _, err := st.ApplyUserCheckin(checkinOnly.ID, "activity-day", 1, 100, false); err != nil || status != CheckinApplied {
		t.Fatalf("checkin: status=%s err=%v", status, err)
	}
	var failureProduct, checkinProduct int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=? AND api_attempts=1 AND api_successes=0 AND console_actions=0`, failureOnly.ID).Scan(&failureProduct); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=? AND checkins=1 AND api_successes=0 AND console_actions=0`, checkinOnly.ID).Scan(&checkinProduct); err != nil {
		t.Fatal(err)
	}
	if failureProduct != 1 || checkinProduct != 1 {
		t.Fatalf("failure/checkin rows missing: %d/%d", failureProduct, checkinProduct)
	}
	var productAfter, attemptedAfter, checkinOnlyAfter int64
	if err := st.db.QueryRow(`SELECT product_active,attempted_api_active,checkin_only_active FROM site_activity_daily WHERE day=?`, utcDay(at)).
		Scan(&productAfter, &attemptedAfter, &checkinOnlyAfter); err != nil {
		t.Fatal(err)
	}
	if productAfter != 1 || attemptedAfter != 2 || checkinOnlyAfter != 1 {
		t.Fatalf("failure/checkin site counts = product %d attempted %d checkin-only %d", productAfter, attemptedAfter, checkinOnlyAfter)
	}

	cross, _ := st.CreateUser("activity-midnight", "midnight", "")
	day := utcDay(at)
	beforeMidnight := time.Unix(day-1, 0).UTC()
	afterMidnight := time.Unix(day, 0).UTC()
	if err := st.AddRequestLog(cross.ID, "before", "general", beforeMidnight, beforeMidnight, "error", "x"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddRequestLog(cross.ID, "after", "general", afterMidnight, afterMidnight, "success", ""); err != nil {
		t.Fatal(err)
	}
	var midnightRows int
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=?`, cross.ID).Scan(&midnightRows)
	if midnightRows != 2 {
		t.Fatalf("UTC midnight rows=%d, want 2", midnightRows)
	}

	admin, _ := st.EnsureAdminUser("root")
	if err := st.AddRequestLog(admin.ID, "m", "general", at, at, "success", ""); err != nil {
		t.Fatal(err)
	}
	var adminRows int
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=?`, admin.ID).Scan(&adminRows)
	if adminRows != 0 {
		t.Fatalf("admin activity rows = %d, want 0", adminRows)
	}
}

func TestActivityBackfillIdempotentAndLogRollback(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("backfill", "backfill", "")
	now := time.Now().UTC()
	if _, err := st.db.Exec(`DELETE FROM site_activity_daily; DELETE FROM user_activity_daily; DELETE FROM settings WHERE key=?`, activityBackfillKey); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO request_logs(user_id,model,service,started_at,ended_at,status,error_code) VALUES
		(?,?,?,?,?,'success',''),(?,?,?,?,?,'error','x')`,
		u.ID, "a", "general", now.Unix(), now.Unix(), u.ID, "b", "general", now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.initializeActivity(now); err != nil {
			t.Fatalf("initialize %d: %v", i, err)
		}
	}
	var attempts, successes int
	if err := st.db.QueryRow(`SELECT api_attempts,api_successes FROM user_activity_daily WHERE user_id=?`, u.ID).Scan(&attempts, &successes); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || successes != 1 {
		t.Fatalf("backfill = %d/%d, want 2/1", attempts, successes)
	}

	broken, _ := st.CreateUser("rollback", "rollback", "")
	if _, err := st.db.Exec(`CREATE TRIGGER fail_activity BEFORE INSERT ON user_activity_daily
		WHEN NEW.user_id=` + fmt.Sprint(broken.ID) + ` BEGIN SELECT RAISE(ABORT,'activity sentinel'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.AddRequestLog(broken.ID, "rollback", "general", now, now, "success", ""); err == nil {
		t.Fatal("request log succeeded despite activity trigger")
	}
	var logs int
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE user_id=?`, broken.ID).Scan(&logs)
	if logs != 0 {
		t.Fatalf("request log was not rolled back: %d", logs)
	}
	if err := st.RecordConsoleAction(broken.ID, now); err == nil {
		t.Fatal("console activity succeeded despite activity trigger")
	}
	if status, _, _, err := st.ApplyUserCheckin(broken.ID, "rollback-day", 10, 100, false); err == nil || status != "" {
		t.Fatalf("checkin activity failure did not roll back: status=%q err=%v", status, err)
	}
	got, _ := st.GetUserByID(broken.ID)
	if got.LastCheckinDay != "" || got.Credits != 0 {
		t.Fatalf("checkin business row survived activity failure: %+v", got)
	}
}

func TestActivityFreezeSuppressionDeleteAndRetention(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	users := make([]*User, 5)
	for i := range users {
		users[i], _ = st.CreateUser(fmt.Sprintf("freeze-%d", i), "freeze", "")
		if err := st.AddRequestLog(users[i].ID, "m", "general", yesterday, yesterday, "success", ""); err != nil {
			t.Fatal(err)
		}
		if err := st.AddRequestLog(users[i].ID, "current", "general", now, now, "success", ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		old, _ := st.CreateUser(fmt.Sprintf("rolling-old-%d", i), "old", "")
		at := now.AddDate(0, 0, -28)
		if err := st.AddRequestLog(old.ID, "m", "general", at, at, "success", ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		disabled, _ := st.CreateUser(fmt.Sprintf("rolling-disabled-%d", i), "disabled", "")
		if err := st.AddRequestLog(disabled.ID, "m", "general", now, now, "success", ""); err != nil {
			t.Fatal(err)
		}
		if err := st.SetUserDisabled(disabled.ID, true, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	var product, attempts int64
	var finalized int64
	if err := st.db.QueryRow(`SELECT product_active,api_attempts,finalized_at FROM site_activity_daily WHERE day=?`, utcDay(yesterday)).Scan(&product, &attempts, &finalized); err != nil {
		t.Fatal(err)
	}
	if product != 5 || attempts != 5 || finalized == 0 {
		t.Fatalf("frozen snapshot = product %d attempts %d finalized %d", product, attempts, finalized)
	}
	if err := st.DeleteUser(users[0].ID); err != nil {
		t.Fatal(err)
	}
	var currentProduct int64
	if err := st.db.QueryRow(`SELECT product_active FROM site_activity_daily WHERE day=?`, utcDay(now)).Scan(&currentProduct); err != nil || currentProduct != 4 {
		t.Fatalf("current snapshot was not deducted: product=%d err=%v", currentProduct, err)
	}
	if err := st.db.QueryRow(`SELECT product_active FROM site_activity_daily WHERE day=?`, utcDay(yesterday)).Scan(&product); err != nil || product != 5 {
		t.Fatalf("frozen snapshot rewritten: product=%d err=%v", product, err)
	}

	smallDay := now.AddDate(0, 0, -2)
	for i := 1; i < 5; i++ {
		if err := st.AddRequestLog(users[i].ID, "m", "general", smallDay, smallDay, "success", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	var suppressedProduct, suppressedAttempts any
	if err := st.db.QueryRow(`SELECT product_active,api_attempts FROM site_activity_daily WHERE day=?`, utcDay(smallDay)).Scan(&suppressedProduct, &suppressedAttempts); err != nil {
		t.Fatal(err)
	}
	if suppressedProduct != nil || suppressedAttempts != nil {
		t.Fatalf("k suppression leaked values: product=%v attempts=%v", suppressedProduct, suppressedAttempts)
	}
	late, _ := st.CreateUser("freeze-late", "late", "")
	if err := st.AddRequestLog(late.ID, "m", "general", smallDay, smallDay, "success", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT product_active FROM site_activity_daily WHERE day=?`, utcDay(smallDay)).Scan(&suppressedProduct); err != nil || suppressedProduct != nil {
		t.Fatalf("late event changed frozen suppression: %v / %v", suppressedProduct, err)
	}

	keepDay := utcDay(now) - (ActivityRetentionDays-1)*activityDaySeconds
	dropDay := keepDay - activityDaySeconds
	if _, err := st.db.Exec(`INSERT OR REPLACE INTO user_activity_daily(day,user_id,updated_at) VALUES(?,?,?),(?,?,?)`, keepDay, users[1].ID, now.Unix(), dropDay, users[1].ID, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT OR REPLACE INTO site_activity_daily(day,finalized_at) VALUES(?,?),(?,?)`, keepDay, now.Unix(), dropDay, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	var kept, dropped int
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE day=?`, keepDay).Scan(&kept)
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE day=?`, dropDay).Scan(&dropped)
	if kept != 1 || dropped != 0 {
		t.Fatalf("retention keep/drop = %d/%d", kept, dropped)
	}

	bundle, err := st.ExportUserData(users[1].ID)
	if err != nil || len(bundle.Activity) == 0 {
		t.Fatalf("activity export missing: len=%d err=%v", len(bundle.Activity), err)
	}
	if err := st.DeleteUser(users[1].ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	_ = st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=?`, users[1].ID).Scan(&remaining)
	if remaining != 0 {
		t.Fatalf("activity survived DeleteUser: %d", remaining)
	}
}

func crossMidnightActivityUsers(t *testing.T, st *Store, now time.Time, count int) []*User {
	t.Helper()
	users := make([]*User, count)
	for i := range users {
		var err error
		users[i], err = st.CreateUser(fmt.Sprintf("midnight-%d-%d", now.Unix(), i), "midnight", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	return users
}

func recordCrossMidnightActivity(t *testing.T, st *Store, users []*User, now time.Time) {
	t.Helper()
	for _, user := range users {
		for _, at := range []time.Time{now.AddDate(0, 0, -1), now} {
			if err := st.AddRequestLog(user.ID, "midnight", "general", at, at, "success", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func requireSiteProduct(t *testing.T, st *Store, day int64, wantProduct, wantFinalized int64) {
	t.Helper()
	var product sql.NullInt64
	var finalized int64
	if err := st.db.QueryRow(`SELECT product_active,finalized_at FROM site_activity_daily WHERE day=?`, day).
		Scan(&product, &finalized); err != nil {
		t.Fatal(err)
	}
	if !product.Valid || product.Int64 != wantProduct || finalized != wantFinalized {
		t.Fatalf("site day %d = product %v finalized %d, want %d/%d", day, product, finalized, wantProduct, wantFinalized)
	}
}

func TestSetUserDisabledFreezesCompletedDayBeforeStateChange(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Date(2030, 6, 2, 12, 0, 0, 0, time.UTC)
	users := crossMidnightActivityUsers(t, st, now, 5)
	recordCrossMidnightActivity(t, st, users, now)
	yesterday := utcDay(now.AddDate(0, 0, -1))
	today := utcDay(now)
	requireSiteProduct(t, st, yesterday, 5, 0)

	if err := st.setUserDisabledAt(users[0].ID, true, "midnight", now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
	requireSiteProduct(t, st, today, 4, 0)
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
}

func TestUnbanUserFreezesCompletedDayBeforeStateChange(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Date(2030, 6, 2, 12, 0, 0, 0, time.UTC)
	users := crossMidnightActivityUsers(t, st, now, 6)
	target := users[0]
	if _, err := st.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, target.ID); err != nil {
		t.Fatal(err)
	}
	recordCrossMidnightActivity(t, st, users, now)
	yesterday := utcDay(now.AddDate(0, 0, -1))
	today := utcDay(now)
	requireSiteProduct(t, st, yesterday, 5, 0)

	if err := st.unbanUserAt(target.ID, now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
	requireSiteProduct(t, st, today, 6, 0)
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
}

func TestDeleteUserFreezesCompletedDayBeforeDelete(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Date(2030, 6, 2, 12, 0, 0, 0, time.UTC)
	users := crossMidnightActivityUsers(t, st, now, 5)
	target := users[0]
	recordCrossMidnightActivity(t, st, users, now)
	yesterday := utcDay(now.AddDate(0, 0, -1))
	today := utcDay(now)
	requireSiteProduct(t, st, yesterday, 5, 0)

	if err := st.deleteUserAt(target.ID, now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
	requireSiteProduct(t, st, today, 4, 0)
	if err := st.MaintainActivity(now); err != nil {
		t.Fatal(err)
	}
	requireSiteProduct(t, st, yesterday, 5, now.Unix())
	var activityRows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM user_activity_daily WHERE user_id=?`, target.ID).Scan(&activityRows); err != nil {
		t.Fatal(err)
	}
	if activityRows != 0 {
		t.Fatalf("deleted user retained %d activity rows", activityRows)
	}
}

func TestActivityRollingDistinctBoundariesAndExplain(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now().UTC()
	users := make([]*User, 5)
	for i := range users {
		users[i], _ = st.CreateUser(fmt.Sprintf("rolling-%d", i), "rolling", "")
		for _, offset := range []int{-27, -6, 0} {
			at := now.AddDate(0, 0, offset)
			if err := st.AddRequestLog(users[i].ID, "m", "general", at, at, "success", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	outside, _ := st.CreateUser("rolling-outside", "rolling", "")
	outsideAt := now.AddDate(0, 0, -28)
	if err := st.AddRequestLog(outside.ID, "m", "general", outsideAt, outsideAt, "success", ""); err != nil {
		t.Fatal(err)
	}
	stats, err := st.ActivityStats(utcDay(now)-27*activityDaySeconds, utcDay(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary.DAU == nil || *stats.Summary.DAU != 5 || stats.Summary.WAU == nil || *stats.Summary.WAU != 5 ||
		stats.Summary.Active28D == nil || *stats.Summary.Active28D != 5 || stats.Summary.Engaged28D == nil || *stats.Summary.Engaged28D != 5 {
		t.Fatalf("rolling summary = %+v", stats.Summary)
	}
	if len(stats.ByDay) != 28 {
		t.Fatalf("by_day len=%d, want 28", len(stats.ByDay))
	}

	rows, err := st.db.Query(`EXPLAIN QUERY PLAN SELECT uad.user_id FROM user_activity_daily uad
		WHERE uad.day BETWEEN ? AND ? GROUP BY uad.user_id`, utcDay(now)-399*activityDaySeconds, utcDay(now))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, " | ")
	if !strings.Contains(joined, "SEARCH uad USING") && !strings.Contains(joined, "SEARCH user_activity_daily USING") {
		t.Fatalf("activity range query does not use an index: %s", joined)
	}
	t.Logf("activity range EXPLAIN: %s", joined)
}

func TestActivityActivationFunnelLaunchAndSevenDayBoundary(t *testing.T) {
	st, _ := openTemp(t)
	base := time.Unix(utcDay(time.Now())+activityDaySeconds+12*int64(time.Hour/time.Second), 0).UTC()
	users := make([]*User, 7)
	for i := range users {
		var err error
		users[i], err = st.CreateUser(fmt.Sprintf("activation-%d", i), "activation", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`UPDATE users SET created_at=?,updated_at=? WHERE id=?`, base.Unix(), base.Unix(), users[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		cfg, err := st.CreateAppConfig(users[i].ID, fmt.Sprintf("[general]activation-%d", i), "https://example.com", "key", "")
		if err != nil {
			t.Fatal(err)
		}
		configuredAt := base.Add(time.Hour)
		if _, err := st.db.Exec(`UPDATE app_configs SET created_at=?,updated_at=? WHERE id=?`, configuredAt.Unix(), configuredAt.Unix(), cfg.ID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		at := base.AddDate(0, 0, 6).Add(time.Hour)
		if err := st.AddRequestLog(users[i].ID, "m", "general", at, at, "success", ""); err != nil {
			t.Fatal(err)
		}
	}
	// A success on the eighth UTC bucket is outside the registration day plus
	// the following six days and must not enter the seven-day funnel step.
	late := base.AddDate(0, 0, 7).Add(time.Hour)
	if err := st.AddRequestLog(users[5].ID, "m", "general", late, late, "success", ""); err != nil {
		t.Fatal(err)
	}

	now := base.AddDate(0, 0, 10)
	stats, err := st.ActivityStats(utcDay(base), utcDay(now), now)
	if err != nil {
		t.Fatal(err)
	}
	value := func(v *int64) any {
		if v == nil {
			return nil
		}
		return *v
	}
	if stats.Activation.EligibleRegistrations == nil || *stats.Activation.EligibleRegistrations != 7 ||
		stats.Activation.Configured == nil || *stats.Activation.Configured != 6 ||
		stats.Activation.FirstSuccessWithin7D == nil || *stats.Activation.FirstSuccessWithin7D != 5 {
		t.Fatalf("activation funnel = %v/%v/%v, want 7/6/5",
			value(stats.Activation.EligibleRegistrations), value(stats.Activation.Configured), value(stats.Activation.FirstSuccessWithin7D))
	}
}
