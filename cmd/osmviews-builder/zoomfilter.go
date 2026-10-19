// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"golang.org/x/sync/errgroup"
)

// The zoom filter removes artifacts that some clients leave in the tile
// logs: straight "lanes" of requests, often radiating from Null Island,
// which appear on only one to three adjacent zoom levels. Real map usage
// spreads over many zoom levels, so the filter compares each area's
// requests at zoom z with the same area's requests at nearby zooms, and
// scales down zoom z where it is far above what its neighbours predict.
//
// The comparison works on cells: tiles at zoom z-2, each covering 4×4
// tiles at zoom z. For every cell, Dz is the sum of its zoom-z requests,
// compared with the requests at zoom z-k and z+k (k = 2 or 3) inside the
// same cell. A log ratio ln((Dz+1)/(D+1)) is normalised by its median over
// busy cells, because some zooms are always more popular than others. A
// cell is flagged when zoom z exceeds both neighbours by more than
// zoomFilterFactor times their usual ratio; its zoom-z tiles are then
// scaled down to the larger of the two predictions.
//
// Some cities, mostly in China, also appear on only one or two zooms, but
// most likely because the tile logs omit tiles with fewer than ten requests
// per day. A shape rule keeps those: within flagged cells, connected groups
// of zoom-z tiles that are wide and compact are left alone, unless they are
// stretched along a ray from Null Island, which is what a dot of a dotted
// lane looks like (a map viewport that paused while panning).
//
// A second stage, the lane filter (lanefilter.go), then finds the lanes as
// whole lines and cleans the traces this stage leaves along them.
const (
	zoomFilterVersion = 2 // bump when the algorithm changes; part of cache names

	zoomFilterMinZoom = 9  // lowest zoom that gets filtered
	zoomFilterMaxZoom = 16 // highest zoom that gets filtered

	zoomFilterFactor   = 4.0  // flag cells above zoomFilterFactor × usual ratio
	zoomFilterMinViews = 20.0 // ignore cells with fewer zoom-z requests than this
	zoomFilterBusy     = 50.0 // cells used for the median ratio need this many

	// zoomFilterPrefilter discards cells during the streaming pass, before
	// the medians are known. A cell can only be flagged if its raw ratios
	// exceed zoomFilterFactor × median, and the medians are around 1 to 4,
	// so a raw ratio below 2 never gets flagged unless a median is below 0.5.
	zoomFilterPrefilter = 2.0

	zoomFilterMinWidth  = 6.0  // compact groups at least this wide (in tiles) are kept
	zoomFilterMaxAspect = 10.0 // ...and no longer than this many times their width
	zoomFilterMinElong  = 1.2  // ...unless elongated by at least this much
	zoomFilterMaxRayDev = 12.0 // ...within this many degrees of the ray from Null Island
)

// TileCorrection replaces the count of one tile in one week's tile logs.
type TileCorrection struct {
	Key   TileKey
	Count uint64
}

func compareTileCorrections(a, b TileCorrection) int {
	return cmp.Compare(a.Key, b.Key)
}

// ZoomFilterStats summarises what the zoom filter did to one week, per zoom.
type ZoomFilterStats struct {
	Zooms []ZoomFilterZoomStats
}

type ZoomFilterZoomStats struct {
	Zoom          uint8
	Cells         int     // cells with zoom-z requests
	Flagged       int     // cells whose zoom-z tiles are scaled down
	KeptGroups    int     // compact tile groups exempted by the shape rule
	Total         uint64  // zoom-z requests before filtering
	Removed       uint64  // zoom-z requests removed by the filter
	RatioLow      float64 // usual ratio of zoom z to zoom z-2
	RatioHigh     float64 // usual ratio of zoom z to zoom z+2
	LowMedianWarn bool    // a median ratio was so low that the prefilter may have missed cells
}

func (s ZoomFilterStats) String() string {
	var b bytes.Buffer
	for i, z := range s.Zooms {
		if i > 0 {
			b.WriteString("; ")
		}
		pct := 0.0
		if z.Total > 0 {
			pct = 100 * float64(z.Removed) / float64(z.Total)
		}
		fmt.Fprintf(&b, "z%d: flagged %d of %d cells, kept %d compact groups, removed %.2f%%",
			z.Zoom, z.Flagged, z.Cells, z.KeptGroups, pct)
		if z.LowMedianWarn {
			b.WriteString(" (warning: low median ratio)")
		}
	}
	return b.String()
}

