package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestV130ToV131RuntimeIndexesMigration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v130.db")
	keyPath := filepath.Join(dir, "v130.key")
	oldSchema := stripV131ActivitySchema(t, stripV131RuntimeIndexes(t, schema))
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("apply v1.3.0 schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (id,discord_id,username,created_at,updated_at) VALUES (1,'v130','kept',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO sessions (id,user_id,expires_at,created_at) VALUES ('kept-session',1,99,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO donation_applications
		(user_id,service,model,dify_base_url,dify_api_key_enc,total_count,deadline,donation_id,created_at)
		VALUES (1,'general','kept-model','https://example.com','enc',1,99,42,1)`); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC().Unix()
	if _, err := raw.Exec(`INSERT INTO request_logs
		(user_id,model,service,started_at,ended_at,status,error_code)
		VALUES (1,'kept-success','general',?,?,'success',''),
		       (1,'kept-failure','general',?,?,'error','upstream_error')`,
		startedAt, startedAt, startedAt, startedAt); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	for openNumber := 1; openNumber <= 2; openNumber++ {
		st, err := Open(dbPath, keyPath)
		if err != nil {
			t.Fatalf("Open #%d: %v", openNumber, err)
		}
		for _, name := range []string{"idx_sessions_expires", "idx_sessions_created", "idx_da_donation", "idx_uad_user_day"} {
			var found string
			if err := st.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found); err != nil || found != name {
				t.Errorf("Open #%d index %s: found=%q err=%v", openNumber, name, found, err)
			}
		}
		var username, sessionID, model string
		if err := st.db.QueryRow(`SELECT username FROM users WHERE id=1`).Scan(&username); err != nil || username != "kept" {
			t.Errorf("Open #%d user data: %q %v", openNumber, username, err)
		}
		if err := st.db.QueryRow(`SELECT id FROM sessions WHERE id='kept-session'`).Scan(&sessionID); err != nil || sessionID != "kept-session" {
			t.Errorf("Open #%d session data: %q %v", openNumber, sessionID, err)
		}
		if err := st.db.QueryRow(`SELECT model FROM donation_applications WHERE donation_id=42`).Scan(&model); err != nil || model != "kept-model" {
			t.Errorf("Open #%d application data: %q %v", openNumber, model, err)
		}
		var attempts, successes int
		if err := st.db.QueryRow(`SELECT api_attempts,api_successes FROM user_activity_daily WHERE user_id=1`).Scan(&attempts, &successes); err != nil {
			t.Errorf("Open #%d activity backfill: %v", openNumber, err)
		} else if attempts != 2 || successes != 1 {
			t.Errorf("Open #%d activity backfill=%d/%d, want 2/1", openNumber, attempts, successes)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close #%d: %v", openNumber, err)
		}
	}
}

func TestCharityReservationRecoveryAfterDatabaseReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recovery.db")
	keyPath := filepath.Join(dir, "recovery.key")
	st, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	consumer, _ := st.CreateUser("restart-consumer", "consumer", "")
	donor, _ := st.CreateUser("restart-donor", "donor", "")
	if err := st.SetUserCredits(consumer.ID, 100); err != nil {
		t.Fatal(err)
	}
	donation, err := st.CreateDonation(&Donation{
		Service: "general", Model: "restart", DifyBaseURL: "https://example.com",
		SourceUserID: sql.NullInt64{Int64: donor.ID, Valid: true},
		Deadline:     time.Now().Add(time.Hour).Unix(), TotalCount: 4,
	}, "key")
	if err != nil {
		t.Fatal(err)
	}
	reserved, _ := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 10, 3)
	dispatched, _ := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 10, 3)
	if err := st.MarkCharityDispatched(context.Background(), dispatched.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	released, committed, err := reopened.RecoverCharityReservations(context.Background())
	if err != nil || released != 1 || committed != 1 {
		t.Fatalf("restart recovery released=%d committed=%d err=%v", released, committed, err)
	}
	for id, want := range map[string]string{reserved.ID: ReservationReleased, dispatched.ID: ReservationCommitted} {
		r, err := reopened.GetCharityReservation(id)
		if err != nil || r == nil || r.Status != want {
			t.Fatalf("reservation %s=%+v err=%v want=%s", id, r, err, want)
		}
	}
	gotDonation, _ := reopened.GetDonation(donation.ID)
	gotConsumer, _ := reopened.GetUserByID(consumer.ID)
	gotDonor, _ := reopened.GetUserByID(donor.ID)
	if gotDonation.RemainingCount != 3 || gotDonation.SuccessCount != 1 || gotConsumer.Credits != 90 || gotDonor.Credits != 3 || gotDonor.DonationCredit != 1 {
		t.Fatalf("restart accounting: donation=%+v consumer=%+v donor=%+v", gotDonation, gotConsumer, gotDonor)
	}
}

func explainDetails(t *testing.T, st *Store, query string, args ...interface{}) string {
	t.Helper()
	rows, err := st.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %q: %v", query, err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, " | ")
}

func TestRuntimeHardeningExplainUsesIndexes(t *testing.T) {
	st, _ := openTemp(t)
	cases := []struct {
		name, query, want string
		args              []interface{}
	}{
		{"session idle cleanup", `DELETE FROM sessions WHERE expires_at<=?`, "idx_sessions_expires", []interface{}{time.Now().Unix()}},
		{"session absolute cleanup", `DELETE FROM sessions WHERE created_at<=?`, "idx_sessions_created", []interface{}{time.Now().Unix()}},
		{"session renewal", `UPDATE sessions SET expires_at=? WHERE id=? AND expires_at=?`, "sqlite_autoindex_sessions_1", []interface{}{3, "id", 2}},
		{"user log cutoff", `SELECT id FROM request_logs WHERE user_id=? AND started_at>=? ORDER BY started_at DESC LIMIT ?`, "idx_request_logs_user", []interface{}{1, 2, 100}},
		{"global log cutoff", `SELECT id FROM request_logs WHERE started_at>=? ORDER BY started_at DESC`, "idx_request_logs_started", []interface{}{2}},
		{"log purge", `DELETE FROM request_logs WHERE started_at<?`, "idx_request_logs_started", []interface{}{2}},
		{"application donation", `SELECT id FROM donation_applications WHERE donation_id=?`, "idx_da_donation", []interface{}{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := explainDetails(t, st, tc.query, tc.args...)
			if !strings.Contains(detail, tc.want) {
				t.Fatalf("plan %q does not use %s", detail, tc.want)
			}
		})
	}
}

func TestRequestLogReadSurfacesEnforceExactRetention(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("strict-retention", "strict", "")
	now := time.Unix(2_000_000_000, 0)
	cutoff := now.Add(-RequestLogRetention)
	for _, started := range []time.Time{cutoff.Add(-time.Second), cutoff, cutoff.Add(time.Second)} {
		if err := st.AddRequestLog(u.ID, fmt.Sprintf("m-%d", started.Unix()), "general", started, started.Add(time.Second), "success", ""); err != nil {
			t.Fatal(err)
		}
	}

	userLogs, err := st.listRequestLogsForUserAt(u.ID, 100, now)
	if err != nil || len(userLogs) != 2 || userLogs[1].StartedAt != cutoff.Unix() {
		t.Fatalf("user list boundary: logs=%+v err=%v", userLogs, err)
	}
	exported, err := st.exportRequestLogsAt(u.ID, now)
	if err != nil || len(exported) != 2 {
		t.Fatalf("user export boundary: len=%d err=%v", len(exported), err)
	}
	adminLogs, total, err := st.listAllRequestLogsAt(LogFilter{Since: cutoff.Add(-time.Hour).Unix()}, 100, 0, now)
	if err != nil || len(adminLogs) != 2 || total != 2 {
		t.Fatalf("admin list boundary: len=%d total=%d err=%v", len(adminLogs), total, err)
	}
	adminExport, err := st.exportAllRequestLogsAt(LogFilter{Since: cutoff.Add(-time.Hour).Unix()}, now)
	if err != nil || len(adminExport) != 2 {
		t.Fatalf("admin export boundary: len=%d err=%v", len(adminExport), err)
	}
	stats, err := st.logStatsByHourAt(LogFilter{Since: cutoff.Add(-time.Hour).Unix()}, now)
	if err != nil || totalStats(stats) != 2 {
		t.Fatalf("stats boundary: %+v err=%v", stats, err)
	}
	var physical int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&physical); err != nil || physical != 3 {
		t.Fatalf("physical fixture rows=%d err=%v", physical, err)
	}
}

func TestRequestLogRetentionDoesNotExtendDonationOrOrphanRows(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("donation-retention", "strict", "")
	now := time.Now()
	d, err := st.CreateDonation(&Donation{Service: "general", Model: "m", DifyBaseURL: "https://example.com", Deadline: now.Add(24 * time.Hour).Unix(), TotalCount: 10}, "key")
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-RequestLogRetention - time.Hour)
	for _, donationID := range []int64{0, d.ID, d.ID + 9999} {
		if _, err := st.AddRequestLogFull(u.ID, "old", "general", old, old.Add(time.Second), "success", "", 200, "", donationID, 0, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AddRequestLog(u.ID, "recent", "general", now.Add(-time.Hour), now, "success", ""); err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListRequestLogs(u.ID, 100)
	if err != nil || len(logs) != 1 || logs[0].Model != "recent" {
		t.Fatalf("visible logs=%+v err=%v", logs, err)
	}
}

func TestSessionRenewalIsThrottledAndConcurrentSafe(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("session-throttle", "session", "")
	token, _, err := st.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(2_100_000_000, 0)
	if _, err := st.db.Exec(`UPDATE sessions SET created_at=?, expires_at=? WHERE id=?`, base.Unix(), base.Add(SessionTTL).Unix(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TABLE session_update_count (n INTEGER NOT NULL); INSERT INTO session_update_count VALUES (0);
		CREATE TRIGGER count_session_renewal AFTER UPDATE OF expires_at ON sessions BEGIN UPDATE session_update_count SET n=n+1; END;`); err != nil {
		t.Fatal(err)
	}
	if got, err := st.getSessionUserAt(token, base.Add(SessionRenewalThreshold)); err != nil || got == nil {
		t.Fatalf("threshold lookup: user=%v err=%v", got, err)
	}
	const parallel = 32
	start := make(chan struct{})
	errs := make(chan error, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := st.getSessionUserAt(token, base.Add(SessionRenewalThreshold+time.Second))
			if err != nil {
				errs <- err
			} else if got == nil {
				errs <- fmt.Errorf("session unexpectedly invalid")
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var updates int
	if err := st.db.QueryRow(`SELECT n FROM session_update_count`).Scan(&updates); err != nil || updates != 1 {
		t.Fatalf("renewal updates=%d err=%v, want 1", updates, err)
	}
}

func TestSessionRenewalFailureDoesNotAuthenticate(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("session-renew-fail", "session", "")
	token, _, _ := st.CreateSession(u.ID)
	base := time.Unix(2_100_000_000, 0)
	if _, err := st.db.Exec(`UPDATE sessions SET created_at=?, expires_at=? WHERE id=?`, base.Unix(), base.Add(SessionTTL).Unix(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_session_renewal BEFORE UPDATE OF expires_at ON sessions BEGIN SELECT RAISE(FAIL, 'renew failed'); END;`); err != nil {
		t.Fatal(err)
	}
	if got, err := st.getSessionUserAt(token, base.Add(SessionRenewalThreshold+time.Second)); err == nil || got != nil {
		t.Fatalf("renewal failure authenticated user=%v err=%v", got, err)
	}
}

func BenchmarkRuntimeDataHardening(b *testing.B) {
	dir := b.TempDir()
	st, err := Open(filepath.Join(dir, "bench.db"), filepath.Join(dir, "bench.key"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	tx, err := st.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	userStmt, err := tx.Prepare(`INSERT INTO users (discord_id,username,created_at,updated_at) VALUES (?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 20_000; i++ {
		if _, err := userStmt.Exec(fmt.Sprintf("bench-%d", i), "bench", 1, 1); err != nil {
			b.Fatal(err)
		}
	}
	userStmt.Close()
	logStmt, err := tx.Prepare(`INSERT INTO request_logs (user_id,model,service,started_at,ended_at,status) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 100_000; i++ {
		started := now - int64(i%int(RequestLogRetention.Seconds()/2))
		if _, err := logStmt.Exec(1+i%20_000, "m", "general", started, started+1, "success"); err != nil {
			b.Fatal(err)
		}
	}
	logStmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}

	b.Run("stats-100k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := st.LogStatsByHour(LogFilter{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("export-prepare-100k", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := st.ExportAllRequestLogs(LogFilter{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cleanup-indexed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			tx, err := st.db.Begin()
			if err != nil {
				b.Fatal(err)
			}
			stmt, err := tx.Prepare(`INSERT INTO request_logs (user_id,model,service,started_at,ended_at,status) VALUES (1,'expired','general',?,?, 'error')`)
			if err != nil {
				b.Fatal(err)
			}
			for row := 0; row < 10_000; row++ {
				started := now - int64(RequestLogRetention.Seconds()) - int64(row+1)
				if _, err := stmt.Exec(started, started+1); err != nil {
					b.Fatal(err)
				}
			}
			stmt.Close()
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, _, err := st.PurgeExpiredRequestLogs(time.Now().Unix()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
