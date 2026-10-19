// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
)

// The lane filter is the second stage of the zoom filter. The cross-zoom
// stage (zoomfilter.go) removes most of each lane, but leaves faint traces:
// stretches where the lane covers several neighbouring zooms, so that no
// zoom stands out, and short blobs that look like small towns. The lane
// filter finds the lanes as whole lines and cleans along them.
//
// Lanes are straight lines in plain longitude/latitude through Null Island
// (they curve on the Web Mercator map), and the cross-zoom stage has
// already reduced many tiles along them. Per zoom, the lane filter bins
// those reduced tiles ("seeds") by their bearing from Null Island and
// accepts a bearing as a lane when its seeds spread over many distances
// from Null Island. Along each lane, it compares a strip of 2·laneHalf+1
// tiles with the strips beside it, in stretches of laneSegment tiles. A
// lane must show a clear excess (laneMinRatio times its sides) in most
// stretches; then each such stretch is scaled down to the level of its
// sides. Stretches with a busy background, like a city the lane happens to
// cross, are left alone: there the lane hardly shows, and scaling the
// strip would cut into real use.
//
// Finally, the filter drops all requests for tiles that lie entirely
// within nullIslandRadius degrees of Null Island. That area is open sea;
// the requests there come from map clients that start at (0, 0).
const (
	laneMinZoom = zoomFilterMinZoom
	laneMaxZoom = zoomFilterMaxZoom

	laneMaxDist     = 70.0 // only look this many degrees (lon/lat) from Null Island
	laneAngleBin    = 0.05 // bearing bins, degrees
	laneRadialBin   = 0.5  // distance bins, degrees
	laneMinSpan     = 15   // a lane's seeds cover at least this many distance bins
	laneMaxLines    = 40   // at most this many lanes per zoom
	laneSuppress    = 0.3  // no second lane within this many degrees of bearing
	laneHalf        = 2.0  // the lane strip is |offset| <= laneHalf tiles
	laneSide0       = 4.0  // the side strips are laneSide0..laneSide1 tiles away
	laneSide1       = 10.0
	laneSegment     = 16.0 // stretch length, tiles
	laneMinRatio    = 2.0  // a stretch is hot if the strip exceeds its sides this much
	laneMinFrac     = 0.5  // a lane is hot in at least this share of its stretches...
	laneMinSegments = 4    // ...and in at least this many
	laneExtend      = 4.0  // beyond the seeds, follow hot stretches this many degrees apart
	laneBusyPerDay  = 10.0 // leave stretches whose sides average more requests per tile and day

	nullIslandRadius = 3.0  // tiles entirely within this many degrees of Null Island
	nullIslandRing   = 6.0  // ... are lowered to the background of the ring out to here
	nullIslandTrim   = 0.05 // fraction of the ring's busiest positions left out of its background
)

// laneSeed is a tile that the cross-zoom stage reduced.
type laneSeed struct {
	x, y    uint32
	removed float64
}

// laneLine is a lane found at one zoom: its bearing from Null Island in
// lon/lat degrees (counter-clockwise from east), and how far its seeds reach.
type laneLine struct {
	bearing float64
	reach   float64
	support int
	end     float64 // how far it was cleaned; set by laneClean
	hot     int     // stretches cleaned; set by laneClean
}

// laneTile is a tile near some lane, collected in the second pass.
type laneTile struct {
	x, y        uint32
	count       float64 // current count, lowered by laneClean
	orig        float64 // count after the cross-zoom stage
	angle, dist float64 // bearing and distance from Null Island, lon/lat degrees
	annulus     bool    // center lies in the background ring around Null Island
}

// LaneFilterStats summarises what the lane filter did to one week.
type LaneFilterStats struct {
	Zooms      []LaneFilterZoomStats
	NullIsland uint64 // requests removed around Null Island
}

type LaneFilterZoomStats struct {
	Zoom    uint8
	Lines   []laneLine
	Removed uint64 // requests removed by the lane filter, on top of the cross-zoom stage
}

