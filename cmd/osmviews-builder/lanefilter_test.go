// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

// lonLatTile returns the tile at zoom z that contains lon/lat.
func lonLatTile(zoom uint8, lon, lat float64) (uint32, uint32) {
	n := float64(uint64(1) << zoom)
	return uint32((lon + 180) / 360 * n), uint32(mercatorY(n, lat))
}

// laneTiles returns the tiles of a straight lon/lat line from Null Island
// with the given bearing, between two distances, at zoom z.
func laneTilesAt(zoom uint8, bearing, from, to float64) [][2]uint32 {
	r := bearing * math.Pi / 180
	n := float64(uint64(1) << zoom)
	var tiles [][2]uint32
	seen := map[[2]uint32]bool{}
	for d := from; d <= to; d += 90 / n {
		x, y := lonLatTile(zoom, d*math.Cos(r), d*math.Sin(r))
		if !seen[[2]uint32{x, y}] {
			seen[[2]uint32{x, y}] = true
			tiles = append(tiles, [2]uint32{x, y})
		}
	}
	return tiles
}

func TestLaneDetect(t *testing.T) {
	var seeds []laneSeed
	for _, p := range laneTilesAt(12, 40, 10, 30) {
		seeds = append(seeds, laneSeed{x: p[0], y: p[1], removed: 100})
	}
	// A compact blob of seeds is not a lane: it covers few distances.
	x0, y0 := lonLatTile(12, -20, 10)
	for dx := uint32(0); dx < 10; dx++ {
		for dy := uint32(0); dy < 10; dy++ {
			seeds = append(seeds, laneSeed{x: x0 + dx, y: y0 + dy, removed: 100})
		}
	}
	lines := laneDetect(12, seeds)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %+v", len(lines), lines)
	}
	if math.Abs(lines[0].bearing-40) > 0.05 || math.Abs(lines[0].reach-30) > 0.5 {
		t.Errorf("got %+v, want bearing 40°, reach 30°", lines[0])
	}
	if got := laneDetect(12, nil); got != nil {
		t.Errorf("no seeds: got %v", got)
	}
}

func TestLaneGeometry(t *testing.T) {
	for _, bearing := range []float64{10, 40, 80, 100, 200, 300} {
		g := newLaneGeometry(14, bearing)
		for _, p := range laneTilesAt(14, bearing, 5, 50) {
			along, off := g.locate(p[0], p[1])
			if math.Abs(off) > 1 || along < 4.9 || along > 50.1 {
				t.Errorf("bearing %g: tile %v at along %g, off %g; want on the lane", bearing, p, along, off)
			}
		}
		// A tile 5 tiles beside the lane, perpendicular on the map.
		mid := laneTilesAt(14, bearing, 20, 20)[0]
		x, y := mid[0]+5, mid[1]
		if g.byLon {
			x, y = mid[0], mid[1]+5
		}
		_, off := g.locate(x, y)
		if math.Abs(off) < 3 || math.Abs(off) > 5.5 {
			t.Errorf("bearing %g: off %g, want between 1 and 5.5", bearing, off)
		}
	}
}

func TestLaneClean(t *testing.T) {
	const zoom = 13
	line := laneLine{bearing: 40, reach: 30}
	var tiles []laneTile
	add := func(x, y uint32, count float64) {
		lon, lat := tileLonLat(float64(uint64(1)<<zoom), float64(x)+0.5, float64(y)+0.5)
		tiles = append(tiles, laneTile{x: x, y: y, count: count, orig: count,
			angle: math.Atan2(lat, lon) * 180 / math.Pi, dist: math.Hypot(lon, lat)})
	}
	onLane := map[[2]uint32]bool{}
	for _, p := range laneTilesAt(zoom, 40, 5, 30) {
		onLane[p] = true
		add(p[0], p[1], 300)
	}
	// A busy city across the lane at 20°: real use on both sides of it.
	cx, cy := lonLatTile(zoom, 20*math.Cos(40*math.Pi/180), 20*math.Sin(40*math.Pi/180))
	city := map[[2]uint32]bool{}
	for x := cx - 12; x <= cx+12; x++ {
		for y := cy - 12; y <= cy+12; y++ {
			if !onLane[[2]uint32{x, y}] {
				add(x, y, 3000)
			}
			city[[2]uint32{x, y}] = true
		}
	}

	// Stretches that touch the city's sides are busy and left alone.
	farFromCity := func(p [2]uint32) bool {
		return math.Hypot(float64(p[0])-float64(cx), float64(p[1])-float64(cy)) > 60
	}
	kept := laneClean(zoom, []laneLine{line}, tiles, 7)
	if len(kept) != 1 {
		t.Fatalf("got %d lanes, want 1", len(kept))
	}
	for _, tile := range tiles {
		p := [2]uint32{tile.x, tile.y}
		switch {
		case city[p] && tile.count != tile.orig:
			t.Errorf("city tile %v changed from %g to %g", p, tile.orig, tile.count)
		case onLane[p] && farFromCity(p) && tile.count != 0:
			t.Errorf("lane tile %v kept %g of %g", p, tile.count, tile.orig)
		}
	}

	// Where the strip is no busier than its sides, there is no lane.
	var flat []laneTile
	for _, tile := range tiles {
		if !city[[2]uint32{tile.x, tile.y}] {
			for d := uint32(0); d <= 12; d++ {
				t2 := tile
				t2.x += d
				t2.count, t2.orig = 50, 50
				flat = append(flat, t2)
			}
		}
	}
	if kept := laneClean(zoom, []laneLine{line}, flat, 7); len(kept) != 0 {
		t.Errorf("uniform background: got %+v, want no lane", kept)
	}
}

