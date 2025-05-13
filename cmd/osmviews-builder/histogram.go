package main

import (
	"math"
	"math/rand"
)

// SharedTile keeps information about a tile that is used more than once.
// In our GeoTIFF, 93.1% of all tile offsets point to a shared tile.
// Usually these are patches of oceans or deserts into which no OSM user
// ever zooms deeply, so their view counts are identical across vast
// areas of land or water.
type SharedTile struct {
	UseCount    int         // Total number of tiles sharing this data.
	SampleTiles []TileIndex // A random sample of tiles that share this data.
}

type SharedTiles map[uint32]*SharedTile

// FindSharedTiles detects shared tiles from an array of tile offsets
// in a TIFF image. In our GeoTIFFs, about 93.1% of all tile offsets
// are getting shared. Typically these tiles are for deserts or oceans.
func findSharedTiles(tileOffsets []uint32) SharedTiles {
	shared := make(SharedTiles, 20)     // 16 for GeoTIFF of 2022-01-24
	uses := make(map[uint32]int, 80000) // 72138 for TIFF of 2022-01-24
	for _, off := range tileOffsets {
		uses[off] += 1
	}

	for off, n := range uses {
		if n > 1 {
			r := SharedTile{UseCount: n, SampleTiles: make([]TileIndex, 2000)}
			for i := 0; i < len(r.SampleTiles); i++ {
				r.SampleTiles[i] = -1
			}
			shared[off] = &r
		}
	}

	stride := 1 << (math.Ilogb(float64(len(tileOffsets))) / 2)
	for _, y := range rand.Perm(stride) {
		for x := 0; x < stride; x++ {
			tile := TileIndex(y*stride + x)
			off := tileOffsets[tile]
			if r, ok := shared[off]; ok {
				key := int(tile) % len(r.SampleTiles)
				if r.SampleTiles[key] < 0 || rand.Intn(50) == 0 {
					r.SampleTiles[key] = tile
				}
			}
		}
	}

	// If any slots are left unused, remove them.
	for _, st := range shared {
		j := 0
		for i := 0; i < len(st.SampleTiles); i++ {
			if st.SampleTiles[i] >= 0 {
				st.SampleTiles[j] = st.SampleTiles[i]
				j++
			}
		}
		st.SampleTiles = st.SampleTiles[0:j]
	}

	return shared
}
