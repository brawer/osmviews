// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"math/rand"
	"testing"
)

// syntheticLog builds a zoom-12 tile log with densely viewed "land",
// random noise, and lanes radiating from the given centers.
func syntheticLog(rng *rand.Rand, centers []Point, lanesPerCenter []int) []Tile {
	const size = 4096
	occupied := make(map[[2]uint32]uint64)
	add := func(x, y float64, n uint64) {
		if x < 0 || y < 0 || x >= size || y >= size {
			return
		}
		occupied[[2]uint32{uint32(x), uint32(y)}] += n
	}
	disk := func(c Point, r float64, n uint64) {
		for y := c.Y - r; y <= c.Y+r; y++ {
			for x := c.X - r; x <= c.X+r; x++ {
				if math.Hypot(x-c.X, y-c.Y) <= r {
					add(x, y, n)
				}
			}
		}
	}

	// Land, viewed nearly everywhere.
	for range 6 {
		disk(Point{rng.Float64() * size, rng.Float64() * size}, 80+rng.Float64()*80, 500)
	}
	// Sparse noise.
	for range 15000 {
		add(rng.Float64()*size, rng.Float64()*size, 10+uint64(rng.Intn(20)))
	}
	// Lanes, starting outside each center's dense halo.
	for ci, c := range centers {
		disk(c, 12, 5000)
		for range lanesPerCenter[ci] {
			angle := rng.Float64() * 2 * math.Pi
			dir := Point{math.Cos(angle), math.Sin(angle)}
			length := 400 + rng.Float64()*1100
			for t := 20.0; t < length; t += 0.5 {
				if rng.Float64() < 0.15 {
					continue // tile fell below the log's threshold
				}
				q := c.Add(dir.Scale(t))
				add(q.X, q.Y, 12)
			}
		}
	}

	tiles := make([]Tile, 0, len(occupied))
	for k, n := range occupied {
		tiles = append(tiles, Tile{X: k[0], Y: k[1], Count: n})
	}
	return tiles
}

func TestFindLanes_Synthetic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	truth := []Point{{2048, 2048}, {900, 1400}}
	numLanes := []int{14, 6}
	tiles := syntheticLog(rng, truth, numLanes)

	res := FindLanes(12, tiles, DefaultParams())
	for i, want := range truth {
		var found *Center
		for _, c := range res.Centers {
			if c.P.Dist(want) < 3 {
				found = c
			}
		}
		if found == nil {
			t.Errorf("center %d at %v not found; got %d centers", i, want, len(res.Centers))
			for _, c := range res.Centers {
				t.Logf("    center at %v with %d lanes", c.P, len(c.Lanes))
			}
			continue
		}
		if got := len(found.Rays); got < numLanes[i]*8/10 || got > numLanes[i] {
			t.Errorf("center %d: got %d rays, want about %d", i, got, numLanes[i])
		}
	}
	if len(res.Centers) != len(truth) {
		t.Errorf("got %d centers, want %d", len(res.Centers), len(truth))
	}
}

func TestFindLanes_NoLanes(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	tiles := syntheticLog(rng, nil, nil)
	res := FindLanes(12, tiles, DefaultParams())
	if len(res.Lanes) != 0 || len(res.Centers) != 0 {
		t.Errorf("got %d lanes and %d centers in pure noise, want none", len(res.Lanes), len(res.Centers))
	}
}

func TestToLatLng(t *testing.T) {
	for _, tc := range []struct {
		p        Point
		lat, lng float64
	}{
		{Point{2048, 2048}, 0, 0},
		{Point{0, 2048}, 0, -180},
		{Point{4096, 0}, 85.0511, 180},
	} {
		lat, lng := ToLatLng(tc.p, 12)
		if math.Abs(lat-tc.lat) > 1e-4 || math.Abs(lng-tc.lng) > 1e-4 {
			t.Errorf("ToLatLng(%v) = %.4f, %.4f; want %.4f, %.4f", tc.p, lat, lng, tc.lat, tc.lng)
		}
	}
}
