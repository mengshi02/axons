// Package frontend provides embedded static files for the web UI.
package static

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded filesystem for the frontend dist directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
