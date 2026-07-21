// Package web embeds the gateway's frontend single-page application.
package web

import "embed"

//go:embed static
var Static embed.FS
