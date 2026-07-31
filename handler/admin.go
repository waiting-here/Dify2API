package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dify2api/db"
	"dify2api/translator"
)

// Admin user-management endpoints (admin UI lands in T10; these are the
// reserved APIs). All require an admin session.

// --- POST /api/admin/users/{id}/ban ---
// Body: {"until": <unix seconds>} for a timed ban, or {"permanent": true}.
// Banning also invalidates all of the user's sessions.
func (g *Gateway) handleAdminBanUser(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	target, err := g.Store.GetUserByID(id)
	if err != nil || target == nil || target.IsAdmin {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}

	var req struct {
		Until     int64  `json:"until"`
		Permanent bool   `json:"permanent"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Permanent {
		if err := g.Store.SetUserDisabled(id, true, req.Reason); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	} else {
		if req.Until <= time.Now().Unix() {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "until must be a future unix timestamp (or set permanent=true)")
			return
		}
		if err := g.Store.BanUser(id, time.Unix(req.Until, 0), req.Reason); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	if err := g.Store.DeleteUserSessions(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- DELETE /api/admin/users/{id} ---
// Permanently deletes a user and ALL their records (sessions, configs, keys,
// logs). Unlike banning (which retains the row and blocks re-registration),
// a deleted user may register again via Discord OAuth.
func (g *Gateway) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	target, err := g.Store.GetUserByID(id)
	if err != nil || target == nil || target.IsAdmin {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err := g.Store.DeleteUser(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- GET /api/admin/users ---
// Lists all normal users (admin UI). Includes ban status for display.
func (g *Gateway) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	users, err := g.Store.ListUsers()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]interface{}{
			"id":              u.ID,
			"rpm_limit_a":     nullableInt(u.RPMLimitA),
			"rpm_limit_b":     nullableInt(u.RPMLimitB),
			"rpm_limit_c":     nullableInt(u.RPMLimitC),
			"credits":         u.Credits,
			"donation_credit": u.DonationCredit,
			"discord_id":      u.DiscordID,
			"username":        u.Username,
			"avatar":          u.Avatar,
			"disabled":        u.Disabled,
			"banned_until":    u.BannedUntil,
			"banned":          db.IsBanned(u),
			"created_at":      u.CreatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"users": out})
}

// --- POST /api/admin/users/{id}/reset-key ---
// Regenerates the target user's caller key (old key stops working). The new
// key is NOT shown to the admin — the user fetches it from their dashboard.
func (g *Gateway) handleAdminResetUserKey(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	target, err := g.Store.GetUserByID(id)
	if err != nil || target == nil || target.IsAdmin {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if _, err := g.Store.SetCallerKey(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// nullableInt converts sql.NullInt64 to a JSON-friendly value (nil = null).
func nullableInt(v sql.NullInt64) interface{} {
	if v.Valid {
		return v.Int64
	}
	return nil
}

// --- GET /api/admin/settings ---
func (g *Gateway) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	guildID, _ := g.Store.GetSetting(db.SettingGuildID)
	roleID, _ := g.Store.GetSetting(db.SettingRoleID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"guild_id":              guildID,
		"role_id":               roleID,
		"rpm_limit_a":           g.Store.GetSettingInt(db.SettingRPMLimitA, db.DefaultRPMLimitA),
		"rpm_limit_b":           g.Store.GetSettingInt(db.SettingRPMLimitB, db.DefaultRPMLimitB),
		"rpm_limit_c":           g.Store.GetSettingInt(db.SettingRPMLimitC, db.DefaultRPMLimitC),
		"rpm_violation_limit":   g.Store.GetSettingInt(db.SettingRPMViolationLimit, db.DefaultRPMViolationLimit),
		"rpm_ban_hours":         g.Store.GetSettingInt(db.SettingRPMBanHours, db.DefaultRPMBanHours),
		"probe_limit_per_user":  g.Store.GetSettingInt(db.SettingProbeLimitPerUser, db.DefaultProbeLimitPerUser),
		"checkin_min":           g.Store.GetSettingInt(db.SettingCheckinMin, db.DefaultCheckinMin),
		"checkin_max":           g.Store.GetSettingInt(db.SettingCheckinMax, db.DefaultCheckinMax),
		"credits_cap":           g.Store.GetSettingIntAllowZero(db.SettingCreditsCap, db.DefaultCreditsCap),
		"donation_fail_limit":   g.Store.GetSettingInt(db.SettingDonationFailLimit, db.DefaultDonationFailLimit),
		"donation_review_limit": g.Store.GetSettingInt(db.SettingDonationReviewLimit, db.DefaultDonationReviewLimit),
		"mailer_cool_minutes":   g.Store.GetSettingInt(db.SettingMailerCoolMinutes, db.DefaultMailerCoolMinutes),
		"donation_enabled":      g.Store.GetSettingString(db.SettingDonationEnabled, "") == "true",
		"charity_enabled":       g.Store.GetSettingString(db.SettingCharityEnabled, "") == "true",
		"maintenance_mode":      g.Store.GetSettingString(db.SettingMaintenanceMode, "") == "true",
	})
}

// --- PUT /api/admin/settings ---
func (g *Gateway) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		GuildID             string `json:"guild_id"`
		RoleID              string `json:"role_id"`
		RPMLimitA           *int   `json:"rpm_limit_a"`
		RPMLimitB           *int   `json:"rpm_limit_b"`
		RPMLimitC           *int   `json:"rpm_limit_c"`
		RPMViolationLimit   *int   `json:"rpm_violation_limit"`
		RPMBanHours         *int   `json:"rpm_ban_hours"`
		ProbeLimitPerUser   *int   `json:"probe_limit_per_user"`
		CheckinMin          *int   `json:"checkin_min"`
		CheckinMax          *int   `json:"checkin_max"`
		CreditsCap          *int   `json:"credits_cap"`
		DonationFailLimit   *int   `json:"donation_fail_limit"`
		DonationReviewLimit *int   `json:"donation_review_limit"`
		MailerCoolMinutes   *int   `json:"mailer_cool_minutes"`
		DonationEnabled     *bool  `json:"donation_enabled"`
		CharityEnabled      *bool  `json:"charity_enabled"`
		MaintenanceMode     *bool  `json:"maintenance_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := g.Store.SetSetting(db.SettingGuildID, req.GuildID); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := g.Store.SetSetting(db.SettingRoleID, req.RoleID); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// Optional RPM tunables: each must be >= 1 when present.
	for _, f := range []struct {
		val *int
		key string
	}{
		{req.RPMLimitA, db.SettingRPMLimitA},
		{req.RPMLimitB, db.SettingRPMLimitB},
		{req.RPMLimitC, db.SettingRPMLimitC},
		{req.RPMViolationLimit, db.SettingRPMViolationLimit},
		{req.RPMBanHours, db.SettingRPMBanHours},
		{req.ProbeLimitPerUser, db.SettingProbeLimitPerUser},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < 1 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", f.key+" must be >= 1")
			return
		}
		if err := g.Store.SetSetting(f.key, strconv.Itoa(*f.val)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	// Optional check-in tunables: each must be >= 1 when present.
	for _, f := range []struct {
		val *int
		key string
	}{
		{req.CheckinMin, db.SettingCheckinMin},
		{req.CheckinMax, db.SettingCheckinMax},
		{req.DonationFailLimit, db.SettingDonationFailLimit},
		{req.MailerCoolMinutes, db.SettingMailerCoolMinutes},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < 1 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", f.key+" must be >= 1")
			return
		}
		if err := g.Store.SetSetting(f.key, strconv.Itoa(*f.val)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	// credits_cap may be 0 (check-in disabled).
	if req.CreditsCap != nil {
		if *req.CreditsCap < 0 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "credits_cap must be >= 0")
			return
		}
		if err := g.Store.SetSetting(db.SettingCreditsCap, strconv.Itoa(*req.CreditsCap)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	// Donation review limit (optional, >= 0).
	if req.DonationReviewLimit != nil {
		if *req.DonationReviewLimit < 0 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "donation_review_limit must be >= 0")
			return
		}
		if err := g.Store.SetSetting(db.SettingDonationReviewLimit, strconv.Itoa(*req.DonationReviewLimit)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Donation enabled switch (optional bool).
	if req.DonationEnabled != nil {
		val := "false"
		if *req.DonationEnabled {
			val = "true"
		}
		if err := g.Store.SetSetting(db.SettingDonationEnabled, val); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Charity enabled switch (optional bool).
	if req.CharityEnabled != nil {
		val := "false"
		if *req.CharityEnabled {
			val = "true"
		}
		if err := g.Store.SetSetting(db.SettingCharityEnabled, val); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Maintenance mode switch (optional bool).
	if req.MaintenanceMode != nil {
		val := "false"
		if *req.MaintenanceMode {
			val = "true"
		}
		if err := g.Store.SetSetting(db.SettingMaintenanceMode, val); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Global limits may have changed — drop every cached per-user limit.
	g.invalidateRPMCache(0)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- POST /api/admin/users/{id}/rpm ---
// Sets or clears the per-user three-class RPM overrides. Each field is
// optional in the JSON body: a number >= 1 sets the override, null (or
// omitting the field) clears it back to the global default.
func (g *Gateway) handleAdminSetUserRPM(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	target, err := g.Store.GetUserByID(id)
	if err != nil || target == nil || target.IsAdmin {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	var req struct {
		LimitA *int `json:"rpm_limit_a"`
		LimitB *int `json:"rpm_limit_b"`
		LimitC *int `json:"rpm_limit_c"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	for _, v := range []*int{req.LimitA, req.LimitB, req.LimitC} {
		if v != nil && *v < 1 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "rpm limits must be >= 1 (or null to clear)")
			return
		}
	}
	if err := g.Store.SetUserRPMLimits(id, req.LimitA, req.LimitB, req.LimitC); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	g.invalidateRPMCache(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- POST /api/admin/users/{id}/unban ---
// Clears both the timed ban and the permanent disabled flag.
func (g *Gateway) handleAdminUnbanUser(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	target, err := g.Store.GetUserByID(id)
	if err != nil || target == nil || target.IsAdmin {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err := g.Store.UnbanUser(id); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- POST /api/admin/users/credits ---
// Batch credits operation (set/add/sub). Admin users are skipped.
func (g *Gateway) handleAdminBatchCredits(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		UserIDs []int64 `json:"user_ids"`
		Action  string  `json:"action"`
		Amount  int     `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Amount < 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "amount must be >= 0")
		return
	}
	if len(req.UserIDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "user_ids is required")
		return
	}
	if req.Action != "set" && req.Action != "add" && req.Action != "sub" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "action must be set/add/sub")
		return
	}

	updated := 0
	for _, uid := range req.UserIDs {
		u, err := g.Store.GetUserByID(uid)
		if err != nil || u == nil || u.IsAdmin {
			continue
		}
		switch req.Action {
		case "set":
			if err := g.Store.SetUserCredits(uid, req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		case "add":
			if _, err := g.Store.AdjustUserCredits(uid, req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		case "sub":
			if _, err := g.Store.AdjustUserCredits(uid, -req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "updated": updated})
}

// --- POST /api/admin/users/donation_credit ---
// Batch donation-credit operation (set/add/sub). Admin users are skipped.
func (g *Gateway) handleAdminBatchDonationCredit(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		UserIDs []int64 `json:"user_ids"`
		Action  string  `json:"action"`
		Amount  int     `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Amount < 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "amount must be >= 0")
		return
	}
	if len(req.UserIDs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "user_ids is required")
		return
	}
	if req.Action != "set" && req.Action != "add" && req.Action != "sub" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "action must be set/add/sub")
		return
	}

	updated := 0
	for _, uid := range req.UserIDs {
		u, err := g.Store.GetUserByID(uid)
		if err != nil || u == nil || u.IsAdmin {
			continue
		}
		switch req.Action {
		case "set":
			if err := g.Store.SetUserDonationCredit(uid, req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		case "add":
			if _, err := g.Store.AdjustUserDonationCredit(uid, req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		case "sub":
			if _, err := g.Store.AdjustUserDonationCredit(uid, -req.Amount); err != nil {
				g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			updated++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "updated": updated})
}

// --- Charity Pricing Admin Endpoints (beta.2) ---

// GET /api/admin/pricing — returns all pricing entries.
func (g *Gateway) handleListPricing(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	list, err := g.Store.ListPricing()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, p := range list {
		out = append(out, pricingJSON(p))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"pricing": out})
}

// PUT /api/admin/pricing — upsert a pricing entry.
func (g *Gateway) handleUpsertPricing(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		Service string `json:"service"`
		Model   string `json:"model"`
		Price   int    `json:"price"`
		Reward  *int   `json:"reward"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if !translator.IsSupportedService(req.Service) {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("不支持的服务 %q", req.Service))
		return
	}
	if strings.ContainsAny(req.Model, "[]") {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"模型名不得包含方括号")
		return
	}
	if req.Price < 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"price 必须 >= 0")
		return
	}
	if req.Reward != nil && *req.Reward < 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request",
			"reward 必须 >= 0")
		return
	}

	p, err := g.Store.UpsertPricing(req.Service, req.Model, req.Price, req.Reward)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"pricing": pricingJSON(p),
	})
}