func TestInsideNullIsland(t *testing.T) {
	for _, c := range []struct {
		zoom uint8
		x, y uint32
		want bool
	}{
		{10, 512, 512, true},
		{10, 511, 511, true},
		{10, 520, 500, false}, // about 3.5° away
		{6, 32, 32, false},    // 5.6° wide
		{18, 1 << 17, 1 << 17, true},
		{3, 4, 4, false},
	} {
		if got := insideNullIsland(c.zoom, c.x, c.y); got != c.want {
			t.Errorf("insideNullIsland(%d, %d, %d) = %v, want %v", c.zoom, c.x, c.y, got, c.want)
		}
	}
}

func TestFilterWeek(t *testing.T) {
	w := &zfTestWorld{counts: make(map[TileKey]uint64)}

	// Ordinary use: every tile of zoom-9 tile 9/100/300 at zooms 7 to 18.
	for zoom := uint8(7); zoom <= 18; zoom++ {
		if zoom <= 9 {
			sh := 9 - zoom
			w.add(zoom, 100>>sh, 300>>sh, 100)
			continue
		}
		sh := zoom - 9
		for x := uint32(100) << sh; x < uint32(101)<<sh; x++ {
			for y := uint32(300) << sh; y < uint32(301)<<sh; y++ {
				w.add(zoom, x, y, 100)
			}
		}
	}
	// A lane at zoom 13, straight in lon/lat, and requests at Null Island.
	lane := map[TileKey]bool{}
	for _, p := range laneTilesAt(13, 30, 4, 25) {
		w.add(13, p[0], p[1], 400)
		lane[MakeTileKey(13, p[0], p[1])] = true
	}
	for zoom := uint8(8); zoom <= 14; zoom++ {
		x, y := lonLatTile(zoom, 0.1, 0.1)
		w.add(zoom, x, y, 1000)
	}
	// Background in the ring around Null Island at zoom 8: 100 per tile,
	// plus a city that the trimmed mean leaves out.
	for x := uint32(110); x < 146; x++ {
		for y := uint32(110); y < 146; y++ {
			if inNullIslandRing(8, x, y) {
				w.add(8, x, y, 100)
			}
		}
	}
	cx, cy := lonLatTile(8, -0.2, 5.6)
	w.add(8, cx, cy, 100000)

	data := w.reader().(*strings.Reader)
	text, _ := io.ReadAll(data)
	opened := 0
	open := func() (io.ReadCloser, error) {
		opened++
		return io.NopCloser(strings.NewReader(string(text))), nil
	}
	corr, stats, err := FilterWeek(open, 7)
	if err != nil {
		t.Fatal(err)
	}
	if opened != 2 {
		t.Errorf("logs opened %d times, want 2", opened)
	}
	if !slices.IsSortedFunc(corr, compareTileCorrections) {
		t.Error("corrections not sorted")
	}
	got := map[TileKey]uint64{}
	for i, c := range corr {
		if i > 0 && corr[i-1].Key == c.Key {
			t.Errorf("duplicate correction for %s", c.Key)
		}
		got[c.Key] = c.Count
	}
	for k := range lane {
		if c, ok := got[k]; !ok || c >= 400 {
			t.Errorf("lane tile %s: got %d, %v; want reduced", k, c, ok)
		}
	}
	for zoom := uint8(8); zoom <= 14; zoom++ {
		x, y := lonLatTile(zoom, 0.1, 0.1)
		c, ok := got[MakeTileKey(zoom, x, y)]
		// Zoom 8 tiles are coarse, so fewer tile centers fall in the
		// ring than its area suggests.
		if zoom == 8 && (!ok || c < 70 || c > 110) {
			t.Errorf("Null Island tile at zoom 8: got %d, %v; want about 100", c, ok)
		} else if zoom > 8 && (!ok || c != 0) {
			t.Errorf("Null Island tile at zoom %d: got %d, %v; want 0", zoom, c, ok)
		}
	}
	for k := range got {
		z, x, y := k.ZoomXY()
		if !lane[k] && !insideNullIsland(z, x, y) {
			t.Errorf("unexpected correction for %s", k)
		}
	}
	x8, y8 := lonLatTile(8, 0.1, 0.1)
	if want := fmt.Sprintf("Null Island: removed %d", 7000-got[MakeTileKey(8, x8, y8)]); !strings.Contains(stats, want) {
		t.Errorf("stats = %q", stats)
	}
}