func (s LaneFilterStats) String() string {
	var b bytes.Buffer
	for _, z := range s.Zooms {
		fmt.Fprintf(&b, "z%d: %d lanes, removed %d more;", z.Zoom, len(z.Lines), z.Removed)
		for i, l := range z.Lines {
			if i < 6 {
				fmt.Fprintf(&b, " %.2f°", l.bearing)
			}
		}
		b.WriteString("; ")
	}
	fmt.Fprintf(&b, "Null Island: removed %d", s.NullIsland)
	return b.String()
}

// tileLonLat returns the lon/lat of a point given in tile units at zoom z.
func tileLonLat(n float64, tx, ty float64) (lon, lat float64) {
	lon = tx/n*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*ty/n))) * 180 / math.Pi
	return lon, lat
}

// mercatorY returns the tile-unit y coordinate of latitude lat at zoom z.
func mercatorY(n float64, lat float64) float64 {
	r := lat * math.Pi / 180
	return (1 - math.Log(math.Tan(r)+1/math.Cos(r))/math.Pi) / 2 * n
}

// laneDetect finds lanes among the seeds of one zoom.
func laneDetect(zoom uint8, seeds []laneSeed) []laneLine {
	if len(seeds) == 0 {
		return nil
	}
	n := float64(uint64(1) << zoom)
	numBins := int(math.Round(360 / laneAngleBin))
	numRadial := int(laneMaxDist/laneRadialBin) + 1
	type seedPos struct {
		angle, dist, weight float64
		bin                 int
	}
	pos := make([]seedPos, 0, len(seeds))
	occupied := make([]bool, numBins*numRadial)
	support := make([]float64, numBins)
	for _, s := range seeds {
		lon, lat := tileLonLat(n, float64(s.x)+0.5, float64(s.y)+0.5)
		dist := math.Hypot(lon, lat)
		if dist > laneMaxDist {
			continue
		}
		angle := math.Mod(math.Atan2(lat, lon)*180/math.Pi+360, 360)
		bin := min(int(angle/laneAngleBin), numBins-1)
		rb := min(int(dist/laneRadialBin), numRadial-1)
		if !occupied[bin*numRadial+rb] {
			occupied[bin*numRadial+rb] = true
			support[bin]++
		}
		pos = append(pos, seedPos{angle, dist, s.removed, bin})
	}
	sup3 := make([]float64, numBins)
	for i := range sup3 {
		sup3[i] = support[(i+numBins-1)%numBins] + support[i] + support[(i+1)%numBins]
	}
	near := func(a, b int) bool {
		d := (a - b + numBins) % numBins
		return d <= 1 || d >= numBins-1
	}
	suppress := int(math.Round(laneSuppress / laneAngleBin))
	var lines []laneLine
	for len(lines) < laneMaxLines {
		best := 0
		for i, v := range sup3 {
			if v > sup3[best] {
				best = i
			}
		}
		if sup3[best] < laneMinSpan {
			break
		}
		// Bearing: weighted circular mean of the seeds in the three bins.
		var sx, sy, w, reach float64
		for _, p := range pos {
			if near(p.bin, best) {
				r := p.angle * math.Pi / 180
				sx += p.weight * math.Cos(r)
				sy += p.weight * math.Sin(r)
				w += p.weight
				reach = max(reach, p.dist)
			}
		}
		if w > 0 {
			bearing := math.Mod(math.Atan2(sy, sx)*180/math.Pi+360, 360)
			lines = append(lines, laneLine{bearing: bearing, reach: reach, support: int(sup3[best])})
		}
		for d := -suppress; d <= suppress; d++ {
			sup3[(best+d+numBins)%numBins] = 0
		}
	}
	return lines
}

// laneGeometry locates a tile relative to a lane: how far along the lane
// it lies (lon/lat degrees from Null Island) and how far off the lane
// (in tiles, perpendicular on the Mercator map).
type laneGeometry struct {
	c, s  float64 // cosine and sine of the bearing
	byLon bool    // the lane is closer to horizontal than vertical
	n     float64 // 2^zoom
}

