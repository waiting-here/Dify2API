package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"dify2api/db"
)

// gameStartLimitPerUser caps fishing round starts per user per minute
// (ticket purchases are the money gate; this bounds automation on top of
// the credits floor).
const gameStartLimitPerUser = 30

// GET /api/me/games — game list with live switches, economy params, the
// caller's credits and leaderboard anonymity switch.
func (g *Gateway) handleGamesList(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	master := g.Store.GamesMasterEnabled()
	fishing := g.Store.FishingConfig()
	anon, _ := g.Store.LeaderboardAnon(u.ID)

	games := []map[string]interface{}{
		{
			"id":      db.GameIDFishing,
			"enabled": master && fishing.Enabled,
			"params": map[string]interface{}{
				"baits":         fishing.BaitPriceList(),
				"rtp":           fishing.RTP,
				"rtp_premium":   fishing.RTPPremium,
				"treasure_mult": fishing.TreasureMult,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"master_enabled":   master,
		"credits":          u.Credits,
		"leaderboard_anon": anon,
		"games":            games,
	})
}

// POST /api/me/games/fishing/start — buys a bait ticket and opens a round
// whose outcome is decided server-side immediately. The ticket is consumed
// here; the result is revealed by settle (idempotent).
func (g *Gateway) handleFishingStart(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	if u.IsAdmin {
		g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "管理员不参与小游戏", "Admins cannot play mini-games"))
		return
	}
	if !g.Store.GamesMasterEnabled() {
		g.writeError(w, http.StatusForbidden, "games_disabled", t(g.resolveLang(r), "小游戏暂未开放", "Mini-games are not available yet"))
		return
	}
	cfg := g.Store.FishingConfig()
	if !cfg.Enabled {
		g.writeError(w, http.StatusForbidden, "game_disabled", t(g.resolveLang(r), "池塘垂钓暂未开放", "Pond fishing is not available yet"))
		return
	}
	// Per-user start rate limit (window 60s).
	if !g.gameStartLimiter.allow(u.ID, gameStartLimitPerUser, time.Now()) {
		g.writeError(w, http.StatusTooManyRequests, "rate_limited", t(g.resolveLang(r), "操作过于频繁，请稍后再试", "Too many attempts, please try again later"))
		return
	}
	var body struct {
		Bait string `json:"bait"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	round, err := g.Store.StartFishingRound(u.ID, body.Bait, cfg, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, db.ErrGamesDisabled):
			g.writeError(w, http.StatusForbidden, "games_disabled", t(g.resolveLang(r), "小游戏暂未开放", "Mini-games are not available yet"))
		case errors.Is(err, db.ErrGameDisabled):
			g.writeError(w, http.StatusForbidden, "game_disabled", t(g.resolveLang(r), "池塘垂钓暂未开放", "Pond fishing is not available yet"))
		case errors.Is(err, db.ErrBaitUnknown):
			g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "未知的鱼饵", "Unknown bait"))
		case errors.Is(err, db.ErrAdminCannotPlay):
			g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "管理员不参与小游戏", "Admins cannot play mini-games"))
		case errors.Is(err, db.ErrInsufficientCredits):
			g.writeError(w, http.StatusForbidden, "insufficient_credits", t(g.resolveLang(r), "积分不足，无法购买鱼饵（可先签到获取积分）", "Insufficient credits to buy bait (check in to earn credits)"))
		default:
			g.writeError(w, http.StatusInternalServerError, "internal", "failed to start round")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"round_id": round.ID,
		"bait":     round.BaitTier,
		"price":    round.Price,
		"credits":  u.Credits - round.Price,
	})
}

// POST /api/me/games/fishing/settle — reveals the outcome and pays out
// exactly once. Idempotent: replaying the same round returns the stored
// result without touching balances again.
func (g *Gateway) handleFishingSettle(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	var body struct {
		RoundID string `json:"round_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.RoundID == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "round_id is required")
		return
	}
	res, _, err := g.Store.SettleFishingRound(u.ID, body.RoundID, time.Now())
	if err != nil {
		if errors.Is(err, db.ErrRoundNotFound) {
			g.writeError(w, http.StatusNotFound, "round_not_found", t(g.resolveLang(r), "对局不存在或不属于当前用户", "Round not found or not owned by you"))
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to settle round")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// GET /api/me/games/fishing/leaderboard?board=single|total — Top 20 plus the
// caller's own standing. single = all-time best fish size; total = rolling
// 30-day credits won.
func (g *Gateway) handleFishingLeaderboard(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	board := r.URL.Query().Get("board")
	if board == "" {
		board = "single"
	}
	if board != "single" && board != "total" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "board must be 'single' or 'total'")
		return
	}
	res, err := g.Store.FishingLeaderboard(db.GameIDFishing, board, u.ID, time.Now())
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to load leaderboard")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// PUT /api/me/games/anonymous — toggles the caller's leaderboard anonymity.
func (g *Gateway) handleGamesAnonymous(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	var body struct {
		Anonymous bool `json:"anonymous"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if err := g.Store.SetLeaderboardAnon(u.ID, body.Anonymous); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to save preference")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"anonymous": body.Anonymous})
}