// PATCH /api/admin/pricing — partial update pricing fields.
func (g *Gateway) handlePatchPricing(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		Service string `json:"service"`
		Model   string `json:"model"`
		Enabled *bool  `json:"enabled"`
		Price   *int   `json:"price"`
		Reward  *int   `json:"reward"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Service == "" || req.Model == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "service and model are required")
		return
	}

	p, err := g.Store.GetPricing(req.Service, req.Model)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if p == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "pricing entry not found")
		return
	}

	// Handle enabled toggle.
	if req.Enabled != nil {
		if err := g.Store.SetPricingEnabled(req.Service, req.Model, *req.Enabled); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Handle price/reward update (via UpsertPricing).
	if req.Price != nil || req.Reward != nil {
		price := p.Price
		var rewardPtr *int
		if req.Price != nil {
			if *req.Price < 0 {
				g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "price 必须 >= 0", "price must be >= 0"))
				return
			}
			price = *req.Price
		}
		if req.Reward != nil {
			if *req.Reward < 0 {
				g.writeError(w, http.StatusBadRequest, "invalid_request", t(g.resolveLang(r), "reward 必须 >= 0", "reward must be >= 0"))
				return
			}
			r := *req.Reward
			rewardPtr = &r
		} else {
			rewardPtr = &p.Reward
		}
		if _, err := g.Store.UpsertPricing(req.Service, req.Model, price, rewardPtr); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}

	// Re-fetch for accurate response.
	updated, err := g.Store.GetPricing(req.Service, req.Model)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"pricing": pricingJSON(updated),
	})
}

// DELETE /api/admin/pricing — delete a pricing entry.
func (g *Gateway) handleDeletePricing(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		Service string `json:"service"`
		Model   string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := g.Store.DeletePricing(req.Service, req.Model); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// POST /api/admin/pricing/delete/batch — batch delete pricing entries (7.3.1).
func (g *Gateway) handleBatchDeletePricing(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}

	var req struct {
		Pairs []struct {
			Service string `json:"service"`
			Model   string `json:"model"`
		} `json:"pairs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.Pairs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "pairs must be a non-empty array")
		return
	}

	// Atomic all-or-nothing: validate all pairs first.
	for _, pair := range req.Pairs {
		has, err := g.Store.HasDonationsForPair(pair.Service, pair.Model)
		if err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if has {
			writeBatchPairError(w,
				fmt.Sprintf("(%s, %s) 下存在捐赠条目，无法删除定价", pair.Service, pair.Model),
				pair.Service, pair.Model)
			return
		}
	}

	// All passed: delete each.
	for _, pair := range req.Pairs {
		if err := g.Store.DeletePricing(pair.Service, pair.Model); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal",
				fmt.Sprintf("删除定价 (%s, %s) 失败: %v", pair.Service, pair.Model, err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"count": len(req.Pairs),
	})
}