func newLaneGeometry(zoom uint8, bearing float64) laneGeometry {
	r := bearing * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	n := float64(uint64(1) << zoom)
	return laneGeometry{c: c, s: s, byLon: math.Abs(c) >= math.Abs(s), n: n}
}

func (g laneGeometry) locate(x, y uint32) (along, off float64) {
	tx, ty := float64(x)+0.5, float64(y)+0.5
	lon, lat := tileLonLat(g.n, tx, ty)
	along = lon*g.c + lat*g.s
	if g.byLon {
		rl := max(-85, min(85, lon*g.s/g.c))
		ry := mercatorY(g.n, rl)
		slope := math.Abs(g.s/g.c) / math.Cos(rl*math.Pi/180)
		off = (ty - ry) / math.Sqrt(1+slope*slope)
	} else {
		rx := (lat*g.c/g.s + 180) / 360 * g.n
		slope := math.Abs(g.c/g.s) * math.Cos(lat*math.Pi/180)
		off = (tx - rx) / math.Sqrt(1+slope*slope)
	}
	return along, off
}

// laneNear reports whether a tile at zoom z could lie within laneSide1
// tiles of any of the lines; it is a cheap bearing test that lets the
// second pass skip most tiles.
func laneNear(n float64, x, y uint32, lines []laneLine) (near bool, angle, dist float64) {
	lon, lat := tileLonLat(n, float64(x)+0.5, float64(y)+0.5)
	dist = math.Hypot(lon, lat)
	if dist > laneMaxDist+1 || dist < 1e-9 {
		return false, 0, dist
	}
	angle = math.Atan2(lat, lon) * 180 / math.Pi
	for _, l := range lines {
		if laneBearingNear(n, angle, dist, l.bearing) {
			return true, angle, dist
		}
	}
	return false, angle, dist
}

// laneBearingNear reports whether a point at the given bearing and
// distance from Null Island could lie within laneSide1 tiles of a lane
// with bearing b. One tile spans at most 360/n degrees in lon or lat.
func laneBearingNear(n, angle, dist, b float64) bool {
	tol := math.Asin(math.Min(1, (laneSide1+2)*360/n/dist))*180/math.Pi + 0.01
	return math.Abs(math.Mod(angle-b+540, 360)-180) <= tol
}

