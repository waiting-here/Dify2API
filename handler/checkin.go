package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"dify2api/db"
)

// POST /api/me/checkin — signs the user in for the day and awards random credits.
func (g *Gateway) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	// Administrators do not participate in the credits system.
	if u.IsAdmin {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "管理员不参与积分签到")
		return
	}

	// Calculate the check-in day in the configured timezone.
	tzOffset := g.Config.CheckinTZOffset
	today := time.Now().UTC().Add(time.Duration(tzOffset) * time.Hour).Format("2006-01-02")

	// Read check-in parameters from settings.
	checkinMin := g.Store.GetSettingInt(db.SettingCheckinMin, db.DefaultCheckinMin)
	checkinMax := g.Store.GetSettingInt(db.SettingCheckinMax, db.DefaultCheckinMax)
	creditsCap := g.Store.GetSettingInt(db.SettingCreditsCap, db.DefaultCreditsCap)

	// Re-read user to get the latest credits (avoid stale snapshot from currentUser).
	latest, err := g.Store.GetUserByID(u.ID)
	if err != nil || latest == nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to fetch user")
		return
	}

	// Cap check: when credits >= cap, refuse check-in.
	if latest.Credits >= creditsCap {
		g.writeError(w, http.StatusBadRequest, "credits_capped",
			g.Config.CreditsName+fmt.Sprintf("超过上限%d，无法签到", creditsCap))
		return
	}

	// Random bonus in [min, max].
	bonus := checkinMin
	if checkinMax > checkinMin {
		bonus = checkinMin + rand.Intn(checkinMax-checkinMin+1)
	}

	newCredits := latest.Credits + bonus
	ok, err := g.Store.SetUserCheckin(u.ID, today, newCredits)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if !ok {
		g.writeError(w, http.StatusBadRequest, "already_checked_in", "今日已签到")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"bonus":   bonus,
		"credits": newCredits,
	})
}
