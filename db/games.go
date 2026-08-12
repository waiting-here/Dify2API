package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// Mini-game constants. game_id 'fishing' ships in v1.4.0; all tables and
// settings are keyed by game_id so future games need no schema changes.
const (
	GameIDFishing = "fishing"
	// GameRoundsRetention is how long game round rows are kept (rolling 30
	// days, mirroring request-log retention).
	GameRoundsRetention = RequestLogRetention
	// fixedJunkRate is the probability of a zero-payout junk outcome. The
	// fish/treasure probabilities fill the remaining 80%; the per-bait
	// multiplier scale factor is solved so the exact RTP holds.
	fixedJunkRate = 0.20
	// maxFishMultiplier caps the raw (unscaled) fish multiplier at 40x per
	// the frozen design ("倍率 0x–40x").
	maxFishMultiplier = 40.0
)

// Bait tier keys.
const (
	BaitWorm    = "worm"    // 蚯蚓
	BaitLure    = "lure"    // 商品饵
	BaitPremium = "premium" // 拟饵
)

// Errors returned by the games store layer.
var (
	ErrGamesDisabled   = errors.New("games are disabled")
	ErrGameDisabled    = errors.New("game is disabled")
	ErrRoundNotFound   = errors.New("round not found")
	ErrBaitUnknown     = errors.New("unknown bait tier")
	ErrAdminCannotPlay = errors.New("administrators cannot play games")
)

// FishingTier is a catch tier key used by the engine and the UI i18n.
const (
	TierJunk     = "junk"
	TierSmall    = "small"    // 杂鱼
	TierRegular  = "regular"  // 普通
	TierBig      = "big"      // 大物
	TierGiant    = "giant"    // 巨物
	TierLegend   = "legend"   // 传说
	TierTreasure = "treasure" // 宝物
)

// FishingSpecies describes one catchable item (fish, junk or treasure).
type FishingSpecies struct {
	Key   string // stable ascii key (also the i18n key stem)
	Tier  string
	MinCM int // fish size range (cm); 0 for junk/treasure
	MaxCM int
}

// fishingSpeciesList is the frozen species roster: 23 fish + 8 junk items +
// 3 treasures (requirements §4, 2026-08-11 定稿).
var fishingSpeciesList = []FishingSpecies{
	// 杂鱼 (5–25cm)
	{Key: "whitebait", Tier: TierSmall, MinCM: 5, MaxCM: 25},   // 白条
	{Key: "gudgeon", Tier: TierSmall, MinCM: 5, MaxCM: 25},     // 麦穗鱼
	{Key: "horse_mouth", Tier: TierSmall, MinCM: 5, MaxCM: 25}, // 马口鱼
	{Key: "smelt", Tier: TierSmall, MinCM: 5, MaxCM: 25},       // 公鱼
	{Key: "loach", Tier: TierSmall, MinCM: 5, MaxCM: 25},       // 泥鳅
	// 普通 (15–35cm)
	{Key: "crucian", Tier: TierRegular, MinCM: 15, MaxCM: 35},        // 鲫鱼
	{Key: "tilapia", Tier: TierRegular, MinCM: 15, MaxCM: 35},        // 罗非鱼
	{Key: "yellow_catfish", Tier: TierRegular, MinCM: 15, MaxCM: 35}, // 黄颡鱼
	{Key: "ayu", Tier: TierRegular, MinCM: 15, MaxCM: 35},            // 香鱼
	{Key: "stream_carp", Tier: TierRegular, MinCM: 15, MaxCM: 35},    // 追河鱼
	// 大物 (30–80cm)
	{Key: "common_carp", Tier: TierBig, MinCM: 30, MaxCM: 80},   // 鲤鱼
	{Key: "snakehead", Tier: TierBig, MinCM: 30, MaxCM: 80},     // 黑鱼
	{Key: "catfish", Tier: TierBig, MinCM: 30, MaxCM: 80},       // 鲶鱼
	{Key: "mandarin_fish", Tier: TierBig, MinCM: 30, MaxCM: 80}, // 鳜鱼
	{Key: "rainbow_trout", Tier: TierBig, MinCM: 30, MaxCM: 80}, // 虹鳟
	// 巨物 (60–150cm)
	{Key: "grass_carp", Tier: TierGiant, MinCM: 60, MaxCM: 150},   // 草鱼
	{Key: "silver_carp", Tier: TierGiant, MinCM: 60, MaxCM: 150},  // 鲢鱼
	{Key: "bighead_carp", Tier: TierGiant, MinCM: 60, MaxCM: 150}, // 鳙鱼
	{Key: "black_carp", Tier: TierGiant, MinCM: 60, MaxCM: 150},   // 青鱼
	{Key: "japanese_eel", Tier: TierGiant, MinCM: 60, MaxCM: 150}, // 日本鳗鲡
	// 传说 (100–200cm, all 米级)
	{Key: "yellowcheek", Tier: TierLegend, MinCM: 100, MaxCM: 200}, // 鳡鱼
	{Key: "taimen", Tier: TierLegend, MinCM: 100, MaxCM: 200},      // 哲罗鲑
	{Key: "koi", Tier: TierLegend, MinCM: 100, MaxCM: 200},         // 锦鲤（吉祥）
}

