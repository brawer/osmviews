// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

// Command lanefinder looks for straight lanes of viewed tiles in
// OpenStreetMap tile logs, and for the centers where those lanes
// converge. See README.md.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/ulikunitz/xz"
)

func main() {
	defaults := DefaultParams()
	zoom := flag.Uint("zoom", 12, "zoom level to analyze")
	geojsonPath := flag.String("geojson", "", "if set, write lanes and centers to this GeoJSON file")
	p := defaults
	flag.IntVar(&p.FillRadius, "fill-radius", defaults.FillRadius, "radius of the window for the thin-structure filter, in tiles")
	flag.Float64Var(&p.MaxFill, "max-fill", defaults.MaxFill, "max fraction of viewed tiles in the window for a tile to vote")
	flag.IntVar(&p.ThetaBins, "theta-bins", defaults.ThetaBins, "number of Hough angle bins")
	flag.Float64Var(&p.Tolerance, "tolerance", defaults.Tolerance, "max distance of a lane's tiles from its line, in tiles")
	flag.Float64Var(&p.MaxGap, "max-gap", defaults.MaxGap, "max gap within a lane, in tiles")
	flag.IntVar(&p.MinTiles, "min-tiles", defaults.MinTiles, "min number of tiles in a lane")
	flag.Float64Var(&p.MinLength, "min-length", defaults.MinLength, "min length of a lane, in tiles")
	flag.Float64Var(&p.MinDensity, "min-density", defaults.MinDensity, "min tiles per tile of lane length")
	flag.IntVar(&p.MinLanesPerCenter, "min-lanes", defaults.MinLanesPerCenter, "min number of lanes per center")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: lanefinder [flags] tiles-2026-10-01.txt.xz [more logs...]\n\n")
		fmt.Fprintf(os.Stderr, "Logs may be plain text, .xz (daily OSM logs) or .br (weekly builder logs).\n")
		fmt.Fprintf(os.Stderr, "Counts of the same tile in several logs are summed.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 || *zoom > 20 {
		flag.Usage()
		os.Exit(2)
	}

	start := time.Now()
	tiles, err := readTiles(flag.Args(), uint8(*zoom))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("read %d tiles at zoom %d in %s", len(tiles), *zoom, time.Since(start).Round(time.Millisecond))

	start = time.Now()
	res := FindLanes(uint8(*zoom), tiles, p)
	log.Printf("%d tiles voted, found %d lanes and %d centers in %s",
		res.Voters, len(res.Lanes), len(res.Centers), time.Since(start).Round(time.Millisecond))

	writeReport(os.Stdout, res)
	if *geojsonPath != "" {
		if err := writeGeoJSON(*geojsonPath, res); err != nil {
			log.Fatal(err)
		}
	}
}

var tileLine = regexp.MustCompile(`^(\d+)/(\d+)/(\d+)\s+(\d+)$`)

// readTiles returns the tiles at the given zoom from all logs, with the
// counts of each tile summed across logs.
func readTiles(paths []string, zoom uint8) ([]Tile, error) {
	prefix := strconv.Itoa(int(zoom)) + "/"
	sums := make(map[[2]uint32]uint64)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		var r io.Reader = bufio.NewReaderSize(f, 1<<20)
		switch {
		case strings.HasSuffix(path, ".xz"):
			if r, err = xz.NewReader(r); err != nil {
				f.Close()
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		case strings.HasSuffix(path, ".br"):
			r = brotli.NewReader(r)
		}
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			m := tileLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			x, _ := strconv.ParseUint(m[2], 10, 32)
			y, _ := strconv.ParseUint(m[3], 10, 32)
			n, _ := strconv.ParseUint(m[4], 10, 64)
			if x >= 1<<zoom || y >= 1<<zoom {
				continue
			}
			sums[[2]uint32{uint32(x), uint32(y)}] += n
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	tiles := make([]Tile, 0, len(sums))
	for k, n := range sums {
		tiles = append(tiles, Tile{X: k[0], Y: k[1], Count: n})
	}
	sort.Slice(tiles, func(i, j int) bool {
		if tiles[i].Y != tiles[j].Y {
			return tiles[i].Y < tiles[j].Y
		}
		return tiles[i].X < tiles[j].X
	})
	return tiles, nil
}

func writeReport(w io.Writer, res *Result) {
	for ci, c := range res.Centers {
		lat, lng := ToLatLng(c.P, res.Zoom)
		fmt.Fprintf(w, "center %d at %.4f, %.4f: %d rays, %d lanes\n", ci, lat, lng, len(c.Rays), len(c.Lanes))
		for ri, ray := range c.Rays {
			for _, li := range ray {
				l := res.Lanes[li]
				alat, alng := ToLatLng(l.A, res.Zoom)
				blat, blng := ToLatLng(l.B, res.Zoom)
				fmt.Fprintf(w, "    ray %3d  %9.4f, %9.4f → %9.4f, %9.4f  length=%5.0f tiles=%5d median_count=%d\n",
					ri, alat, alng, blat, blng, l.Length(), len(l.Tiles), l.MedianCount)
			}
		}
	}
	free := 0
	for _, l := range res.Lanes {
		if l.Center < 0 {
			free++
		}
	}
	fmt.Fprintf(w, "%d lanes without a center\n", free)
}

func writeGeoJSON(path string, res *Result) error {
	type geometry struct {
		Type        string `json:"type"`
		Coordinates any    `json:"coordinates"`
	}
	type feature struct {
		Type       string         `json:"type"`
		Geometry   geometry       `json:"geometry"`
		Properties map[string]any `json:"properties"`
	}
	lngLat := func(p Point) []float64 {
		lat, lng := ToLatLng(p, res.Zoom)
		return []float64{lng, lat}
	}
	features := make([]feature, 0, len(res.Lanes)+len(res.Centers))
	for ci, c := range res.Centers {
		features = append(features, feature{
			Type:     "Feature",
			Geometry: geometry{"Point", lngLat(c.P)},
			Properties: map[string]any{
				"kind": "center", "center": ci, "lanes": len(c.Lanes), "zoom": res.Zoom,
			},
		})
	}
	for li, l := range res.Lanes {
		features = append(features, feature{
			Type:     "Feature",
			Geometry: geometry{"LineString", [][]float64{lngLat(l.A), lngLat(l.B)}},
			Properties: map[string]any{
				"kind": "lane", "lane": li, "center": l.Center, "tiles": len(l.Tiles),
				"median_count": l.MedianCount, "zoom": res.Zoom,
			},
		})
	}
	data, err := json.MarshalIndent(map[string]any{"type": "FeatureCollection", "features": features}, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
