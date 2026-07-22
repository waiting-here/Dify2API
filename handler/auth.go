package handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

// oauthStates tracks in-flight OAuth state parameters (CSRF protection).
var oauthStates = struct {
	sync.Mutex
	m map[string]time.Time
}{m: make(map[string]time.Time)}

func newOAuthState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := fmt.Sprintf("%x", raw)
	oauthStates.Lock()
	defer oauthStates.Unlock()
	// Purge expired entries (>10 min) opportunistically.
	for s, exp := range oauthStates.m {
		if time.Now().After(exp) {
			delete(oauthStates.m, s)
		}
	}
	oauthStates.m[state] = time.Now().Add(10 * time.Minute)
	return state, nil
}

func consumeOAuthState(state string) bool {
	oauthStates.Lock()
	defer oauthStates.Unlock()
	exp, ok := oauthStates.m[state]
	if !ok || time.Now().After(exp) {
		return false
	}
	delete(oauthStates.m, state)
	return true
}

// currentUser resolves the session cookie to a live, non-disabled user.
func (g *Gateway) currentUser(r *http.Request) *db.User {
	token := auth.SessionToken(r)
	if token == "" {
		return nil
	}
	u, err := g.Store.GetSessionUser(token)
	if err != nil || u == nil || db.IsBanned(u) {
		return nil
	}
	return u
}

// --- POST /api/auth/admin/login ---
func (g *Gateway) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// L1: constant minimum latency for every attempt (masks timing and caps
	// the practical attempt rate).
	started := time.Now()
	minLatency := g.loginThrottle.minLatency
	defer func() {
		if d := time.Since(started); d < minLatency {
			time.Sleep(minLatency - d)
		}
	}()

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	// L2: temporary lock after repeated failures (per ip+username).
	key := clientIP(r) + "|" + req.Username
	now := time.Now()
	if g.loginThrottle.locked(key, now) {
		g.writeError(w, http.StatusForbidden, "login_locked", "尝试次数过多，请 15 分钟后再试")
		return
	}

	if req.Username != g.Config.Admin.Username || !auth.VerifyPassword(g.Config.Admin.Password, req.Password) {
		if justLocked := g.loginThrottle.fail(key, now); justLocked {
			log.Printf("[AUTH] admin login locked for %s until %v (too many failures)", key, now.Add(g.loginThrottle.lockDur))
			g.writeError(w, http.StatusForbidden, "login_locked", "尝试次数过多，请 15 分钟后再试")
			return
		}
		g.writeError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}
	g.loginThrottle.succeed(key)

	adminUser, err := g.Store.EnsureAdminUser(g.Config.Admin.Username)
	if err != nil {
		log.Printf("[ERROR] ensure admin user: %v", err)
		g.writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	g.issueSession(w, adminUser)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- POST /api/auth/logout ---
func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if token := auth.SessionToken(r); token != "" {
		g.Store.DeleteSession(token)
	}
	auth.ClearSessionCookie(w, g.Config.Admin.SiteBaseURL)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// --- GET /api/me ---
func (g *Gateway) handleMe(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       u.ID,
		"username": u.Username,
		"avatar":   u.Avatar,
		"is_admin": u.IsAdmin,
	})
}

// --- GET /auth/discord/login ---
func (g *Gateway) handleDiscordLogin(w http.ResponseWriter, r *http.Request) {
	state, err := newOAuthState()
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	auth.SetOAuthStateCookie(w, state, g.Config.Admin.SiteBaseURL)
	redirectURI := g.Config.Admin.SiteBaseURL + "/auth/discord/callback"
	http.Redirect(w, r, auth.AuthorizeURL(g.Config.Admin.DiscordClientID, redirectURI, state), http.StatusFound)
}

