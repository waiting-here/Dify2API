package db

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// TestFishingEconomyExactRTP verifies the engine's expected value equals the
// configured RTP exactly (by construction) and the junk rate is fixed.
func TestFishingEconomyExactRTP(t *testing.T) {
	cfg := DefaultFishingConfig()
	for _, bait := range []string{BaitWorm, BaitLure, BaitPremium} {
		eng, err := cfg.FishingEngine(bait)
		if err != nil {
			t.Fatalf("%s: %v", bait, err)
		}
		var totalProb, ev float64
		for _, o := range eng.outcomes {
			totalProb += o.prob
			if o.tier == TierTreasure {
				ev += o.prob * float64(cfg.TreasureMult[o.speciesKey])
			} else {
				ev += o.prob * expectedTierMultiplier(o.tier) * eng.scale
			}
		}
		if math.Abs(totalProb-(1-fixedJunkRate)) > 1e-9 {
			t.Errorf("%s: positive probability %.6f, want %.2f", bait, totalProb, 1-fixedJunkRate)
		}
		want := eng.bait.rtp
		if math.Abs(ev-want) > 1e-6 {
			t.Errorf("%s: EV %.6f, want %.6f", bait, ev, want)
		}
		if eng.scale <= 0 {
			t.Errorf("%s: invalid scale %f", bait, eng.scale)
		}
	}
}

// TestFishingMultiplierRange checks the raw multiplier design space (0x–40x)
// and that larger fish pay at least as much as smaller ones.
func TestFishingMultiplierRange(t *testing.T) {
	for _, sp := range fishingSpeciesList {
		if sp.Tier == TierJunk {
			continue
		}
		if m := fishMultiplierRaw(sp.Tier, sp.MinCM); m < 0 {
			t.Errorf("%s min multiplier %f < 0", sp.Key, m)
		}
		if m := fishMultiplierRaw(sp.Tier, sp.MaxCM); m > maxFishMultiplier+1e-9 {
			t.Errorf("%s max multiplier %f > 40", sp.Key, m)
		}
	}
	// 米级 modifier: >=100cm pays 1.5x the unmodified value.
	if a, b := fishMultiplierRaw(TierGiant, 99), fishMultiplierRaw(TierGiant, 100); b <= a {
		t.Errorf("米级 must pay more (99cm=%f, 100cm=%f)", a, b)
	}
}

// TestFishingRollDistribution sanity-checks a large roll sample: junk rate
// near 20% and no invalid outcomes.
func TestFishingRollDistribution(t *testing.T) {
	cfg := DefaultFishingConfig()
	eng, err := cfg.FishingEngine(BaitWorm)
	if err != nil {
		t.Fatal(err)
	}
	const n = 20000
	junk, treasure, fish := 0, 0, 0
	for i := 0; i < n; i++ {
		_, tier, size, credits, isJunk, isTreasure, err := rollFishingOutcome(eng, cfg.WormPrice)
		if err != nil {
			t.Fatal(err)
		}
		if isJunk {
			junk++
			if credits != 0 || size != 0 {
				t.Fatalf("junk outcome with credits/size: %d/%d", credits, size)
			}
			continue
		}
		if isTreasure {
			treasure++
			if tier != TierTreasure {
				t.Fatalf("treasure tier %s", tier)
			}
			continue
		}
		fish++
		if size <= 0 {
			t.Fatalf("fish with zero size")
		}
		if credits <= 0 {
			t.Fatalf("fish with zero credits")
		}
	}
	junkRate := float64(junk) / n
	if junkRate < 0.15 || junkRate > 0.25 {
		t.Errorf("junk rate %.3f, want ~0.20", junkRate)
	}
	if treasure == 0 {
		t.Error("no treasure outcomes in sample")
	}
	if fish == 0 {
		t.Error("no fish outcomes in sample")
	}
}