// laneClean cleans along the lines of one zoom. It changes the counts of
// tiles in place and returns the lines that turned out to be lanes.
func laneClean(zoom uint8, lines []laneLine, tiles []laneTile, numDays int) []laneLine {
	n := float64(uint64(1) << zoom)
	half := n / 2
	maxBusy := laneBusyPerDay * float64(max(numDays, 1))
	var kept []laneLine
	type stretch struct {
		along     float64 // mean position along the lane, degrees
		on        []int   // indices of the tiles in the lane strip
		onSum, bg float64 // requests in the lane strip, and expected from the sides
		hot       bool
		sumAlong  float64
		numTiles  int
		sideSum   float64
	}
	for _, line := range lines {
		g := newLaneGeometry(zoom, line.bearing)
		stretches := make(map[int]*stretch)
		for i, t := range tiles {
			if !laneBearingNear(n, t.angle, t.dist, line.bearing) {
				continue
			}
			along, off := g.locate(t.x, t.y)
			off = math.Abs(off)
			if off > laneSide1 || along <= 0 || along > laneMaxDist {
				continue
			}
			rm := math.Hypot(float64(t.x)+0.5-half, float64(t.y)+0.5-half)
			k := int(math.Floor(rm / laneSegment))
			st := stretches[k]
			if st == nil {
				st = &stretch{}
				stretches[k] = st
			}
			st.sumAlong += along
			st.numTiles++
			if off <= laneHalf {
				st.on = append(st.on, i)
				st.onSum += t.count
			} else if off >= laneSide0 {
				st.sideSum += t.count
			}
		}
		var segs []*stretch
		for _, st := range stretches {
			if len(st.on) == 0 || st.onSum <= 0 {
				continue
			}
			st.along = st.sumAlong / float64(st.numTiles)
			st.bg = st.sideSum * (2*laneHalf + 1) / (2 * (laneSide1 - laneSide0 + 1))
			busy := st.bg/(2*laneHalf+1)/laneSegment > maxBusy
			st.hot = st.onSum > laneMinRatio*math.Max(st.bg, 1) && !busy
			segs = append(segs, st)
		}
		sort.Slice(segs, func(i, j int) bool { return segs[i].along < segs[j].along })

		inner, hot := 0, 0
		for _, st := range segs {
			if st.along <= line.reach+2 {
				inner++
				if st.hot {
					hot++
				}
			}
		}
		if hot < laneMinSegments || float64(hot) < laneMinFrac*float64(inner) {
			continue
		}

		// Follow the lane beyond its seeds while hot stretches continue.
		end := line.reach + 2
		var beyond []float64
		for _, st := range segs {
			if st.hot && st.along > end {
				beyond = append(beyond, st.along)
			}
		}
		for i, d := range beyond {
			if d > end+laneExtend {
				break
			}
			count := 0
			for _, e := range beyond[i:] {
				if e <= d+laneExtend {
					count++
				}
			}
			if count >= 2 {
				end = max(end, d)
			}
		}

		line.end = end
		for _, st := range segs {
			if !st.hot || st.along > end+0.5 {
				continue
			}
			f := math.Max(st.bg, 0) / st.onSum
			for _, i := range st.on {
				tiles[i].count = math.Round(tiles[i].count * f)
			}
			line.hot++
		}
		kept = append(kept, line)
	}
	return kept
}

// inNullIslandRing reports whether a tile's center lies in the ring
// between nullIslandRadius and nullIslandRing degrees around Null Island.
// Its mean count per tile is the background that replaces the counts
// inside the circle.
func inNullIslandRing(zoom uint8, x, y uint32) bool {
	if zoom < 7 {
		return false
	}
	lon, lat := tileLonLat(float64(uint64(1)<<zoom), float64(x)+0.5, float64(y)+0.5)
	d := math.Hypot(lon, lat)
	return d > nullIslandRadius && d <= nullIslandRing
}

// nullIslandRingTiles approximates the number of tile positions in the
// ring; near the equator, a tile spans about 360/n degrees both ways.
func nullIslandRingTiles(zoom uint8) float64 {
	s := float64(uint64(1)<<zoom) / 360
	return math.Pi * (nullIslandRing*nullIslandRing - nullIslandRadius*nullIslandRadius) * s * s
}

// nullIslandBackground returns the background count per tile in the ring
// around Null Island, given the counts of the ring's tiles in the logs.
// Positions without requests count as zero. The busiest nullIslandTrim of
// positions are left out, so a city in the ring (Accra lies 5.6° from
// Null Island) or the remains of a lane do not raise the background.
func nullIslandBackground(zoom uint8, counts []float64) uint64 {
	positions := nullIslandRingTiles(zoom)
	trim := int(positions * nullIslandTrim)
	if positions-float64(trim) < 1 {
		return 0
	}
	slices.Sort(counts)
	var sum float64
	for _, c := range counts[:max(len(counts)-trim, 0)] {
		sum += c
	}
	return uint64(math.Round(sum / (positions - float64(trim))))
}

// insideNullIsland reports whether a tile lies entirely within
// nullIslandRadius degrees (lon/lat) of Null Island.
func insideNullIsland(zoom uint8, x, y uint32) bool {
	if zoom < 7 {
		return false
	}
	n := float64(uint64(1) << zoom)
	for _, dx := range [2]float64{0, 1} {
		for _, dy := range [2]float64{0, 1} {
			lon, lat := tileLonLat(n, float64(x)+dx, float64(y)+dy)
			if math.Hypot(lon, lat) > nullIslandRadius {
				return false
			}
		}
	}
	return true
}