// zfTile is a zoom-z tile inside a cell.
type zfTile struct {
	x, y  uint32
	count float64
}

// zfCell holds what the streaming pass collected about one candidate cell.
type zfCell struct {
	dz       float64 // requests at zoom z (= cell zoom + 2)
	dl2, dh2 float64 // requests at zoom z-2 and z+2
	dl3, dh3 float64 // requests at zoom z-3 (area share) and z+3
	has3     bool
	tiles    []zfTile
}

// zfNode is an open tile on the stack of the streaming pass.
type zfNode struct {
	key   TileKey
	count float64
	desc  [6]float64 // desc[d]: sum of requests at zoom+d inside this tile
	tiles []zfTile   // zoom+2 tiles, collected only for cell zooms
}

// zfZoom accumulates per-zoom data during the streaming pass.
type zfZoom struct {
	cells          int
	total          float64
	a2, b2, a3, b3 []float64 // log ratios of busy cells, for the medians
	candidates     []zfCell
}

type zoomFilter struct {
	stack []zfNode // stack[i] is the open tile at zoom i
	zooms [zoomFilterMaxZoom + 1]zfZoom
	seeds [zoomFilterMaxZoom + 1][]laneSeed // tiles this stage reduced, for the lane filter
}

// ComputeZoomFilter reads one week of tile logs, in the TileKey-sorted
// "zoom/x/y count" format of the weekly log files, and returns the
// corrections of the cross-zoom stage, sorted by TileKey.
func ComputeZoomFilter(r io.Reader) ([]TileCorrection, ZoomFilterStats, error) {
	corr, _, stats, err := computeCrossZoom(r)
	return corr, stats, err
}

// FilterWeek runs both stages of the zoom filter over one week of tile
// logs with numDays days of data. open is called twice, because the lane
// filter needs a second pass over the logs.
func FilterWeek(open func() (io.ReadCloser, error), numDays int) ([]TileCorrection, string, error) {
	r, err := open()
	if err != nil {
		return nil, "", err
	}
	corr, seeds, zstats, err := computeCrossZoom(r)
	r.Close()
	if err != nil {
		return nil, "", err
	}
	r, err = open()
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	all, lstats, err := laneFilter(r, corr, seeds, numDays)
	if err != nil {
		return nil, "", err
	}
	return all, fmt.Sprintf("cross-zoom: %s; lanes: %s", zstats, lstats), nil
}

func computeCrossZoom(r io.Reader) ([]TileCorrection, [zoomFilterMaxZoom + 1][]laneSeed, ZoomFilterStats, error) {
	f := &zoomFilter{stack: make([]zfNode, 0, maxZoom+1)}
	scanner := bufio.NewScanner(r)
	last := WorldTile
	first := true
	for scanner.Scan() {
		tc, ok := parseTileLine(scanner.Bytes())
		if !ok || tc.Key.Zoom() > maxZoom {
			continue
		}
		if !first && tc.Key < last {
			return nil, f.seeds, ZoomFilterStats{}, fmt.Errorf("tile logs not sorted: %s after %s", tc.Key, last)
		}
		first = false
		last = tc.Key
		f.add(tc)
	}
	if err := scanner.Err(); err != nil {
		return nil, f.seeds, ZoomFilterStats{}, err
	}
	for len(f.stack) > 0 {
		f.pop()
	}
	corr, stats := f.finish()
	return corr, f.seeds, stats, nil
}

// add feeds one tile into the streaming pass. Tiles arrive in pre-order,
// so every ancestor of a tile arrives before it, and all tiles inside a
// cell arrive before the cell's next sibling.
func (f *zoomFilter) add(tc TileCount) {
	zoom := int(tc.Key.Zoom())
	for len(f.stack) > 0 {
		top := f.stack[len(f.stack)-1].key
		if top == tc.Key || top.Contains(tc.Key) {
			break
		}
		f.pop()
	}
	if n := len(f.stack); n > zoom {
		// Same key twice: the weekly logs are aggregated, but be lenient.
		f.stack[zoom].count += float64(tc.Count)
	} else {
		// Push missing ancestors, which had no requests of their own.
		for z := n; z < zoom; z++ {
			f.stack = append(f.stack, zfNode{key: tc.Key.ToZoom(uint8(z))})
		}
		f.stack = append(f.stack, zfNode{key: tc.Key, count: float64(tc.Count)})
	}

	count := float64(tc.Count)
	for d := 1; d <= 5 && d <= zoom; d++ {
		f.stack[zoom-d].desc[d] += count
	}
	if g := zoom - 2; g >= zoomFilterMinZoom-2 && g <= zoomFilterMaxZoom-2 {
		_, x, y := tc.Key.ZoomXY()
		cell := &f.stack[g]
		cell.tiles = append(cell.tiles, zfTile{x: x, y: y, count: count})
	}
}

