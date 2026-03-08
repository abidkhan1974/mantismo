// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package dashboard embeds the React SPA and provides an HTTP handler that
// serves the single-page application. All routes not matching /api/* fall back
// to index.html for client-side routing.
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed ui/dist
var staticFiles embed.FS

// Handler returns an HTTP handler that serves the React SPA.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFiles, "ui/dist")
	if err != nil {
		panic("dashboard: " + err.Error())
	}
	return &spaHandler{fsys: sub}
}

type spaHandler struct {
	fsys fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if file exists; fall back to index.html for SPA routing.
	if _, err := fs.Stat(h.fsys, path); err != nil {
		path = "index.html"
	}

	http.ServeFileFS(w, r, h.fsys, path)
}