// laneFilter runs the lane filter's second pass over one week of tile logs
// in TileKey order. corr holds the cross-zoom stage's corrections, sorted
// by TileKey; the result combines them with the lane filter's own.
func laneFilter(r io.Reader, corr []TileCorrection, seeds [laneMaxZoom + 1][]laneSeed, numDays int) ([]TileCorrection, LaneFilterStats, error) {
	var stats LaneFilterStats
	var lines [laneMaxZoom + 1][]laneLine
	for z := laneMinZoom; z <= laneMaxZoom; z++ {
		lines[z] = laneDetect(uint8(z), seeds[z])
	}

	var near [laneMaxZoom + 1][]laneTile
	var island []TileCorrection
	var ring [maxZoom + 1][]float64 // counts of the tiles in the ring around Null Island
	scanner := bufio.NewScanner(r)
	ci := 0 // next correction to look at; corr and the logs are both in TileKey order
	for scanner.Scan() {
		tc, ok := parseTileLine(scanner.Bytes())
		if !ok || tc.Key.Zoom() > maxZoom {
			continue
		}
		for ci < len(corr) && corr[ci].Key < tc.Key {
			ci++
		}
		count := float64(tc.Count)
		if ci < len(corr) && corr[ci].Key == tc.Key {
			count = float64(corr[ci].Count)
		}
		zoom, x, y := tc.Key.ZoomXY()
		if insideNullIsland(zoom, x, y) {
			if count > 0 {
				island = append(island, TileCorrection{Key: tc.Key, Count: uint64(count)})
			}
			continue
		}
		inRing := inNullIslandRing(zoom, x, y)
		z := int(zoom)
		if z >= laneMinZoom && z <= laneMaxZoom && len(lines[z]) > 0 {
			if ok, angle, dist := laneNear(float64(uint64(1)<<zoom), x, y, lines[z]); ok {
				near[z] = append(near[z], laneTile{x: x, y: y, count: count, orig: count, angle: angle, dist: dist, annulus: inRing})
				continue // added to the ring sum after cleaning
			}
		}
		if inRing {
			ring[zoom] = append(ring[zoom], count)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, stats, err
	}

	var laneCorr []TileCorrection
	for z := laneMinZoom; z <= laneMaxZoom; z++ {
		st := LaneFilterZoomStats{Zoom: uint8(z)}
		if len(lines[z]) > 0 {
			st.Lines = laneClean(uint8(z), lines[z], near[z], numDays)
		}
		for _, t := range near[z] {
			if t.annulus {
				ring[z] = append(ring[z], t.count)
			}
			if t.count < t.orig {
				st.Removed += uint64(t.orig - t.count)
				laneCorr = append(laneCorr, TileCorrection{
					Key:   MakeTileKey(uint8(z), t.x, t.y),
					Count: uint64(t.count),
				})
			}
		}
		stats.Zooms = append(stats.Zooms, st)
	}

	// Inside the circle, lower each tile to the ring's background.
	var bgs [maxZoom + 1]uint64
	for z := range ring {
		bgs[z] = nullIslandBackground(uint8(z), ring[z])
	}
	j := 0
	for _, tc := range island {
		bg := bgs[tc.Key.Zoom()]
		if bg >= tc.Count {
			continue
		}
		stats.NullIsland += tc.Count - bg
		island[j] = TileCorrection{Key: tc.Key, Count: bg}
		j++
	}
	island = island[:j]

	// Combine: Null Island first, then the lane filter, then the cross-zoom stage.
	all := make([]TileCorrection, 0, len(corr)+len(laneCorr)+len(island))
	all = append(all, island...)
	all = append(all, laneCorr...)
	all = append(all, corr...)
	slices.SortStableFunc(all, compareTileCorrections)
	all = slices.CompactFunc(all, func(a, b TileCorrection) bool { return a.Key == b.Key })
	return all, stats, nil
}
