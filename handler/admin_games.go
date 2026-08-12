package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"dify2api/db"
)

// GET /api/admin/games — master switch + per-game switches and all economy
// parameters, plus the frozen defaults for the restore button.
func (g *Gateway) handleAdminGamesGet(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	fishing := g.Store.FishingConfig()
	defaults := db.DefaultFishingConfig()
	games := []map[string]interface{}{
		{
			"id":      db.GameIDFishing,
			"enabled": fishing.Enabled,
			"params": map[string]interface{}{
				"bait_worm_price":    fishing.WormPrice,
				"bait_lure_price":    fishing.LurePrice,
				"bait_premium_price": fishing.PremiumPrice,
				"rtp":                fishing.RTP,
				"rtp_premium":        fishing.RTPPremium,
				"treasure_bottle":    fishing.TreasureMult["bottle"],
				"treasure_clover":    fishing.TreasureMult["clover"],
				"treasure_shell":     fishing.TreasureMult["shell"],
			},
			"defaults": map[string]interface{}{
				"bait_worm_price":    defaults.WormPrice,
				"bait_lure_price":    defaults.LurePrice,
				"bait_premium_price": defaults.PremiumPrice,
				"rtp":                defaults.RTP,
				"rtp_premium":        defaults.RTPPremium,
				"treasure_bottle":    defaults.TreasureMult["bottle"],
				"treasure_clover":    defaults.TreasureMult["clover"],
				"treasure_shell":     defaults.TreasureMult["shell"],
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"master_enabled": g.Store.GamesMasterEnabled(),
		"games":          games,
	})
}

// PUT /api/admin/games — saves the master switch, per-game switches and
// economy parameters. All values are validated before any write.
func (g *Gateway) handleAdminGamesPut(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	var body struct {
		MasterEnabled *bool `json:"master_enabled"`
		Games         []struct {
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
			Params  *struct {
				BaitWormPrice    *int `json:"bait_worm_price"`
				BaitLurePrice    *int `json:"bait_lure_price"`
				BaitPremiumPrice *int `json:"bait_premium_price"`
				RTP              *int `json:"rtp"`
				RTPPremium       *int `json:"rtp_premium"`
				TreasureBottle   *int `json:"treasure_bottle"`
				TreasureClover   *int `json:"treasure_clover"`
				TreasureShell    *int `json:"treasure_shell"`
			} `json:"params"`
		} `json:"games"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	// Validate everything first; apply nothing on any error.
	for _, gm := range body.Games {
		if gm.ID != db.GameIDFishing {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "unknown game id")
			return
		}
		if gm.Params != nil {
			p := gm.Params
			for name, v := range map[string]*int{
				"bait_worm_price": p.BaitWormPrice, "bait_lure_price": p.BaitLurePrice,
				"bait_premium_price": p.BaitPremiumPrice, "rtp": p.RTP, "rtp_premium": p.RTPPremium,
				"treasure_bottle": p.TreasureBottle, "treasure_clover": p.TreasureClover,
				"treasure_shell": p.TreasureShell,
			} {
				if v == nil {
					continue
				}
				if *v < 1 || *v > 1000 {
					g.writeError(w, http.StatusBadRequest, "invalid_request",
						name+" must be between 1 and 1000")
					return
				}
			}
		}
	}
	// Merge every requested field in memory, validate the resulting economy,
	// then persist all switches and parameters in one settings transaction.
	// This prevents a valid master switch from being committed before a later
	// invalid economy field rejects the same request.
	cfg := g.Store.FishingConfig()
	masterEnabled := g.Store.GamesMasterEnabled()
	if body.MasterEnabled != nil {
		masterEnabled = *body.MasterEnabled
	}
	for _, gm := range body.Games {
		if gm.Enabled != nil {
			cfg.Enabled = *gm.Enabled
		}
		if gm.Params == nil {
			continue
		}
		p := gm.Params
		set := func(v *int, current int) int {
			if v != nil {
				return *v
			}
			return current
		}
		cfg.WormPrice = set(p.BaitWormPrice, cfg.WormPrice)
		cfg.LurePrice = set(p.BaitLurePrice, cfg.LurePrice)
		cfg.PremiumPrice = set(p.BaitPremiumPrice, cfg.PremiumPrice)
		cfg.RTP = set(p.RTP, cfg.RTP)
		cfg.RTPPremium = set(p.RTPPremium, cfg.RTPPremium)
		cfg.TreasureMult["bottle"] = set(p.TreasureBottle, cfg.TreasureMult["bottle"])
		cfg.TreasureMult["clover"] = set(p.TreasureClover, cfg.TreasureMult["clover"])
		cfg.TreasureMult["shell"] = set(p.TreasureShell, cfg.TreasureMult["shell"])
	}
	for _, bait := range []string{db.BaitWorm, db.BaitLure, db.BaitPremium} {
		if _, err := cfg.FishingEngine(bait); err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	updates := []db.SettingUpdate{
		{Key: db.SettingGamesEnabled, Value: boolStr(masterEnabled)},
		{Key: db.SettingGameFishingEnabled, Value: boolStr(cfg.Enabled)},
		{Key: db.SettingGameFishingBaitWormPrice, Value: itoa(cfg.WormPrice)},
		{Key: db.SettingGameFishingBaitLurePrice, Value: itoa(cfg.LurePrice)},
		{Key: db.SettingGameFishingBaitPremiumPrice, Value: itoa(cfg.PremiumPrice)},
		{Key: db.SettingGameFishingRTP, Value: itoa(cfg.RTP)},
		{Key: db.SettingGameFishingRTPPremium, Value: itoa(cfg.RTPPremium)},
		{Key: db.SettingGameFishingTreasureBottle, Value: itoa(cfg.TreasureMult["bottle"])},
		{Key: db.SettingGameFishingTreasureClover, Value: itoa(cfg.TreasureMult["clover"])},
		{Key: db.SettingGameFishingTreasureShell, Value: itoa(cfg.TreasureMult["shell"])},
	}
	if err := g.Store.SetSettings(updates); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// POST /api/admin/games/restore-defaults — one-click restore of the frozen
// defaults for every game (master switch on, fishing on, economy defaults).
func (g *Gateway) handleAdminGamesRestoreDefaults(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "admin login required")
		return
	}
	cfg := db.DefaultFishingConfig()
	updates := []db.SettingUpdate{
		{Key: db.SettingGamesEnabled, Value: "true"},
		{Key: db.SettingGameFishingEnabled, Value: "true"},
		{Key: db.SettingGameFishingBaitWormPrice, Value: itoa(cfg.WormPrice)},
		{Key: db.SettingGameFishingBaitLurePrice, Value: itoa(cfg.LurePrice)},
		{Key: db.SettingGameFishingBaitPremiumPrice, Value: itoa(cfg.PremiumPrice)},
		{Key: db.SettingGameFishingRTP, Value: itoa(cfg.RTP)},
		{Key: db.SettingGameFishingRTPPremium, Value: itoa(cfg.RTPPremium)},
		{Key: db.SettingGameFishingTreasureBottle, Value: itoa(cfg.TreasureMult["bottle"])},
		{Key: db.SettingGameFishingTreasureClover, Value: itoa(cfg.TreasureMult["clover"])},
		{Key: db.SettingGameFishingTreasureShell, Value: itoa(cfg.TreasureMult["shell"])},
	}
	if err := g.Store.SetSettings(updates); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
