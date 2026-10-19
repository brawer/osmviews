// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"container/heap"
	"context"
	"io"
)

// mergeTileCounts merges the per-week, TileKey-sorted readers in r into a
// single TileKey-sorted stream on out. weights[i] scales the impression
// counts of r[i] (used to extrapolate weeks with missing days to a full
// week); a nil or short weights slice means a factor of 1.0. corrections[i],
// if present and not nil, holds TileKey-sorted "zoom/x/y count" lines whose
// counts replace those of the same tiles in r[i] before weighting (see
// ComputeZoomFilter).
func mergeTileCounts(r []io.Reader, weights []float64, corrections []io.Reader, out chan<- TileCount, ctx context.Context) error {
	defer close(out)
	if len(r) == 0 {
		return nil
	}

	merger := NewTileCountMerger(r, weights, corrections)
	for merger.Advance() {
		// Check if our task has been canceled. Typically this can happen
		// because of an error in another goroutine in the same x.sync.errroup.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		out <- merger.TileCount()
	}

	if err := merger.Err(); err != nil {
		return err
	}

	return nil
}

// tileCountPair is a merged tile count before and after corrections.
type tileCountPair struct {
	Key           TileKey
	Raw, Filtered uint64
}

// mergeTileCountPairs is like mergeTileCounts, but sends every count both
// before and after its corrections.
func mergeTileCountPairs(r []io.Reader, weights []float64, corrections []io.Reader, out chan<- tileCountPair, ctx context.Context) error {
	defer close(out)
	if len(r) == 0 {
		return nil
	}
	merger := NewTileCountMerger(r, weights, corrections)
	for merger.Advance() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		tc := merger.TileCount()
		out <- tileCountPair{Key: tc.Key, Raw: merger.RawCount(), Filtered: tc.Count}
	}
	return merger.Err()
}

type TileCountMerger struct {
	heap   tileCountHeap
	err    error
	inited bool
}

func NewTileCountMerger(r []io.Reader, weights []float64, corrections []io.Reader) *TileCountMerger {
	m := &TileCountMerger{}
	m.heap = make(tileCountHeap, 0, len(r))
	for i, rr := range r {
		weight := 1.0
		if i < len(weights) {
			weight = weights[i]
		}
		stream := &tileCountStream{scanner: bufio.NewScanner(rr), weight: weight}
		if i < len(corrections) && corrections[i] != nil {
			stream.corrections = newCorrectionStream(corrections[i])
		}
		if stream.scan() {
			m.heap = append(m.heap, stream)
		}
		if err := stream.Err(); err != nil {
			m.err = err
			return m
		}
	}
	return m
}

func (m *TileCountMerger) Advance() bool {
	if m.err != nil {
		return false
	}
	if len(m.heap) == 0 {
		return false
	}
	if !m.inited {
		heap.Init(&m.heap)
		m.inited = true
		return true
	}
	stream := m.heap[0]
	if stream.scan() {
		heap.Fix(&m.heap, 0)
	} else {
		heap.Remove(&m.heap, 0)
	}
	if err := stream.Err(); err != nil {
		m.err = err
		return false
	}
	return len(m.heap) > 0
}

func (m *TileCountMerger) Err() error {
	return m.err
}

// RawCount returns the count of the current TileCount before corrections.
func (m *TileCountMerger) RawCount() uint64 {
	if len(m.heap) > 0 {
		return m.heap[0].raw
	}
	return 0
}

func (m *TileCountMerger) TileCount() TileCount {
	n := len(m.heap)
	if n > 0 {
		return m.heap[0].tc
	} else {
		return TileCount{NoTile, 0}
	}
}

type tileCountStream struct {
	tc          TileCount // with corrections applied
	raw         uint64    // tc.Count without corrections
	scanner     *bufio.Scanner
	weight      float64
	corrections *correctionStream // nil if there are no corrections
	index       int
}

// Err returns the first error from reading the stream or its corrections.
func (s *tileCountStream) Err() error {
	if err := s.scanner.Err(); err != nil {
		return err
	}
	if s.corrections != nil {
		return s.corrections.err
	}
	return nil
}

// scan reads the next record from the stream into s.tc, applying any
// correction for its tile and scaling its impression count by s.weight.
// It returns false at end of input.
func (s *tileCountStream) scan() bool {
	if !s.scanner.Scan() {
		return false
	}
	s.tc = ParseTileCount(s.scanner.Text())
	s.raw = s.weigh(s.tc.Count)
	if s.corrections != nil {
		if count, ok := s.corrections.lookup(s.tc.Key); ok {
			s.tc.Count = count
		}
	}
	s.tc.Count = s.weigh(s.tc.Count)
	return true
}

func (s *tileCountStream) weigh(count uint64) uint64 {
	if s.weight != 1.0 && count > 0 {
		return uint64(float64(count)*s.weight + 0.5)
	}
	return count
}

type tileCountHeap []*tileCountStream

func (h tileCountHeap) Len() int { return len(h) }

func (h tileCountHeap) Less(i, j int) bool {
	return TileCountLess(h[i].tc, h[j].tc)
}

func (h tileCountHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *tileCountHeap) Push(x interface{}) {
	stream := x.(*tileCountStream)
	stream.index = len(*h)
	*h = append(*h, stream)
}

func (h *tileCountHeap) Pop() interface{} {
	old := *h
	n := len(old)
	stream := old[n-1]
	old[n-1] = nil // avoid memory leak
	stream.index = -1
	*h = old[0 : n-1]
	return stream
}
