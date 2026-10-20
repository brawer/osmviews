<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Evaluation

Computes every number and data figure in the dataset paper
([`docs/dataset-paper`](../docs/dataset-paper)) and writes them into its
`generated/` folder.

```sh
make            # download inputs, run all evaluations, write the paper's files
make test       # unit tests
```

`make` sets up a Python virtual environment from `requirements.txt` and
downloads each input listed in [`inputs.toml`](inputs.toml) once into
`cache/`, checking its SHA-256. Inputs without a pinned URL yet, such as the
GeoTIFFs of a release that is not out, are given as local files through the
environment variable named in their entry:

```sh
OSMVIEWS_TIFF=osmviews.tiff OSMVIEWS_FILTERED_TIFF=osmviews-filtered.tiff make
```

## Evaluations

| Module | Paper section | What it measures |
|---|---|---|
| `agreement.py` | Effect of the filter | Share of pixels the filter changes, rank correlation regular vs. filtered |
| `figures.py` | Artifacts and their removal | Side-by-side crops of both GeoTIFFs |
| `ranking.py` | Ranking places | Ranking functions (`functions.py`) scored on sample sets (`samples.py`): share of tied points, Zipf exponent, Kendall's tau-b between every pair of functions (`agreement_stats.py`) |
| `nominatim.py` | Ranking functions; Wikidata items and QRank | Nominatim's area importance raster as a ranking function, and its primary importance of Wikidata items compared with QRank and OSMViews |
| `cities.py` | Ranking cities | Each function summed within 5 km of GeoNames cities, compared with their population |
| `qrank.py` | Wikidata items and QRank | Rank correlation of QRank with OSMViews at the coordinates of Wikidata items |
| `external.py` | Stability and coverage | Rank correlation with GHSL population, VIIRS night lights and Commons photo locations on 10 km pixels, by continent |
| `stability.py` | Stability and coverage | Rank correlation and top-1% overlap between consecutive weekly releases |
| `coverage.py` | Stability and coverage | Views per country compared with internet users (World Bank) |

Helpers: `cog.py` reads the GeoTIFF, `grid.py` brings other rasters and
points onto its pixels, `regions.py` rasterizes Natural Earth countries,
`wikimedia.py` reads the Wikimedia SQL dumps.

Overture places and iNaturalist observations are too large to keep: only
their coordinates are read, part by part or as a stream, and cached as
`points-*.npz`. They are pinned by release path and ETag rather than by
checksum, so archive these cached files with the paper.

The evaluations of the filter (`agreement.py`, `figures.py`) need the
filtered GeoTIFF; without it they are skipped and their earlier results in
`results.json` are kept.

Planned: the injection test, QRank items on lanes against a matched control
set, OSMDiffs, single-week stability and partial weeks.

The overview levels of the GeoTIFF are max-pooled, so sums over them are not
view counts; the evaluations compare ranks and densities. `cog.views` sums
true view counts from the full-resolution level, which takes a few minutes.

The inputs and intermediate files take about 25 GB in `cache/`. The first
run takes a few hours, mostly downloading and reading AllThePlaces,
Overture and iNaturalist; later runs take about 20 minutes. A machine with
16 GB of memory is enough.
