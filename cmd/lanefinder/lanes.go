// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"container/heap"
	"math"
	"sort"
)

// Tile is one tile of a tile log at the zoom level being analyzed.
type Tile struct {
	X, Y  uint32
	Count uint64
}

// Params tunes lane and center detection. All distances are in tiles
// at the analyzed zoom level.
type Params struct {
	// Thin-structure filter: a tile only votes in the Hough transform
	// if at most MaxFill of the tiles in its (2·FillRadius+1)² window
	// appear in the log. This removes land, where nearly every tile is
	// viewed, and keeps narrow lanes over sparsely viewed areas.
	FillRadius int
	MaxFill    float64

	// Hough accumulator resolution.
	ThetaBins int     // number of angle bins over [0, π)
	RhoBin    float64 // width of one distance bin

	// Lane acceptance.
	Tolerance  float64 // max perpendicular distance of a lane's tiles
	MaxGap     float64 // max gap along a lane before it is split
	MinTiles   int     // min number of tiles in a lane
	MinLength  float64 // min length of a lane
	MinDensity float64 // min tiles per unit of lane length

	// Center detection.
	MinLanesPerCenter int // min number of lanes converging on a center
	MaxLanes          int // stop after this many lanes
}

// DefaultParams returns parameters that work on synthetic data at zoom 12.
// They are a starting point for exploring real tile logs.
func DefaultParams() Params {
	return Params{
		FillRadius:        4,
		MaxFill:           0.35,
		ThetaBins:         2048,
		RhoBin:            2,
		Tolerance:         1.5,
		MaxGap:            8,
		MinTiles:          40,
		MinLength:         60,
		MinDensity:        0.4,
		MinLanesPerCenter: 3,
		MaxLanes:          1000,
	}
}

// Point is a position in tile coordinates at the analyzed zoom level.
// Straight lines in this space are straight lines in Web Mercator.
type Point struct{ X, Y float64 }

func (p Point) Sub(q Point) Point     { return Point{p.X - q.X, p.Y - q.Y} }
func (p Point) Add(q Point) Point     { return Point{p.X + q.X, p.Y + q.Y} }
func (p Point) Scale(f float64) Point { return Point{p.X * f, p.Y * f} }
func (p Point) Dot(q Point) float64   { return p.X*q.X + p.Y*q.Y }
func (p Point) Dist(q Point) float64  { return math.Hypot(p.X-q.X, p.Y-q.Y) }
func tileCenter(t Tile) Point         { return Point{float64(t.X) + 0.5, float64(t.Y) + 0.5} }
func perp(p Point) Point              { return Point{-p.Y, p.X} }
func cross(p, q Point) float64        { return p.X*q.Y - p.Y*q.X }
func median(v []uint64) uint64 {
	s := append([]uint64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
func absf(x float64) float64                { return math.Abs(x) }
func lineDist(p, origin, dir Point) float64 { return absf(cross(dir, p.Sub(origin))) }

// Lane is a straight run of viewed tiles, as left by a map animating
// between two places.
type Lane struct {
	A, B        Point  // end points, A..B in direction Dir
	Dir         Point  // unit vector from A to B
	Tiles       []Tile // the lane's tiles, sorted from A to B
	MedianCount uint64 // median view count of the lane's tiles
	Center      int    // index into Centers, or -1
}

func (l *Lane) Length() float64 { return l.A.Dist(l.B) }

// Center is a point where several lanes converge, typically a map’s
// default view such as Null Island.
type Center struct {
	P     Point
	Lanes []int   // indices into Lanes
	Rays  [][]int // Lanes grouped by bearing from P; see groupRays
}

// Result is the outcome of FindLanes.
type Result struct {
	Zoom    uint8
	Voters  int // number of tiles that passed the thin-structure filter
	Lanes   []*Lane
	Centers []*Center
}

// FindLanes detects lanes and their convergence centers in the tiles of
// one zoom level.
func FindLanes(zoom uint8, tiles []Tile, p Params) *Result {
	voters := thinTiles(tiles, p)
	lanes := houghLanes(zoom, voters, p)
	centers := findCenters(lanes, p)
	return &Result{Zoom: zoom, Voters: len(voters), Lanes: lanes, Centers: centers}
}

// thinTiles returns the tiles that lie in sparsely viewed surroundings.
func thinTiles(tiles []Tile, p Params) []Tile {
	occupied := make(map[uint64]struct{}, len(tiles))
	key := func(x, y int64) uint64 { return uint64(x)<<32 | uint64(uint32(y)) }
	for _, t := range tiles {
		occupied[key(int64(t.X), int64(t.Y))] = struct{}{}
	}
	r := int64(p.FillRadius)
	window := float64((2*r + 1) * (2*r + 1))
	maxCount := int(p.MaxFill * window)
	result := make([]Tile, 0, len(tiles)/4)
	for _, t := range tiles {
		n := 0
		x0, y0 := int64(t.X), int64(t.Y)
	scan:
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if _, ok := occupied[key(x0+dx, y0+dy)]; ok {
					n++
					if n > maxCount {
						break scan
					}
				}
			}
		}
		if n <= maxCount {
			result = append(result, t)
		}
	}
	return result
}

