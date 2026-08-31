// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package version

import (
	"regexp"
	"testing"
)

func TestResolve(t *testing.T) {
	// A release build sets the value at link time; keep it verbatim.
	if got := Resolve("OSMViews/0.7.3"); got != "OSMViews/0.7.3" {
		t.Errorf("Resolve(release) = %q, want it unchanged", got)
	}

	// Any other build appends the source revision, when the toolchain
	// recorded one (it does not for `go test` binaries, hence the
	// fallback to the bare base string).
	got := Resolve(Base)
	ok := got == Base ||
		regexp.MustCompile(`^OSMViews/git-[0-9a-f]{7,12}(-modified)?$`).MatchString(got)
	if !ok {
		t.Errorf("Resolve(%q) = %q, want %q or OSMViews/git-<rev>", Base, got, Base)
	}
}
