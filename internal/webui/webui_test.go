// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package webui

import (
	"io/fs"
	"testing"
)

// FS must always be usable, even on a checkout where the frontend has not been
// built (dist/ then holds only the placeholder .gitignore).
func TestFS(t *testing.T) {
	f := FS()
	if _, err := fs.Stat(f, "."); err != nil {
		t.Fatalf("stat root: %v", err)
	}
	// index.html may or may not be present depending on whether "npm run build"
	// ran; either way, reading a clearly absent asset must be a fs.ErrNotExist.
	if _, err := fs.ReadFile(f, "assets/does-not-exist.js"); err == nil {
		t.Error("expected an error reading a nonexistent asset")
	}
}