func pricingJSON(p *db.CharityPricing) map[string]interface{} {
	return map[string]interface{}{
		"service": p.Service,
		"model":   p.Model,
		"price":   p.Price,
		"reward":  p.Reward,
		"enabled": p.Enabled,
	}
}

// --- Anti-abuse admin endpoints ---

// GET /api/admin/anti-abuse — returns all services' anti-abuse configs.
func (g *Gateway) handleListAntiAbuse(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	configs := g.antiAbuseConfigList()
	list := make([]map[string]interface{}, 0, len(configs))
	for _, c := range configs {
		list = append(list, antiAbuseJSON(c))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"configs": list})
}

// PUT /api/admin/anti-abuse — batch upsert anti-abuse configs.
func (g *Gateway) handlePutAntiAbuse(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		Configs []struct {
			Service              string `json:"service"`
			Mode                 int    `json:"mode"`
			MinChars             int    `json:"min_chars"`
			PenaltyDeductCredits int    `json:"penalty_deduct_credits"`
			PenaltyBanHours      int    `json:"penalty_ban_hours"`
		} `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if len(req.Configs) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "configs must be a non-empty array")
		return
	}

	for _, c := range req.Configs {
		if !translator.IsSupportedService(c.Service) {
			g.writeError(w, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("不支持的服务 %q", c.Service))
			return
		}
		if _, err := g.Store.UpsertAntiAbuseConfig(c.Service, c.Mode, c.MinChars, c.PenaltyDeductCredits, c.PenaltyBanHours); err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	// Refresh cache so hot path sees the changes.
	g.refreshAntiAbuseCache()

	// Return updated list.
	configs := g.antiAbuseConfigList()
	list := make([]map[string]interface{}, 0, len(configs))
	for _, c := range configs {
		list = append(list, antiAbuseJSON(c))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"configs": list,
	})
}

func antiAbuseJSON(c *db.AntiAbuseConfig) map[string]interface{} {
	return map[string]interface{}{
		"service":                c.Service,
		"mode":                   c.Mode,
		"min_chars":              c.MinChars,
		"penalty_deduct_credits": c.PenaltyDeductCredits,
		"penalty_ban_hours":      c.PenaltyBanHours,
	}
}
