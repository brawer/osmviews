// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"math"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Quantizer converts 32-bit IEEE floating-point raster tiles
// to 16-bit unsigned integers. In the output image, the uint16
// value of each pixel corresponds to the natural logarithm of
// its relative rank. If an output pixel has an uint16 value
// of n/65535, the corresponding geographic area ranks between
// e^n and e^(n+1) - 1 in terms of how often it gets viewed
// on openstreetmap.org compared to all other geographic areas.
// Thus, the most-viewed area of the planet gets assigned a value
// of 0; less-viewed areas get exponentially higher values.
type Quantizer struct {
	buckets []rankBucket
}

type rankBucket struct {
	count      int64
	minValue   float64
	maxValue   float64
	logMinRank float64
	logMaxRank float64
}

// RankBucketizer computes an image histogram over an input
// TIFF image.
type rankBucketizer struct {
	tiff    *TiffReader // thread-safe without mutex protection
	mutex   sync.Mutex
	buckets []rankBucket
}

func newRankBucketizer(tiff *TiffReader) *rankBucketizer {
	buckets := make([]rankBucket, 100000)
	infinity := math.Inf(+1)
	for i := range len(buckets) {
		buckets[i].minValue = infinity
	}
	return &rankBucketizer{
		tiff:    tiff,
		buckets: buckets,
	}
}

func (r *rankBucketizer) bucketize() ([]rankBucket, error) {
	type task struct {
		tile     TileIndex
		useCount int64
	}
	work := make(chan task, 1000)
	group, ctx := errgroup.WithContext(context.Background())

	// Single producer posts tasks on work channel.
	group.Go(func() error {
		shared := findSharedTiles(r.tiff.tileOffsets)
		for _, tile := range shared {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case work <- task{tile.SampleTiles[0], int64(tile.UseCount)}:
				continue
			}
		}
		for tile, off := range r.tiff.tileOffsets {
			if _, isShared := shared[off]; isShared {
				continue
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case work <- task{TileIndex(tile), 1}:
				continue
			}
		}
		close(work)
		return nil
	})

	// Multiple parallel workers pick up tasks from work channel.
	for _ = range runtime.NumCPU() {
		group.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case t, more := <-work:
					if !more {
						return nil // channel closed
					}
					r.update(t.tile, t.useCount)
				}
			}
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	rank := int64(1)
	for i, _ := range r.buckets {
		bucket := &r.buckets[i]
		bucket.logMinRank = math.Log(float64(rank))
		bucket.logMaxRank = math.Log(float64(rank + bucket.count))
	}

	return r.buckets, nil
}

func (r *rankBucketizer) update(tile TileIndex, useCount int64) error {
	values := make([]float32, r.tiff.tileWidth*r.tiff.tileHeight)
	if err := r.tiff.ReadTile(int(tile), values); err != nil {
		return err
	}

	numBuckets := len(r.buckets)
	scale := float32(numBuckets-1) / float32(r.tiff.maxValue)

	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, v := range values {
		value := float64(v)
		buck := numBuckets - 1 - int(v*scale)
		if buck < 0 {
			buck = 0
		} else if buck >= numBuckets {
			buck = numBuckets - 1
		}
		bucket := &r.buckets[buck]
		bucket.count += useCount
		bucket.minValue = math.Min(bucket.minValue, value)
		bucket.maxValue = math.Max(bucket.maxValue, value)
	}

	return nil
}
