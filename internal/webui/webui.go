// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

// Package webui embeds the built frontend single-page app that cmd/webserver
// serves under /beta/. The contents of dist/ are produced by "npm run build"
// (Vite) — locally, in CI, and in the Toolforge buildpack build before the Go
// build — and are not checked in; see dist/.gitignore.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built SPA assets, rooted at dist/. On a checkout where the
// frontend has not been built, dist/ holds only a placeholder and this FS has
// no index.html; callers must degrade gracefully (cmd/webserver serves a short
// "not built" page in that case).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a constant embed path; Sub cannot fail here
	}
	return sub
}