// fishingJunkList: 永不空军 彩蛋 — all zero payout; 小鱼苗 is the "release
// the fry" joke item.
var fishingJunkList = []FishingSpecies{
	{Key: "boot", Tier: TierJunk},
	{Key: "seaweed", Tier: TierJunk},
	{Key: "plastic_bag", Tier: TierJunk},
	{Key: "branch", Tier: TierJunk},
	{Key: "old_tire", Tier: TierJunk},
	{Key: "glasses", Tier: TierJunk},
	{Key: "phone_case", Tier: TierJunk},
	{Key: "fry", Tier: TierJunk}, // 小鱼苗（放生）
}

// fishingTreasureList: rare positive rewards with admin-tunable multipliers.
var fishingTreasureList = []FishingSpecies{
	{Key: "bottle", Tier: TierTreasure}, // 漂流瓶
	{Key: "clover", Tier: TierTreasure}, // 幸运四叶草
	{Key: "shell", Tier: TierTreasure},  // 神秘贝壳
}

// fishingBaitSpec carries the per-bait relative tier/treasure weights. The
// engine normalizes them so fish+treasure probabilities sum to 1-junkRate
// (0.80); the multiplier scale factor then makes the exact RTP hold.
type fishingBaitSpec struct {
	key   string
	rtp   float64 // target return-to-player as a fraction
	tiers map[string]int
	tres  map[string]int
}

var fishingBaits = map[string]fishingBaitSpec{
	BaitWorm: {
		key: BaitWorm, rtp: 0.90,
		tiers: map[string]int{TierSmall: 500, TierRegular: 340, TierBig: 55, TierGiant: 45, TierLegend: 5},
		tres:  map[string]int{"bottle": 13, "clover": 7, "shell": 3},
	},
	BaitLure: {
		key: BaitLure, rtp: 0.90,
		tiers: map[string]int{TierSmall: 400, TierRegular: 380, TierBig: 90, TierGiant: 80, TierLegend: 12},
		tres:  map[string]int{"bottle": 18, "clover": 10, "shell": 4},
	},
	BaitPremium: {
		key: BaitPremium, rtp: 0.88,
		tiers: map[string]int{TierSmall: 340, TierRegular: 400, TierBig: 115, TierGiant: 110, TierLegend: 18},
		tres:  map[string]int{"bottle": 22, "clover": 13, "shell": 5},
	},
}

// fishTierMult returns the raw multiplier range (base..base+span) of a tier,
// before the 米级 (>=100cm) modifier and the per-bait scale factor.
func fishTierMult(tier string) (base, span float64) {
	switch tier {
	case TierSmall:
		return 0.4, 0.6 // 0.4–1.0
	case TierRegular:
		return 1.0, 0.8 // 1.0–1.8
	case TierBig:
		return 2.0, 2.0 // 2.0–4.0
	case TierGiant:
		return 5.0, 6.0 // 5.0–11.0
	case TierLegend:
		return 20.0, 6.666666666666667 // 20.0–26.667
	}
	return 0, 0
}

