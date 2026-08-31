<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Defensive publication: Memory-bounded construction of a globally complete, high-zoom-level Cloud-Optimized GeoTIFF from tile-access logs

**Author:** Sascha Brawer

**Status:** Defensive publication, intended for submission to the Technical
Disclosure Commons (<https://www.tdcommons.org/>).

**Reference implementation:** <https://github.com/brawer/osmviews>
(`cmd/osmviews-builder`), released under the MIT license. A narrative
description also appears in that repository as `docs/technical-design.md`.

This document is published to establish prior art. The techniques described
here, and obvious variations of them, are disclosed for free public use; this
publication is intended to prevent anyone from obtaining patent rights that
would restrict that use.

---

## Abstract

A method is disclosed for building a single raster image that (a) covers the
entire surface of a planet, (b) has a resolution equivalent to web-map zoom
level 18 (on the order of 10¹¹ cells), (c) is a valid Cloud-Optimized GeoTIFF
including a full reduced-resolution overview pyramid, and (d) is derived by
aggregating a long time series of per-tile access counts — while using an
amount of working memory that is essentially constant in the number of cells,
and producing an output file several hundred times smaller than a naïve dense
encoding. The method combines a level-embedding space-filling tile key, a
two-stage disk-backed ordering (per-period external sort followed by a
cross-period streaming merge), a bounded root-to-leaf raster working set that
follows from consuming the ordered stream in tree pre-order, single-pass
inline overview generation, per-cell robust aggregation computed without
materialising the time series, and three cooperating output-size reductions
(magnitude quantisation, tile deduplication by shared file offsets, and a
monotone value transform). A worked configuration builds a global zoom-18
raster in under one gigabyte of resident memory and emits an approximately
600 MB file where a dense encoding would require approximately 275 GB.

## 1. Problem

Consider a data set of *tile-access counts*: for a web map tiled in the usual
`zoom/x/y` scheme, a stream of records stating how many times each tile was
requested in a given period (for example, one day). Such logs are published,
among others, by the OpenStreetMap Foundation.

The goal is to convert a rolling window of such logs (for example, the most
recent 52 weekly periods) into a geospatial raster in which each cell holds an
aggregated, area-normalised “view density” for the corresponding location, at
a spatial resolution equivalent to zoom level 18 — roughly 150 m per cell at
the equator — with coverage of the entire globe, and delivered as a
Cloud-Optimized GeoTIFF (COG) so that clients can read arbitrary sub-regions
and zoom levels over HTTP range requests.

The naïve approach — allocate the full dense zoom-18 array, accumulate into
it, then write it — is infeasible on commodity hardware. A global zoom-18
raster has 2¹⁸ × 2¹⁸ ≈ 6.9 × 10¹⁰ cells; at four bytes per cell that is
approximately 275 GB, before overviews. The aggregation also requires holding,
per cell, enough of the time series to compute a robust statistic such as a
median. Neither fits in memory, and materialising either on disk in dense form
is wasteful because the overwhelming majority of the planet (oceans, deserts,
ice) has little or no data.

The techniques below build such a raster in a single streaming pass, with
peak memory that scales with the *depth* of the tile tree (a small constant,
e.g. 11 or 19) rather than its *breadth* (the cell count), and with an output
file whose size tracks the information content of the data rather than the
dimensions of the grid.

## 2. Overview of the approach

1. Encode each `zoom/x/y` tile as an integer *tile key* whose numeric order is
   a depth-first pre-order traversal of the tile quad-tree (§3.1).
2. For each time period, sort that period’s records by tile key using an
   external (disk-backed) sort, and coalesce equal keys; store the sorted,
   coalesced period as a compressed file (§3.2).
3. Merge the sorted period files with a streaming k-way merge, so that a single
   globally key-ordered record stream is produced, in which every period’s
   records for a given tile arrive consecutively (§3.2).
4. Consume that stream. For each tile, collect its per-period values and reduce
   them to one aggregated value with a robust statistic that tolerates missing
   periods, computed directly from the ordered run (§3.3).
5. Paint aggregated values into 256 × 256 raster tiles held in a tree. Because
   the stream is in pre-order, only the chain of raster tiles from the root to
   the tile currently being filled is ever live; completed raster tiles are
   evicted immediately (§3.4).
6. On eviction, subsample the completed raster tile into its parent, building
   the overview pyramid inline during the same pass (§3.5).
7. When writing each raster tile, apply three cooperating size reductions:
   quantise low-magnitude tiles to a common value; store each distinct tile
   body once and make other tiles reference the same file bytes; and store a
   monotone transform of the value (§3.6).
8. Assemble the COG: image file directories first, tile bodies staged in a
   temporary file, offsets back-patched (§3.7).

Individually, several ingredients are well known — Morton/Z-order curves,
external sorting, k-way merges, mipmap generation. The disclosure is the
specific integration that yields constant-memory, single-pass construction of
a *globally complete, high-zoom, overview-complete COG from a time series*,
together with the specific size-reduction techniques of §3.6, in particular
tile deduplication by shared file offsets (§3.6.2).

## 3. Detailed description

### 3.1 Level-embedding space-filling tile key

Each tile `(z, x, y)` is mapped to a fixed-width unsigned integer key as
follows. Reserve the least-significant *b* bits for the zoom level `z`
(`b = 5` suffices for `z ≤ 31`). Above those, allocate two bits per level,
filled from the most-significant level downward: for level `i`
(`0 ≤ i < z`), place bit `i` of `x` and bit `i` of `y` into the pair of key
bits for that level. Unused low levels are zero.

This is an interleaving (Z-order / Morton) code with two properties that the
method depends on:

* **Numeric order equals pre-order traversal.** For two keys, the comparison
  proceeds level by level from the root. A tile’s key is numerically smaller
  than the keys of all tiles strictly inside it, and an entire sub-tree
  occupies one contiguous key interval. Sorting keys ascending therefore
  yields a depth-first, pre-order enumeration of the quad-tree.
* **Cheap tree operations.** “Is tile A an ancestor of tile B”, “truncate B to
  level *k*”, and “successor of A in pre-order at maximum level *m*” are all
  constant-time bit manipulations of the key.

Embedding the level in the key (rather than keying only on `x`, `y` at a fixed
level) is what makes a *mixed-level* record stream — logs contain tiles at
many zoom levels — sortable into a single valid traversal.

*Variations.* Any bijective encoding with the pre-order property works: the
level bits may occupy the high end instead of the low end; a Hilbert-curve
ordering may be substituted where its locality is preferred and the ancestor
test is adjusted; trees of branching factor other than four (e.g. binary
“tile pyramids”, octrees for volumetric data) generalise directly by changing
the bits-per-level.

### 3.2 Two-stage ordering: per-period external sort, cross-period streaming merge

**Per period.** The records of one period are parsed to `(tile key, count)`
pairs and fed to an external sort keyed on the tile key, which spills runs to
temporary files and merges them, so that peak memory is bounded by the sort’s
configured buffer regardless of the number of distinct tiles. The sorted
output is streamed once more to coalesce equal keys (a tile requested on
several days of the period becomes one record with the summed count) and
written to a compressed file. These per-period files are cached (locally and,
optionally, in object storage) and reused on subsequent runs, so that a
periodic rebuild only sorts the newest period(s).

**Across periods.** To aggregate the window, the *N* sorted period files are
combined by a streaming k-way merge: a min-heap of *N* cursors ordered by tile
key; repeatedly emit the smallest and advance its cursor. The result is one
record stream, globally ordered by tile key, in which **all *N* periods'
records for a given tile arrive consecutively**, followed by all records for
the next tile, in quad-tree pre-order. Working memory for the merge is the
heap plus one buffered record per period — constant in the cell count and
linear only in the (small, fixed) window size *N*.

An optional per-period scalar weight may be applied to counts as they are read
from each cursor (for example, to extrapolate a period with missing days to a
full period by scaling by `days_expected / days_present`); this does not
affect ordering.

### 3.3 Per-cell aggregation from the ordered stream

The consumer reads the merged stream and accumulates the run of records that
share a tile key into a small buffer (at most *N* values). When the key
changes, it reduces the buffer to one value.

A robust reduction that tolerates missing periods, and that is computable
directly from the ordered run, is the **windowed median**: treat periods with
no record for this tile as zeros, conceptually prepended to the sorted list of
present values. The median is then the element at index `⌊N/2⌋` of the
length-`N` conceptual list, i.e. present value at index
`⌊N/2⌋ − (N − count_present)`, or zero if that index is negative (the tile
appears in fewer than half the periods). Because the k-way merge breaks
tile-key ties by ascending count, the buffered present values already arrive
sorted, so no per-cell sort is needed. The median suppresses transient spikes,
seasonality, and the occasional noisy weighted period.

The reduced value is then divided by the tile’s true surface area (which, in
web-Mercator, varies with latitude) to obtain an area-normalised density.

*Variations.* Any order statistic (a different quantile, trimmed mean),
or a decay-weighted mean, may be substituted; the point is that the statistic
is obtained from the length-`count_present` ascending run plus the count of
absent periods, without ever holding all *N* periods for all cells.

### 3.4 Bounded raster working set via pre-order consumption

The output raster is produced in fixed-size **raster tiles** of `T × T` cells
(e.g. `T = 256`). The full-resolution level is a grid of `2^L × 2^L` raster
tiles, where `L = zoom − log2(T)` (e.g. `L = 10` for zoom 18, `T = 256`). A
raster tile is a `T × T` array of the cell type plus a pointer to its parent
raster tile.

The consumer maintains only a **path of raster tiles from the root to the one
currently being filled** — at most `L + 1` of them. When a record arrives
whose target raster tile is not contained in the current one, the pre-order
property guarantees that no later record can fall inside the current raster
tile or inside any ancestor that does not contain the new target. Therefore
those raster tiles are *final*. The consumer:

1. evicts each finalised raster tile (compress its body, append it to the
   output staging file, record its offset and length; see §3.5 and §3.6);
2. walks down toward the new target, allocating the chain of intermediate
   raster tiles, and for every sibling sub-tree that is skipped entirely,
   emits a single uniform raster tile (§3.6.1) carrying a fill value (zero, or
   a nearest-neighbour carry-forward);
3. allocates the new leaf raster tile.

Consequently the peak live raster memory is `(L + 1) · T² · sizeof(cell)` —
for `L = 10`, `T = 256`, four-byte cells, about 2.8 MiB — **independent of how
many cells the planet has**. Peak process memory in a worked implementation is
a few hundred MiB, dominated by language-runtime overhead, the compressor’s
buffers, and one buffered input period, not by the raster.

Regions never mentioned by any record are emitted as uniform raster tiles
during a final sweep, so the raster has no holes: it covers the whole grid.

### 3.5 Single-pass inline overview generation

When a leaf or interior raster tile is finalised and about to be evicted, it
is first subsampled `2 × 2 → 1` into the appropriate quadrant of its parent
raster tile (e.g. by max-pooling, which preserves visible hotspots; average-
or min-pooling are alternatives). Because eviction happens in pre-order and a
parent is only finalised after all four of its children, the entire overview
pyramid (levels `L−1` down to `0`) is produced **within the same single pass**
over the input, with no separate downsampling stage and no re-reading of the
full-resolution data.

### 3.6 Output-size reduction

Three techniques act together so that the file size tracks the data’s
information content rather than the grid dimensions.

#### 3.6.1 Magnitude quantisation

Before compressing a raster tile, test whether every cell, *rounded to the
storage precision* (e.g. to the nearest integer density), equals the same
value. Because the great majority of the planet has a density far below one
unit, huge contiguous areas round to a single value (typically zero). A raster
tile that is uniform after rounding is handled as a uniform tile keyed by that
rounded value. This both removes the need to compress most tiles and feeds
§3.6.2.

#### 3.6.2 Tile deduplication by shared file offsets

Maintain, per pyramid level, a map from *tile body content* (in practice, from
the rounded uniform value for uniform tiles, and optionally from a hash for
non-uniform tiles) to the file offset and length at which a tile with that
content was first written. When a later tile has content already in the map,
**do not write its body again**: instead set its entry in the format’s
tile-offset and tile-bytecount arrays to point at the *same* byte range as the
earlier tile.

The result is that each pyramid level of the file stores exactly one
compressed body for “all-zero”, one for “all-one”, and so on, and the hundreds
of thousands of ocean tiles at that level all reference it. Multiple image
tiles resolving to one shared byte range is unusual — most encoders never do
it — but it is permitted by the TIFF 6.0 specification (the `TileOffsets` and
`TileByteCounts` arrays are just arrays of pointers and lengths; nothing
requires them to be distinct or monotone), and mainstream readers (e.g. GDAL,
`tifffile`, browser GeoTIFF libraries) decode it correctly.

A corollary: the file’s structural metadata must **not** advertise a strict
tile ordering or contiguous tile layout (for COGs, the `BLOCK_ORDER` ghost
option is deliberately omitted), because such a declaration would forbid
tiles sharing bytes.

#### 3.6.3 Monotone value transform

Cell values are stored as a fixed monotone transform of the density, for
example `ln(1 + density)`. This preserves the ordering of cells (so any
threshold or ranking is unchanged) while compressing a very skewed dynamic
range (e.g. roughly `0` to `2.3 × 10⁵`) into a near-linear small range (e.g. `0` to `12`),
which improves both the compressibility of tile bodies and the out-of-the-box
behaviour of visualisation tools. The transform’s maximum over the raster is
recorded in a standard tag so that consumers can invert or re-normalise it.

### 3.7 Cloud-Optimized GeoTIFF assembly

A COG requires the image file directories (IFDs), and a small block of
structural “ghost” metadata, to precede the tile data, so that one HTTP range
request for the header locates every tile. But the tile offsets are not known
until the tiles have been written. The method therefore:

1. streams compressed tile bodies to a temporary file during the pass (§3.4),
   recording each body’s provisional offset within that file and its length;
2. writes the file header, the ghost metadata, and all IFDs (full-resolution
   level and every overview level), with placeholder tile-offset values;
3. appends the tile bodies from the temporary file, now at their final
   absolute offsets;
4. seeks back and patches every tile-offset (and the inter-IFD “next IFD”
   pointers) to the final values.

Classic (32-bit) TIFF is sufficient because the output stays below 4 GiB
thanks to §3.6. Standard geo-referencing tags place the grid in the target
projection; provenance tags record the input date range, the producing
software version, and the date of the most recent input period.

## 4. Resulting properties

* **Peak memory constant in cell count.** Determined by tree depth `L`, window
  size `N`, one buffered input period, and compressor buffers. A worked
  configuration builds a global zoom-18 raster with pyramid in well under
  1 GiB resident.
* **Single pass.** The input is read once; overviews are a by-product; no
  stage re-reads the full-resolution raster.
* **Output size tracks information, not dimensions.** A worked configuration
  emits ≈ 600 MB where a dense zoom-18 encoding would be ≈ 275 GB.
* **Incremental.** Re-running for a shifted window re-sorts only the new
  period(s); everything else is reused from cache.
* **Standards-compliant output.** A valid Cloud-Optimized GeoTIFF readable by
  unmodified mainstream tools, including the shared-offset tiles of §3.6.2.
* **Commodity hardware.** No cluster, no large-memory machine, no GPU.

## 5. Variations and generalisations

The disclosure covers, without limitation, the following variations:

* **Any raster produced by aggregating a key-ordered time series into a tiled
  pyramid**, not only view-density maps: population, emissions, elevation
  change, sensor coverage, edit activity, etc.
* **Any spatial tree.** Quad-trees (this document), binary tile pyramids,
  octrees / 3D tiles for volumetric or point data, or non-square tilings, by
  adjusting the bits-per-level of the key of §3.1 and the arity of the
  eviction/subsample step.
* **Any projection or CRS**, geographic or projected; the area-normalisation of
  §3.3 is projection-specific but optional.
* **Any robust per-cell reduction** computed from an ascending run plus an
  absent-period count: other quantiles, trimmed or winsorised means, decay- or
  recency-weighted means, min/max.
* **Any pooling rule** for overview generation: max, min, mean, median,
  mode, or a learned kernel.
* **Any container format** whose tile index is an array of (offset, length)
  pairs and does not mandate distinctness — e.g. TIFF/BigTIFF/GeoTIFF, or a
  bespoke format — for the deduplication of §3.6.2; and BigTIFF where §3.6
  does not keep the file under 4 GiB.
* **Any lossless codec** for tile bodies; **any monotone transform**
  (logarithm, square root, µ-law, a fitted CDF) for §3.6.3.
* **Any spill-to-disk external sort and any k-way merge** for §3.2, including
  merging directly from remote object storage.
* **Gap fill** by zero, by nearest painted ancestor, by interpolation, or left
  as an explicit nodata value.
* **Parallelisation.** The per-period sorts of §3.2 are embarrassingly
  parallel; the merge-and-paint pass of §3.3–§3.7 can be sharded by top-level
  sub-tree, each shard producing a disjoint set of tile bodies and IFD
  entries that are concatenated.
* **Deferred / range-served overviews.** Emitting fewer overview levels, or
  none, and relying on the reader.

## 6. Reference implementation

A complete, working implementation in the Go programming language is publicly
available under the MIT license at
<https://github.com/brawer/osmviews> (directory `cmd/osmviews-builder`; the
tile key is in `tile.go`, the two-stage ordering in `tilelogs.go` and
`merge.go`, the bounded raster path and inline overviews in `paint.go`, and
the size reductions and COG assembly in `raster.go`). The commit history of
that repository predates or accompanies this publication.

