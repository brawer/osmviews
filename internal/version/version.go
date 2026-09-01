// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

// Package version resolves the build identifier that osmviews-builder and
// webserver report (in the GeoTIFF Software tag and the Server HTTP header).
package version

import "runtime/debug"

// Base is the value of the version variables before a release build
// overrides them with a linker flag.
const Base = "OSMViews"

// Resolve returns the identifier to report for v:
//
//   - a linker flag that set v at build time (v != Base) is used verbatim;
//   - otherwise, on a release build ([Release] is set) it returns
//     "OSMViews/v<release>", with the source revision appended as
//     SemVer build metadata when the toolchain recorded one, e.g.
//     "OSMViews/v0.1.2+1a2b3c4d5e6f" ("…-modified" if the tree was dirty);
//   - otherwise it returns "OSMViews/git-<revision>", or the bare Base
//     when no revision was recorded (e.g. a `go test` binary).
func Resolve(v string) string {
	return resolve(v, Release, revision())
}

func resolve(v, release, rev string) string {
	if v != Base {
		return v
	}
	if release != "" {
		if rev == "" {
			return v + "/v" + release
		}
		return v + "/v" + release + "+" + rev
	}
	if rev == "" {
		return v
	}
	return v + "/git-" + rev
}

// revision returns the source revision recorded in the build info, trimmed
// to 12 hex digits and suffixed with "-modified" when the working tree was
// dirty. It is "" if no revision was recorded.
func revision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
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
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-modified"
	}
	return rev
}
