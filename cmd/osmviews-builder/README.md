<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Builder

The `builder` tool is a cronjob that computes the weekly GeoTIFF from
OpenStreetMap tile-log impressions. Each run publishes, by date, the raster
(`osmviews-<date>.tiff`) and a CycloneDX bill of materials
(`osmviews-<date>.cdx.json`); the pixel-value histogram is embedded in the
GeoTIFF itself. See [`docs/downloads.md`](../../docs/downloads.md).

How it builds a globally complete, zoom-18 Cloud-Optimized GeoTIFF in bounded
memory — the level-embedding tile key, the streaming per-period sort and
cross-period merge, the constant-memory raster construction with inline
overviews — is written up in the
[defensive publication](https://raw.githubusercontent.com/brawer/osmviews/main/docs/defensive-publication/defensive-publication.pdf).

## Experimental zoom filter

Some clients leave straight "lanes" of tile requests in the logs, mostly
radiating from Null Island, and many map clients request tiles at Null
Island itself when they start. The zoom filter removes both, in three steps:

1. A cross-zoom check scales down areas where one zoom level far exceeds
   its neighbouring zooms ([`zoomfilter.go`](zoomfilter.go)).
2. A lane filter finds the lanes as straight lines in longitude/latitude
   through Null Island, and cleans the faint traces the first step leaves
   along them, except where they cross busy areas
   ([`lanefilter.go`](lanefilter.go)).
3. Tiles entirely within 3° of Null Island, which is open sea, are lowered
   to the background of the ring between 3° and 6°: its mean request count
   per tile, leaving out the busiest 5% of tiles (Accra lies in the ring).
   The map shows ordinary sea there instead of a hole.

After the regular GeoTIFF of a week is published, a later run paints a
second GeoTIFF with the filter applied and uploads it to
`data/experimental/osmviews-<date>-filtered.tiff` in the public bucket,
so it can be downloaded from the CDN like the regular files. It is
not listed in the BOM or `datapackage.json` until it has been evaluated.
Painting takes about as long for either GeoTIFF, so they are painted in
separate runs to stay within the job's time limit; the job runs daily, and
a new week of logs arrives only weekly.

Each week's corrections are cached as `zoomfilter-v<N>-<week>.br`, in the
workdir and in the internal bucket next to the weekly tile logs. A run
spends at most 40 minutes computing missing ones; the filtered GeoTIFF is
painted once all weeks are ready, so a first run with an empty cache spreads
the work over several daily runs. Failures of the filter are logged and do
not stop the regular output.

With `-zoom-filter`, the builder paints both GeoTIFFs into the workdir,
`osmviews-<date>.tiff` and `osmviews-<date>-filtered.tiff`, in one pass
over the tile logs, and exits without publishing anything.
