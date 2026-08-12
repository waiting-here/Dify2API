package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

const (
	ActivityRetentionDays = 400
	activityK             = int64(5)
	activityDaySeconds    = int64(24 * time.Hour / time.Second)
	activityBackfillKey   = "internal_activity_backfill_v1"
	activityLaunchKey     = "internal_activity_launch_at"
)

type activityDelta struct {
	attempts int64
	success  int64
	console  int64
	checkins int64
	games    int64
}

// UserActivityDaily is the portable, user-associated daily aggregate.
type UserActivityDaily struct {
	Day            int64 `json:"day"`
	APIAttempts    int64 `json:"api_attempts"`
	APISuccesses   int64 `json:"api_successes"`
	ConsoleActions int64 `json:"console_actions"`
	Checkins       int64 `json:"checkins"`
	GameRounds     int64 `json:"game_rounds"`
	UpdatedAt      int64 `json:"updated_at"`
}

// ActivityDayStats contains only site-wide aggregates. Pointer fields encode
// k-anonymity suppression as JSON null.
type ActivityDayStats struct {
	Day                 int64  `json:"day"`
	NewUsers            *int64 `json:"new_users"`
	ProductActive       *int64 `json:"product_active"`
	SuccessfulAPIActive *int64 `json:"successful_api_active"`
	AttemptedAPIActive  *int64 `json:"attempted_api_active"`
	ConsoleActive       *int64 `json:"console_active"`
	GameActive          *int64 `json:"game_active"`
	APIAttempts         *int64 `json:"api_attempts"`
	APISuccesses        *int64 `json:"api_successes"`
	WAU                 *int64 `json:"-"`
	Active28D           *int64 `json:"-"`
	Engaged28D          *int64 `json:"-"`
	CheckinOnlyActive   *int64 `json:"-"`
}

type ActivitySummary struct {
	RegisteredTotal *int64 `json:"registered_total"`
	DAU             *int64 `json:"dau"`
	WAU             *int64 `json:"wau"`
	Active28D       *int64 `json:"active_28d"`
	Engaged28D      *int64 `json:"engaged_28d"`
	Attempted28D    *int64 `json:"attempted_28d"`
	Console28D      *int64 `json:"console_28d"`
	CheckinOnly28D  *int64 `json:"checkin_only_28d"`
}

type ActivityActivation struct {
	EligibleRegistrations *int64 `json:"eligible_registrations"`
	Configured            *int64 `json:"configured"`
	FirstSuccessWithin7D  *int64 `json:"first_success_within_7d"`
}

type ActivityStats struct {
	Timezone   string             `json:"timezone"`
	Summary    ActivitySummary    `json:"summary"`
	ByDay      []ActivityDayStats `json:"by_day"`
	Activation ActivityActivation `json:"activation"`
}

func utcDay(t time.Time) int64 {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC).Unix()
}

