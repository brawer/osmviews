<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Builder

The `builder` tool is a cronjob that computes the weekly GeoTIFF from
OpenStreetMap tile-log impressions. Each run publishes, by date, the raster
(`osmviews-<date>.tiff`), a statistics file (`osmviews-stats-<date>.json`) and
a CycloneDX bill of materials (`osmviews-<date>.cdx.json`); see
[`docs/downloads.md`](../../docs/downloads.md).

How it builds a globally complete, zoom-18 Cloud-Optimized GeoTIFF in bounded
memory — the level-embedding tile key, the streaming per-period sort and
cross-period merge, the constant-memory raster construction with inline
overviews — is written up in the
[defensive publication](https://raw.githubusercontent.com/brawer/osmviews/main/docs/defensive-publication/defensive-publication.pdf).