// fishMultiplierRaw computes the raw multiplier for a fish of the given tier
// and size: linear in size within the tier range, ×1.5 when size >= 100cm
// (米级), capped at maxFishMultiplier. The per-bait scale factor is applied
// by the caller.
func fishMultiplierRaw(tier string, size int) float64 {
	base, span := fishTierMult(tier)
	if span <= 0 {
		return 0
	}
	var minCM, maxCM int
	for _, sp := range fishingSpeciesList {
		if sp.Tier == tier {
			minCM, maxCM = sp.MinCM, sp.MaxCM
			break
		}
	}
	f := 0.0
	if maxCM > minCM {
		f = float64(size-minCM) / float64(maxCM-minCM)
	}
	m := base + f*span
	if size >= 100 {
		m *= 1.5
	}
	return math.Min(maxFishMultiplier, m)
}

// fishingEngine is the immutable per-bait probability model. It is derived
// from the specs above and the live admin-tunable economy settings.
type fishingEngine struct {
	bait         fishingBaitSpec
	scale        float64 // multiplier scale factor solving exact RTP
	outcomes     []fishingOutcome
	junk         []FishingSpecies
	treasures    []FishingSpecies
	treasureMult map[string]int // live configured multipliers
}

type fishingOutcome struct {
	speciesKey string
	tier       string
	prob       float64 // absolute probability (fish+treasure sum to 1-junkRate)
}

// buildFishingEngine derives the probability model for one bait from the
// spec and the live config (treasure multipliers are admin-tunable, so the
// scale factor must be recomputed whenever the config changes).
func buildFishingEngine(spec fishingBaitSpec, treasureMult map[string]int) (fishingEngine, error) {
	var tierSum int
	for _, w := range spec.tiers {
		tierSum += w
	}
	var tresSum int
	for _, w := range spec.tres {
		tresSum += w
	}
	total := tierSum + tresSum
	if total <= 0 {
		return fishingEngine{}, fmt.Errorf("fishing: empty bait spec")
	}
	eng := fishingEngine{bait: spec, scale: 1, treasureMult: treasureMult}
	positiveProb := 1 - fixedJunkRate

	// Expected (unscaled) fish payout: Σ p_i · E[mult_i].
	var fishEV float64
	for tier, w := range spec.tiers {
		p := positiveProb * float64(w) / float64(total)
		eng.outcomes = append(eng.outcomes, fishingOutcome{speciesKey: tier, tier: tier, prob: p})
		fishEV += p * expectedTierMultiplier(tier)
	}
	var treasureEV float64
	for key, w := range spec.tres {
		mult := float64(treasureMult[key])
		if mult <= 0 {
			mult = 1
		}
		p := positiveProb * float64(w) / float64(total)
		eng.outcomes = append(eng.outcomes, fishingOutcome{speciesKey: key, tier: TierTreasure, prob: p})
		treasureEV += p * mult
	}
	if fishEV > 0 {
		eng.scale = (spec.rtp - treasureEV) / fishEV
		if eng.scale < 0.1 || eng.scale > 3 {
			return fishingEngine{}, fmt.Errorf("fishing: invalid economy (scale %.3f): check RTP/treasure settings", eng.scale)
		}
	}
	eng.junk = fishingJunkList
	eng.treasures = fishingTreasureList
	return eng, nil
}

// expectedTierMultiplier computes E[raw multiplier] over the uniform integer
// size distribution of the tier (including the 米级 modifier).
func expectedTierMultiplier(tier string) float64 {
	var minCM, maxCM int
	found := false
	for _, sp := range fishingSpeciesList {
		if sp.Tier == tier {
			minCM, maxCM = sp.MinCM, sp.MaxCM
			found = true
			break
		}
	}
	if !found || maxCM < minCM {
		return 0
	}
	var sum float64
	for size := minCM; size <= maxCM; size++ {
		sum += fishMultiplierRaw(tier, size)
	}
	return sum / float64(maxCM-minCM+1)
}

// FishingConfig is the live, admin-tunable economy for 池塘垂钓.
type FishingConfig struct {
	Enabled      bool
	WormPrice    int
	LurePrice    int
	PremiumPrice int
	RTP          int // percent
	RTPPremium   int // percent
	TreasureMult map[string]int
}

// DefaultFishingConfig returns the frozen design defaults.
func DefaultFishingConfig() FishingConfig {
	return FishingConfig{
		Enabled:      DefaultGameFishingEnabled,
		WormPrice:    DefaultGameFishingBaitWormPrice,
		LurePrice:    DefaultGameFishingBaitLurePrice,
		PremiumPrice: DefaultGameFishingBaitPremiumPrice,
		RTP:          DefaultGameFishingRTP,
		RTPPremium:   DefaultGameFishingRTPPremium,
		TreasureMult: map[string]int{
			"bottle": DefaultGameFishingTreasureBottle,
			"clover": DefaultGameFishingTreasureClover,
			"shell":  DefaultGameFishingTreasureShell,
		},
	}
}