func initializeActivitySetting(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES (?,?)`, key, value)
	return err
}

func (s *Store) initializeActivity(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := initializeActivitySetting(tx, activityLaunchKey, strconv.FormatInt(now.Unix(), 10)); err != nil {
		return err
	}
	var backfilled string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key=?`, activityBackfillKey).Scan(&backfilled)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		// Exact replacement of API counters makes recovery safe even if a
		// pre-release build created partial aggregate rows before this marker.
		if _, err := tx.Exec(`UPDATE user_activity_daily
			SET api_attempts=0, api_successes=0, console_actions=0, checkins=0, game_rounds=0, updated_at=?`, now.Unix()); err != nil {
			return fmt.Errorf("reset backfilled activity: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO user_activity_daily
				(day, user_id, api_attempts, api_successes, console_actions, checkins, updated_at)
			SELECT (rl.started_at / ?) * ?, rl.user_id, COUNT(*),
				SUM(CASE WHEN rl.status='success' AND rl.error_code<>'debug_dry_run' THEN 1 ELSE 0 END), 0, 0, ?
			FROM request_logs rl
			JOIN users u ON u.id=rl.user_id AND u.is_admin=0
			GROUP BY (rl.started_at / ?), rl.user_id
			ON CONFLICT(day, user_id) DO UPDATE SET
				api_attempts=excluded.api_attempts,
				api_successes=excluded.api_successes,
				updated_at=excluded.updated_at`,
			activityDaySeconds, activityDaySeconds, now.Unix(), activityDaySeconds); err != nil {
			return fmt.Errorf("backfill user activity: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM user_activity_daily
			WHERE api_attempts=0 AND api_successes=0 AND console_actions=0 AND checkins=0 AND game_rounds=0`); err != nil {
			return fmt.Errorf("remove empty backfilled activity: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO site_activity_daily
				(day, new_users, product_active, successful_api_active, attempted_api_active,
				 console_active, checkin_only_active, api_attempts, api_successes, wau,
				 active_28d, engaged_28d, finalized_at)
			SELECT day,0,0,0,0,0,0,0,0,0,0,0,0 FROM (
				SELECT day FROM user_activity_daily
				UNION
				SELECT (created_at / ?) * ? FROM users WHERE is_admin=0
			)`, activityDaySeconds, activityDaySeconds); err != nil {
			return fmt.Errorf("seed site activity: %w", err)
		}
		if err := initializeActivitySetting(tx, activityBackfillKey, "1"); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.MaintainActivity(now)
}

func (s *Store) activityLaunchAt() (int64, error) {
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, activityLaunchKey).Scan(&raw); err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func recordActivityTx(tx *sql.Tx, userID int64, at time.Time, d activityDelta) error {
	var isAdmin, disabled int
	if err := tx.QueryRow(`SELECT is_admin, disabled FROM users WHERE id=?`, userID).Scan(&isAdmin, &disabled); err != nil {
		if err == sql.ErrNoRows {
			// request_logs intentionally permit orphan rows so old/manual data
			// remains visible for its own retention period. There is no user
			// aggregate to attach in that compatibility case.
			return nil
		}
		return err
	}
	if isAdmin != 0 {
		return nil
	}
	day := utcDay(at)
	var oldAttempts, oldSuccess, oldConsole, oldCheckins, oldGames int64
	err := tx.QueryRow(`SELECT api_attempts, api_successes, console_actions, checkins, game_rounds
		FROM user_activity_daily WHERE day=? AND user_id=?`, day, userID).
		Scan(&oldAttempts, &oldSuccess, &oldConsole, &oldCheckins, &oldGames)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO user_activity_daily
			(day,user_id,api_attempts,api_successes,console_actions,checkins,game_rounds,updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(day,user_id) DO UPDATE SET
			api_attempts=api_attempts+excluded.api_attempts,
			api_successes=api_successes+excluded.api_successes,
			console_actions=console_actions+excluded.console_actions,
			checkins=checkins+excluded.checkins,
			game_rounds=game_rounds+excluded.game_rounds,
			updated_at=excluded.updated_at`,
		day, userID, d.attempts, d.success, d.console, d.checkins, d.games, at.Unix()); err != nil {
		return err
	}
	if disabled != 0 {
		return nil
	}
	if err := ensureOpenSiteDayTx(tx, day); err != nil {
		return err
	}
	oldProduct := oldSuccess > 0 || oldConsole > 0 || oldGames > 0
	newProduct := oldSuccess+d.success > 0 || oldConsole+d.console > 0 || oldGames+d.games > 0
	oldCheckinOnly := oldCheckins > 0 && !oldProduct
	newCheckinOnly := oldCheckins+d.checkins > 0 && !newProduct
	_, err = tx.Exec(`UPDATE site_activity_daily SET
		attempted_api_active=COALESCE(attempted_api_active,0)+?,
		successful_api_active=COALESCE(successful_api_active,0)+?,
		console_active=COALESCE(console_active,0)+?,
		game_active=COALESCE(game_active,0)+?,
		product_active=COALESCE(product_active,0)+?,
		checkin_only_active=COALESCE(checkin_only_active,0)+?,
		api_attempts=COALESCE(api_attempts,0)+?,
		api_successes=COALESCE(api_successes,0)+?
		WHERE day=? AND finalized_at=0`,
		boolDelta(oldAttempts > 0, oldAttempts+d.attempts > 0),
		boolDelta(oldSuccess > 0, oldSuccess+d.success > 0),
		boolDelta(oldConsole > 0, oldConsole+d.console > 0),
		boolDelta(oldGames > 0, oldGames+d.games > 0),
		boolDelta(oldProduct, newProduct), boolDelta(oldCheckinOnly, newCheckinOnly),
		d.attempts, d.success, day)
	return err
}

func boolDelta(old, next bool) int64 {
	switch {
	case !old && next:
		return 1
	case old && !next:
		return -1
	default:
		return 0
	}
}

func ensureOpenSiteDayTx(tx *sql.Tx, day int64) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO site_activity_daily
		(day,new_users,product_active,successful_api_active,attempted_api_active,
		 console_active,checkin_only_active,api_attempts,api_successes,game_active,wau,active_28d,engaged_28d,finalized_at)
		VALUES (?,0,0,0,0,0,0,0,0,0,0,0,0,0)`, day)
	return err
}

func recordNewUserTx(tx *sql.Tx, createdAt time.Time) error {
	day := utcDay(createdAt)
	if err := ensureOpenSiteDayTx(tx, day); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE site_activity_daily
		SET new_users=COALESCE(new_users,0)+1 WHERE day=? AND finalized_at=0`, day)
	return err
}

