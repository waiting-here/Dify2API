package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

// --- helpers ---

var gamesUserSeq atomic.Int64

// makeGamesUser creates a non-admin user with the given credits and returns
// the user plus a session cookie. Each call uses a unique Discord id so
// multiple users can coexist in one test.
func makeGamesUser(t *testing.T, store *db.Store, credits int) (*db.User, *http.Cookie) {
	t.Helper()
	seq := gamesUserSeq.Add(1)
	u, err := store.CreateUser(fmt.Sprintf("game-%d", seq), "gamer", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.SetUserCredits(u.ID, credits); err != nil {
		t.Fatalf("set credits: %v", err)
	}
	sess, _, err := store.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return u, &http.Cookie{Name: auth.SessionCookieName, Value: sess}
}

func gamesMux(gw *Gateway) *http.ServeMux {
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)
	return mux
}

func gamesGet(mux *http.ServeMux, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func gamesJSON(mux *http.ServeMux, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// errCode extracts error.code from a JSON error envelope ("" when absent).
func errCode(body []byte) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(body, &e)
	return e.Error.Code
}

// lbEntry mirrors db.LeaderboardEntry for JSON decoding in tests.
type lbEntry struct {
	Rank       int    `json:"rank"`
	Username   string `json:"username"`
	Anonymous  bool   `json:"anonymous"`
	SpeciesKey string `json:"species_key,omitempty"`
	SizeCM     int    `json:"size_cm,omitempty"`
	Total      int64  `json:"total,omitempty"`
}

type adminGameParams struct {
	BaitWormPrice    int `json:"bait_worm_price"`
	BaitLurePrice    int `json:"bait_lure_price"`
	BaitPremiumPrice int `json:"bait_premium_price"`
	RTP              int `json:"rtp"`
	RTPPremium       int `json:"rtp_premium"`
	TreasureBottle   int `json:"treasure_bottle"`
	TreasureClover   int `json:"treasure_clover"`
	TreasureShell    int `json:"treasure_shell"`
}

type adminGamesResp struct {
	MasterEnabled bool `json:"master_enabled"`
	Games         []struct {
		ID       string          `json:"id"`
		Enabled  bool            `json:"enabled"`
		Params   adminGameParams `json:"params"`
		Defaults adminGameParams `json:"defaults"`
	} `json:"games"`
}

func decodeAdminGames(t *testing.T, body []byte) adminGamesResp {
	t.Helper()
	var r adminGamesResp
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("decode admin games: %v (body=%s)", err, body)
	}
	return r
}

// --- 1. unauthenticated access ---