// FishingConfig loads the live fishing economy from settings.
func (s *Store) FishingConfig() FishingConfig {
	cfg := DefaultFishingConfig()
	enabledFallback := "false"
	if cfg.Enabled {
		enabledFallback = "true"
	}
	cfg.Enabled = s.GetSettingString(SettingGameFishingEnabled, enabledFallback) == "true"
	cfg.WormPrice = s.GetSettingInt(SettingGameFishingBaitWormPrice, cfg.WormPrice)
	cfg.LurePrice = s.GetSettingInt(SettingGameFishingBaitLurePrice, cfg.LurePrice)
	cfg.PremiumPrice = s.GetSettingInt(SettingGameFishingBaitPremiumPrice, cfg.PremiumPrice)
	cfg.RTP = s.GetSettingInt(SettingGameFishingRTP, cfg.RTP)
	cfg.RTPPremium = s.GetSettingInt(SettingGameFishingRTPPremium, cfg.RTPPremium)
	for _, t := range fishingTreasureList {
		switch t.Key {
		case "bottle":
			cfg.TreasureMult[t.Key] = s.GetSettingInt(SettingGameFishingTreasureBottle, cfg.TreasureMult[t.Key])
		case "clover":
			cfg.TreasureMult[t.Key] = s.GetSettingInt(SettingGameFishingTreasureClover, cfg.TreasureMult[t.Key])
		case "shell":
			cfg.TreasureMult[t.Key] = s.GetSettingInt(SettingGameFishingTreasureShell, cfg.TreasureMult[t.Key])
		}
	}
	return cfg
}

// FishingBaitPrice returns the configured price of a bait tier (0 when
// unknown).
func (c FishingConfig) FishingBaitPrice(bait string) int {
	switch bait {
	case BaitWorm:
		return c.WormPrice
	case BaitLure:
		return c.LurePrice
	case BaitPremium:
		return c.PremiumPrice
	}
	return 0
}

// FishingEngine builds the probability model for one bait from the live
// config.
func (c FishingConfig) FishingEngine(bait string) (fishingEngine, error) {
	spec, ok := fishingBaits[bait]
	if !ok {
		return fishingEngine{}, ErrBaitUnknown
	}
	if bait == BaitPremium {
		spec.rtp = float64(c.RTPPremium) / 100
	} else {
		spec.rtp = float64(c.RTP) / 100
	}
	return buildFishingEngine(spec, c.TreasureMult)
}

// GameRound is one fishing round (ticket + outcome).
type GameRound struct {
	ID         string
	GameID     string
	UserID     int64
	BaitTier   string
	Price      int
	Status     string // started | settled
	SpeciesKey string
	SizeCM     int
	IsJunk     bool
	IsTreasure bool
	CreditsWon int
	CreatedAt  int64
	SettledAt  int64
}

// FishingResult is the user-facing outcome of one round.
type FishingResult struct {
	RoundID    string `json:"round_id"`
	Bait       string `json:"bait"`
	Price      int    `json:"price"`
	SpeciesKey string `json:"species_key"`
	Tier       string `json:"tier"`
	SizeCM     int    `json:"size_cm"`
	IsJunk     bool   `json:"is_junk"`
	IsTreasure bool   `json:"is_treasure"`
	Meter      bool   `json:"meter"` // 米级: fish >= 100cm
	CreditsWon int    `json:"credits_won"`
	Credits    int    `json:"credits"`
	Settled    bool   `json:"settled"` // false when newly settled, true on idempotent replay
}

// cryptoRandFloat64 returns a uniform float64 in [0,1) from crypto/rand.
func cryptoRandFloat64() (float64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	u := binary.BigEndian.Uint64(b[:]) >> 11
	return float64(u) / float64(1<<53), nil
}

// randInt returns a uniform integer in [lo, hi] (inclusive).
func randInt(lo, hi int) (int, error) {
	if hi <= lo {
		return lo, nil
	}
	f, err := cryptoRandFloat64()
	if err != nil {
		return 0, err
	}
	return lo + int(f*float64(hi-lo+1)), nil
}