// houghLanes runs a progressive probabilistic-style Hough transform:
// accumulate votes once, then visit accumulator peaks from strongest
// to weakest, re-counting each peak's support among tiles that are not
// yet part of an accepted lane.
func houghLanes(zoom uint8, tiles []Tile, p Params) []*Lane {
	if len(tiles) == 0 {
		return nil
	}
	size := float64(uint64(1) << zoom)
	rhoMin := -size
	numRho := int(math.Ceil((size*math.Sqrt2-rhoMin)/p.RhoBin)) + 1
	numTheta := p.ThetaBins
	cosT := make([]float64, numTheta)
	sinT := make([]float64, numTheta)
	for i := range numTheta {
		theta := math.Pi * float64(i) / float64(numTheta)
		cosT[i], sinT[i] = math.Cos(theta), math.Sin(theta)
	}

	pts := make([]Point, len(tiles))
	acc := make([]int32, numTheta*numRho)
	for i, t := range tiles {
		pts[i] = tileCenter(t)
		for ti := range numTheta {
			rho := pts[i].X*cosT[ti] + pts[i].Y*sinT[ti]
			ri := int((rho - rhoMin) / p.RhoBin)
			acc[ti*numRho+ri]++
		}
	}

	// Collect local maxima as candidates.
	var cands candHeap
	minVotes := int32(p.MinTiles)
	for ti := range numTheta {
		for ri := 1; ri < numRho-1; ri++ {
			v := acc[ti*numRho+ri]
			if v < minVotes {
				continue
			}
			isMax := true
			for dt := -1; dt <= 1 && isMax; dt++ {
				tj := (ti + dt + numTheta) % numTheta
				for dr := -1; dr <= 1; dr++ {
					if (dt != 0 || dr != 0) && acc[tj*numRho+ri+dr] > v {
						isMax = false
						break
					}
				}
			}
			if isMax {
				cands = append(cands, cand{votes: v, theta: ti, rho: ri})
			}
		}
	}
	acc = nil
	heap.Init(&cands)

	idx := newPointIndex(pts, size)
	used := make([]bool, len(pts))
	var lanes []*Lane
	for cands.Len() > 0 && len(lanes) < p.MaxLanes {
		c := heap.Pop(&cands).(cand)
		normal := Point{cosT[c.theta], sinT[c.theta]}
		rho := rhoMin + (float64(c.rho)+0.5)*p.RhoBin
		var inliers []int
		idx.nearLine(normal.Scale(rho), perp(normal), p.RhoBin, func(i int) {
			if !used[i] {
				inliers = append(inliers, i)
			}
		})
		if v := int32(len(inliers)); v < c.votes {
			// Support dropped since accumulation; retry later.
			if v >= minVotes {
				c.votes = v
				heap.Push(&cands, c)
			}
			continue
		}
		lane := fitLane(idx, tiles, used, inliers, p)
		if lane == nil {
			continue
		}
		lanes = append(lanes, lane)
	}
	return lanes
}

