// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"golang.org/x/sync/errgroup"
)

func TestParseTileLine(t *testing.T) {
	for _, s := range []string{
		"0/0/0 1", "18/262143/0 42", "3/7/7 18446744073709551615",
		"4/16/0 1", "4/0/16 1", "25/0/0 1", "1/0/0", "1/0 1", "1//0 1",
		"1/0/0 1 ", " 1/0/0 1", "1/0/0 x", "", "1/0/0  1", "1/0/0\t1", "x",
	} {
		got, ok := parseTileLine([]byte(s))
		want := ParseTileCount(s)
		if ok != (want.Key != NoTile) || (ok && got != want) {
			t.Errorf("parseTileLine(%q) = %v, %v; ParseTileCount = %v", s, got, ok, want)
		}
	}
	if _, ok := parseTileLine([]byte("1/0/0 99999999999999999999")); ok {
		t.Error("parseTileLine accepted a count that overflows uint64")
	}
}

// zfTestWorld builds a synthetic week of tile logs: ordinary map use
// across all zooms in one area, plus a lane, a dot of a lane and two
// compact single-zoom areas elsewhere.
type zfTestWorld struct {
	counts map[TileKey]uint64
}

func (w *zfTestWorld) add(zoom uint8, x, y uint32, count uint64) {
	w.counts[MakeTileKey(zoom, x, y)] += count
}

func (w *zfTestWorld) reader() io.Reader {
	keys := make([]TileKey, 0, len(w.counts))
	for k := range w.counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var buf strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&buf, "%s %d\n", k, w.counts[k])
	}
	return strings.NewReader(buf.String())
}

func TestComputeZoomFilter(t *testing.T) {
	w := &zfTestWorld{counts: make(map[TileKey]uint64)}

	// Ordinary use: every tile of zoom-9 tile 9/100/300 at zooms 7 to 18.
	for zoom := uint8(7); zoom <= 18; zoom++ {
		if zoom <= 9 {
			s := 9 - zoom
			w.add(zoom, 100>>s, 300>>s, 100)
			continue
		}
		s := zoom - 9
		for x := uint32(100) << s; x < uint32(101)<<s; x++ {
			for y := uint32(300) << s; y < uint32(301)<<s; y++ {
				w.add(zoom, x, y, 100)
			}
		}
	}

	// A lane at zoom 13, heading north-east from Null Island (4096, 4096).
	lane := make(map[TileKey]bool)
	for i := uint32(100); i < 400; i++ {
		w.add(13, 4096+i, 4096-i, 500)
		lane[MakeTileKey(13, 4096+i, 4096-i)] = true
	}

	// A dot of that lane: a blob at zoom 13 stretched along the ray.
	dot := make(map[TileKey]bool)
	// A blob just as big, but stretched across the ray; it should be kept.
	across := make(map[TileKey]bool)
	for a := uint32(0); a < 24; a++ {
		for b := uint32(0); b < 24; b++ {
			if a > b+5 || b > a+5 {
				continue
			}
			w.add(13, 4096+600+a, 4096-600-b, 300)
			dot[MakeTileKey(13, 4096+600+a, 4096-600-b)] = true
			w.add(13, 4096+900+a, 4096-900-24+b, 300)
			across[MakeTileKey(13, 4096+900+a, 4096-900-24+b)] = true
		}
	}

	// A compact city at zoom 12 only, as when the other zooms fall
	// below the logging threshold; it should be kept.
	city := make(map[TileKey]bool)
	for x := uint32(800); x < 812; x++ {
		for y := uint32(3000); y < 3012; y++ {
			w.add(12, x, y, 100)
			city[MakeTileKey(12, x, y)] = true
		}
	}

	corr, stats, err := ComputeZoomFilter(w.reader())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSortedFunc(corr, compareTileCorrections) {
		t.Error("corrections not sorted")
	}

	corrected := make(map[TileKey]uint64)
	for _, c := range corr {
		corrected[c.Key] = c.Count
		if !lane[c.Key] && !dot[c.Key] {
			t.Errorf("unexpected correction %s → %d", c.Key, c.Count)
		}
		if c.Count >= w.counts[c.Key] {
			t.Errorf("correction %s → %d does not reduce %d", c.Key, c.Count, w.counts[c.Key])
		}
	}
	for k := range lane {
		if _, ok := corrected[k]; !ok {
			t.Errorf("lane tile %s not corrected", k)
		}
	}
	for k := range dot {
		if _, ok := corrected[k]; !ok {
			t.Errorf("dot tile %s not corrected", k)
		}
	}

	var z13 ZoomFilterZoomStats
	for _, s := range stats.Zooms {
		if s.Zoom == 13 {
			z13 = s
		}
	}
	if z13.KeptGroups != 1 || z13.Removed == 0 {
		t.Errorf("zoom 13 stats: %+v", z13)
	}
	if !strings.Contains(stats.String(), "z13: flagged") {
		t.Errorf("stats.String() = %q", stats.String())
	}
}

