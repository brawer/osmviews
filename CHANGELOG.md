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

## [0.2.0](https://github.com/brawer/osmviews/compare/v0.1.6...v0.2.0) (2026-09-08)


### ⚠ BREAKING CHANGES

* **osmviews-builder:** osmviews-builder and the webserver now require PUBLIC_S3_* (ENDPOINT, KEY, SECRET, BUCKET, optional REGION). The current published objects must be copied to the public bucket's data/ prefix before this deploys, or osmviews.toolforge.org/download/osmviews.tiff 404s until the next weekly build.
* **osmviews-builder:** osmviews-builder now requires INTERNAL_S3_ENDPOINT, INTERNAL_S3_KEY, INTERNAL_S3_SECRET and INTERNAL_S3_BUCKET instead of S3_ENDPOINT / S3_KEY / S3_SECRET. Set them before deploying.
* **osmviews-builder:** keep dated CycloneDX BOMs forever
* **webserver:** drop the Link: rel="describedby" header
* **osmviews-builder:** the `osmviews-stats-<date>.json` sidecar is no longer published. Its `externalReference` is gone from the CycloneDX BOM. Consumers that need the value distribution read the Raster Attribute Table embedded in the GeoTIFF instead.

### 🆕 Features

* **osmviews-builder:** configure the internal bucket via INTERNAL_S3_* env ([d4219eb](https://github.com/brawer/osmviews/commit/d4219eb6e9cd1814110c640ffb07c01a777ba8d0))
* **osmviews-builder:** embed the value histogram in the GeoTIFF ([3275863](https://github.com/brawer/osmviews/commit/327586378fbb18710ab58f8d3c75ea0fe5da6038)), closes [#109](https://github.com/brawer/osmviews/issues/109)
* **osmviews-builder:** emit data/datapackage.json ([15ec7d2](https://github.com/brawer/osmviews/commit/15ec7d24dea6c635b02a5ec55bd667d935a262df))
* **osmviews-builder:** keep dated CycloneDX BOMs forever ([c3eff12](https://github.com/brawer/osmviews/commit/c3eff12d43cc1ee799d03bafeb19329a0d295c8a))
* **osmviews-builder:** publish the GeoTIFF and BOM to the public CDN bucket ([3330131](https://github.com/brawer/osmviews/commit/3330131b3fd09ecb1dd41e586a15489c913176b9))
* **webserver:** drop the Link: rel="describedby" header ([c7d2fe3](https://github.com/brawer/osmviews/commit/c7d2fe325960548a371fad4bf86cac1cec6566ab))

## [0.1.6](https://github.com/brawer/osmviews/compare/v0.1.5...v0.1.6) (2026-09-04)


### 🐞 Fixes

* **webserver:** keep release builds from being stamped -modified ([7074cb4](https://github.com/brawer/osmviews/commit/7074cb41e8a564d851ab5c507e955dffbd224743))

## [0.1.5](https://github.com/brawer/osmviews/compare/v0.1.4...v0.1.5) (2026-09-04)


### 🆕 Features

* **webserver:** add --dev flag for local runs without object storage ([da2289f](https://github.com/brawer/osmviews/commit/da2289f31c4c5033207df9fa06e4bcb3277827d1))
* **webserver:** serve an embedded /beta/ web app shell ([b23c028](https://github.com/brawer/osmviews/commit/b23c0289ae8584270593499bb51924fb1dac8067))

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