// fitLane refines a Hough candidate with a total-least-squares fit, then
// keeps the longest run of tiles without big gaps.
func fitLane(idx *pointIndex, tiles []Tile, used []bool, inliers []int, p Params) *Lane {
	pts := idx.pts
	for iter := 0; iter < 2; iter++ {
		origin, dir := fitLine(pts, inliers)
		var next []int
		idx.nearLine(origin, dir, p.Tolerance, func(i int) {
			if !used[i] {
				next = append(next, i)
			}
		})
		if len(next) < p.MinTiles {
			return nil
		}
		inliers = next
	}
	origin, dir := fitLine(pts, inliers)
	sort.Slice(inliers, func(a, b int) bool {
		return pts[inliers[a]].Sub(origin).Dot(dir) < pts[inliers[b]].Sub(origin).Dot(dir)
	})

	bestStart, bestEnd, start := 0, 0, 0
	for i := 1; i <= len(inliers); i++ {
		if i == len(inliers) || pts[inliers[i]].Sub(pts[inliers[i-1]]).Dot(dir) > p.MaxGap {
			if i-start > bestEnd-bestStart {
				bestStart, bestEnd = start, i
			}
			start = i
		}
	}
	run := inliers[bestStart:bestEnd]
	if len(run) < p.MinTiles {
		return nil
	}
	ta := pts[run[0]].Sub(origin).Dot(dir)
	tb := pts[run[len(run)-1]].Sub(origin).Dot(dir)
	length := tb - ta
	if length < p.MinLength || float64(len(run))/length < p.MinDensity {
		return nil
	}

	lane := &Lane{
		A:      origin.Add(dir.Scale(ta)),
		B:      origin.Add(dir.Scale(tb)),
		Dir:    dir,
		Center: -1,
	}
	counts := make([]uint64, len(run))
	for k, i := range run {
		used[i] = true
		lane.Tiles = append(lane.Tiles, tiles[i])
		counts[k] = tiles[i].Count
	}
	lane.MedianCount = median(counts)
	return lane
}

// pointIndex buckets points into a grid of square cells, so that the
// points near a line can be found without scanning all points.
type pointIndex struct {
	pts   []Point
	size  float64 // extent of the world, in tiles
	n     int     // number of cells per row and column
	cells [][]int32
	stamp []uint32 // epoch of the last visit, per cell
	epoch uint32
}

const indexCell = 16.0

func newPointIndex(pts []Point, size float64) *pointIndex {
	n := int(math.Ceil(size / indexCell))
	idx := &pointIndex{pts: pts, size: size, n: n, cells: make([][]int32, n*n), stamp: make([]uint32, n*n)}
	for i, pt := range pts {
		c := idx.cell(pt)
		idx.cells[c] = append(idx.cells[c], int32(i))
	}
	return idx
}

func (idx *pointIndex) cell(pt Point) int {
	cx := min(max(int(pt.X/indexCell), 0), idx.n-1)
	cy := min(max(int(pt.Y/indexCell), 0), idx.n-1)
	return cy*idx.n + cx
}

// nearLine calls fn for every point within tol of the line through
// origin in direction dir, a unit vector. Requires tol < indexCell.
func (idx *pointIndex) nearLine(origin, dir Point, tol float64, fn func(i int)) {
	idx.epoch++
	// Walk along the line in half-cell steps, clipped to the world
	// (plus a margin of one cell), visiting each touched cell and its
	// eight neighbors once.
	lo, hi := math.Inf(-1), math.Inf(1)
	for _, axis := range [2][2]float64{{origin.X, dir.X}, {origin.Y, dir.Y}} {
		o, d := axis[0], axis[1]
		if math.Abs(d) < 1e-12 {
			if o < -indexCell || o > idx.size+indexCell {
				return
			}
			continue
		}
		a, b := (-indexCell-o)/d, (idx.size+indexCell-o)/d
		lo, hi = math.Max(lo, math.Min(a, b)), math.Min(hi, math.Max(a, b))
	}
	for t := lo; t <= hi+indexCell/2; t += indexCell / 2 {
		q := origin.Add(dir.Scale(math.Min(t, hi)))
		cx, cy := int(math.Floor(q.X/indexCell)), int(math.Floor(q.Y/indexCell))
		for y := cy - 1; y <= cy+1; y++ {
			for x := cx - 1; x <= cx+1; x++ {
				if x < 0 || y < 0 || x >= idx.n || y >= idx.n {
					continue
				}
				c := y*idx.n + x
				if idx.stamp[c] == idx.epoch {
					continue
				}
				idx.stamp[c] = idx.epoch
				for _, i := range idx.cells[c] {
					if lineDist(idx.pts[i], origin, dir) <= tol {
						fn(int(i))
					}
				}
			}
		}
	}
}