## Appendix: enumerated disclosed techniques

For the avoidance of doubt, the following are disclosed for free public use,
alone and in combination:

1. Encoding mixed-level map tiles as integers whose ascending numeric order is
   a pre-order traversal of the tiling tree, by reserving low (or high) bits
   for the level and interleaving coordinate bits per level.
2. Aggregating a windowed time series of per-tile counts by (a) an external,
   disk-backed sort of each period on that key, then (b) a streaming k-way
   merge across periods, yielding one key-ordered stream in which all periods'
   records for a tile are consecutive.
3. Applying a per-period scalar weight to counts during the merge to normalise
   periods with missing sub-intervals.
4. Computing a per-cell windowed order statistic (e.g. median with zero-fill
   for absent periods) directly from the ascending per-cell run and the count
   of absent periods, without materialising the window for all cells.
5. Painting the aggregate into a tree of fixed-size raster tiles while holding
   only the root-to-current-leaf path, justified by the pre-order arrival of
   records, with immediate eviction of finalised raster tiles.
6. Generating the entire reduced-resolution overview pyramid inline, by
   subsampling each raster tile into its parent at eviction time, in the same
   single pass over the input.
7. Emitting skipped sub-trees and never-touched regions as uniform raster
   tiles so the output has global coverage with no holes.
8. Quantising raster tiles to a common value at storage precision so that
   large low-magnitude regions collapse to identical tiles.
9. Deduplicating identical raster tiles by setting multiple entries of the
   container’s tile-offset / tile-length index to point at a single shared
   byte range, and correspondingly omitting any strict-tile-order declaration
   from the container’s structural metadata.
10. Storing a monotone transform of the cell value to compress dynamic range
    while preserving order, with the transform’s extent recorded in metadata.
11. Assembling a header-first (Cloud-Optimized) GeoTIFF by staging tile bodies
    in a temporary file and back-patching offsets after the single pass.
12. The combination of 1–11 to build a globally complete, high-zoom,
    overview-complete Cloud-Optimized GeoTIFF from tile-access logs in memory
    that is constant in the number of cells.
