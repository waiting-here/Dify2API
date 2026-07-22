package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"dify2api/db"
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
			"id":           u.ID,
			"rpm_limit_a":  nullableInt(u.RPMLimitA),
			"rpm_limit_b":  nullableInt(u.RPMLimitB),
			"rpm_limit_c":  nullableInt(u.RPMLimitC),
			"discord_id":   u.DiscordID,
			"username":     u.Username,
			"avatar":       u.Avatar,
			"disabled":     u.Disabled,
			"banned_until": u.BannedUntil,
			"banned":       db.IsBanned(u),
			"created_at":   u.CreatedAt,
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
		"guild_id":               guildID,
		"role_id":                roleID,
		"rpm_limit_a":            g.Store.GetSettingInt(db.SettingRPMLimitA, db.DefaultRPMLimitA),
		"rpm_limit_b":            g.Store.GetSettingInt(db.SettingRPMLimitB, db.DefaultRPMLimitB),
		"rpm_limit_c":            g.Store.GetSettingInt(db.SettingRPMLimitC, db.DefaultRPMLimitC),
		"rpm_violation_limit":    g.Store.GetSettingInt(db.SettingRPMViolationLimit, db.DefaultRPMViolationLimit),
		"rpm_ban_hours":          g.Store.GetSettingInt(db.SettingRPMBanHours, db.DefaultRPMBanHours),
		"charity_global_enabled": g.Store.GetSettingString(db.SettingCharityGlobalEnabled, "") == "true",
	})
}

// --- PUT /api/admin/settings ---
func (g *Gateway) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		GuildID              string `json:"guild_id"`
		RoleID               string `json:"role_id"`
		RPMLimitA            *int   `json:"rpm_limit_a"`
		RPMLimitB            *int   `json:"rpm_limit_b"`
		RPMLimitC            *int   `json:"rpm_limit_c"`
		RPMViolationLimit    *int   `json:"rpm_violation_limit"`
		RPMBanHours          *int   `json:"rpm_ban_hours"`
		CharityGlobalEnabled *bool  `json:"charity_global_enabled"`
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
	// Charity global switch (optional bool).
	if req.CharityGlobalEnabled != nil {
		val := "false"
		if *req.CharityGlobalEnabled {
			val = "true"
		}
		if err := g.Store.SetSetting(db.SettingCharityGlobalEnabled, val); err != nil {
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
