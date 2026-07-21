// Package auth implements Discord OAuth login and credential verification
// for the gateway's web frontend.
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIBase is the Discord API base URL (a variable so tests can stub it).
var APIBase = "https://discord.com/api"

// HTTPClient is used for Discord API calls (short timeout; login is interactive).
var HTTPClient = &http.Client{Timeout: 15 * time.Second}

// OAuthScopes requested at authorize time: identify (user id/name/avatar) and
// guilds.members.read (role check without a bot).
const OAuthScopes = "identify guilds.members.read"

// AuthorizeURL builds the Discord OAuth authorize URL.
func AuthorizeURL(clientID, redirectURI, state string) string {
	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("response_type", "code")
	v.Set("scope", OAuthScopes)
	v.Set("state", state)
	return "https://discord.com/oauth2/authorize?" + v.Encode()
}

// ExchangeCode trades an authorization code for an access token.
func ExchangeCode(clientID, clientSecret, redirectURI, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	resp, err := HTTPClient.PostForm(APIBase+"/oauth2/token", form)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("token exchange: status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("token exchange decode: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token exchange: empty access token")
	}
	return out.AccessToken, nil
}

// UserInfo is the Discord user identity.
type UserInfo struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
}

// DisplayName returns the best human-readable name.
func (u *UserInfo) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// FetchUser calls GET /users/@me with the access token.
func FetchUser(accessToken string) (*UserInfo, error) {
	var u UserInfo
	if err := getJSON(APIBase+"/users/@me", accessToken, &u); err != nil {
		return nil, err
	}
	if u.ID == "" {
		return nil, fmt.Errorf("discord returned empty user id")
	}
	return &u, nil
}

// HasGuildRole reports whether the user is a member of guildID holding roleID,
// using the user's own access token (scope guilds.members.read, no bot needed).
func HasGuildRole(accessToken, guildID, roleID string) (bool, error) {
	if guildID == "" || roleID == "" {
		return false, nil
	}
	var member struct {
		Roles []string `json:"roles"`
	}
	err := getJSON(fmt.Sprintf("%s/users/@me/guilds/%s/member", APIBase, guildID), accessToken, &member)
	if err != nil {
		// 404 = not a member of the guild.
		if strings.Contains(err.Error(), "status 404") {
			return false, nil
		}
		return false, err
	}
	for _, r := range member.Roles {
		if r == roleID {
			return true, nil
		}
	}
	return false, nil
}

func getJSON(endpoint, accessToken string, out interface{}) error {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord api: status %d: %s", resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
