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

Planned: the injection test, agreement with GHSL population, VIIRS night
lights, OpenStreetMap node density and Wikimedia Commons photo locations,
QRank, week-to-week stability, and coverage by country.

The overview levels of the GeoTIFF are max-pooled, so sums over them are not
view counts; the evaluations compare ranks and densities.