func TestGames_Unauthenticated(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := gamesMux(gw)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/me/games", ""},
		{http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`},
		{http.MethodPost, "/api/me/games/fishing/settle", `{"round_id":"x"}`},
		{http.MethodGet, "/api/me/games/fishing/leaderboard", ""},
		{http.MethodPut, "/api/me/games/anonymous", `{"anonymous":true}`},
	}
	for _, c := range cases {
		var rec *httptest.ResponseRecorder
		if c.method == http.MethodGet {
			rec = gamesGet(mux, nil, c.path)
		} else {
			rec = gamesJSON(mux, c.method, c.path, c.body, nil)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status=%d want 401; body=%s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// --- 2. GET /api/me/games shape ---

func TestGames_List(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	_, cookie := makeGamesUser(t, store, 42)
	mux := gamesMux(gw)

	rec := gamesGet(mux, cookie, "/api/me/games")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		MasterEnabled   bool `json:"master_enabled"`
		Credits         int  `json:"credits"`
		LeaderboardAnon bool `json:"leaderboard_anon"`
		Games           []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
			Params  struct {
				Baits []struct {
					Bait  string `json:"bait"`
					Price int    `json:"price"`
				} `json:"baits"`
				RTP          int            `json:"rtp"`
				RTPPremium   int            `json:"rtp_premium"`
				TreasureMult map[string]int `json:"treasure_mult"`
			} `json:"params"`
		} `json:"games"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.MasterEnabled {
		t.Error("master_enabled should be true by default")
	}
	if resp.Credits != 42 {
		t.Errorf("credits=%d want 42", resp.Credits)
	}
	if len(resp.Games) != 1 || resp.Games[0].ID != db.GameIDFishing {
		t.Fatalf("games=%+v want [fishing]", resp.Games)
	}
	baits := resp.Games[0].Params.Baits
	if len(baits) != 3 {
		t.Fatalf("baits=%d want 3", len(baits))
	}
	wantPrice := map[string]int{"worm": 5, "lure": 10, "premium": 15}
	for _, b := range baits {
		if b.Price != wantPrice[b.Bait] {
			t.Errorf("bait %s price=%d want %d", b.Bait, b.Price, wantPrice[b.Bait])
		}
	}
	tm := resp.Games[0].Params.TreasureMult
	if tm["bottle"] != 2 || tm["clover"] != 3 || tm["shell"] != 5 {
		t.Errorf("treasure_mult=%+v want bottle=2,clover=3,shell=5", tm)
	}
}

// --- 3. full round flow + idempotent settle ---

func TestGames_FishingRoundFlow(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	_, cookie := makeGamesUser(t, store, 100)
	mux := gamesMux(gw)

	// start(worm) -> 200, credits 95 (100 - 5 ticket).
	rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var start struct {
		RoundID string `json:"round_id"`
		Price   int    `json:"price"`
		Credits int    `json:"credits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if start.Price != 5 {
		t.Errorf("price=%d want 5", start.Price)
	}
	if start.Credits != 95 {
		t.Errorf("credits after start=%d want 95", start.Credits)
	}

	// settle -> credits 95 + won, settled=false (newly settled).
	rec = gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/settle",
		fmt.Sprintf(`{"round_id":%q}`, start.RoundID), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("settle: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var s1 struct {
		SpeciesKey string `json:"species_key"`
		CreditsWon int    `json:"credits_won"`
		Credits    int    `json:"credits"`
		Settled    bool   `json:"settled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &s1); err != nil {
		t.Fatalf("decode settle: %v", err)
	}
	if s1.Settled {
		t.Error("first settle should report settled=false")
	}
	if s1.Credits != 95+s1.CreditsWon {
		t.Errorf("credits after settle=%d want %d", s1.Credits, 95+s1.CreditsWon)
	}

	// idempotent replay: same outcome, no balance change, settled=true.
	rec = gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/settle",
		fmt.Sprintf(`{"round_id":%q}`, start.RoundID), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("settle replay: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var s2 struct {
		SpeciesKey string `json:"species_key"`
		CreditsWon int    `json:"credits_won"`
		Credits    int    `json:"credits"`
		Settled    bool   `json:"settled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &s2); err != nil {
		t.Fatalf("decode settle replay: %v", err)
	}
	if !s2.Settled {
		t.Error("replay should report settled=true")
	}
	if s2.CreditsWon != s1.CreditsWon || s2.SpeciesKey != s1.SpeciesKey {
		t.Errorf("replay result changed: won %d->%d species %q->%q",
			s1.CreditsWon, s2.CreditsWon, s1.SpeciesKey, s2.SpeciesKey)
	}
	if s2.Credits != s1.Credits {
		t.Errorf("replay changed balance: %d -> %d", s1.Credits, s2.Credits)
	}
}

// --- 4. insufficient credits ---

func TestGames_InsufficientCredits(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	_, cookie := makeGamesUser(t, store, 0)
	mux := gamesMux(gw)

	rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403; body=%s", rec.Code, rec.Body.String())
	}
	if c := errCode(rec.Body.Bytes()); c != "insufficient_credits" {
		t.Errorf("code=%q want insufficient_credits; body=%s", c, rec.Body.String())
	}
}

// --- 5. master / per-game switches ---

func TestGames_DisabledSwitches(t *testing.T) {
	t.Run("master", func(t *testing.T) {
		gw, store := setupAuthGateway(t, "s3cret")
		if err := store.SetSetting(db.SettingGamesEnabled, "false"); err != nil {
			t.Fatal(err)
		}
		_, cookie := makeGamesUser(t, store, 100)
		mux := gamesMux(gw)
		rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d want 403; body=%s", rec.Code, rec.Body.String())
		}
		if c := errCode(rec.Body.Bytes()); c != "games_disabled" {
			t.Errorf("code=%q want games_disabled; body=%s", c, rec.Body.String())
		}
	})
	t.Run("fishing", func(t *testing.T) {
		gw, store := setupAuthGateway(t, "s3cret")
		if err := store.SetSetting(db.SettingGameFishingEnabled, "false"); err != nil {
			t.Fatal(err)
		}
		_, cookie := makeGamesUser(t, store, 100)
		mux := gamesMux(gw)
		rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d want 403; body=%s", rec.Code, rec.Body.String())
		}
		if c := errCode(rec.Body.Bytes()); c != "game_disabled" {
			t.Errorf("code=%q want game_disabled; body=%s", c, rec.Body.String())
		}
	})
}

// --- 6. per-user start rate limit (10/min) ---

func TestGames_StartRateLimit(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	u, cookie := makeGamesUser(t, store, 1000)
	mux := gamesMux(gw)

	// The first gameStartLimitPerUser starts succeed (worm = 5 credits each).
	for i := 0; i < gameStartLimitPerUser; i++ {
		rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("start %d: status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	// The next start is rate-limited before the ticket deduction.
	rec := gamesJSON(mux, http.MethodPost, "/api/me/games/fishing/start", `{"bait":"worm"}`, cookie)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("start %d: status=%d want 429; body=%s", gameStartLimitPerUser+1, rec.Code, rec.Body.String())
	}
	if c := errCode(rec.Body.Bytes()); c != "rate_limited" {
		t.Errorf("code=%q want rate_limited; body=%s", c, rec.Body.String())
	}
	// Credits untouched by the rejected attempt: 1000 - 10*5 = 950.
	got, _ := store.GetUserByID(u.ID)
	if got.Credits != 950 {
		t.Errorf("credits after rate-limited attempt=%d want 950", got.Credits)
	}
}

// --- 7. leaderboard (single + total + anonymity) ---

func TestGames_Leaderboard(t *testing.T) {
	gw, store := setupAuthGateway(t, "s3cret")
	alice, _ := store.CreateUser("lb-a", "alice", "")
	bob, _ := store.CreateUser("lb-b", "bob", "")
	now := time.Now()

	// Deterministic started rounds: alice lands a 75cm common_carp (15
	// credits), bob a 30cm crucian (3 credits). Outcomes are decided at
	// start time, so we insert the started rows directly and settle them.
	if _, err := store.RawExec(`INSERT INTO game_rounds
		(id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('h-lb-1','fishing',?,'worm',5,'started','common_carp',75,15,?)`,
		alice.ID, now.Unix()); err != nil {
		t.Fatalf("insert alice round: %v", err)
	}
	if _, err := store.RawExec(`INSERT INTO game_rounds
		(id, game_id, user_id, bait_tier, price, status, species_key, size_cm, credits_won, created_at)
		VALUES ('h-lb-2','fishing',?,'worm',5,'started','crucian',30,3,?)`,
		bob.ID, now.Unix()); err != nil {
		t.Fatalf("insert bob round: %v", err)
	}
	if _, _, err := store.SettleFishingRound(alice.ID, "h-lb-1", now); err != nil {
		t.Fatalf("settle alice: %v", err)
	}
	if _, _, err := store.SettleFishingRound(bob.ID, "h-lb-2", now); err != nil {
		t.Fatalf("settle bob: %v", err)
	}

	sess, _, _ := store.CreateSession(alice.ID)
	aliceCookie := &http.Cookie{Name: auth.SessionCookieName, Value: sess}
	mux := gamesMux(gw)

	// single board: alice (75cm) ranks above bob (30cm); me = alice rank 1.
	rec := gamesGet(mux, aliceCookie, "/api/me/games/fishing/leaderboard?board=single")
	if rec.Code != http.StatusOK {
		t.Fatalf("single: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var single struct {
		Entries []lbEntry `json:"entries"`
		Me      *lbEntry  `json:"me"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &single); err != nil {
		t.Fatalf("decode single: %v", err)
	}
	if len(single.Entries) != 2 {
		t.Fatalf("single entries=%d want 2", len(single.Entries))
	}
	if single.Entries[0].SizeCM != 75 || single.Entries[0].Username != "alice" || single.Entries[0].Rank != 1 {
		t.Errorf("single entries[0]=%+v want alice/75cm/rank1", single.Entries[0])
	}
	if single.Entries[1].SizeCM != 30 || single.Entries[1].Username != "bob" || single.Entries[1].Rank != 2 {
		t.Errorf("single entries[1]=%+v want bob/30cm/rank2", single.Entries[1])
	}
	if single.Me == nil || single.Me.Rank != 1 || single.Me.SizeCM != 75 {
		t.Errorf("single me=%+v want rank1/75cm", single.Me)
	}

	// Toggle anonymity as alice.
	rec = gamesJSON(mux, http.MethodPut, "/api/me/games/anonymous", `{"anonymous":true}`, aliceCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("anon PUT: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var anonResp struct {
		Anonymous bool `json:"anonymous"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &anonResp); err != nil {
		t.Fatalf("decode anon: %v", err)
	}
	if !anonResp.Anonymous {
		t.Error("anon response should echo anonymous=true")
	}

	// single board again: alice's entry is masked (username="", anonymous=true),
	// still rank 1 with size 75.
	rec = gamesGet(mux, aliceCookie, "/api/me/games/fishing/leaderboard?board=single")
	if rec.Code != http.StatusOK {
		t.Fatalf("single after anon: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &single); err != nil {
		t.Fatalf("decode single after anon: %v", err)
	}
	if !single.Entries[0].Anonymous || single.Entries[0].Username != "" || single.Entries[0].SizeCM != 75 {
		t.Errorf("anon not applied to entries[0]: %+v", single.Entries[0])
	}

	// total board: alice 15 > bob 3; me total = 15.
	rec = gamesGet(mux, aliceCookie, "/api/me/games/fishing/leaderboard?board=total")
	if rec.Code != http.StatusOK {
		t.Fatalf("total: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var total struct {
		Entries []lbEntry `json:"entries"`
		Me      *lbEntry  `json:"me"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &total); err != nil {
		t.Fatalf("decode total: %v", err)
	}
	if len(total.Entries) != 2 {
		t.Fatalf("total entries=%d want 2", len(total.Entries))
	}
	if total.Entries[0].Total != 15 {
		t.Errorf("total entries[0].total=%d want 15", total.Entries[0].Total)
	}
	if total.Me == nil || total.Me.Total != 15 {
		t.Errorf("total me=%+v want total=15", total.Me)
	}
}

// --- 8. admin endpoints ---

func TestAdminGames(t *testing.T) {
	t.Run("Unauthenticated", func(t *testing.T) {
		gw, _ := setupAuthGateway(t, "s3cret")
		mux := gamesMux(gw)
		if rec := gamesGet(mux, nil, "/api/admin/games"); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET no session: status=%d want 401", rec.Code)
		}
		if rec := gamesJSON(mux, http.MethodPut, "/api/admin/games", `{"games":[]}`, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("PUT no session: status=%d want 401", rec.Code)
		}
		if rec := gamesJSON(mux, http.MethodPost, "/api/admin/games/restore-defaults", `{}`, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("restore no session: status=%d want 401", rec.Code)
		}
	})

	t.Run("GetPutRestore", func(t *testing.T) {
		gw, _ := setupAuthGateway(t, "s3cret")
		admin := loginCookie(t, gw, "root", "s3cret")
		mux := gamesMux(gw)

		// GET: defaults present.
		rec := gamesGet(mux, admin, "/api/admin/games")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: status=%d body=%s", rec.Code, rec.Body.String())
		}
		get := decodeAdminGames(t, rec.Body.Bytes())
		if !get.MasterEnabled {
			t.Error("master_enabled should be true")
		}
		if len(get.Games) != 1 || get.Games[0].ID != db.GameIDFishing {
			t.Fatalf("games=%+v want [fishing]", get.Games)
		}

		// PUT invalid: rtp=0 -> 400 invalid_request and no earlier field in
		// the same request may be committed (single atomic settings batch).
		rec = gamesJSON(mux, http.MethodPut, "/api/admin/games",
			`{"master_enabled":false,"games":[{"id":"fishing","params":{"rtp":0}}]}`, admin)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid PUT: status=%d want 400; body=%s", rec.Code, rec.Body.String())
		}
		if c := errCode(rec.Body.Bytes()); c != "invalid_request" {
			t.Errorf("code=%q want invalid_request; body=%s", c, rec.Body.String())
		}
		rec = gamesGet(mux, admin, "/api/admin/games")
		if afterInvalid := decodeAdminGames(t, rec.Body.Bytes()); !afterInvalid.MasterEnabled {
			t.Error("invalid economy request partially committed master_enabled=false")
		}

		// PUT valid custom bait prices, then read back.
		rec = gamesJSON(mux, http.MethodPut, "/api/admin/games",
			`{"games":[{"id":"fishing","params":{"bait_worm_price":7,"bait_lure_price":12,"bait_premium_price":20}}]}`, admin)
		if rec.Code != http.StatusOK {
			t.Fatalf("custom PUT: status=%d body=%s", rec.Code, rec.Body.String())
		}
		rec = gamesGet(mux, admin, "/api/admin/games")
		get = decodeAdminGames(t, rec.Body.Bytes())
		p := get.Games[0].Params
		if p.BaitWormPrice != 7 || p.BaitLurePrice != 12 || p.BaitPremiumPrice != 20 {
			t.Errorf("after custom PUT params=%+v want 7/12/20", p)
		}

		// restore-defaults, then GET back to the frozen defaults.
		rec = gamesJSON(mux, http.MethodPost, "/api/admin/games/restore-defaults", `{}`, admin)
		if rec.Code != http.StatusOK {
			t.Fatalf("restore: status=%d body=%s", rec.Code, rec.Body.String())
		}
		rec = gamesGet(mux, admin, "/api/admin/games")
		get = decodeAdminGames(t, rec.Body.Bytes())
		p = get.Games[0].Params
		if p.BaitWormPrice != 5 || p.BaitLurePrice != 10 || p.BaitPremiumPrice != 15 {
			t.Errorf("restore prices=%+v want 5/10/15", p)
		}
		if p.RTP != 90 || p.RTPPremium != 88 {
			t.Errorf("restore rtp=%d/%d want 90/88", p.RTP, p.RTPPremium)
		}
		if p.TreasureBottle != 2 || p.TreasureClover != 3 || p.TreasureShell != 5 {
			t.Errorf("restore treasure=%d/%d/%d want 2/3/5", p.TreasureBottle, p.TreasureClover, p.TreasureShell)
		}
	})
}

// --- 9. host isolation via gw.Wrap ---

func TestGames_HostSeparation(t *testing.T) {
	gw, _ := setupAuthGateway(t, "s3cret")
	mux := gamesMux(gw)
	wrapped := gw.Wrap(mux)

	// User site -> /api/admin/games -> 404 (hostSeparation blocks /api/admin/
	// on the user host before any auth).
	req := httptest.NewRequest(http.MethodGet,
		"http://"+gw.Config.Admin.SiteHost+"/api/admin/games", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("user host admin games: status=%d want 404; body=%s", rec.Code, rec.Body.String())
	}

	// Admin site -> /api/me/games -> 404 (admin host allowlists only the
	// exact /api/me path, not its sub-paths).
	req = httptest.NewRequest(http.MethodGet,
		"http://"+gw.Config.Admin.AdminHost+"/api/me/games", nil)
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("admin host me games: status=%d want 404; body=%s", rec.Code, rec.Body.String())
	}
}
