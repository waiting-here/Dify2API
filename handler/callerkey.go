package handler

import (
	"encoding/json"
	"net/http"
)

// --- GET /api/caller-key ---
// Returns the session user's caller key (full value — this endpoint backs the
// UI's copy-to-clipboard button; the key is never rendered into HTML).
func (g *Gateway) handleGetCallerKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	key, err := g.Store.GetCallerKeyPlain(u.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var out interface{}
	if key != "" {
		out = key
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"key": out})
}

// --- POST /api/caller-key/reset ---
// Generates a fresh caller key (invalidating the previous one) and returns it.
func (g *Gateway) handleResetCallerKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	key, err := g.Store.SetCallerKey(u.ID)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"key": key})
}