// fitLine returns the total-least-squares line through pts[idx].
func fitLine(pts []Point, idx []int) (origin, dir Point) {
	for _, i := range idx {
		origin = origin.Add(pts[i])
	}
	origin = origin.Scale(1 / float64(len(idx)))
	var sxx, sxy, syy float64
	for _, i := range idx {
		d := pts[i].Sub(origin)
		sxx += d.X * d.X
		sxy += d.X * d.Y
		syy += d.Y * d.Y
	}
	theta := 0.5 * math.Atan2(2*sxy, sxx-syy)
	return origin, Point{math.Cos(theta), math.Sin(theta)}
}

// pointsTo returns true if lane l, extended beyond one of its ends,
// passes close to c. The allowed miss grows with the distance from the
// lane's end, to allow for the angular error of the fit.
func pointsTo(l *Lane, c Point) bool {
	t := c.Sub(l.A).Dot(l.Dir)
	length := l.Length()
	var beyond float64
	switch {
	case t <= 0.1*length:
		beyond = math.Max(0, -t)
	case t >= 0.9*length:
		beyond = math.Max(0, t-length)
	default:
		return false // c lies along the lane, not beyond one of its ends
	}
	if beyond > 2*length+200 {
		return false
	}
	return lineDist(c, l.A, l.Dir) <= 3+0.02*beyond
}

// findCenters looks for points where several lanes meet when extended.
// Every pair of non-parallel lanes votes for its intersection point;
// dense clusters of intersections become centers, which are then refined
// by a least-squares intersection of their lanes.
func findCenters(lanes []*Lane, p Params) []*Center {
	const cell = 16.0
	grid := make(map[[2]int64][]vote)
	for i := range lanes {
		for j := i + 1; j < len(lanes); j++ {
			a, b := lanes[i], lanes[j]
			denom := cross(a.Dir, b.Dir)
			if absf(denom) < math.Sin(2*math.Pi/180) {
				continue
			}
			s := cross(b.A.Sub(a.A), b.Dir) / denom
			x := a.A.Add(a.Dir.Scale(s))
			if !pointsTo(a, x) || !pointsTo(b, x) {
				continue
			}
			k := [2]int64{int64(math.Floor(x.X / cell)), int64(math.Floor(x.Y / cell))}
			grid[k] = append(grid[k], vote{x, i, j})
		}
	}

	var centers []*Center
	assigned := make([]bool, len(lanes))
	for {
		// Pick the 3×3 grid neighborhood with the most votes from
		// lanes that do not yet belong to a center.
		var best []vote
		for k := range grid {
			var votes []vote
			for dy := int64(-1); dy <= 1; dy++ {
				for dx := int64(-1); dx <= 1; dx++ {
					for _, v := range grid[[2]int64{k[0] + dx, k[1] + dy}] {
						if !assigned[v.i] && !assigned[v.j] {
							votes = append(votes, v)
						}
					}
				}
			}
			if len(votes) > len(best) {
				best = votes
			}
		}
		if len(best) == 0 {
			break
		}
		xs := make([]float64, len(best))
		ys := make([]float64, len(best))
		for k, v := range best {
			xs[k], ys[k] = v.p.X, v.p.Y
		}
		sort.Float64s(xs)
		sort.Float64s(ys)
		c := Point{xs[len(xs)/2], ys[len(ys)/2]}

		// Assign lanes, refine the center, and re-assign once.
		var members []int
		for iter := 0; iter < 2; iter++ {
			members = members[:0]
			for i, l := range lanes {
				if !assigned[i] && pointsTo(l, c) {
					members = append(members, i)
				}
			}
			if len(members) < 2 {
				break
			}
			c = intersect(lanes, members)
		}
		if len(members) < p.MinLanesPerCenter {
			// Too weak; drop these votes so we do not pick them again.
			for k, votes := range grid {
				kept := votes[:0]
				for _, v := range votes {
					if !containsVote(best, v.i, v.j) {
						kept = append(kept, v)
					}
				}
				grid[k] = kept
			}
			continue
		}
		center := &Center{P: c, Lanes: append([]int(nil), members...)}
		for _, i := range members {
			assigned[i] = true
			lanes[i].Center = len(centers)
			// Orient the lane so that A is the end closer to the center.
			if lanes[i].B.Dist(c) < lanes[i].A.Dist(c) {
				reverse(lanes[i])
			}
		}
		center.Rays = groupRays(lanes, center)
		centers = append(centers, center)
	}
	return centers
}

