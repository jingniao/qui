// Package web embeds the production UI into the panel executable.
package web

import (
	"embed"
	"io/fs"
)

// Build the frontend before building the panel binary. The wildcard also
// permits Go checks in a clean checkout containing only dist/.gitkeep.
//
//go:embed all:dist
var assets embed.FS

func Files() fs.FS {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
