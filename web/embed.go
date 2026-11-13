// Package web embeds the Vite build from web/dist; without a frontend build it only holds .gitkeep.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
