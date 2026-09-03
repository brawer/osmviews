<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Changelog

All notable changes to OSMViews are recorded here. From version 0.1.1 on, this
file is maintained by
[release-please](https://github.com/googleapis/release-please) from the
Conventional Commit history. Versioning follows
[Semantic Versioning](https://semver.org); while OSMViews is pre-1.0 a bump of
the **minor** version may be breaking — see
[RELEASING.md](RELEASING.md#choosing-the-version-number).

## [0.1.4](https://github.com/brawer/osmviews/compare/v0.1.3...v0.1.4) (2026-09-03)


### 🆕 Features

* **osmviews-builder:** reference the stats JSON from the BOM ([a9adb9f](https://github.com/brawer/osmviews/commit/a9adb9f29f2de99dc872e6b26240c3d05427ac17))
* **webserver:** serve dated BOM URLs and link them from the GeoTIFF ([82923ca](https://github.com/brawer/osmviews/commit/82923caf67b09d34fdda02cf33aa5d4f523f5064))
* **webserver:** serve the stats JSON dated, like the BOM ([dc78b79](https://github.com/brawer/osmviews/commit/dc78b792779e3c1108866c91403c76251a88d11d))


### 🐞 Fixes

* **osmviews-builder:** upload the stats JSON before the BOM ([d553e8e](https://github.com/brawer/osmviews/commit/d553e8e0e6cf650f4b0c9d7d7378b8e5d5949906))
* **webserver:** sanitize the request path in the download error log ([ff76582](https://github.com/brawer/osmviews/commit/ff7658295b80302ba8c2acb32592a329596caafd))

## [0.1.3](https://github.com/brawer/osmviews/compare/v0.1.2...v0.1.3) (2026-09-02)


### 🆕 Features

* **osmviews-builder:** publish a CycloneDX BOM for each GeoTIFF ([0e0f3fb](https://github.com/brawer/osmviews/commit/0e0f3fb14fae5fb0f2526f54bf2a57d6558ca873))

## [0.1.2](https://github.com/brawer/osmviews/compare/v0.1.1...v0.1.2) (2026-09-01)


### 🐞 Fixes

* report the release version from deployed binaries ([8f5becc](https://github.com/brawer/osmviews/commit/8f5becc6b686c8d72136ed263158b4fd31e18f34))

## [0.1.1](https://github.com/brawer/osmviews/releases/tag/0.1.1) (2026-08-31)

Earlier tags (`0.0.2`…`0.1.1`) predate this changelog; see the
[GitHub tags page](https://github.com/brawer/osmviews/tags) for their commits.
Highlights of the `0.1.x` line: the Go rewrite of the pipeline and web server,
the streaming zoom-18 raster build, and Toolforge components deployment.