// pop closes the tile on top of the stack. When it is a cell, its counts
// are complete: record them for the medians, and keep it if it could be
// flagged.
func (f *zoomFilter) pop() {
	n := len(f.stack) - 1
	node := &f.stack[n]
	g := n
	z := g + 2
	if z >= zoomFilterMinZoom && z <= zoomFilterMaxZoom && node.desc[2] > 0 {
		zz := &f.zooms[z]
		c := zfCell{dz: node.desc[2], dl2: node.count, dh2: node.desc[4]}
		if z-3 >= zoomFilterMinZoom-2 && z+3 <= maxZoom {
			c.has3 = true
			c.dl3 = f.stack[g-1].count / 4
			c.dh3 = node.desc[5]
		}
		zz.cells++
		zz.total += c.dz
		a2, b2 := zfLogRatio(c.dz, c.dl2), zfLogRatio(c.dz, c.dh2)
		if c.dz >= zoomFilterBusy && c.dl2 >= zoomFilterBusy && c.dh2 >= zoomFilterBusy {
			zz.a2 = append(zz.a2, a2)
			zz.b2 = append(zz.b2, b2)
		}
		raw := math.Min(a2, b2)
		if c.has3 {
			a3, b3 := zfLogRatio(c.dz, c.dl3), zfLogRatio(c.dz, c.dh3)
			if c.dz >= zoomFilterBusy && c.dl3 >= zoomFilterBusy && c.dh3 >= zoomFilterBusy {
				zz.a3 = append(zz.a3, a3)
				zz.b3 = append(zz.b3, b3)
			}
			raw = math.Max(raw, math.Min(a3, b3))
		}
		if c.dz >= zoomFilterMinViews && raw > math.Log(zoomFilterPrefilter) {
			c.tiles = slices.Clone(node.tiles)
			zz.candidates = append(zz.candidates, c)
		}
	}
	node.tiles = node.tiles[:0]
	f.stack = f.stack[:n]
}

func zfLogRatio(a, b float64) float64 {
	return math.Log((a + 1) / (b + 1))
}

