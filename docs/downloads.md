<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Downloads & provenance

Every weekly build publishes two files. The GeoTIFF has a stable URL that always
serves the newest version; the bill of materials is addressed by date so that a
URL names the exact GeoTIFF it belongs to.

| File | URL | Format | Retention |
|---|---|---|---|
| Raster | `https://osmviews.toolforge.org/download/osmviews.tiff` | Cloud-Optimized GeoTIFF, EPSG:3857, zoom 0–18, ~580 MB | latest only |
| Bill of materials | `…/download/osmviews-<YYYYMMDD>.cdx.json` | [CycloneDX](https://cyclonedx.org) 1.7 JSON | kept indefinitely |

`<YYYYMMDD>` is the last day of the most recent tile-log week that went into the
build — the same date the GeoTIFF carries in its `DateTime` tag.

Every bill of materials is kept indefinitely (a few kilobytes each), so a dated
URL stays resolvable for good.


## Pixel values and histogram

Each pixel holds `ln(1 + weekly user views per km²)` — a logarithmic view
density, not raw views. The natural-log compression keeps the value range
roughly linear, which is what makes a plain grayscale stretch in QGIS legible;
undo it with `expm1(pixel)` to get views per km².

The value histogram travels inside the GeoTIFF, in the `GDAL_METADATA` tag
(42112), as a band-1 Raster Attribute Table with linear binning: buckets
1/16 wide in ln space, a `min`/`max` bound and a pixel `count` per bucket.
GDAL 3.12+ and a recent QGIS expose it via `GetDefaultRAT()`; older readers
ignore the tag and the raster still opens. Bucket 0 holds every pixel whose
density rounds to zero — most of the planet — so plot the counts on a log
axis.


## Checking for updates

`osmviews.tiff` is refreshed weekly at the same URL. To skip re-downloading
~580 MB when nothing has changed, poll with an HTTP
[conditional request](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Conditional_requests):
keep the `ETag` from your last download and send it back as `If-None-Match`. An
unchanged file answers `304 Not Modified` with no body; a new build answers
`200` with the new bytes and a new `ETag` (`Last-Modified` / `If-Modified-Since`
work too).

```
curl --etag-compare etag.txt --etag-save etag.txt \
     -o osmviews.tiff https://osmviews.toolforge.org/download/osmviews.tiff
```


## Which version am I looking at?

The bill of materials is the single source of truth for what a download is and
how it was made. The GeoTIFF carries its version date in the `DateTime` tag
(306), and the BOM is named after that same date:

```
DateTime (306): 2026:08:30 00:00:00  →  /download/osmviews-20260830.cdx.json
```

The dated BOM URL is immutable and kept indefinitely, so you can resolve it at
any time from a GeoTIFF you downloaded earlier.


## The bill of materials

A [CycloneDX](https://cyclonedx.org) 1.7 document describing one dated GeoTIFF:

- `metadata.component` — the GeoTIFF: `version` (ISO date), `hashes` (SHA-256 and
  SHA-512 of the exact bytes), a `pkg:generic/osmviews@<date>` purl with
  `checksum` and `download_url` qualifiers, and `externalReferences` for its
  distribution, website, and source.
- `metadata.tools.components[0]` — `osmviews-builder`, with its `pkg:github`
  purl (resolved to the full source revision), license, and build string.
- `components[0]` — the OpenStreetMap tile logs the GeoTIFF is derived from
  (`dependencies` records that edge). Their public-domain status is
  substantiated with a `license` external reference to the
  [OSM Foundation Licensing Working Group minutes](https://osmfoundation.org/wiki/Licensing_Working_Group/Minutes/2022-05-12#Using_tile_logs_for_ranking_a_non-ODbL_dataset).
- `formulation` — the build workflow: the tile logs in, the GeoTIFF out,
  `osmviews-builder` as the tool.

`metadata.timestamp` is the build's version date, not when the BOM was written.


## Verifying a download

1. `GET /download/osmviews.tiff`
2. read the `DateTime` tag (306) → `osmviews-<YYYYMMDD>.cdx.json` → BOM URL
3. `GET` the BOM
4. assert `sha256(step 1 bytes) == metadata.component.hashes["SHA-256"]`

The BOM URL in step 2 is derived from the bytes you downloaded, so step 4 fails
only if those bytes are internally inconsistent — a download resumed across a
weekly update, or a caching proxy mixing builds. If so, re-download and retry.


## Recording OSMViews in your data BOM

If your pipeline emits a bill of materials for its own output, record the
OSMViews GeoTIFF as an input:

- add a `data` component with `purl` and `hashes` copied from our
  `metadata.component` — the purl is self-contained:

  ```
  pkg:generic/osmviews@<date>?checksum=sha256:<hex>&download_url=<encoded url>
  ```

- give that component an `externalReference` of type `bom`, with a `hashes`
  entry, for the full provenance — the producing `osmviews-builder` revision,
  the OpenStreetMap tile-log inputs, the build workflow.

Our BOMs are kept indefinitely, so that `bom` reference can point straight at
the dated URL. If your provenance needs to outlive this project's hosting,
archive a copy alongside your output as well.
