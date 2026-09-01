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
* `docs` contains further [documentation](docs/).

Client libraries are maintained in separate repositories:

* Python: [brawer/osmviews-py](https://github.com/brawer/osmviews-py) [![PyPI](https://img.shields.io/pypi/v/osmviews?label=pypi)](https://pypi.org/project/osmviews/)
* Rust: [brawer/osmviews-rs](https://github.com/brawer/osmviews-rs) [![crates.io](https://img.shields.io/crates/v/osmviews)](https://crates.io/crates/osmviews)

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).


## How it works — and prior art

The techniques used by `cmd/osmviews-builder` — and by the
[`osmviews-rs`](https://github.com/brawer/osmviews-rs) and
[`osmviews-py`](https://github.com/brawer/osmviews-py) client libraries — are
written up as a **defensive publication**. It doubles as a high-level tour of
the whole system: the level-embedding tile key, the streaming per-period sort
and cross-period merge, the constant-memory raster construction with inline
overviews, and the Cloud-Optimized GeoTIFF layout.

> *Method for Memory-Bounded Construction of a Globally Complete,
> High-Zoom-Level Cloud-Optimized GeoTIFF from Tile-Access Logs.*
> Technical Disclosure Commons, 2026.
> <!-- add the tdcommons.org URL here once the entry is live -->

**[Download the PDF](https://raw.githubusercontent.com/brawer/osmviews/main/docs/defensive-publication/defensive-publication.pdf)**
&nbsp;·&nbsp;
[source and build instructions](https://github.com/brawer/osmviews/tree/main/docs/defensive-publication)

It is published to establish prior art and keep these techniques free to use.


## License

Code: **MIT** — see [`LICENSE`](LICENSE).

The defensive publication under
[`docs/defensive-publication/`](docs/defensive-publication) — the LaTeX source
and the rendered PDF — is licensed **CC BY 4.0**.

The OSMViews raster this pipeline produces is released into the public domain
under **CC0 1.0**.

This repository is [REUSE](https://reuse.software) compliant: every file
declares its copyright and license, either in an SPDX header or via
[`REUSE.toml`](REUSE.toml).
