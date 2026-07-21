package auth

import (
	"net/http"
	"strings"
	"time"
)

// SessionCookieName is the name of the session cookie.
const SessionCookieName = "dify2api_session"

// SetSessionCookie writes the session cookie. Secure is set only when the
// site is served over HTTPS (so local HTTP deployments keep working).
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, siteBaseURL string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(siteBaseURL), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter, siteBaseURL string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(siteBaseURL), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionToken extracts the session token from the request cookie.
func SessionToken(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// OAuthStateCookieName is the name of the OAuth state binding cookie.
const OAuthStateCookieName = "dify2api_oauth_state"

// SetOAuthStateCookie writes a short-lived cookie that binds the OAuth state
// parameter to the browser that initiated the login flow (login-CSRF protection).
func SetOAuthStateCookie(w http.ResponseWriter, state string, siteBaseURL string) {
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes — matches server-side state TTL
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(siteBaseURL), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}

// OAuthStateFromRequest extracts the OAuth state binding cookie value.
func OAuthStateFromRequest(r *http.Request) string {
	c, err := r.Cookie(OAuthStateCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// ClearOAuthStateCookie expires the OAuth state binding cookie.
func ClearOAuthStateCookie(w http.ResponseWriter, siteBaseURL string) {
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(siteBaseURL), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}
