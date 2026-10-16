<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Lane finder

An exploration tool for the straight "shipping lanes" in the OSMViews raster
that converge on Null Island and a few other points
([#156](https://github.com/brawer/osmviews/issues/156)).

## Hypothesis

The lanes are real tile requests, made while a map animates from its default
view (such as 0°, 0°) to the place it actually wants to show. Leaflet
(`flyTo`, `panTo`), MapLibre (`flyTo`, `easeTo`) and OpenLayers (`animate`)
all move the map center along a straight line in projected coordinates, so
every animation leaves a trail of tiles that is straight in Web Mercator and
starts at the default view. OpenStreetMap only publishes tiles with enough
requests per day, so a single flight disappears from the logs. A lane shows up
only where many clients fly the same path, which suggests quantized
destinations such as coarse IP geolocation or rounded coordinates.

## What the tool does

```sh
go run ./cmd/lanefinder -zoom 12 -geojson lanes.geojson \
    tiles-2026-10-05.txt.xz tiles-2026-10-06.txt.xz
```

Input can be daily OpenStreetMap logs (`.xz`), the builder's weekly logs
(`tilelogs-*.br`) or plain text. Counts of the same tile are summed over all
inputs. The tool looks at a single zoom level:

1. **Thin-structure filter.** A tile only votes if at most `-max-fill` of the
   tiles in the surrounding 9×9 window appear in the log. Land, where nearly
   every tile is viewed, drops out, and narrow lanes over sparsely viewed
   areas stay.
2. **Hough transform.** Every remaining tile votes for all lines through it,
   with tile coordinates as the plane, because straight lines there are
   straight in Web Mercator. Accumulator peaks are visited from strongest to
   weakest. Each peak is refined with a total-least-squares fit and cut into
   gap-free runs, and its tiles are removed before the next peak is counted.
3. **Centers.** Every pair of lanes votes for the point where the two meet
   when extended beyond one of their ends. Dense clusters of these points
   become centers, refined by a least-squares intersection of their lanes.
   Several centers are found in a single pass, not just Null Island.
4. **Rays.** A lane that crosses densely viewed land is found as two pieces,
   because the filter removes its tiles over land. Pieces with the same
   bearing from a center are grouped into one ray.

It prints each center with its rays and the median count along each lane,
and optionally writes everything as GeoJSON for viewing on top of the raster
in QGIS or geojson.io.

The defaults are tuned only on the synthetic logs in `lanes_test.go`, which
contain land, noise and two centers. On those logs the tool recovers both
centers to within a tenth of a tile and finds every ray, in about three
seconds at zoom 12. Real lanes may be wider than one tile (an animating map
loads its whole viewport), or appear only over part of their length at a
given zoom (`flyTo` zooms out mid-flight), so `-max-fill`, `-tolerance` and
`-min-density` will probably need adjusting.

## Where this could fit into the pipeline

Detection needs a global view of a zoom level, so it cannot run inside the
`Painter`, which streams tiles in depth-first order and holds only one branch
of rasters. It fits better as a separate per-week stage:

1. **Detect, once per week.** After `GetTileLogs` has written
   `tilelogs-<week>.br`, read that file once more and keep only a few zoom
   levels. Run detection and store the result as `lanes-<week>.json` next to
   the tile logs, in the workdir and in object storage, so that later runs
   reuse it like they reuse the logs. Doing this per week matters because
   the apps causing lanes, and their destinations, change over time.
2. **Describe a ray independently of zoom.** That is a center, a bearing and
   a far end, plus a baseline count per zoom level: the median count of the
   ray's tiles over sparsely viewed areas at that zoom, which is roughly what
   the passing animations contribute to each tile.
3. **Subtract while merging.** In `tileCountStream.scan`
   (`cmd/osmviews-builder/merge.go`), right after the weekly weight is
   applied, subtract the ray's baseline for that zoom from every tile the ray
   passes through, and clamp the result at zero. The stream stays sorted by
   `TileKey`, so `mergeTileCounts` and the `Painter` stay unchanged. A tile
   on a ray is found by a distance test against the week's rays, prefiltered
   by their bounding boxes.

Subtracting a baseline, instead of dropping the tiles, keeps real places that
happen to lie on a ray: a city on a lane loses the lane's share but keeps its
own views. Because rays are described from their center to their far end,
the subtraction also covers the stretches over land, where detection cannot
see the lane.

The list of centers also gives #156 a data-driven mask: instead of
hard-coding (0, 0), the builder could mask or down-weight the hotspot at each
detected center, and report the centers in the data package.

## Next steps

* Run the tool on one week of real logs at zoom 10–14 and look at the
  GeoJSON on top of the raster. That shows whether the hypothesis holds, and
  which zoom levels carry the lanes.
* Check where the rays end. Whole-degree points or country centroids would
  confirm quantized destinations.
* Tune the parameters on real data, then move detection into the builder as
  described above.