// --- GET /auth/discord/callback ---
func (g *Gateway) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		log.Printf("[AUTH] discord login failed: %s", msg)
		q := url.Values{"reason": {msg}}
		http.Redirect(w, r, "/403?"+q.Encode(), http.StatusFound)
	}

	queryState := r.URL.Query().Get("state")
	if !consumeOAuthState(queryState) {
		fail("登录会话已过期，请返回重试。")
		return
	}
	// Login-CSRF hardening: the OAuth state must also match the cookie set
	// when the login flow started.  This prevents an attacker from
	// tricking a victim into logging into the attacker's account.
	cookieState := auth.OAuthStateFromRequest(r)
	if cookieState == "" || cookieState != queryState {
		auth.ClearOAuthStateCookie(w, g.Config.Admin.SiteBaseURL)
		fail("登录会话已过期，请返回重试。")
		return
	}
	auth.ClearOAuthStateCookie(w, g.Config.Admin.SiteBaseURL)

	code := r.URL.Query().Get("code")
	if code == "" {
		fail("Discord 授权失败：未收到授权码。")
		return
	}

	redirectURI := g.Config.Admin.SiteBaseURL + "/auth/discord/callback"
	token, err := auth.ExchangeCode(g.Config.Admin.DiscordClientID, g.Config.Admin.DiscordClientSecret, redirectURI, code)
	if err != nil {
		fail("Discord 授权失败，请检查 Discord Application 配置。")
		return
	}
	info, err := auth.FetchUser(token)
	if err != nil {
		fail("无法获取 Discord 用户信息，请重试。")
		return
	}

	user, err := g.Store.GetUserByDiscordID(info.ID)
	if err != nil {
		fail("服务器内部错误，请稍后重试。")
		return
	}

	if user == nil {
		// Registration attempt: must hold the configured guild role.
		guildID, _ := g.Store.GetSetting(db.SettingGuildID)
		roleID, _ := g.Store.GetSetting(db.SettingRoleID)
		ok, err := auth.HasGuildRole(token, guildID, roleID)
		if err != nil {
			fail("无法验证 Discord 服务器成员身份，请检查配置。")
			return
		}
		if !ok {
			fail("未满足注册条件（需要指定服务器的指定身份组）")
			return
		}
		user, err = g.Store.CreateUser(info.ID, info.DisplayName(), info.Avatar)
		if err != nil {
			fail("服务器内部错误，请稍后重试。")
			return
		}
		log.Printf("[AUTH] registered new user %s (%s)", user.Username, user.DiscordID)
	}
	if db.IsBanned(user) {
		// Redirect to the public 403 page with ban details so the banned
		// user can see the reason, duration, and contact info.
		q := make(url.Values)
		q.Set("banned", "1")
		if user.Disabled {
			q.Set("permanent", "1")
		} else if user.BannedUntil > 0 {
			q.Set("until", strconv.FormatInt(user.BannedUntil, 10))
		}
		if user.BanReason != "" {
			q.Set("reason", user.BanReason)
		}
		http.Redirect(w, r, "/403?"+q.Encode(), http.StatusFound)
		return
	}

	// Refresh username/avatar from Discord on every login (the user may have
	// changed their name or avatar on Discord since the last login).
	discordName := info.DisplayName()
	discordAvatar := info.Avatar
	if user.Username != discordName || user.Avatar != discordAvatar {
		if err := g.Store.UpdateUserProfile(user.ID, discordName, discordAvatar); err != nil {
			log.Printf("[WARN] refresh user profile (user %d): %v", user.ID, err)
		}
	}

	// Auto-provision a caller key on first use (registration or first login
	// after this version) so the dashboard's copy button always has a key.
	if ok, _ := g.Store.CallerKeyExists(user.ID); !ok {
		if _, err := g.Store.SetCallerKey(user.ID); err != nil {
			log.Printf("[WARN] auto-provision caller key (user %d): %v", user.ID, err)
		}
	}

	g.issueSession(w, user)
	http.Redirect(w, r, "/", http.StatusFound)
}

// requireAdmin resolves the session and demands an admin user.
func (g *Gateway) requireAdmin(r *http.Request) *db.User {
	u := g.currentUser(r)
	if u == nil || !u.IsAdmin {
		return nil
	}
	return u
}

// issueSession creates a session and sets the cookie.
func (g *Gateway) issueSession(w http.ResponseWriter, u *db.User) {
	token, expiresAt, err := g.Store.CreateSession(u.ID)
	if err != nil {
		log.Printf("[ERROR] create session: %v", err)
		return
	}
	auth.SetSessionCookie(w, token, expiresAt, g.Config.Admin.SiteBaseURL)
}
