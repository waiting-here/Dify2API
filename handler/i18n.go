package handler

import (
	"net/http"

	"dify2api/db"
)

// resolveLang determines the user's preferred language for a request.
// Priority: ?lang query param → logged-in user's Lang field → default "zh".
// This is the canonical language detection used by both page rendering and
// i18n error messages.
func (g *Gateway) resolveLang(r *http.Request) string {
	if q := r.URL.Query().Get("lang"); q == "en" || q == "zh" {
		return q
	}
	if u := g.currentUser(r); u != nil && u.Lang == "en" {
		return "en"
	}
	return "zh"
}

// userLang returns the preferred language of an API caller resolved from a
// caller key. /v1 requests carry no session cookie, so resolveLang cannot see
// the user; the caller-key user record is the authoritative source there.
func userLang(u *db.User) string {
	if u != nil && u.Lang == "en" {
		return "en"
	}
	return "zh"
}

// t is a package-level helper that returns the translated string for a given
// language code. When lang is "en", returns en; otherwise returns zh.
// Usage: t(g.resolveLang(r), "中文", "English")
func t(lang, zh, en string) string {
	if lang == "en" {
		return en
	}
	return zh
}