func zfMedian(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// finish flags cells, applies the shape rule and returns the corrections.
func (f *zoomFilter) finish() ([]TileCorrection, ZoomFilterStats) {
	var corr []TileCorrection
	var stats ZoomFilterStats
	threshold := math.Log(zoomFilterFactor)
	for z := zoomFilterMinZoom; z <= zoomFilterMaxZoom; z++ {
		zz := &f.zooms[z]
		ma2, mb2 := zfMedian(zz.a2), zfMedian(zz.b2)
		ma3, mb3 := zfMedian(zz.a3), zfMedian(zz.b3)
		st := ZoomFilterZoomStats{
			Zoom:      uint8(z),
			Cells:     zz.cells,
			Total:     uint64(zz.total),
			RatioLow:  math.Exp(ma2),
			RatioHigh: math.Exp(mb2),
		}
		lowest := min(ma2, mb2)
		if len(zz.a3) > 0 {
			lowest = min(lowest, ma3, mb3)
		}
		if threshold+lowest < math.Log(zoomFilterPrefilter) {
			st.LowMedianWarn = true
		}

		// Flag cells and remember their scale factor, per zoom-z tile.
		var tiles []zfTile
		var scales []float64
		for _, c := range zz.candidates {
			s := math.Min(zfLogRatio(c.dz, c.dl2)-ma2, zfLogRatio(c.dz, c.dh2)-mb2)
			expected := math.Max((c.dl2+1)*math.Exp(ma2), (c.dh2+1)*math.Exp(mb2)) - 1
			if c.has3 {
				s3 := math.Min(zfLogRatio(c.dz, c.dl3)-ma3, zfLogRatio(c.dz, c.dh3)-mb3)
				if s3 > s {
					s = s3
					expected = math.Max((c.dl3+1)*math.Exp(ma3), (c.dh3+1)*math.Exp(mb3)) - 1
				}
			}
			if s <= threshold || c.dz < zoomFilterMinViews {
				continue
			}
			st.Flagged++
			scale := math.Min(1, math.Max(expected, 0)/c.dz)
			for _, t := range c.tiles {
				tiles = append(tiles, t)
				scales = append(scales, scale)
			}
		}

		exempt, kept := zfShapeRule(uint8(z), tiles)
		st.KeptGroups = kept
		for i, t := range tiles {
			if exempt[i] {
				continue
			}
			newCount := math.Round(t.count * scales[i])
			if newCount == t.count {
				continue
			}
			st.Removed += uint64(t.count - newCount)
			f.seeds[z] = append(f.seeds[z], laneSeed{x: t.x, y: t.y, removed: t.count - newCount})
			corr = append(corr, TileCorrection{
				Key:   MakeTileKey(uint8(z), t.x, t.y),
				Count: uint64(newCount),
			})
		}
		stats.Zooms = append(stats.Zooms, st)
		zz.candidates = nil
	}
	slices.SortFunc(corr, compareTileCorrections)
	return corr, stats
}

// zfShapeRule groups the tiles of flagged cells into 8-connected
// components and returns which tiles belong to compact, wide components
// that should be kept, plus the number of such components.
func zfShapeRule(zoom uint8, tiles []zfTile) ([]bool, int) {
	exempt := make([]bool, len(tiles))
	index := make(map[[2]uint32]int, len(tiles))
	for i, t := range tiles {
		index[[2]uint32{t.x, t.y}] = i
	}
	seen := make([]bool, len(tiles))
	half := float64(uint64(1)<<zoom) / 2
	minRayCos := math.Cos(zoomFilterMaxRayDev * math.Pi / 180)
	kept := 0
	var comp, stack []int
	for start := range tiles {
		if seen[start] {
			continue
		}
		comp = comp[:0]
		stack = append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, i)
			t := tiles[i]
			for dx := -1; dx <= 1; dx++ {
				for dy := -1; dy <= 1; dy++ {
					nx, ny := int64(t.x)+int64(dx), int64(t.y)+int64(dy)
					if nx < 0 || ny < 0 || nx > math.MaxUint32 || ny > math.MaxUint32 {
						continue
					}
					if j, ok := index[[2]uint32{uint32(nx), uint32(ny)}]; ok && !seen[j] {
						seen[j] = true
						stack = append(stack, j)
					}
				}
			}
		}
		if float64(len(comp)) < zoomFilterMinWidth*zoomFilterMinWidth {
			continue
		}

		// Principal axes of the component's tile positions.
		var mx, my float64
		for _, i := range comp {
			mx += float64(tiles[i].x)
			my += float64(tiles[i].y)
		}
		n := float64(len(comp))
		mx, my = mx/n, my/n
		var sxx, sxy, syy float64
		for _, i := range comp {
			dx, dy := float64(tiles[i].x)-mx, float64(tiles[i].y)-my
			sxx += dx * dx
			sxy += dx * dy
			syy += dy * dy
		}
		sxx, sxy, syy = sxx/n, sxy/n, syy/n
		tr, det := sxx+syy, sxx*syy-sxy*sxy
		disc := math.Sqrt(math.Max(tr*tr/4-det, 0))
		ev1, ev2 := tr/2+disc, tr/2-disc // ev1 >= ev2
		length := math.Sqrt(12*math.Max(ev1, 0)) + 1
		width := math.Sqrt(12*math.Max(ev2, 0)) + 1
		if width < zoomFilterMinWidth || length >= zoomFilterMaxAspect*width {
			continue
		}

		// Unit vector along the major axis.
		vx, vy := sxy, ev1-sxx
		if math.Abs(vx) < 1e-12 && math.Abs(vy) < 1e-12 {
			if sxx >= syy {
				vx, vy = 1, 0
			} else {
				vx, vy = 0, 1
			}
		}
		vn := math.Hypot(vx, vy)
		vx, vy = vx/vn, vy/vn

		// Unit vector from Null Island to the component's centre.
		rx, ry := mx+0.5-half, my+0.5-half
		rn := math.Max(math.Hypot(rx, ry), 1e-9)
		rx, ry = rx/rn, ry/rn

		onRay := length >= zoomFilterMinElong*width && math.Abs(vx*rx+vy*ry) >= minRayCos
		if onRay {
			continue
		}
		kept++
		for _, i := range comp {
			exempt[i] = true
		}
	}
	return exempt, kept
}