// groupRays groups a center's lanes by their bearing from the center.
// Where a lane crosses densely viewed land, the thin-structure filter
// removes its tiles there, and the lane is found as two collinear pieces;
// both pieces belong to the same ray.
func groupRays(lanes []*Lane, c *Center) [][]int {
	const maxDiff = 1.0 * math.Pi / 180
	bearing := func(i int) float64 {
		d := lanes[i].B.Sub(c.P)
		return math.Atan2(d.Y, d.X)
	}
	members := append([]int(nil), c.Lanes...)
	sort.Slice(members, func(a, b int) bool { return bearing(members[a]) < bearing(members[b]) })
	var rays [][]int
	for k, i := range members {
		if k > 0 && bearing(i)-bearing(members[k-1]) < maxDiff {
			rays[len(rays)-1] = append(rays[len(rays)-1], i)
		} else {
			rays = append(rays, []int{i})
		}
	}
	// Bearings wrap around at ±π.
	if n := len(rays); n > 1 && bearing(rays[0][0])+2*math.Pi-bearing(rays[n-1][len(rays[n-1])-1]) < maxDiff {
		rays[0] = append(rays[n-1], rays[0]...)
		rays = rays[:n-1]
	}
	return rays
}

// vote is an intersection point of lanes i and j.
type vote struct {
	p    Point
	i, j int
}

func containsVote(votes []vote, i, j int) bool {
	for _, v := range votes {
		if v.i == i && v.j == j {
			return true
		}
	}
	return false
}

// intersect returns the point minimizing the sum of squared distances
// to the given lanes' lines.
func intersect(lanes []*Lane, idx []int) Point {
	var a11, a12, a22, b1, b2 float64
	for _, i := range idx {
		n := perp(lanes[i].Dir)
		d := n.Dot(lanes[i].A)
		a11 += n.X * n.X
		a12 += n.X * n.Y
		a22 += n.Y * n.Y
		b1 += n.X * d
		b2 += n.Y * d
	}
	det := a11*a22 - a12*a12
	return Point{(a22*b1 - a12*b2) / det, (a11*b2 - a12*b1) / det}
}

func reverse(l *Lane) {
	l.A, l.B = l.B, l.A
	l.Dir = l.Dir.Scale(-1)
	for i, j := 0, len(l.Tiles)-1; i < j; i, j = i+1, j-1 {
		l.Tiles[i], l.Tiles[j] = l.Tiles[j], l.Tiles[i]
	}
}

type cand struct {
	votes      int32
	theta, rho int
}

type candHeap []cand

func (h candHeap) Len() int           { return len(h) }
func (h candHeap) Less(i, j int) bool { return h[i].votes > h[j].votes }
func (h candHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candHeap) Push(x any)        { *h = append(*h, x.(cand)) }
func (h *candHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// ToLatLng converts tile coordinates at zoom to WGS84 degrees.
func ToLatLng(p Point, zoom uint8) (lat, lng float64) {
	n := float64(uint64(1) << zoom)
	lng = p.X/n*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*p.Y/n))) * 180 / math.Pi
	return lat, lng
}
