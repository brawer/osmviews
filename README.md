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
* [`docs/technical-design.md`](docs/technical-design.md) explains how it all works.
* `docs` contains further [documentation](docs/).

Client libraries are maintained in separate repositories:

* Python: [brawer/osmviews-py](https://github.com/brawer/osmviews-py) [![PyPI](https://img.shields.io/pypi/v/osmviews?label=pypi)](https://pypi.org/project/osmviews/)
* Rust: [brawer/osmviews-rs](https://github.com/brawer/osmviews-rs) [![crates.io](https://img.shields.io/crates/v/osmviews)](https://crates.io/crates/osmviews)

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