func TestWeekLogDays(t *testing.T) {
	for name, want := range map[string]int{
		"tilelogs-2026-W41-2d.br": 2,
		"tilelogs-2026-W40.br":    7,
		"tilelogs-2026-W40-9d.br": 7,
		"tilelogs-x.br":           7,
	} {
		if got := weekLogDays(name); got != want {
			t.Errorf("weekLogDays(%q) = %d, want %d", name, got, want)
		}
	}
}

func writeBrotli(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	bw := brotli.NewWriter(f)
	if _, err := io.WriteString(bw, content); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareZoomFilters_Storage(t *testing.T) {
	ctx := context.Background()
	storage := NewFakeStorage()
	names := []string{weekLogName("2026-W40", 3), weekLogName("2026-W41", 7)}

	// With no time budget left, nothing is computed.
	dir := t.TempDir()
	for _, name := range names {
		writeBrotli(t, filepath.Join(dir, name), "0/0/0 10\n10/512/512 7\n")
	}
	complete, err := prepareZoomFilters(dir, names, storage, "osmviews", time.Nanosecond, ctx)
	if err != nil || complete {
		t.Errorf("tiny budget: got %v, %v; want incomplete", complete, err)
	}

	// Computed corrections are uploaded to storage...
	complete, err = prepareZoomFilters(dir, names, storage, "osmviews", time.Hour, ctx)
	if err != nil || !complete {
		t.Fatalf("got %v, %v; want complete", complete, err)
	}
	for _, name := range names {
		key := "internal/osmviews-builder/" + zoomFilterCacheName(name)
		if _, ok := storage.Files[key]; !ok {
			t.Errorf("%s not uploaded", key)
		}
	}

	// ...and downloaded in a later run, without reading the tile logs.
	dir2 := t.TempDir()
	readers, err := openZoomFilters(dir2, names, storage, "osmviews", ctx)
	if err != nil {
		t.Fatal(err)
	}
	corr, err := readTileCorrections(readers[0])
	if err != nil {
		t.Fatal(err)
	}
	want := []TileCorrection{{MakeTileKey(10, 512, 512), 0}}
	if !slices.Equal(corr, want) {
		t.Errorf("got %v, want %v", corr, want)
	}
}

func TestPaint_FilteredOutput(t *testing.T) {
	dir := t.TempDir()
	plain, filtered := filepath.Join(dir, "plain.tiff"), filepath.Join(dir, "filtered.tiff")
	outputs := []PaintOutput{{Path: plain}, {Path: filtered, Filtered: true}}
	readers := []io.Reader{strings.NewReader("3/1/1 3\n11/1100/700 100000\n")}
	corrections := []io.Reader{strings.NewReader("11/1100/700 0\n")}
	if err := paint(outputs, 11, readers, nil, corrections, context.Background()); err != nil {
		t.Fatal(err)
	}
	a, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if bytes := len(a); bytes == 0 || slices.Equal(a, b) {
		t.Error("filtered GeoTIFF should differ from the plain one")
	}

	// The plain output of a combined run equals a run without corrections.
	alone := filepath.Join(dir, "alone.tiff")
	readers = []io.Reader{strings.NewReader("3/1/1 3\n11/1100/700 100000\n")}
	if err := paint([]PaintOutput{{Path: alone}}, 11, readers, nil, nil, context.Background()); err != nil {
		t.Fatal(err)
	}
	c, err := os.ReadFile(alone)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a, c) {
		t.Error("plain output changed by painting a filtered one alongside")
	}
}

func TestNullIslandRingTiles(t *testing.T) {
	for _, zoom := range []uint8{9, 11, 13} {
		n := uint32(1) << zoom
		lo, hi := n/2-n/40, n/2+n/40
		count := 0
		for x := lo; x < hi; x++ {
			for y := lo; y < hi; y++ {
				if inNullIslandRing(zoom, x, y) {
					count++
				}
			}
		}
		want := nullIslandRingTiles(zoom)
		if math.Abs(float64(count)-want) > 0.05*want {
			t.Errorf("zoom %d: %d tiles in ring, estimate %.1f", zoom, count, want)
		}
	}
}

func TestNullIslandBackground(t *testing.T) {
	// Zoom 10 has about 686 positions in the ring. The 34 busiest are
	// left out: the two outliers and 32 ordinary ones, so the background
	// is 268 * 13 / 652.
	counts := []float64{1e6, 5e5}
	for range 300 {
		counts = append(counts, 13)
	}
	if got := nullIslandBackground(10, counts); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := nullIslandBackground(10, nil); got != 0 {
		t.Errorf("empty ring: got %d, want 0", got)
	}
}
