// SPDX-FileCopyrightText: 2025 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

func TestFindSharedTiles(t *testing.T) {
	// Tiles 1 and 3 share the same data offset.
	shared := findSharedTiles([]uint32{12, 72, 88, 72, 32, 18})
	if len(shared) != 1 {
		t.Fatalf("want len(shared) == 1, got %d", len(shared))
	}

	tile, got := shared[72]
	if !got {
		t.Fatalf("expected tile with offset=72 among shared tiles")
	}

	if tile.UseCount != 2 {
		t.Errorf("expected shared[72].UseCount=2, got %d", tile.UseCount)
	}

	samples := tile.SampleTiles
	found := false
	for _, tile := range samples {
		if tile == 1 || tile == 3 {
			found = true
		} else {
			t.Errorf("unexpected tile %d; samples=%v", tile, samples)
		}
		if !found {
			t.Errorf("expected 1 and/or 3; samples=%v", samples)
		}
	}
}
