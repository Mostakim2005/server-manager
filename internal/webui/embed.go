package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:static
var staticFS embed.FS

// Assets returns the embedded static frontend filesystem.
func Assets() (fs.FS, error) {
	return fs.Sub(staticFS, "static")
}

// FileServer serves embedded UI assets.
func FileServer() http.Handler {
	sub, err := Assets()
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}
