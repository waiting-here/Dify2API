package handler

import (
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
		var rpm interface{}
		if u.RPMLimit.Valid {
			rpm = u.RPMLimit.Int64
		}
		out = append(out, map[string]interface{}{
			"id":           u.ID,
			"discord_id":   u.DiscordID,
			"username":     u.Username,
			"avatar":       u.Avatar,
			"disabled":     u.Disabled,
			"banned_until": u.BannedUntil,
			"banned":       db.IsBanned(u),
			"rpm_limit":    rpm,
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

// --- POST /api/admin/users/{id}/rpm ---
// Sets (limit >= 1) or clears (default=true) the per-user RPM override.
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
		Limit   int64 `json:"limit"`
		Default bool  `json:"default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Default {
		if err := g.Store.SetUserRPMLimit(id, nil); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	} else {
		if req.Limit < 1 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "limit must be >= 1 (or default=true)")
			return
		}
		if err := g.Store.SetUserRPMLimit(id, &req.Limit); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	}
	g.invalidateRPMCache(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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
		"guild_id":  guildID,
		"role_id":   roleID,
		"rpm_limit": g.Store.GetGlobalRPM(),
	})
}

// --- PUT /api/admin/settings ---
func (g *Gateway) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	if g.requireAdmin(r) == nil {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin only")
		return
	}
	var req struct {
		GuildID  string `json:"guild_id"`
		RoleID   string `json:"role_id"`
		RPMLimit *int   `json:"rpm_limit"`
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
	if req.RPMLimit != nil {
		if *req.RPMLimit < 1 {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "rpm_limit must be >= 1")
			return
		}
		if err := g.Store.SetSetting(db.SettingRPMLimit, strconv.Itoa(*req.RPMLimit)); err != nil {
			g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		// Global RPM changed — invalidate all cached entries so the next
		// request for every user re-reads the new default from the DB.
		g.limiter.rpmCacheMu.Lock()
		g.limiter.rpmCache = make(map[int64]int)
		g.limiter.rpmCacheMu.Unlock()
	}
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