// RecordConsoleAction records one successful self-service business write.
func (s *Store) RecordConsoleAction(userID int64, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordActivityTx(tx, userID, at, activityDelta{console: 1}); err != nil {
		return err
	}
	return tx.Commit()
}

func adjustOpenSiteForUserTx(tx *sql.Tx, userID, day, direction int64, includeRegistration bool) error {
	var a, s, c, ch, g int64
	err := tx.QueryRow(`SELECT api_attempts,api_successes,console_actions,checkins,game_rounds
		FROM user_activity_daily WHERE day=? AND user_id=?`, day, userID).Scan(&a, &s, &c, &ch, &g)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err := ensureOpenSiteDayTx(tx, day); err != nil {
		return err
	}
	newUser := int64(0)
	if includeRegistration {
		newUser = direction
	}
	product := s > 0 || c > 0 || g > 0
	checkinOnly := ch > 0 && !product
	_, err = tx.Exec(`UPDATE site_activity_daily SET
		new_users=MAX(0,COALESCE(new_users,0)+?),
		product_active=MAX(0,COALESCE(product_active,0)+?),
		successful_api_active=MAX(0,COALESCE(successful_api_active,0)+?),
		attempted_api_active=MAX(0,COALESCE(attempted_api_active,0)+?),
		console_active=MAX(0,COALESCE(console_active,0)+?),
		game_active=MAX(0,COALESCE(game_active,0)+?),
		checkin_only_active=MAX(0,COALESCE(checkin_only_active,0)+?),
		api_attempts=MAX(0,COALESCE(api_attempts,0)+?),
		api_successes=MAX(0,COALESCE(api_successes,0)+?)
		WHERE day=? AND finalized_at=0`, newUser,
		direction*boolInt(product), direction*boolInt(s > 0), direction*boolInt(a > 0),
		direction*boolInt(c > 0), direction*boolInt(g > 0), direction*boolInt(checkinOnly),
		direction*a, direction*s, day)
	return err
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func suppress(v int64) *int64 {
	if v < activityK {
		return nil
	}
	x := v
	return &x
}

func exact(v int64) *int64 {
	x := v
	return &x
}

func nullableValue(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func (s *Store) rebuildSiteDayTx(tx *sql.Tx, day, finalizedAt int64, doSuppress bool) error {
	var newUsers, product, successful, attempted, console, gameActive, checkinOnly, attempts, successes int64
	if err := tx.QueryRow(`SELECT
		(SELECT COUNT(*) FROM users WHERE is_admin=0 AND disabled=0 AND created_at>=? AND created_at<?),
		COALESCE(SUM(CASE WHEN uad.api_successes>0 OR uad.console_actions>0 OR uad.game_rounds>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN uad.api_successes>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN uad.api_attempts>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN uad.console_actions>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN uad.game_rounds>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN uad.checkins>0 AND uad.api_successes=0 AND uad.console_actions=0 AND uad.game_rounds=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(uad.api_attempts),0), COALESCE(SUM(uad.api_successes),0)
		FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
		WHERE uad.day=? AND u.is_admin=0 AND u.disabled=0`,
		day, day+activityDaySeconds, day).Scan(&newUsers, &product, &successful, &attempted, &console, &gameActive, &checkinOnly, &attempts, &successes); err != nil {
		return err
	}
	wau, err := rollingProductUsersTx(tx, day-6*activityDaySeconds, day)
	if err != nil {
		return err
	}
	active28, err := rollingProductUsersTx(tx, day-27*activityDaySeconds, day)
	if err != nil {
		return err
	}
	var engaged int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM (
		SELECT uad.user_id FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
		WHERE uad.day BETWEEN ? AND ? AND (uad.api_successes>0 OR uad.console_actions>0 OR uad.game_rounds>0)
		AND u.is_admin=0 AND u.disabled=0 GROUP BY uad.user_id HAVING COUNT(*)>=3
	)`, day-27*activityDaySeconds, day).Scan(&engaged); err != nil {
		return err
	}
	values := []any{exact(newUsers), exact(product), exact(successful), exact(attempted), exact(console), exact(checkinOnly), exact(attempts), exact(successes), exact(gameActive), exact(wau), exact(active28), exact(engaged)}
	if doSuppress {
		values = []any{suppress(newUsers), suppress(product), suppress(successful), suppress(attempted), suppress(console), suppress(checkinOnly), nil, nil, suppress(gameActive), suppress(wau), suppress(active28), suppress(engaged)}
		if attempted >= activityK {
			values[6] = exact(attempts)
		}
		if successful >= activityK {
			values[7] = exact(successes)
		}
	}
	if err := ensureOpenSiteDayTx(tx, day); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE site_activity_daily SET new_users=?,product_active=?,successful_api_active=?,
		attempted_api_active=?,console_active=?,checkin_only_active=?,api_attempts=?,api_successes=?,
		game_active=?,wau=?,active_28d=?,engaged_28d=?,finalized_at=? WHERE day=?`,
		values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], values[10], values[11], finalizedAt, day)
	return err
}