// parseTileLine parses a "zoom/x/y count" line of the weekly tile logs.
// It is equivalent to ParseTileCount but avoids regular expressions,
// because the zoom filter reads every week of logs once more.
func parseTileLine(b []byte) (TileCount, bool) {
	var v [4]uint64
	part := 0
	digits := 0
	for _, ch := range b {
		switch {
		case ch >= '0' && ch <= '9':
			if part == 2 && digits == -1 {
				part, digits = 3, 0
			}
			d := uint64(ch - '0')
			if v[part] > (math.MaxUint64-d)/10 {
				return TileCount{}, false
			}
			v[part] = v[part]*10 + d
			digits++
		case ch == '/' && part < 2 && digits > 0:
			part++
			digits = 0
		case (ch == ' ' || ch == '\t' || ch == '\r' || ch == '\f' || ch == '\v') && part == 2 && digits != 0:
			digits = -1 // inside the whitespace between y and count
		default:
			return TileCount{}, false
		}
	}
	if part != 3 || digits <= 0 {
		return TileCount{}, false
	}
	zoom := v[0]
	if zoom > 24 || v[1] >= 1<<zoom || v[2] >= 1<<zoom {
		return TileCount{}, false
	}
	return TileCount{Key: MakeTileKey(uint8(zoom), uint32(v[1]), uint32(v[2])), Count: v[3]}, true
}

// zoomFilterCacheName returns the workdir file name that caches the zoom
// filter's corrections for the weekly log file weekLogName.
func zoomFilterCacheName(weekLogName string) string {
	return fmt.Sprintf("zoomfilter-v%d-%s", zoomFilterVersion, strings.TrimPrefix(weekLogName, "tilelogs-"))
}

// weekLogDays returns the number of days of logs in a weekly log file,
// from its name (see weekLogName).
func weekLogDays(weekLogName string) int {
	name := strings.TrimSuffix(weekLogName, ".br")
	if i := strings.LastIndex(name, "-"); i >= 0 && strings.HasSuffix(name, "d") {
		if n, err := strconv.Atoi(name[i+1 : len(name)-1]); err == nil && n >= 1 && n <= 7 {
			return n
		}
	}
	return 7
}

