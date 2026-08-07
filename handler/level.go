package handler

import (
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"dify2api/db"
)

// R-A user-level helpers shared by the admin API, the user API and the
// check-in privilege. Levels are computed lazily at read time (never stored
// for automatic users), so threshold or donation-credit changes take effect
// immediately; level 5 is manual-only.

const (
	// maxLevelNameLen bounds a custom level name (in runes).
	maxLevelNameLen = 20
	// maxLevelBannerLen bounds the level banner text (in runes).
	maxLevelBannerLen = 200
)

// levelView is the level-related subset exposed to API clients.
type levelView struct {
	Level      int    // effective level 1-5
	Manual     bool   // true when a manual override is in force
	Name       string // custom name, or the numeric level when unset
	BannerText string // configured banner ("" = no banner)
}

// resolveLevelView assembles the level-related fields for a user by reading
// the current thresholds/names/banner settings.
func (g *Gateway) resolveLevelView(u *db.User) levelView {
	th := g.Store.LevelThresholds()
	level, manual := db.GetUserLevel(u, th)
	name := g.Store.GetSettingString(db.LevelNameKeys[level-1], "")
	if name == "" {
		name = strconv.Itoa(level)
	}
	return levelView{
		Level:      level,
		Manual:     manual,
		Name:       name,
		BannerText: g.Store.GetSettingString(db.SettingLevelBannerText, ""),
	}
}

// checkinCapExempt reports whether the user's effective level grants the
// level-3 check-in privilege (bypassing the credits >= cap refusal when
// credits_cap > 0). credits_cap == 0 remains a global check-in shutdown that
// no level can bypass — that check lives in the handlers.
func (g *Gateway) checkinCapExempt(u *db.User) bool {
	level, _ := db.GetUserLevel(u, g.Store.LevelThresholds())
	return level >= 3
}

// levelSettingsJSON reads the nine R-A level settings (with defaults) in the
// wire format of GET/PUT /api/admin/level-settings.
func (g *Gateway) levelSettingsJSON() map[string]interface{} {
	th := g.Store.LevelThresholds()
	out := map[string]interface{}{
		"threshold_2": th.T2,
		"threshold_3": th.T3,
		"threshold_4": th.T4,
		"banner_text": g.Store.GetSettingString(db.SettingLevelBannerText, ""),
	}
	for i, key := range db.LevelNameKeys {
		out["name_"+strconv.Itoa(i+1)] = g.Store.GetSettingString(key, "")
	}
	return out
}

// validateLevelText enforces the level-setting text rules: bounded length
// (runes) and no control characters.
func validateLevelText(field, v string, maxRunes int) error {
	if utf8.RuneCountInString(v) > maxRunes {
		return fmt.Errorf("%s too long (max %d characters)", field, maxRunes)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains control characters", field)
		}
	}
	return nil
}
