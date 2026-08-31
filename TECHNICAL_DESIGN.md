<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Technical design

This document explains how OSMViews works, for someone new to the project.
It focuses on the parts that are not obvious from reading the code: the tricks
that let us build a zoom-18 raster with global coverage in a few hundred
megabytes of RAM, and keep the resulting file to a few hundred megabytes on
disk.

## Background

### The metric: view density

OpenStreetMap serves its maps as small square images (“map tiles”). Every time
someone looks at a place on an OSM-based map, their browser fetches the tiles
covering that place. The OSM Foundation publishes daily logs of how many times
each tile was requested.

OSMViews turns that into a **view density**: for any point on the planet, an
estimate of how many times per week a square kilometre around it is looked at.
Densely-populated, touristed, or actively-mapped places score high; open ocean
and empty desert score near zero.

We deliberately do **not** filter noise. The most visible consequence is
[Null Island](https://en.wikipedia.org/wiki/Null_Island): coordinates at
latitude 0, longitude 0 (in the Gulf of Guinea) collect a steady stream of
requests from geocoders and buggy software that emit `(0, 0)` when they have no
real location, so OSMViews shows a hotspot there. That is working as intended —
the raw signal is more useful to most consumers than a cleaned one, and callers
who care can mask it.

### GeoTIFF and Cloud-Optimized GeoTIFF

A **TIFF** is a container of one or more raster images, described by an *image
file directory* (IFD) — a list of tagged fields (width, height, compression, and
crucially the byte offsets of the pixel data). **GeoTIFF** adds tags that pin
the pixels to the Earth: the coordinate reference system, and the affine
transform from pixel coordinates to map coordinates. OSMViews output is a
GeoTIFF in the Web Mercator projection ([EPSG:3857](https://epsg.io/3857)), the
same projection the OSM tiles themselves use.

A **Cloud-Optimized GeoTIFF (COG)** is a GeoTIFF laid out so that a client can
read just the part it needs over HTTP range requests, without downloading the
whole file:

* the image is cut into independently-compressed **tiles** (here 256×256
  pixels), not scanlines, so a reader can fetch one tile;
* reduced-resolution **overviews** (a pyramid: half-size, quarter-size, …) are
  included, so a reader zoomed out fetches a small overview instead of the full
  image;
* the IFDs and a small block of “ghost” metadata sit at the **front** of the
  file (`LAYOUT=IFDS_BEFORE_DATA`), so one range request for the header tells
  the client where every tile is.

Our output is ~600 MB. A naïve zoom-18 global image would be
2¹⁸ × 2¹⁸ = 6.9 × 10¹⁰ pixels — 275 GB uncompressed at 4 bytes/pixel. The rest
of this document is largely about the gap between those two numbers.

## Data flow

```
planet.openstreetmap.org/tile_logs/            (daily .txt.xz, "zoom/x/y count")
        │
        ▼   cmd/osmviews-builder, once per ISO week
  ┌───────────────┐
  │ weekly merge  │  external sort by TileKey, sum per tile, brotli-compress
  └───────────────┘  → tilelogs-<week>.br   (cached on disk and in S3)
        │
        ▼   once per run, over the last 52 weeks
  ┌───────────────┐
  │    paint      │  k-way merge → per-tile median → streaming raster build
  └───────────────┘  → osmviews-<date>.tiff  (Cloud-Optimized GeoTIFF)
        │
        ▼
  ┌───────────────┐
  │  build stats  │  read the TIFF back, histogram → osmviews-stats-<date>.json
  └───────────────┘
        │
        ▼   upload to S3;  cmd/webserver serves it at osmviews.toolforge.org
```

## The key idea: everything is sorted in quad-tree order

A web-mercator tile `zoom/x/y` is encoded into a 64-bit **`TileKey`**
(`tile.go`, `MakeTileKey`): the low 5 bits hold the zoom, and above them, two
bits per zoom level interleave one bit of `x` and one bit of `y`, most
significant level first.

The point of this layout: **sorting TileKeys numerically is a depth-first
pre-order traversal of the tile quad-tree.** A tile sorts immediately before all
of its descendants, and a whole subtree occupies one contiguous range of keys.
`TileKey.Contains`, `.ToZoom` and `.Next` are all cheap bit operations on this
encoding.

Every stage relies on this. Sorting happens on disk (an external sort in the
weekly merge, a streaming k-way merge in the paint step), so no stage ever holds
more than a small window of tiles in memory.

## Stage 1 — weekly merged logs (`tilelogs.go`, `GetTileLogs`)

For each ISO week we need one stream of `TileCount{TileKey, count}` records,
sorted by TileKey, with one record per tile (the week’s total).

1. Fetch the week’s daily `.txt.xz` files from OSM. Since mid-2026 OSM only
   publishes a few days per week; missing days are skipped and the weekly total
   is later scaled by `7 / daysPresent` so a partial week still estimates a full
   one. (Upstream: openstreetmap/operations#1398.)
2. Parse each line to a `TileCount` and feed it into
   [`lanrat/extsort`](https://github.com/lanrat/extsort), which sorts by TileKey,
   **spilling to temporary files** so memory stays bounded no matter how many
   distinct tiles a week contains.
3. Stream the sorted output, summing runs of equal keys.
4. Write the result as a brotli-compressed text file `tilelogs-<week>.br`,
   cached on local disk and in object storage (key
   `internal/osmviews-builder/tilelogs-<week>.br`, or `…-<n>d.br` for a partial
   week so a later run recomputes it when more days appear).

Historical weeks are computed once and then read from the cache forever, so a
routine daily run only does real work for the one or two most recent weeks.

## Stage 2 — painting the GeoTIFF (`paint.go`, `raster.go`, `merge.go`)

Input: up to 52 weekly readers, each already TileKey-sorted, plus a
`7 / daysPresent` weight per week. Two goroutines connected by a channel.

### Goroutine A: k-way merge (`merge.go`, `mergeTileCounts`)

A min-heap of the 52 stream cursors, ordered by TileKey (then by count). Pop the
smallest, emit it, advance that stream, repeat. As each record is read, its
count is multiplied by the week’s weight.

Output: a single stream of `TileCount`, globally TileKey-sorted — so **all 52
weeks' records for a given tile arrive consecutively**, then all records for the
next tile, in quad-tree pre-order. Memory is O(52): the heap plus one buffered
line per week.

### Goroutine B: the painter (`paint.go` consumer loop, `Painter`)

It collects the consecutive same-key records into a slice `counts` (one entry
per week that has this tile — at most 52), and when the key changes:

**Median over the window** (`Painter.Paint`). Let `N` be the number of weeks in
the window (normally 52). The counts arrive sorted ascending (the merge breaks
TileKey ties by count). Weeks with no record for this tile are treated as zeros
at the front, so the median is the element at global index `N/2`:
`medianPos = N/2 - (N - len(counts))`. If that is negative — the tile appears in
fewer than half the weeks — the median is 0. The median smooths out individual
spikes, seasonal effects, and the occasional noisy scaled-up partial week.

**Views per km²**: `median / TileArea(zoom, y)`. `TileArea` accounts for Web
Mercator distortion — a tile near the poles covers far less ground than one at
the equator, so the same request count there means a higher density.

### The memory trick: a root-to-leaf raster path

The painter builds the image at a **raster zoom of 10** (`zoom - 8`): the output
GeoTIFF has 256×256-pixel tiles at zoom levels 0…10, and the full-resolution
level 10 is 2¹⁸ pixels on a side — the zoom-18 resolution we want. There are
~1 million raster tiles at level 10.

A `Raster` is one 256×256 `float32` image (256 KiB) plus a pointer to its parent
raster. The painter keeps **only the chain of rasters from the root down to the
one currently being filled** — at most 11 of them, ~2.8 MiB.

This works *because* the input arrives in quad-tree pre-order. When a record
arrives for a raster tile that the current raster does not contain, we are
**permanently finished** with the current raster and any ancestors that don’t
contain the new one: pre-order guarantees no later record can fall inside them.
So `setupRaster`:

1. `emitRaster()`s the rasters we have passed (see below);
2. walks down from the last position to the new raster tile, allocating the
   ancestor chain, and emitting a shared **uniform tile** for every sibling
   subtree that was skipped entirely (open ocean between two data points);
3. allocates the new leaf raster.

`emitRaster()` does three things before dropping a finished raster:

* **`parent.PaintChild(child)`** — subsample the 256×256 child 2×2→1 by
  max-pooling into the correct quadrant of the parent. This builds the overview
  pyramid *inline*, during the same single pass over the data — no second pass.
* **`writer.Write(raster)`** — compress and append to a temporary file
  (see below), recording the byte offset and length.
* unlink it from the tree so the garbage collector can reclaim it.

Peak memory is therefore essentially constant in the size of the planet. It
scales with the *depth* of the tile tree (11 raster levels), not its breadth
(10¹¹ pixels). The `~800 MiB` a real run uses is Go runtime overhead, zlib and
brotli buffers, and one buffered daily-log file — not the raster.

## Keeping the file small (`raster.go`, `RasterWriter`)

Three things get the ~275 GB naïve size down to ~600 MB:

### 1. Rounding low-density tiles together

Before compressing a raster, `Write` checks whether every pixel rounds to the
same integer views/km². Most of the planet has a density well below 1, so after
rounding, enormous areas collapse to a single value (usually 0). In a typical
output about 55% of all raster tiles come out uniform this way; each is handed to
`WriteUniform` with that integer as its colour.

### 2. De-duplicating identical tiles via shared offsets

`WriteUniform` keeps a map from colour → the tile that first used it. Every
later uniform tile of the same colour does **not** store its own pixel data;
its entry in the IFD’s `TileOffsets` / `TileByteCounts` arrays is set to point
at the **same byte range** as the first one.

So each zoom level of the file contains just one compressed 256×256 block of
solid `0`, one of solid `1`, and so on, and its hundreds of thousands of ocean
tiles all reference the same block. Multiple tiles pointing at one offset+length
is unusual — most TIFF writers never do it — but it is explicitly allowed by the
TIFF 6.0 specification, and readers (GDAL, `geotiff.js`, `tifffile`) handle it
correctly.

This is also why the COG “ghost” metadata deliberately omits
`BLOCK_ORDER=ROW_MAJOR`: declaring a strict tile order would forbid the sharing.

### 3. Storing the logarithm of the value

Pixels are stored as `ln(1 + viewsPerKm²)`, not the raw density. The log
transform doesn’t change the ordering of pixels, but it maps a
`0`–`230000` range onto a roughly linear `0`–`12`, which visualisation tools (QGIS,
`plotty`) can colour-map without manual fiddling. The maximum (also logged) is
written into the `SMaxSampleValue` tag; the clients read it to normalise
`rank()` to `0.0`–`1.0`.

### Writing the file

Because a COG puts the IFDs before the pixel data, the tile offsets aren’t known
when the IFDs are written. `RasterWriter` writes the compressed tiles to a
temporary file as it goes, then in `writeTiff` streams the IFDs (with
placeholder offsets), then the tile data, and finally seeks back to patch every
offset (`writeIFDList`, `patchOffset`). Classic little-endian TIFF, not BigTIFF —
the output stays under the 4 GiB limit.

Provenance is written into standard tags: `ImageDescription` (the ingested-log
date range and generation time), `Software` (the build’s version or git commit),
`DateTime` (the date of the most recent ingested daily log).

## Stage 3 — statistics (`stats.go`)

`BuildStats` reads the finished TIFF back and builds a histogram of pixel
values, sampling non-shared tiles and folding in the shared tiles weighted by
how many tiles reference them. From that it derives a rank-versus-value curve
and the median, and writes `osmviews-stats-<date>.json` (the web-server homepage
plots it) and a PNG.

## The web server (`cmd/webserver`)

* Serves a static homepage and `/download/*` (the GeoTIFF and the stats JSON)
  with permissive CORS and HTTP range support, so a browser or a GIS can do COG
  range reads directly against it.
* `Storage` polls object storage every 30 seconds, keeps a local-disk cache of
  the newest `public/osmviews-<date>.*` objects, and serves from disk. It picks
  the most recent date per file name.
* Runs on Wikimedia Toolforge as a web service.

## Running it

The builder runs as a daily scheduled job on Toolforge. Each run:

1. lists the available weeks (skipping any that aren’t finished yet);
2. fetches or reuses the 52 weekly files;
3. checks whether the output for the most recent week is already in object
   storage — if so, it is **done** (the run is a no-op);
4. otherwise paints the GeoTIFF, builds stats, uploads both, and garbage-collects
   old outputs.

The output filename is keyed on the last day of the most recent week, so re-runs
within the same week are idempotent no-ops, and a crashed run simply starts over
next time (weekly files it already computed are cached).

See [`docs/release.md`](docs/release.md) for how a new version is built and
deployed.
