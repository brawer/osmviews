// SPDX-FileCopyrightText: 2025 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTiffReader(t *testing.T) {
	file, err := os.Open(filepath.Join("testdata", "zurich_f32.tiff"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	r, err := NewTiffReader(file)
	if err != nil {
		t.Fatal(err)
	}

	if r.imageWidth != 512 || r.imageHeight != 512 {
		t.Errorf("got imageWidth=%d imageHeight=%d", r.imageWidth, r.imageHeight)
	}

	if r.tileWidth != 256 || r.tileHeight != 256 {
		t.Errorf("got tileWidth=%d tileHeight=%d", r.tileWidth, r.tileHeight)
	}

	numTiles := len(r.tileOffsets)
	if numTiles != 4 {
		t.Errorf("got %d tiles", numTiles)
	}

	if len(r.tileOffsets) != len(r.tileByteCounts) {
		t.Error("len(tileOffsets) should equal len(tileByteCounts")
	}

	if r.maxValue < 0.07098 || r.maxValue > 0.07099 {
		t.Errorf("got maxValue=%f", r.maxValue)
	}

	data := make([]float32, r.tileWidth*r.tileHeight)
	if err := r.ReadTile(1, data); err != nil {
		t.Fatal(err)
	}
}
