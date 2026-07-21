package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"dify2api/auth"
)

// --- GET /api/me/export ---
// Self-service data export (GDPR right of access / data portability).
// Returns a JSON file download with all user data, including decrypted
// Dify API keys and caller key. Sessions are excluded.
//
// DESIGN NOTE: decrypted credentials are intentionally included so the user
// can migrate to another platform (GDPR Art. 20 "data portability" requires
// machine-readable, commonly-used formats).  This means a stolen session
// cookie (despite HttpOnly + Secure + SameSite) would grant access to all
// stored secrets.  Deployers should therefore enforce HTTPS and keep
// session TTL reasonable.
func (g *Gateway) handleMeExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	bundle, err := g.Store.ExportUserData(u.ID)
	if err != nil {
		log.Printf("[ERROR] export user %d: %v", u.ID, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "export failed")
		return
	}
	if bundle == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}

	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "marshal failed")
		return
	}

	filename := fmt.Sprintf("dify2api-export-%d-%s.json", u.ID, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// --- DELETE /api/me ---
// Self-service account deletion (GDPR right to erasure).
// Requires ?confirm=DELETE for double confirmation. On success the session
// is invalidated and the client is expected to redirect to the login page.
func (g *Gateway) handleMeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	if u.IsAdmin {
		g.writeError(w, http.StatusForbidden, "forbidden", "admin accounts cannot be self-deleted; use the startup file to change credentials")
		return
	}

	// Double-confirmation guard: the first call returns instructions; the
	// second call with ?confirm=DELETE actually performs the deletion.
	if r.URL.Query().Get("confirm") != "DELETE" {
		g.writeError(w, http.StatusBadRequest, "confirmation_required",
			"此操作不可撤销。请附加 ?confirm=DELETE 以确认删除您的账号及全部数据。")
		return
	}

	if err := g.Store.DeleteUser(u.ID); err != nil {
		log.Printf("[ERROR] self-delete user %d: %v", u.ID, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "deletion failed")
		return
	}

	// Invalidate session — the cookie is still present but the token is gone.
	if token := auth.SessionToken(r); token != "" {
		g.Store.DeleteSession(token)
	}
	auth.ClearSessionCookie(w, g.Config.Admin.SiteBaseURL)

	log.Printf("[AUTH] user %d (%s) self-deleted their account", u.ID, u.Username)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "您的账号及全部数据已永久删除。感谢使用 Dify2API。",
	})
}

// --- GET /api/admin/users/{id}/export ---
// Admin export of a single user's data (GDPR compliance — allows an admin to
// fulfil a data-portability request on behalf of a user).
func (g *Gateway) handleAdminExportUser(w http.ResponseWriter, r *http.Request) {
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

	bundle, err := g.Store.ExportUserData(id)
	if err != nil {
		log.Printf("[ERROR] admin export user %d: %v", id, err)
		g.writeError(w, http.StatusInternalServerError, "internal", "export failed")
		return
	}
	if bundle == nil {
		g.writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}

	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "marshal failed")
		return
	}

	filename := fmt.Sprintf("dify2api-export-%d-%s.json", id, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
