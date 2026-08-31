<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# OSMViews

[![CI](https://github.com/brawer/osmviews/actions/workflows/build-test.yml/badge.svg)](https://github.com/brawer/osmviews/actions/workflows/build-test.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/brawer/osmviews/badge)](https://scorecard.dev/viewer/?uri=github.com/brawer/osmviews)
[![Version](https://img.shields.io/github/v/tag/brawer/osmviews?sort=semver&label=version)](https://github.com/brawer/osmviews/tags)
[![REUSE status](https://api.reuse.software/badge/github.com/brawer/osmviews)](https://api.reuse.software/info/github.com/brawer/osmviews)
[![Code: MIT](https://img.shields.io/badge/code-MIT-blue.svg)](LICENSE)
[![Data: CC0-1.0](https://img.shields.io/badge/data-CC0--1.0-brightgreen.svg)](https://creativecommons.org/publicdomain/zero/1.0/)

World-wide ranking of geographic locations based on OpenStreetMap tile logs.
Updated weekly. Aggregated over the past 52 weeks to smoothen seasonal effects.
For any location on the planet, up to ~150m/z18 resolution.


## Code repository

* `cmd/webserver` is the [OSMViews webserver](https://osmviews.toolforge.org).
* `cmd/osmviews-builder` is the pipeline that computes the data.
* `docs` contains [documentation](docs/).

Client libraries are maintained in separate repositories:

* Python: [brawer/osmviews-py](https://github.com/brawer/osmviews-py) [![PyPI](https://img.shields.io/pypi/v/osmviews?label=pypi)](https://pypi.org/project/osmviews/)
* Rust: [brawer/osmviews-rs](https://github.com/brawer/osmviews-rs) [![crates.io](https://img.shields.io/crates/v/osmviews)](https://crates.io/crates/osmviews)


## Roadmap to 1.0

* Write documentation for the Python client.

* Write documentation for the backend pipeline. Document the tricks
  we use to process such a large dataset on a single machine in reasonable
  time.

* Improve the server homepage, display the histogram whose data already
  gets computed.

* Implement the OpenGIS WMTS protocol in the webserver.

* Extend the webserver homepage to display a heatmap. Currently, users
  can already point QGIS or another GIS to our GeoTIFF file, but not many
  people know how to do this.
