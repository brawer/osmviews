<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Downloads & provenance

Every weekly build publishes three files, served from a CDN and listed in a
small JSON descriptor:

| File | Format | Retention |
|---|---|---|
| `datapackage.json` | [Frictionless Data Package](https://datapackage.org/) v2 | latest only, ~2 KB |
| `osmviews-<YYYYMMDD>.tiff` | Cloud-Optimized GeoTIFF, EPSG:3857, zoom 0–18, ~580 MB | 3 most recent |
| `osmviews-<YYYYMMDD>.cdx.json` | [CycloneDX](https://cyclonedx.org) 1.7 bill of materials | kept indefinitely |

`<YYYYMMDD>` is the last day of the most recent tile-log week in the build — the
same date the GeoTIFF carries in its `DateTime` tag (306).

**Start from the data package** — it names the current version and every file
by relative path, byte size and SHA-256:

```
GET https://osmviews.toolforge.org/download/datapackage.json
```

That URL redirects to the CDN; resolve the `resources[].path` entries against
the URL you land on. (The CDN's canonical host is becoming
`osmviews.brawer.ch`; the `osmviews.toolforge.org/download/…` URLs keep working
as redirects.) The legacy `…/download/osmviews.tiff` also still works — a
redirect to the current dated GeoTIFF.

Every bill of materials is kept indefinitely (a few kilobytes each), so a dated
URL stays resolvable for good. The GeoTIFF itself is "latest plus the two
previous builds"; for anything older, keep your own copy.


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


## The data package

`datapackage.json` is a small [Frictionless v2](https://datapackage.org/)
descriptor — the single, server-independent entry point:

```json
{
  "$schema": "https://datapackage.org/profiles/2.0/datapackage.json",
  "name": "osmviews",
  "version": "2026-09-06",
  "resources": [
    { "name": "osmviews", "path": "osmviews-20260906.tiff",
      "bytes": 581137316, "hash": "sha256:be09…" },
    { "name": "sbom", "path": "osmviews-20260906.cdx.json",
      "bytes": 6967, "hash": "sha256:4a56…", "describes": "osmviews" }
  ]
}
```

`version` is the build date; every `resources[].path` is a bare filename to
resolve next to `datapackage.json`. No parser needed — it's a few lines of
"read JSON, pick a resource, verify the hash".


## Checking for updates

`GET datapackage.json` (~2 KB) and compare `version` to the one you have. Only
download the raster if it changed — no conditional request against ~580 MB.

```sh
curl -s https://osmviews.toolforge.org/download/datapackage.json | jq -r .version
```


## Which version am I looking at?

`datapackage.json` `version` for the current build. For a GeoTIFF you already
have on disk, its `DateTime` tag (306) holds the build date, and the bill of
materials is named after it:

```
DateTime (306): 2026:09:06 00:00:00  →  osmviews-20260906.cdx.json
```

That dated BOM URL is immutable and kept indefinitely, so it resolves at any
time — long after `datapackage.json` has moved on to a newer build.


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

1. `GET datapackage.json`
2. resolve the `osmviews` resource's `path`; `GET` it
3. assert `sha256(step 2 bytes) == resource.hash` (drop the `sha256:` prefix)
4. for full provenance, `GET` the `sbom` resource and check its
   `metadata.component.hashes["SHA-256"]` matches too

If the manifest advanced between steps 1 and 2 (you raced a weekly rebuild),
step 3 mismatches — re-`GET` `datapackage.json` and retry.

Working from a GeoTIFF you already have instead: read its `DateTime` tag (306),
`GET` `osmviews-<YYYYMMDD>.cdx.json`, and check
`sha256(bytes) == metadata.component.hashes["SHA-256"]`.


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
