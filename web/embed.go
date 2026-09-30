// Package web embeds the built React app. Run `npm run build` first.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
