// Package api provides frontend serving handlers.
package api

import (
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/api/static"
)

// FrontendHandler returns an HTTP handler for serving the embedded frontend.
func FrontendHandler() http.Handler {
	distFS, err := static.DistFS()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Frontend not available", http.StatusServiceUnavailable)
		})
	}

	return http.FileServer(http.FS(distFS))
}

// RegisterFrontendRoutes registers routes for serving the frontend SPA.
// This should be called after all API routes are registered.
func (s *Server) RegisterFrontendRoutes() {
	distFS, err := static.DistFS()
	if err != nil {
		return
	}

	// Serve static assets
	s.router.GET("/assets/*filepath", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		// Strip /assets prefix and serve from dist/assets
		path := strings.TrimPrefix(r.URL.Path, "/assets/")
		file, err := distFS.Open("assets/" + path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		http.ServeContent(w, r, path, stat.ModTime(), file.(io.ReadSeeker))
	})

	// Serve favicon and icons
	s.router.GET("/favicon.svg", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		serveFile(w, r, distFS, "favicon.svg")
	})

	s.router.GET("/icons.svg", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		serveFile(w, r, distFS, "icons.svg")
	})

	// Serve plugin-sdk static files (CSS/JS for plugin developers)
	// Route: /plugin-sdk/*filepath → dist/plugin-sdk/*
	s.router.GET("/plugin-sdk/*filepath", func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		filePath := ps.ByName("filepath")
		if filePath == "" || filePath == "/" {
			http.NotFound(w, r)
			return
		}
		// Strip leading slash
		filePath = strings.TrimPrefix(filePath, "/")
		serveFile(w, r, distFS, "plugin-sdk/"+filePath)
	})

	// Serve vendor ESM shims (import-map bridge for plugin bare specifiers)
	// Route: /vendor/*filepath → dist/vendor/* (e.g. /vendor/react.js → dist/vendor/react.js)
	s.router.GET("/vendor/*filepath", func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		filePath := ps.ByName("filepath")
		if filePath == "" || filePath == "/" {
			http.NotFound(w, r)
			return
		}
		filePath = strings.TrimPrefix(filePath, "/")
		serveFile(w, r, distFS, "vendor/"+filePath)
	})

	// Serve index.html for all other routes (SPA fallback)
	s.router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't intercept API routes
		if strings.HasPrefix(r.URL.Path, "/api/") ||
			strings.HasPrefix(r.URL.Path, "/v1/") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		// Serve index.html for SPA
		serveFile(w, r, distFS, "index.html")
	})
}

// serveFile serves a single file from the embedded filesystem.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, path string) {
	file, err := fsys.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	content, err := fs.ReadFile(fsys, path)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set content type based on file extension
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}

	w.Write(content)
}