func rollingProductUsersTx(tx *sql.Tx, since, until int64) (int64, error) {
	var n int64
	err := tx.QueryRow(`SELECT COUNT(*) FROM (
		SELECT uad.user_id FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
		WHERE uad.day BETWEEN ? AND ? AND (uad.api_successes>0 OR uad.console_actions>0 OR uad.game_rounds>0)
		AND u.is_admin=0 AND u.disabled=0 GROUP BY uad.user_id
	)`, since, until).Scan(&n)
	return n, err
}

// finalizeCompletedActivityDaysTx freezes every still-pending UTC day before
// today. Callers that are about to change or delete a user's inclusion state
// must invoke this in the same transaction, while the old users row is still
// visible, so completed days cannot be rebuilt from the new state.
func (s *Store) finalizeCompletedActivityDaysTx(tx *sql.Tx, now time.Time) error {
	today := utcDay(now)
	rows, err := tx.Query(`SELECT day FROM site_activity_daily WHERE finalized_at=0 AND day<? ORDER BY day`, today)
	if err != nil {
		return err
	}
	var days []int64
	for rows.Next() {
		var day int64
		if err := rows.Scan(&day); err != nil {
			rows.Close()
			return err
		}
		days = append(days, day)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, day := range days {
		if err := s.rebuildSiteDayTx(tx, day, now.Unix(), true); err != nil {
			return fmt.Errorf("finalize activity day %d: %w", day, err)
		}
	}
	return nil
}

// MaintainActivity finalizes every completed UTC day and enforces both
// activity tables' 400-day rolling retention.
func (s *Store) MaintainActivity(now time.Time) error {
	today := utcDay(now)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.finalizeCompletedActivityDaysTx(tx, now); err != nil {
		return err
	}
	// Rebuild today's mutable row so startup migration and account-state
	// changes are reflected before normal event-driven increments resume.
	if err := s.rebuildSiteDayTx(tx, today, 0, false); err != nil {
		return fmt.Errorf("rebuild current activity: %w", err)
	}
	cutoff := today - (ActivityRetentionDays-1)*activityDaySeconds
	if _, err := tx.Exec(`DELETE FROM user_activity_daily WHERE day<?`, cutoff); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM site_activity_daily WHERE day<?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListUserActivity(userID int64) ([]UserActivityDaily, error) {
	rows, err := s.db.Query(`SELECT day,api_attempts,api_successes,console_actions,checkins,game_rounds,updated_at
		FROM user_activity_daily WHERE user_id=? ORDER BY day`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserActivityDaily, 0)
	for rows.Next() {
		var v UserActivityDaily
		if err := rows.Scan(&v.Day, &v.APIAttempts, &v.APISuccesses, &v.ConsoleActions, &v.Checkins, &v.GameRounds, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanActivityDay(row interface{ Scan(...any) error }) (ActivityDayStats, error) {
	var out ActivityDayStats
	var newUsers, product, successful, attempted, console, gameActive, checkinOnly, attempts, successes, wau, active28, engaged sql.NullInt64
	err := row.Scan(&out.Day, &newUsers, &product, &successful, &attempted, &console, &checkinOnly, &attempts, &successes, &gameActive, &wau, &active28, &engaged)
	out.NewUsers, out.ProductActive = nullableValue(newUsers), nullableValue(product)
	out.SuccessfulAPIActive, out.AttemptedAPIActive = nullableValue(successful), nullableValue(attempted)
	out.ConsoleActive, out.GameActive = nullableValue(console), nullableValue(gameActive)
	out.CheckinOnlyActive = nullableValue(checkinOnly)
	out.APIAttempts, out.APISuccesses = nullableValue(attempts), nullableValue(successes)
	out.WAU, out.Active28D, out.Engaged28D = nullableValue(wau), nullableValue(active28), nullableValue(engaged)
	return out, err
}

// ActivityStats returns only site-level aggregates for the closed UTC range.
func (s *Store) ActivityStats(since, until int64, now time.Time) (*ActivityStats, error) {
	if err := s.MaintainActivity(now); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT day,new_users,product_active,successful_api_active,attempted_api_active,
		console_active,checkin_only_active,api_attempts,api_successes,game_active,wau,active_28d,engaged_28d
		FROM site_activity_daily WHERE day BETWEEN ? AND ? ORDER BY day`, since, until)
	if err != nil {
		return nil, err
	}
	stored := make(map[int64]ActivityDayStats)
	for rows.Next() {
		v, scanErr := scanActivityDay(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		if until >= utcDay(now) && v.Day == utcDay(now) {
			v.NewUsers = suppressValue(v.NewUsers)
			v.ProductActive = suppressValue(v.ProductActive)
			v.SuccessfulAPIActive = suppressValue(v.SuccessfulAPIActive)
			v.AttemptedAPIActive = suppressValue(v.AttemptedAPIActive)
			v.ConsoleActive = suppressValue(v.ConsoleActive)
			v.GameActive = suppressValue(v.GameActive)
			v.CheckinOnlyActive = suppressValue(v.CheckinOnlyActive)
			if v.AttemptedAPIActive == nil {
				v.APIAttempts = nil
			}
			if v.SuccessfulAPIActive == nil {
				v.APISuccesses = nil
			}
			v.WAU = suppressValue(v.WAU)
			v.Active28D = suppressValue(v.Active28D)
			v.Engaged28D = suppressValue(v.Engaged28D)
		}
		stored[v.Day] = v
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	stats := &ActivityStats{Timezone: "UTC", ByDay: make([]ActivityDayStats, 0, int((until-since)/activityDaySeconds)+1)}
	for day := since; day <= until; day += activityDaySeconds {
		v, ok := stored[day]
		if !ok {
			v = ActivityDayStats{Day: day}
		}
		stats.ByDay = append(stats.ByDay, v)
	}
	last := stored[until]
	stats.Summary.DAU, stats.Summary.WAU = last.ProductActive, last.WAU
	stats.Summary.Active28D, stats.Summary.Engaged28D = last.Active28D, last.Engaged28D
	var registered int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin=0 AND disabled=0`).Scan(&registered); err != nil {
		return nil, err
	}
	stats.Summary.RegisteredTotal = suppress(registered)
	windowSince := until - 27*activityDaySeconds
	var attempted28, console28, checkinOnly28 int64
	if err := s.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN attempts>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN consoles>0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN checkins>0 AND successes=0 AND consoles=0 AND games=0 THEN 1 ELSE 0 END),0)
		FROM (
			SELECT uad.user_id,SUM(api_attempts) attempts,SUM(api_successes) successes,
				SUM(console_actions) consoles,SUM(checkins) checkins,SUM(game_rounds) games
			FROM user_activity_daily uad JOIN users u ON u.id=uad.user_id
			WHERE uad.day BETWEEN ? AND ? AND u.is_admin=0 AND u.disabled=0 GROUP BY uad.user_id
		)`, windowSince, until).Scan(&attempted28, &console28, &checkinOnly28); err != nil {
		return nil, err
	}
	stats.Summary.Attempted28D, stats.Summary.Console28D, stats.Summary.CheckinOnly28D = suppress(attempted28), suppress(console28), suppress(checkinOnly28)
	launchAt, err := s.activityLaunchAt()
	if err != nil {
		return nil, err
	}
	cohortStart := since
	if launchDay := utcDay(time.Unix(launchAt, 0)); cohortStart < launchDay {
		cohortStart = launchDay
	}
	cohortEnd := until + activityDaySeconds
	var eligible, configured, activated int64
	if cohortStart <= until {
		if err := s.db.QueryRow(`SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN EXISTS(SELECT 1 FROM app_configs ac
				WHERE ac.user_id=u.id AND ac.created_at<?) THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN EXISTS(SELECT 1 FROM app_configs ac
				WHERE ac.user_id=u.id AND ac.created_at<? AND EXISTS(
					SELECT 1 FROM user_activity_daily uad
					WHERE uad.user_id=u.id AND uad.api_successes>0
					AND uad.day>=(u.created_at/?)*?
					AND uad.day>=(ac.created_at/?)*?
					AND uad.day<=(u.created_at/?)*?+6*?
					AND uad.day<?)) THEN 1 ELSE 0 END),0)
			FROM users u WHERE u.is_admin=0 AND u.disabled=0 AND u.created_at>=? AND u.created_at<? AND u.created_at>=?`,
			cohortEnd, cohortEnd,
			activityDaySeconds, activityDaySeconds,
			activityDaySeconds, activityDaySeconds,
			activityDaySeconds, activityDaySeconds, activityDaySeconds, cohortEnd,
			cohortStart, cohortEnd, launchAt).
			Scan(&eligible, &configured, &activated); err != nil {
			return nil, err
		}
	}
	stats.Activation.EligibleRegistrations = suppress(eligible)
	stats.Activation.Configured = suppress(configured)
	stats.Activation.FirstSuccessWithin7D = suppress(activated)
	return stats, nil
}

func suppressValue(v *int64) *int64 {
	if v == nil || *v < activityK {
		return nil
	}
	return v
}
