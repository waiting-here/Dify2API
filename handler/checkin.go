package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"dify2api/config"
	"dify2api/db"
)

// GET /api/me/checkin/status — returns today's check-in status.
func (g *Gateway) handleCheckinStatus(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}

	creditsCap := g.Store.GetSettingIntAllowZero(db.SettingCreditsCap, db.DefaultCreditsCap)
	if creditsCap == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"checked_in_today": true,
			"next_checkin_at":  9999999999,
		})
		return
	}

	tzOffset := g.Config.CheckinTZOffset
	now := time.Now().UTC().Add(time.Duration(tzOffset) * time.Hour)
	today := now.Format("2006-01-02")

	// Compute next check-in time: local midnight of the next day, converted to Unix.
	y, m, d := now.Date()
	nextMidnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Add(24 * time.Hour).Add(time.Duration(-tzOffset) * time.Hour)

	checkedIn := u.LastCheckinDay == today

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"checked_in_today": checkedIn,
		"next_checkin_at":  nextMidnight.Unix(),
	})
}

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
	creditsCap := g.Store.GetSettingIntAllowZero(db.SettingCreditsCap, db.DefaultCreditsCap)

	// credits_cap == 0 means check-in is globally disabled.
	if creditsCap == 0 {
		g.writeError(w, http.StatusBadRequest, "checkin_disabled", "签到系统未开放")
		return
	}

	// Re-read user to get the latest credits (avoid stale snapshot from currentUser).
	latest, err := g.Store.GetUserByID(u.ID)
	if err != nil || latest == nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "failed to fetch user")
		return
	}

	// Cap check: when credits >= cap, refuse check-in.
	if latest.Credits >= creditsCap {
		g.writeError(w, http.StatusBadRequest, "credits_capped",
			g.Config.I18N("credits_name", "zh", config.DefaultCreditsName)+fmt.Sprintf("超过上限%d，无法签到", creditsCap))
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
