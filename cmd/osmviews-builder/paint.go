// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"golang.org/x/sync/errgroup"
)

type Painter struct {
	numWeeks int
	zoom     uint8
	last     TileKey
	raster   *Raster
	writer   *RasterWriter
}

func (p *Painter) Paint(tile TileKey, counts []uint64) error {
	raster, err := p.setupRaster(tile)
	if err != nil {
		return err
	}

	// Compute the median weekly views per km² for this tile.
	numWeeksWithoutData := p.numWeeks - len(counts)
	medianPos := p.numWeeks/2 - numWeeksWithoutData
	var median float32
	if medianPos >= 0 {
		median = float32(counts[medianPos])
	}
	zoom, _, y := tile.ZoomXY()
	viewsPerKm2 := median / float32(TileArea(zoom, y))

	if tile == raster.tile {
		raster.viewsPerKm2 = viewsPerKm2
		if raster.parent != nil {
			raster.viewsPerKm2 += raster.parent.viewsPerKm2
		}
	}

	raster.Paint(tile, viewsPerKm2)

	p.last = tile
	return nil
}

func (p *Painter) setupRaster(tile TileKey) (*Raster, error) {
	rasterTile := tile
	if tile.Zoom() >= p.zoom-8 {
		rasterTile = tile.ToZoom(p.zoom - 8)
	}

	// If the current raster is for rasterTile, we’re already set up.
	if p.raster != nil && rasterTile == p.raster.tile {
		return p.raster, nil
	}

	// Since we’re receiving tiles in pre-order depth-first traversal order,
	// we’re completely done with any parent Rasters that do not contain
	// the new rasterTile. Those can be compressed and stored into the
	// output TIFF file.
	for p.raster != nil && !p.raster.tile.Contains(rasterTile) {
		if err := p.emitRaster(); err != nil {
			return nil, err
		}
	}

	if p.raster == nil {
		p.raster = NewRaster(WorldTile, nil)
		if rasterTile == WorldTile {
			return p.raster, nil
		}
	}

	for t := p.last.Next(p.zoom - 8); t < rasterTile; t = t.Next(p.zoom - 8) {
		if t.Contains(rasterTile) {
			p.raster = NewRaster(t, p.raster)
		} else {
			err := p.writer.WriteUniform(t, uint32(p.raster.viewsPerKm2+0.5))
			if err != nil {
				return nil, err
			}
		}
	}

	p.raster = NewRaster(rasterTile, p.raster)
	return p.raster, nil
}

func (p *Painter) Close() error {
	// For the part of the world we haven't covered yet, emit uniform rasters.
	zoom := p.zoom - 8
	for t := p.last.Next(zoom); t != NoTile; t = t.Next(zoom) {
		for p.raster != nil && !p.raster.tile.Contains(t) {
			if err := p.emitRaster(); err != nil {
				return err
			}
		}
		if err := p.writer.WriteUniform(t, uint32(p.raster.viewsPerKm2+0.5)); err != nil {
			return err
		}
	}

	for p.raster != nil {
		if err := p.emitRaster(); err != nil {
			return err
		}
	}

	return p.writer.Close()
}

// Function emitRaster is called when the Painter has finished painting
// pixels into the current Raster. Its pixels are first subsampled into the
// parent raster (building the GeoTIFF overview pyramid inline), then the
// raster is removed from the tree, compressed, and stored into a temporary
// file.
func (p *Painter) emitRaster() error {
	raster := p.raster
	if raster.parent != nil {
		raster.parent.PaintChild(raster)
	}
	p.raster = raster.parent
	raster.parent = nil
	return p.writer.Write(raster)
}

func NewPainter(path string, numWeeks int, zoom uint8, meta TiffMetadata) (*Painter, error) {
	writer, err := NewRasterWriter(path, zoom-8, meta)
	if err != nil {
		return nil, err
	}
	return &Painter{
		numWeeks: numWeeks,
		zoom:     zoom,
		writer:   writer,
	}, nil
}

// PaintOutput is one GeoTIFF that paint produces.
type PaintOutput struct {
	Path     string
	Meta     TiffMetadata // provenance written into the GeoTIFF's tags
	Filtered bool         // paint the counts after corrections, not before
}

// paint produces GeoTIFF files from a set of weekly tile view counts.
// Tile views at zoom level `zoom` become one pixel in the output GeoTIFF.
// weights[i] scales the counts of tilecounts[i] so that a week with only
// some of its seven days of logs is extrapolated to a full week; a nil
// weights slice means a factor of 1.0 for every week. corrections, if not
// nil, is parallel to tilecounts and replaces the counts of some tiles
// (see mergeTileCounts) for the outputs that are Filtered. All outputs are
// painted in the same pass over the logs, which costs much less time than
// painting them one after the other.
func paint(outputs []PaintOutput, zoom uint8, tilecounts []io.Reader, weights []float64, corrections []io.Reader, ctx context.Context) error {
	logger := log.Default()
	start := time.Now()
	painters := make([]*Painter, len(outputs))
	for i, o := range outputs {
		logger.Printf("starting to paint GeoTIFF, path=%s, zoom=%d, filtered=%v", o.Path, zoom, o.Filtered)
		p, err := NewPainter(o.Path, len(tilecounts), zoom, o.Meta)
		if err != nil {
			return err
		}
		painters[i] = p
	}

	// One goroutine is decompressing, parsing and merging the weekly counts;
	// another is painting the images from data that gets sent over a channel.
	ch := make(chan tileCountPair, 100000)
	g, subCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return mergeTileCountPairs(tilecounts, weights, corrections, ch, subCtx)
	})
	g.Go(func() error {
		tile := WorldTile
		counts := make([][]uint64, len(outputs))
		numCounts := make([]int, len(outputs)) // number of counts for the same tile
		for i := range counts {
			counts[i] = make([]uint64, len(tilecounts))
		}
		painted := 0
		flush := func() error {
			for i, p := range painters {
				if numCounts[i] > 0 {
					if err := p.Paint(tile, counts[i][:numCounts[i]]); err != nil {
						return err
					}
				}
				numCounts[i] = 0
			}
			painted++
			return nil
		}
		progress := time.NewTicker(30 * time.Second)
		defer progress.Stop()
		for {
			select {
			case <-subCtx.Done():
				return subCtx.Err()
			case <-progress.C:
				logger.Printf("painting: %d tiles so far, %s elapsed, %s",
					painted, time.Since(start).Round(time.Second), memStats())
			case c, more := <-ch:
				if !more || c.Key != tile {
					if err := flush(); err != nil {
						return err
					}
					tile = c.Key
				}
				if !more {
					logger.Printf("painted %d tiles from weekly counts", painted)
					return nil
				}
				for i, o := range outputs {
					count := c.Raw
					if o.Filtered {
						count = c.Filtered
					}
					if count > 0 {
						if numCounts[i] >= len(tilecounts) {
							return fmt.Errorf("tile %s appears more than %d times in input", tile.String(), len(tilecounts))
						}
						counts[i][numCounts[i]] = count
						numCounts[i]++
					}
				}
			}
		}
	})
	if err := g.Wait(); err != nil {
		return err
	}
	for i, p := range painters {
		if err := p.Close(); err != nil {
			return err
		}
		logger.Printf("finished painting %s", outputs[i].Path)
	}
	logger.Printf("finished painting in %s, %s", time.Since(start).Round(time.Second), memStats())
	return nil
}
