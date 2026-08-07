package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dify2api/auth"
	"dify2api/db"
)

const oauthStateTTL = 10 * time.Minute

// maxLoginUsernameLen bounds the username used as part of the login-throttle
// map key. Without a cap, an attacker could fill the in-memory failure map
// with near-body-sized keys (body limit 256 KiB) at the webThrottle request
// rate, driving multi-hundred-MB retention per source IP within the failure
// window. Truncation (not rejection) keeps a uniform error path and can only
// make throttling stricter for colliding names.
const maxLoginUsernameLen = 128

// newOAuthState returns a self-contained, authenticated state token. Keeping
// the state in the short-lived browser cookie avoids an attacker-controlled
// server-side map while the HMAC and timestamp prevent tampering and stale
// manual replays. Rotating the Discord client secret invalidates in-flight
// login attempts, which is an acceptable fail-closed behaviour.
func (g *Gateway) newOAuthState(now time.Time) (string, error) {
	payload := make([]byte, 8+16)
	binary.BigEndian.PutUint64(payload[:8], uint64(now.Unix()))
	if _, err := rand.Read(payload[8:]); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, g.oauthStateKey())
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func (g *Gateway) validOAuthState(state string, now time.Time) bool {
	if len(state) > 128 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil || len(raw) != 8+16+sha256.Size {
		return false
	}
	payload, suppliedMAC := raw[:24], raw[24:]
	mac := hmac.New(sha256.New, g.oauthStateKey())
	_, _ = mac.Write(payload)
	if !hmac.Equal(suppliedMAC, mac.Sum(nil)) {
		return false
	}
	issued := time.Unix(int64(binary.BigEndian.Uint64(payload[:8])), 0)
	return !issued.After(now.Add(time.Minute)) && now.Sub(issued) <= oauthStateTTL
}

func (g *Gateway) oauthStateKey() []byte {
	sum := sha256.Sum256([]byte("dify2api/oauth-state/v1\x00" + g.Config.Admin.DiscordClientSecret))
	return sum[:]
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
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
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
	// The username is bounded before entering the in-memory throttle key
	// (memory-DoS defence; see maxLoginUsernameLen).
	if len(req.Username) > maxLoginUsernameLen {
		req.Username = req.Username[:maxLoginUsernameLen]
	}
	key := g.clientIP(r) + "|" + req.Username
	now := time.Now()
	if g.loginThrottle.locked(key, now) {
		g.writeError(w, http.StatusForbidden, "login_locked", t(g.resolveLang(r), "尝试次数过多，请 15 分钟后再试", "Too many attempts, please try again in 15 minutes"))
		return
	}

	if req.Username != g.Config.Admin.Username || !auth.VerifyPassword(g.Config.Admin.Password, req.Password) {
		if justLocked := g.loginThrottle.fail(key, now); justLocked {
			lockUntil := now.Add(g.loginThrottle.lockDur)
			log.Printf("[AUTH] admin login locked for %s until %v (too many failures)", key, lockUntil)
			if g.mailer != nil {
				g.mailer.AdminLoginLocked(g.clientIP(r), lockUntil)
			}
			g.writeError(w, http.StatusForbidden, "login_locked", t(g.resolveLang(r), "尝试次数过多，请 15 分钟后再试", "Too many attempts, please try again in 15 minutes"))
			return
		}
		g.writeError(w, http.StatusUnauthorized, "invalid_credentials", t(g.resolveLang(r), "用户名或密码错误", "Invalid username or password"))
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
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
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
	level := g.resolveLevelView(u)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":              u.ID,
		"username":        u.Username,
		"avatar":          u.Avatar,
		"is_admin":        u.IsAdmin,
		"credits":         u.Credits,
		"donation_credit": u.DonationCredit,
		"lang":            u.Lang,
		"level":           level.Level,
		"level_name":      level.Name,
		"level_manual":    level.Manual,
		"banner_text":     level.BannerText,
	})
}

// PUT /api/me/lang — updates the user's preferred UI language.
func (g *Gateway) handleSetLang(w http.ResponseWriter, r *http.Request) {
	u := g.currentUser(r)
	if u == nil {
		g.writeError(w, http.StatusUnauthorized, "unauthorized", "not logged in")
		return
	}
	var req struct {
		Lang string `json:"lang"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Lang != "zh" && req.Lang != "en" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "lang must be 'zh' or 'en'")
		return
	}
	if err := g.Store.SetUserLang(u.ID, req.Lang); err != nil {
		g.writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "lang": req.Lang})
}

// --- GET /auth/discord/login ---
func (g *Gateway) handleDiscordLogin(w http.ResponseWriter, r *http.Request) {
	state, err := g.newOAuthState(time.Now())
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
	cookieState := auth.OAuthStateFromRequest(r)
	auth.ClearOAuthStateCookie(w, g.Config.Admin.SiteBaseURL)
	// Login-CSRF hardening: bind the callback to the initiating browser and
	// authenticate the timestamped state without retaining global state.
	if cookieState == "" || len(cookieState) > 128 || len(queryState) > 128 || len(cookieState) != len(queryState) ||
		subtle.ConstantTimeCompare([]byte(cookieState), []byte(queryState)) != 1 ||
		!g.validOAuthState(queryState, time.Now()) {
		fail("登录会话已过期，请返回重试。")
		return
	}

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

	// On first login (lang is empty), auto-detect language from the browser's
	// Accept-Language header.  Fall back to "en" when unsupported or absent.
	if user.Lang == "" {
		detected := "en"
		if al := r.Header.Get("Accept-Language"); al != "" {
			if strings.HasPrefix(strings.ToLower(al), "zh") {
				detected = "zh"
			}
		}
		if err := g.Store.SetUserLang(user.ID, detected); err != nil {
			log.Printf("[WARN] set initial lang for user %d: %v", user.ID, err)
		} else {
			user.Lang = detected
			log.Printf("[AUTH] auto-detected lang=%s for user %d", detected, user.ID)
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
