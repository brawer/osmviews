// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

// Package version resolves the build identifier that osmviews-builder and
// webserver report (in the GeoTIFF Software tag and the Server HTTP header).
package version

import "runtime/debug"

// Base is the value of the version variables before a release build
// overrides them with a linker flag.
const Base = "OSMViews"

// Resolve returns v unchanged when a release build set it at link time
// (v != Base). Otherwise it appends the source revision recorded in the
// build info, e.g. "OSMViews/git-1a2b3c4d5e6f" or "…-modified"; if no
// revision was recorded, it returns Base.
func Resolve(v string) string {
	if v != Base {
		return v
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return v
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-modified"
	}
	return v + "/git-" + rev
}