func TestComputeZoomFilter_Unsorted(t *testing.T) {
	_, _, err := ComputeZoomFilter(strings.NewReader("2/1/1 5\n1/0/0 7\n"))
	if err == nil {
		t.Error("want error for unsorted input")
	}
}

func TestComputeZoomFilter_Empty(t *testing.T) {
	corr, _, err := ComputeZoomFilter(strings.NewReader(""))
	if err != nil || len(corr) != 0 {
		t.Errorf("got %v, %v; want no corrections", corr, err)
	}
}

func TestTileCorrectionsRoundTrip(t *testing.T) {
	want := []TileCorrection{
		{MakeTileKey(9, 1, 2), 0},
		{MakeTileKey(12, 7, 9), 17},
	}
	slices.SortFunc(want, compareTileCorrections)
	var buf bytes.Buffer
	if err := writeTileCorrections(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := readTileCorrections(brotli.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func readZoomFilters(t *testing.T, dir, name string) []TileCorrection {
	t.Helper()
	readers, err := openZoomFilters(dir, []string{name}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 1 {
		t.Fatalf("got %d readers, want 1", len(readers))
	}
	corr, err := readTileCorrections(readers[0])
	if err != nil {
		t.Fatal(err)
	}
	return corr
}

func TestOpenZoomFilters_Caches(t *testing.T) {
	dir := t.TempDir()
	name := weekLogName("2026-W40", 3)
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	bw := brotli.NewWriter(f)
	if _, err := io.WriteString(bw, "0/0/0 10\n1/0/0 10\n"); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got := readZoomFilters(t, dir, name)
	if len(got) != 0 {
		t.Errorf("got %v, want no corrections", got)
	}
	cache := filepath.Join(dir, "zoomfilter-v1-2026-W40-3d.br")
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("cache file not written: %v", err)
	}

	// A cached result is used without reading the tile logs again.
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	cf, err := os.Create(cache)
	if err != nil {
		t.Fatal(err)
	}
	want := []TileCorrection{{MakeTileKey(10, 5, 5), 3}}
	if err := writeTileCorrections(cf, want); err != nil {
		t.Fatal(err)
	}
	if err := cf.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readZoomFilters(t, dir, name); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if _, err := openZoomFilters(dir, []string{"tilelogs-missing.br"}, context.Background()); err == nil {
		t.Error("want error for missing tile logs")
	}
}

func TestMergeTileCounts_Corrections(t *testing.T) {
	readers := []io.Reader{
		strings.NewReader("1/0/0 5\n2/0/0 7\n2/1/1 9\n"),
		strings.NewReader("2/0/0 4\n"),
	}
	corrections := []io.Reader{strings.NewReader("0/0/0 1\n2/0/0 3\n2/1/1 0\n"), nil}
	ch := make(chan TileCount)
	g, ctx := errgroup.WithContext(context.Background())
	g.Go(func() error {
		return mergeTileCounts(readers, []float64{2.0, 1.0}, corrections, ch, ctx)
	})
	var got []string
	for c := range ch {
		got = append(got, fmt.Sprintf("%s %d", c.Key, c.Count))
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	want := []string{"1/0/0 10", "2/0/0 4", "2/0/0 6", "2/1/1 0"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMergeTileCounts_BadCorrections(t *testing.T) {
	for _, corr := range []string{"2/0/0 3\n1/0/0 1\n", "garbage\n"} {
		readers := []io.Reader{strings.NewReader("1/0/0 5\n2/0/0 7\n")}
		ch := make(chan TileCount, 10)
		err := mergeTileCounts(readers, nil, []io.Reader{strings.NewReader(corr)}, ch, context.Background())
		if err == nil {
			t.Errorf("corrections %q: want error", corr)
		}
	}
}