// prepareZoomFilters makes sure the zoom filter's corrections for the
// weekly log files in workdir named by weekLogNames are cached in workdir
// (see ensureZoomFilter). Each week is analysed only once per filter
// version: the corrections are also kept in object storage next to the
// weekly logs, if storage is not nil, because the production workdir does
// not survive from one run to the next. Missing ones are computed in
// parallel, newest week first. With a positive budget, no new week is
// started once the budget is used up; the result then tells whether every
// week is ready, so that a first run with an empty cache can spread the
// work over several daily runs.
func prepareZoomFilters(workdir string, weekLogNames []string, storage Storage, bucket string, budget time.Duration, ctx context.Context) (bool, error) {
	start := time.Now()
	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(max(1, runtime.GOMAXPROCS(0)/2))
	var mu sync.Mutex
	complete := true
	for _, name := range slices.Backward(weekLogNames) {
		g.Go(func() error {
			if budget > 0 && time.Since(start) > budget {
				if _, err := os.Stat(filepath.Join(workdir, zoomFilterCacheName(name))); err != nil {
					mu.Lock()
					complete = false
					mu.Unlock()
					return nil
				}
			}
			if err := ensureZoomFilter(workdir, name, storage, bucket); err != nil {
				return fmt.Errorf("zoom filter for %s: %w", name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, err
	}
	return complete, nil
}

// openZoomFilters returns, for each of the weekly log files named by
// weekLogNames, a reader for the zoom filter's corrections to it (see
// mergeTileCounts), computing the missing ones first.
func openZoomFilters(workdir string, weekLogNames []string, storage Storage, bucket string, ctx context.Context) ([]io.Reader, error) {
	if _, err := prepareZoomFilters(workdir, weekLogNames, storage, bucket, 0, ctx); err != nil {
		return nil, err
	}
	result := make([]io.Reader, 0, len(weekLogNames))
	for _, name := range weekLogNames {
		f, err := os.Open(filepath.Join(workdir, zoomFilterCacheName(name)))
		if err != nil {
			return nil, err
		}
		result = append(result, brotli.NewReader(f))
	}
	return result, nil
}

// ensureZoomFilter makes sure the zoom filter's corrections for a weekly
// log file are cached in workdir: already there, downloaded from object
// storage, or computed (and then uploaded).
func ensureZoomFilter(workdir, weekLogName string, storage Storage, bucket string) error {
	logger := log.Default()
	ctx := context.Background()
	cacheName := zoomFilterCacheName(weekLogName)
	cachePath := filepath.Join(workdir, cacheName)
	if _, err := os.Stat(cachePath); err == nil {
		return nil
	}
	remotePath := "internal/osmviews-builder/" + cacheName
	if storage != nil {
		if _, err := storage.Stat(ctx, bucket, remotePath); err == nil {
			logger.Printf("loading s3://%s/%s to %s", bucket, remotePath, cachePath)
			return Download(storage, bucket, remotePath, cachePath)
		}
	}

	logPath := filepath.Join(workdir, weekLogName)
	open := func() (io.ReadCloser, error) {
		f, err := os.Open(logPath)
		if err != nil {
			return nil, err
		}
		return struct {
			io.Reader
			io.Closer
		}{brotli.NewReader(f), f}, nil
	}
	start := time.Now()
	corr, stats, err := FilterWeek(open, weekLogDays(weekLogName))
	if err != nil {
		return err
	}
	logger.Printf("zoom filter for %s: %d corrections in %s; %s",
		weekLogName, len(corr), time.Since(start).Round(time.Second), stats)

	// Write to a temporary file and rename it once complete, so a crash
	// never leaves a truncated cache behind.
	tmpPath := cachePath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := writeTileCorrections(out, corr); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return err
	}
	if storage != nil {
		if err := storage.PutFile(ctx, bucket, remotePath, cachePath, "application/x-brotli"); err != nil {
			return fmt.Errorf("uploading s3://%s/%s: %w", bucket, remotePath, err)
		}
	}
	return nil
}

// writeTileCorrections writes corrections as brotli-compressed
// "zoom/x/y count" lines, the same format as the weekly tile logs.
func writeTileCorrections(w io.Writer, corr []TileCorrection) error {
	bw := brotli.NewWriterLevel(w, 6)
	buf := bufio.NewWriter(bw)
	for _, c := range corr {
		zoom, x, y := c.Key.ZoomXY()
		if _, err := fmt.Fprintf(buf, "%d/%d/%d %d\n", zoom, x, y, c.Count); err != nil {
			return err
		}
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	return bw.Close()
}

// correctionStream reads TileKey-sorted "zoom/x/y count" corrections
// lazily, so the corrections for a year of weeks need not fit in memory.
type correctionStream struct {
	scanner *bufio.Scanner
	next    TileCorrection
	ok      bool // next is valid
	err     error
}

func newCorrectionStream(r io.Reader) *correctionStream {
	c := &correctionStream{scanner: bufio.NewScanner(r)}
	c.advance()
	return c
}

func (c *correctionStream) advance() {
	if !c.scanner.Scan() {
		c.ok = false
		c.err = c.scanner.Err()
		return
	}
	tc, ok := parseTileLine(c.scanner.Bytes())
	if !ok {
		c.ok = false
		c.err = fmt.Errorf("malformed tile correction %q", c.scanner.Text())
		return
	}
	if c.ok && tc.Key <= c.next.Key {
		c.ok = false
		c.err = fmt.Errorf("tile corrections not sorted at %s", tc.Key)
		return
	}
	c.next = TileCorrection(tc)
	c.ok = true
}

// lookup returns the corrected count for key, if there is one. Keys must
// be looked up in increasing order.
func (c *correctionStream) lookup(key TileKey) (uint64, bool) {
	for c.ok && c.next.Key < key {
		c.advance()
	}
	if c.ok && c.next.Key == key {
		count := c.next.Count
		c.advance()
		return count, true
	}
	return 0, false
}

// readTileCorrections reads all corrections written by writeTileCorrections.
func readTileCorrections(r io.Reader) ([]TileCorrection, error) {
	var corr []TileCorrection
	c := newCorrectionStream(r)
	for c.ok {
		corr = append(corr, c.next)
		c.advance()
	}
	return corr, c.err
}
