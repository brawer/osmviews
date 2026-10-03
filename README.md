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

**How much does the world look at a place?** OSMViews ranks every location
on Earth by how often people view it on
[OpenStreetMap](https://www.openstreetmap.org), down to ~150 m.
Updated weekly, averaged over a year, free for any use.

[![OSMViews map of Europe and Africa, with a magnifier showing Dublin at 0.731](docs/osmviews.png)](https://osmviews.brawer.ch)

**→ [Explore the map](https://osmviews.brawer.ch)**


## Use the data

* **Python:** `pip install osmviews` — [osmviews-py](https://github.com/brawer/osmviews-py)
* **Rust:** `cargo add osmviews` — [osmviews-rs](https://github.com/brawer/osmviews-rs)
* **Download:** a Cloud-Optimized GeoTIFF, listed in
  [datapackage.json](https://osmviews.brawer.ch/data/datapackage.json) —
  see [docs/downloads.md](docs/downloads.md)


## Learn more

* [How it works](https://www.tdcommons.org/dpubs_series/11589) — a
  [defensive publication](docs/defensive-publication/) describing the pipeline
* [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) ·
  [Map app source](https://github.com/brawer/osmviews-app)


## License

Code: [MIT](LICENSE). Data and screenshot:
[CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/) (public domain).
Docs: CC BY 4.0.