// rollFishingOutcome picks one outcome using the engine and returns the
// resulting catch. Treasure multipliers come from the engine's spec callers
// (already resolved against the live config at build time).
func rollFishingOutcome(eng fishingEngine, price int) (speciesKey, tier string, sizeCM, creditsWon int, isJunk, isTreasure bool, err error) {
	u, err := cryptoRandFloat64()
	if err != nil {
		return "", "", 0, 0, false, false, err
	}
	if u < fixedJunkRate {
		idx, err := randInt(0, len(eng.junk)-1)
		if err != nil {
			return "", "", 0, 0, false, false, err
		}
		j := eng.junk[idx]
		return j.Key, TierJunk, 0, 0, true, false, nil
	}
	// Pick among positive outcomes by cumulative probability.
	rem := u - fixedJunkRate
	var picked *fishingOutcome
	for i := range eng.outcomes {
		if rem < eng.outcomes[i].prob {
			picked = &eng.outcomes[i]
			break
		}
		rem -= eng.outcomes[i].prob
	}
	if picked == nil {
		picked = &eng.outcomes[len(eng.outcomes)-1]
	}
	if picked.tier == TierTreasure {
		mult := eng.treasureMult[picked.speciesKey]
		if mult <= 0 {
			mult = 1
		}
		credits := int(math.Round(float64(mult) * float64(price)))
		if credits < 0 {
			credits = 0
		}
		return picked.speciesKey, TierTreasure, 0, credits, false, true, nil
	}
	// Fish: pick a species of that tier, then a size.
	var candidates []FishingSpecies
	for _, sp := range fishingSpeciesList {
		if sp.Tier == picked.tier {
			candidates = append(candidates, sp)
		}
	}
	idx, err := randInt(0, len(candidates)-1)
	if err != nil {
		return "", "", 0, 0, false, false, err
	}
	size, err := randInt(candidates[idx].MinCM, candidates[idx].MaxCM)
	if err != nil {
		return "", "", 0, 0, false, false, err
	}
	raw := fishMultiplierRaw(picked.tier, size) * eng.scale
	credits := int(math.Round(raw * float64(price)))
	if credits < 0 {
		credits = 0
	}
	return candidates[idx].Key, picked.tier, size, credits, false, false, nil
}

