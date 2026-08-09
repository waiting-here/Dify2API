package db

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func benchmarkExplain(b *testing.B, st *Store, name, query string, args ...any) {
	b.Helper()
	rows, err := st.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		b.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			b.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
	b.Logf("%s EXPLAIN: %s", name, strings.Join(details, " | "))
}

func BenchmarkActivityStatsMillionRows(b *testing.B) {
	dir := b.TempDir()
	st, err := Open(filepath.Join(dir, "million.db"), filepath.Join(dir, "million.key"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	today := utcDay(now)
	tx, err := st.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := tx.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2500)
		INSERT INTO users(id,discord_id,username,created_at,updated_at)
		SELECT x,'bench-'||x,'bench',?,? FROM n`, now.Unix(), now.Unix()); err != nil {
		b.Fatal(err)
	}
	if _, err := tx.Exec(`WITH RECURSIVE d(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM d WHERE x<399)
		INSERT INTO user_activity_daily(day,user_id,api_attempts,api_successes,console_actions,checkins,updated_at)
		SELECT ?-d.x*?,u.id,1,1,CASE WHEN u.id%3=0 THEN 1 ELSE 0 END,0,?
		FROM d CROSS JOIN users u WHERE u.is_admin=0`, today, activityDaySeconds, now.Unix()); err != nil {
		b.Fatal(err)
	}
	if _, err := tx.Exec(`WITH RECURSIVE d(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM d WHERE x<399)
		INSERT INTO site_activity_daily(day,new_users,product_active,successful_api_active,attempted_api_active,
		console_active,checkin_only_active,api_attempts,api_successes,wau,active_28d,engaged_28d,finalized_at)
		SELECT ?-d.x*?,0,2500,2500,2500,833,0,2500,2500,2500,2500,2500,? FROM d`, today, activityDaySeconds, now.Unix()); err != nil {
		b.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	benchmarkExplain(b, st, "site 28/400-day range",
		`SELECT day,new_users,product_active,successful_api_active,attempted_api_active,
		 console_active,checkin_only_active,api_attempts,api_successes,wau,active_28d,engaged_28d
		 FROM site_activity_daily WHERE day BETWEEN ? AND ? ORDER BY day`, today-399*activityDaySeconds, today)
	benchmarkExplain(b, st, "rolling product distinct",
		`SELECT COUNT(*) FROM (SELECT uad.user_id FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
		 WHERE uad.day BETWEEN ? AND ? AND (uad.api_successes>0 OR uad.console_actions>0)
		 AND u.is_admin=0 AND u.disabled=0 GROUP BY uad.user_id)`, today-27*activityDaySeconds, today)
	benchmarkExplain(b, st, "engaged 28-day distinct",
		`SELECT COUNT(*) FROM (SELECT uad.user_id FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
		 WHERE uad.day BETWEEN ? AND ? AND (uad.api_successes>0 OR uad.console_actions>0)
		 AND u.is_admin=0 AND u.disabled=0 GROUP BY uad.user_id HAVING COUNT(*)>=3)`, today-27*activityDaySeconds, today)
	for _, days := range []int{28, 400} {
		b.Run(strconv.Itoa(days)+"d", func(b *testing.B) {
			since := today - int64(days-1)*activityDaySeconds
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.ActivityStats(since, today, now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	consumer, err := st.CreateUser("bench-consumer", "bench", "")
	if err != nil {
		b.Fatal(err)
	}
	donation, err := st.CreateDonation(&Donation{
		Service: "general", Model: "bench", DifyBaseURL: "https://example.com",
		Deadline: time.Now().Add(time.Hour).Unix(), TotalCount: 100000000,
	}, "bench-key")
	if err != nil {
		b.Fatal(err)
	}
	settle := func() error {
		reservation, err := st.ReserveCharityCall(context.Background(), consumer.ID, donation.ID, 0, 0)
		if err != nil {
			return err
		}
		if err := st.MarkCharityDispatched(context.Background(), reservation.ID); err != nil {
			return err
		}
		_, err = st.CommitCharityReservation(context.Background(), reservation.ID)
		return err
	}
	b.Run("settlement_idle", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := settle(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("settlement_with_400d_stats", func(b *testing.B) {
		stop := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			for {
				select {
				case <-stop:
					done <- nil
					return
				default:
					if _, err := st.ActivityStats(today-399*activityDaySeconds, today, now); err != nil {
						done <- err
						return
					}
				}
			}
		}()
		for i := 0; i < b.N; i++ {
			if err := settle(); err != nil {
				close(stop)
				<-done
				b.Fatal(err)
			}
		}
		close(stop)
		if err := <-done; err != nil {
			b.Fatal(err)
		}
	})
}
