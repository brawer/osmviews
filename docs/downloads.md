<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Downloads & provenance

Every weekly build publishes three files. The GeoTIFF has a stable URL that
always serves the newest version; the auxiliary files are addressed by date so
that a URL names the exact GeoTIFF it belongs to.

| File | URL | Format | Retention |
|---|---|---|---|
| Raster | `https://osmviews.toolforge.org/download/osmviews.tiff` | Cloud-Optimized GeoTIFF, EPSG:3857, zoom 0–18, ~580 MB | latest only |
| Bill of materials | `…/download/osmviews-<YYYYMMDD>.cdx.json` | [CycloneDX](https://cyclonedx.org) 1.7 JSON | 3 most recent |
| Statistics | `…/download/osmviews-stats-<YYYYMMDD>.json` | ad-hoc JSON (rank/value distribution) | 3 most recent |

`<YYYYMMDD>` is the last day of the most recent tile-log week that went into the
build — the same date the GeoTIFF carries in its `DateTime` tag.

Only the three most recent builds are kept. If you need provenance or statistics
that outlive that window, copy the files you used into your own storage.


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
how it was made. To get the BOM for a GeoTIFF you just fetched, read the `Link`
header on that same HTTP response (do not make a separate request for it — a
fresh one could land on a newer build):

```
Link: </download/osmviews-20260830.cdx.json>; rel="describedby";
      type="application/vnd.cyclonedx+json"
```

If provenance matters to you, fetch the BOM at download time and keep it next to
the file; don't rely on recovering it later. As a last resort, an orphaned
GeoTIFF's `DateTime` tag (306) holds the version date — `2026:08:30 00:00:00` →
`osmviews-20260830.cdx.json` — but that only works while the build is still among
the three most recent.


## The bill of materials

A [CycloneDX](https://cyclonedx.org) 1.7 document describing one dated GeoTIFF:

- `metadata.component` — the GeoTIFF: `version` (ISO date), `hashes` (SHA-256 and
  SHA-512 of the exact bytes), a `pkg:generic/osmviews@<date>` purl with
  `checksum` and `download_url` qualifiers, and `externalReferences` — including
  one of type `other` pointing at the dated statistics JSON, with its digests.
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
2. read the `Link` header from that response → BOM URL
3. `GET` the BOM
4. assert `sha256(step 1 bytes) == metadata.component.hashes["SHA-256"]`

Fetch the BOM right after the GeoTIFF, while the build is still within the
roughly three-week retention window. Then step 4 always passes — the `Link`
header ships with the download and the BOM is written before the GeoTIFF, so
both come from the same build. It fails only if the pair drifted apart (a
download resumed across a weekly update, or a caching proxy mixing builds); if
so, re-download and retry.

Verify the statistics JSON the same way, against the `hashes` on its
`externalReference` in the BOM.


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

Our BOM is kept for only about three weeks, so point that `bom` reference at a
copy you archived alongside your output, not the Toolforge URL.