// StartFishingRound deducts the ticket and opens a round whose outcome is
// decided server-side immediately (simplified seed scheme). The ticket is
// consumed at start; SettleFishingRound pays out (or not) exactly once.
func (s *Store) StartFishingRound(userID int64, bait string, cfg FishingConfig, now time.Time) (*GameRound, error) {
	// Enforce live switches in the transactional store boundary as well as
	// the HTTP handler, so future callers cannot bypass the game gates.
	if !s.GamesMasterEnabled() {
		return nil, ErrGamesDisabled
	}
	if !cfg.Enabled {
		return nil, ErrGameDisabled
	}
	price := cfg.FishingBaitPrice(bait)
	if price <= 0 {
		return nil, ErrBaitUnknown
	}
	eng, err := cfg.FishingEngine(bait)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var isAdmin int
	if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id=?`, userID).Scan(&isAdmin); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("start fishing round: user not found")
		}
		return nil, err
	}
	if isAdmin != 0 {
		return nil, ErrAdminCannotPlay
	}
	// Atomic ticket deduction with the credits >= 0 floor.
	res, err := tx.Exec(`UPDATE users SET credits=credits-?, updated_at=? WHERE id=? AND credits>=?`,
		price, now.Unix(), userID, price)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, ErrInsufficientCredits
	}

	speciesKey, _, sizeCM, creditsWon, isJunk, isTreasure, err := rollFishingOutcome(eng, price)
	if err != nil {
		return nil, err
	}
	roundID := randomRoundID()
	round := &GameRound{
		ID: roundID, GameID: GameIDFishing, UserID: userID, BaitTier: bait,
		Price: price, Status: "started", SpeciesKey: speciesKey, SizeCM: sizeCM,
		IsJunk: isJunk, IsTreasure: isTreasure, CreditsWon: creditsWon,
		CreatedAt: now.Unix(),
	}
	if _, err := tx.Exec(`INSERT INTO game_rounds
		(id, game_id, user_id, bait_tier, price, status, species_key, size_cm, is_junk, is_treasure, credits_won, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		round.ID, round.GameID, round.UserID, round.BaitTier, round.Price, round.Status,
		round.SpeciesKey, round.SizeCM, boolInt64(round.IsJunk), boolInt64(round.IsTreasure),
		round.CreditsWon, round.CreatedAt); err != nil {
		return nil, err
	}
	// Participation counts toward product activity immediately (scope B).
	if err := recordActivityTx(tx, userID, now, activityDelta{games: 1}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return round, nil
}

// SettleFishingRound settles a started round exactly once: pays out
// credits_won, updates the single-fish leaderboard, and returns the result.
// Repeated settles for the same round are idempotent (same result, no
// double payout). ErrRoundNotFound when the round does not belong to the
// user or does not exist.
func (s *Store) SettleFishingRound(userID int64, roundID string, now time.Time) (*FishingResult, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	var round GameRound
	var isJunk, isTreasure int
	err = tx.QueryRow(`SELECT id, game_id, user_id, bait_tier, price, status, species_key, size_cm,
		is_junk, is_treasure, credits_won, created_at, settled_at
		FROM game_rounds WHERE id=? AND user_id=?`, roundID, userID).
		Scan(&round.ID, &round.GameID, &round.UserID, &round.BaitTier, &round.Price, &round.Status,
			&round.SpeciesKey, &round.SizeCM, &isJunk, &isTreasure, &round.CreditsWon,
			&round.CreatedAt, &round.SettledAt)
	if err == sql.ErrNoRows {
		return nil, false, ErrRoundNotFound
	}
	if err != nil {
		return nil, false, err
	}
	round.IsJunk = isJunk != 0
	round.IsTreasure = isTreasure != 0

	res := &FishingResult{
		RoundID: round.ID, Bait: round.BaitTier, Price: round.Price,
		SpeciesKey: round.SpeciesKey, Tier: tierOf(round), SizeCM: round.SizeCM,
		IsJunk: round.IsJunk, IsTreasure: round.IsTreasure,
		Meter:      !round.IsJunk && !round.IsTreasure && round.SizeCM >= 100,
		CreditsWon: round.CreditsWon,
	}
	if round.Status == "settled" {
		// Idempotent replay: return the stored outcome without touching
		// balances again.
		credits, err := s.creditsInTx(tx, userID)
		if err != nil {
			return nil, false, err
		}
		res.Credits = credits
		res.Settled = true
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return res, false, nil
	}
	if round.Status != "started" {
		return nil, false, ErrRoundNotFound
	}
	if _, err := tx.Exec(`UPDATE game_rounds SET status='settled', settled_at=? WHERE id=? AND status='started'`,
		now.Unix(), round.ID); err != nil {
		return nil, false, err
	}
	if round.CreditsWon > 0 {
		if _, err := tx.Exec(`UPDATE users SET credits=credits+?, updated_at=? WHERE id=?`,
			round.CreditsWon, now.Unix(), userID); err != nil {
			return nil, false, err
		}
	}
	// Single-fish leaderboard: keep the all-time best per user per game.
	if !round.IsJunk && !round.IsTreasure && round.SizeCM > 0 {
		if _, err := tx.Exec(`INSERT INTO game_best (user_id, game_id, species_key, size_cm, caught_at)
			VALUES (?,?,?,?,?)
			ON CONFLICT(user_id, game_id) DO UPDATE SET
				species_key=CASE WHEN excluded.size_cm>game_best.size_cm THEN excluded.species_key ELSE game_best.species_key END,
				size_cm=MAX(game_best.size_cm, excluded.size_cm),
				caught_at=CASE WHEN excluded.size_cm>game_best.size_cm THEN excluded.caught_at ELSE game_best.caught_at END`,
			userID, round.GameID, round.SpeciesKey, round.SizeCM, now.Unix()); err != nil {
			return nil, false, err
		}
	}
	credits, err := s.creditsInTx(tx, userID)
	if err != nil {
		return nil, false, err
	}
	res.Credits = credits
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return res, true, nil
}

func tierOf(r GameRound) string {
	switch {
	case r.IsJunk:
		return TierJunk
	case r.IsTreasure:
		return TierTreasure
	}
	for _, sp := range fishingSpeciesList {
		if sp.Key == r.SpeciesKey {
			return sp.Tier
		}
	}
	return TierJunk
}

func (s *Store) creditsInTx(tx *sql.Tx, userID int64) (int, error) {
	var c int
	if err := tx.QueryRow(`SELECT credits FROM users WHERE id=?`, userID).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

// LeaderboardEntry is one row of a fishing leaderboard.
type LeaderboardEntry struct {
	Rank       int    `json:"rank"`
	Username   string `json:"username"`
	Anonymous  bool   `json:"anonymous"`
	SpeciesKey string `json:"species_key,omitempty"`
	SizeCM     int    `json:"size_cm,omitempty"`
	Total      int64  `json:"total,omitempty"`
}

// LeaderboardResult carries the top entries plus the caller's own standing.
type LeaderboardResult struct {
	Entries []LeaderboardEntry `json:"entries"`
	Me      *LeaderboardEntry  `json:"me,omitempty"`
}

// Leaderboard returns the single-fish (all-time best size) or total-catch
// (rolling 30-day credits won) leaderboard for a game: Top 20 + the given
// user's own standing (nil when the user has no entry).
func (s *Store) FishingLeaderboard(gameID, board string, userID int64, now time.Time) (*LeaderboardResult, error) {
	cutoff := now.Add(-30 * 24 * time.Hour).Unix()
	anonSQL := `CASE WHEN u.leaderboard_anon=1 THEN 1 ELSE 0 END`
	nameSQL := `CASE WHEN u.leaderboard_anon=1 THEN '' ELSE u.username END`

	var rows *sql.Rows
	var err error
	if board == "single" {
		rows, err = s.db.Query(`SELECT gb.size_cm, `+nameSQL+`, `+anonSQL+`, gb.species_key
			FROM game_best gb JOIN users u ON u.id=gb.user_id
			WHERE gb.game_id=? AND u.is_admin=0 AND u.disabled=0
			ORDER BY gb.size_cm DESC, gb.caught_at ASC LIMIT 20`, gameID)
	} else {
		rows, err = s.db.Query(`SELECT SUM(r.credits_won), `+nameSQL+`, `+anonSQL+`, ''
			FROM game_rounds r JOIN users u ON u.id=r.user_id
			WHERE r.game_id=? AND r.status='settled' AND r.created_at>=? AND u.is_admin=0 AND u.disabled=0
			GROUP BY r.user_id ORDER BY SUM(r.credits_won) DESC, MAX(r.settled_at) ASC LIMIT 20`,
			gameID, cutoff)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &LeaderboardResult{Entries: []LeaderboardEntry{}}
	rank := 0
	for rows.Next() {
		var e LeaderboardEntry
		var anon int
		var total sql.NullInt64
		if board == "single" {
			if err := rows.Scan(&e.SizeCM, &e.Username, &anon, &e.SpeciesKey); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&total, &e.Username, &anon, &e.SpeciesKey); err != nil {
				return nil, err
			}
			e.Total = total.Int64
		}
		e.Anonymous = anon != 0
		rank++
		e.Rank = rank
		out.Entries = append(out.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	me, err := s.myLeaderboardStanding(gameID, board, userID, cutoff)
	if err != nil {
		return nil, err
	}
	out.Me = me
	return out, nil
}

func (s *Store) myLeaderboardStanding(gameID, board string, userID int64, cutoff int64) (*LeaderboardEntry, error) {
	var e LeaderboardEntry
	var anon int
	if board == "single" {
		var size sql.NullInt64
		var speciesKey string
		if err := s.db.QueryRow(`SELECT gb.size_cm, gb.species_key, u.leaderboard_anon
			FROM game_best gb JOIN users u ON u.id=gb.user_id
			WHERE gb.game_id=? AND gb.user_id=?`, gameID, userID).
			Scan(&size, &speciesKey, &anon); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}
		if !size.Valid || size.Int64 == 0 {
			return nil, nil
		}
		e.SizeCM = int(size.Int64)
		e.SpeciesKey = speciesKey
		var rank int
		if err := s.db.QueryRow(`SELECT COUNT(*)+1 FROM game_best
			WHERE game_id=? AND size_cm>?`, gameID, size.Int64).Scan(&rank); err != nil {
			return nil, err
		}
		e.Rank = rank
	} else {
		var total sql.NullInt64
		if err := s.db.QueryRow(`SELECT SUM(credits_won) FROM game_rounds
			WHERE game_id=? AND user_id=? AND status='settled' AND created_at>=?`,
			gameID, userID, cutoff).Scan(&total); err != nil {
			return nil, err
		}
		if !total.Valid || total.Int64 <= 0 {
			return nil, nil
		}
		e.Total = total.Int64
		var rank int
		if err := s.db.QueryRow(`SELECT COUNT(*)+1 FROM (
			SELECT r.user_id FROM game_rounds r JOIN users u ON u.id=r.user_id
			WHERE r.game_id=? AND r.status='settled' AND r.created_at>=? AND u.is_admin=0 AND u.disabled=0
			GROUP BY r.user_id HAVING SUM(r.credits_won)>? )`, gameID, cutoff, total.Int64).Scan(&rank); err != nil {
			return nil, err
		}
		e.Rank = rank
	}
	var username string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username); err != nil {
		return nil, err
	}
	if anon != 0 {
		username = ""
	}
	e.Username = username
	e.Anonymous = anon != 0
	return &e, nil
}

// SetLeaderboardAnon toggles whether the user's username appears on game
// leaderboards.
func (s *Store) SetLeaderboardAnon(userID int64, anon bool) error {
	v := 0
	if anon {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE users SET leaderboard_anon=?, updated_at=? WHERE id=?`, v, time.Now().Unix(), userID)
	return err
}

// LeaderboardAnon reports the user's leaderboard anonymity switch.
func (s *Store) LeaderboardAnon(userID int64) (bool, error) {
	var v int
	err := s.db.QueryRow(`SELECT leaderboard_anon FROM users WHERE id=?`, userID).Scan(&v)
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

// PurgeExpiredGameRounds enforces the rolling 30-day retention on game
// round rows. The single-fish best rows (game_best) are kept until the
// account is deleted.
func (s *Store) PurgeExpiredGameRounds(now int64) (int64, error) {
	cutoff := now - int64(GameRoundsRetention.Seconds())
	res, err := s.db.Exec(`DELETE FROM game_rounds WHERE created_at<?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListGameRoundIDsBetween returns round ids of a user within [from, until]
// (for export). Rounds are capped at the rolling retention window.
func (s *Store) ListGameRounds(userID int64, from, until int64) ([]GameRound, error) {
	rows, err := s.db.Query(`SELECT id, game_id, bait_tier, price, status, species_key, size_cm,
		is_junk, is_treasure, credits_won, created_at, settled_at
		FROM game_rounds WHERE user_id=? AND created_at>=? AND created_at<=? ORDER BY created_at`,
		userID, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GameRound{}
	for rows.Next() {
		var r GameRound
		var isJunk, isTreasure int
		if err := rows.Scan(&r.ID, &r.GameID, &r.BaitTier, &r.Price, &r.Status, &r.SpeciesKey,
			&r.SizeCM, &isJunk, &isTreasure, &r.CreditsWon, &r.CreatedAt, &r.SettledAt); err != nil {
			return nil, err
		}
		r.IsJunk = isJunk != 0
		r.IsTreasure = isTreasure != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// GameBestRow is one game_best row (export).
type GameBestRow struct {
	GameID     string
	SpeciesKey string
	SizeCM     int
	CaughtAt   int64
}

// ListGameBest returns the user's per-game best catches.
func (s *Store) ListGameBest(userID int64) ([]GameBestRow, error) {
	rows, err := s.db.Query(`SELECT game_id, species_key, size_cm, caught_at
		FROM game_best WHERE user_id=? ORDER BY game_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GameBestRow{}
	for rows.Next() {
		var b GameBestRow
		if err := rows.Scan(&b.GameID, &b.SpeciesKey, &b.SizeCM, &b.CaughtAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// randomRoundID returns a 128-bit random hex round id.
func randomRoundID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("r%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}

func boolInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// BaitPriceList returns the three bait tiers with prices for the games list.
func (c FishingConfig) BaitPriceList() []map[string]interface{} {
	keys := []string{BaitWorm, BaitLure, BaitPremium}
	out := make([]map[string]interface{}, 0, 3)
	for _, k := range keys {
		out = append(out, map[string]interface{}{
			"bait":  k,
			"price": c.FishingBaitPrice(k),
		})
	}
	return out
}

// GamesMasterEnabled reports the games master switch.
func (s *Store) GamesMasterEnabled() bool {
	fallback := "false"
	if DefaultGamesEnabled {
		fallback = "true"
	}
	return s.GetSettingString(SettingGamesEnabled, fallback) == "true"
}