// TestFishingRoundLifecycle covers start (ticket deduction), settle (payout,
// game_best upsert) and idempotent replay.
func TestFishingRoundLifecycle(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("fish-1", "angler", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultFishingConfig()
	if err := st.SetUserCredits(u.ID, 100); err != nil {
		t.Fatal(err)
	}

	// Start: ticket deducted, credits floored.
	round, err := st.StartFishingRound(u.ID, BaitWorm, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	afterStart, _ := st.GetUserByID(u.ID)
	if afterStart.Credits != 100-cfg.WormPrice {
		t.Fatalf("credits after start = %d, want %d", afterStart.Credits, 100-cfg.WormPrice)
	}
	// Round already carries the decided outcome.
	if round.Status != "started" || round.SpeciesKey == "" {
		t.Fatalf("round state wrong: %+v", round)
	}

	// Settle: payout applied exactly once.
	res, newly, err := st.SettleFishingRound(u.ID, round.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !newly || res.Settled {
		t.Fatalf("first settle must be new: %+v", res)
	}
	afterSettle, _ := st.GetUserByID(u.ID)
	want := 100 - cfg.WormPrice + round.CreditsWon
	if afterSettle.Credits != want {
		t.Fatalf("credits after settle = %d, want %d", afterSettle.Credits, want)
	}

	// Idempotent replay: same result, no balance change.
	res2, newly2, err := st.SettleFishingRound(u.ID, round.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if newly2 || !res2.Settled {
		t.Fatalf("replay must be idempotent: %+v", res2)
	}
	if res2.CreditsWon != res.CreditsWon || res2.SpeciesKey != res.SpeciesKey {
		t.Fatalf("replay result mismatch: %+v vs %+v", res2, res)
	}
	afterReplay, _ := st.GetUserByID(u.ID)
	if afterReplay.Credits != want {
		t.Fatalf("replay changed balance: %d", afterReplay.Credits)
	}

	// game_best updated for real fish.
	if !res.IsJunk && !res.IsTreasure {
		best, err := st.ListGameBest(u.ID)
		if err != nil || len(best) != 1 || best[0].SizeCM != res.SizeCM {
			t.Fatalf("game_best wrong: %+v err=%v", best, err)
		}
	}

	// Unknown round for this user.
	if _, _, err := st.SettleFishingRound(u.ID, "nope", time.Now()); err != ErrRoundNotFound {
		t.Fatalf("want ErrRoundNotFound, got %v", err)
	}
}

func TestFishingStoreEnforcesLiveSwitches(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("fish-switch", "switch", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserCredits(u.ID, 100); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultFishingConfig()
	if err := st.SetSetting(SettingGamesEnabled, "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartFishingRound(u.ID, BaitWorm, cfg, time.Now()); err != ErrGamesDisabled {
		t.Fatalf("master switch: got %v", err)
	}
	if err := st.SetSetting(SettingGamesEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if _, err := st.StartFishingRound(u.ID, BaitWorm, cfg, time.Now()); err != ErrGameDisabled {
		t.Fatalf("game switch: got %v", err)
	}
	got, _ := st.GetUserByID(u.ID)
	if got.Credits != 100 {
		t.Fatalf("disabled starts changed credits to %d", got.Credits)
	}
}

// TestFishingCreditsFloor verifies the 0-floor: starting below the ticket
// price is refused and never drives credits negative.
func TestFishingCreditsFloor(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("fish-2", "poor", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultFishingConfig()
	if _, err := st.StartFishingRound(u.ID, BaitWorm, cfg, time.Now()); err != ErrInsufficientCredits {
		t.Fatalf("want ErrInsufficientCredits, got %v", err)
	}
	// Exactly at the floor: 0 credits cannot buy even the cheapest bait.
	if err := st.SetUserCredits(u.ID, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartFishingRound(u.ID, BaitWorm, cfg, time.Now()); err != ErrInsufficientCredits {
		t.Fatalf("want ErrInsufficientCredits at 4 credits, got %v", err)
	}
}

// TestFishingGameBestUpsert keeps the all-time best fish.
func TestFishingGameBestUpsert(t *testing.T) {
	st, _ := openTemp(t)
	u, err := st.CreateUser("fish-3", "hunter", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Simulate two catches: 30cm then 120cm.
	if _, err := st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at, settled_at)
		VALUES ('r1','fishing',?,'worm',5,'settled','crucian',30,2,?,?)`, u.ID, now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at, settled_at)
		VALUES ('r2','fishing',?,'worm',5,'settled','grass_carp',120,50,?,?)`, u.ID, now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	// Settle path upserts best via game_best write on settlement; simulate
	// the two settlements directly through SettleFishingRound using started
	// rounds instead.
	if _, err := st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('r3','fishing',?,'worm',5,'started','koi',150,60,?)`, u.ID, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SettleFishingRound(u.ID, "r3", now); err != nil {
		t.Fatal(err)
	}
	best, err := st.ListGameBest(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(best) != 1 || best[0].SizeCM != 150 || best[0].SpeciesKey != "koi" {
		t.Fatalf("best catch wrong: %+v", best)
	}
	// A smaller catch must not replace the best.
	if _, err := st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('r4','fishing',?,'worm',5,'started','crucian',40,3,?)`, u.ID, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SettleFishingRound(u.ID, "r4", now); err != nil {
		t.Fatal(err)
	}
	best, _ = st.ListGameBest(u.ID)
	if best[0].SizeCM != 150 {
		t.Fatalf("best catch was replaced: %+v", best)
	}
}

// TestFishingLeaderboards checks single/total boards, ranks and anonymity.
func TestFishingLeaderboards(t *testing.T) {
	st, _ := openTemp(t)
	alice, _ := st.CreateUser("fish-a", "alice", "")
	bob, _ := st.CreateUser("fish-b", "bob", "")
	if err := st.SetUserCredits(alice.ID, 1000); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserCredits(bob.ID, 1000); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Force deterministic distinct best sizes via directly inserted started
	// rounds (no random outcomes) so the board assertions are stable.
	st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('lb1','fishing',?,'worm',5,'started','common_carp',75,15,?)`, alice.ID, now.Unix())
	st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('lb2','fishing',?,'worm',5,'started','crucian',30,3,?)`, bob.ID, now.Unix())
	st.SettleFishingRound(alice.ID, "lb1", now)
	st.SettleFishingRound(bob.ID, "lb2", now)

	single, err := st.FishingLeaderboard(GameIDFishing, "single", alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(single.Entries) != 2 || single.Entries[0].SizeCM != 75 || single.Entries[0].Username != "alice" {
		t.Fatalf("single board wrong: %+v", single.Entries)
	}
	if single.Me == nil || single.Me.Rank != 1 {
		t.Fatalf("alice standing wrong: %+v", single.Me)
	}

	// Anonymity hides the username and marks the entry.
	if err := st.SetLeaderboardAnon(alice.ID, true); err != nil {
		t.Fatal(err)
	}
	single, _ = st.FishingLeaderboard(GameIDFishing, "single", alice.ID, now)
	if !single.Entries[0].Anonymous || single.Entries[0].Username != "" {
		t.Fatalf("anon not applied: %+v", single.Entries[0])
	}

	total, err := st.FishingLeaderboard(GameIDFishing, "total", bob.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(total.Entries) != 2 || total.Me == nil || total.Me.Total <= 0 {
		t.Fatalf("total board wrong: %+v", total)
	}
}

// TestGameRoundsRetention purges rounds past the 30-day window.
func TestGameRoundsRetention(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("fish-r", "old", "")
	old := time.Now().Add(-40 * 24 * time.Hour).Unix()
	if _, err := st.db.Exec(`INSERT INTO game_rounds (id, game_id, user_id, bait_tier, price, status, species_key, created_at)
		VALUES ('old1','fishing',?,'worm',5,'settled','crucian',?)`, u.ID, old); err != nil {
		t.Fatal(err)
	}
	n, err := st.PurgeExpiredGameRounds(time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
}

// TestFishingActivityIntegration verifies game rounds count into product
// activity and game_active. k-anonymity suppresses counts below 5, so the
// test seeds exactly 5 active users to observe exact values.
func TestFishingActivityIntegration(t *testing.T) {
	st, _ := openTemp(t)
	now := time.Now()
	cfg := DefaultFishingConfig()
	for i := 0; i < 5; i++ {
		u, err := st.CreateUser(fmt.Sprintf("fish-act-%d", i), "active", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetUserCredits(u.ID, 100); err != nil {
			t.Fatal(err)
		}
		r, err := st.StartFishingRound(u.ID, BaitWorm, cfg, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.SettleFishingRound(u.ID, r.ID, now); err != nil {
			t.Fatal(err)
		}
	}

	acts, err := st.ListUserActivity(1)
	if err != nil || len(acts) != 1 || acts[0].GameRounds != 1 {
		t.Fatalf("user activity wrong: %+v err=%v", acts, err)
	}
	day := utcDay(now)
	stats, err := st.ActivityStats(day, day, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.ByDay) != 1 {
		t.Fatalf("by day: %+v", stats.ByDay)
	}
	d := stats.ByDay[0]
	if d.ProductActive == nil || *d.ProductActive != 5 {
		t.Errorf("product_active=%v, want 5 (game rounds count into product)", d.ProductActive)
	}
	if d.GameActive == nil || *d.GameActive != 5 {
		t.Errorf("game_active=%v, want 5", d.GameActive)
	}
	if d.ConsoleActive != nil && *d.ConsoleActive != 0 {
		t.Errorf("console_active=%v, want 0", d.ConsoleActive)
	}
	// Summary: DAU comes from product_active which now includes the game.
	if stats.Summary.DAU == nil || *stats.Summary.DAU != 5 {
		t.Errorf("DAU=%v, want 5", stats.Summary.DAU)
	}
}
