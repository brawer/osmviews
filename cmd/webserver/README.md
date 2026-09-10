<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Webserver

The webserver handles requests for
[osmviews.toolforge.org](https://osmviews.toolforge.org/).
It runs on the Wikimedia Toolforge infrastructure behind a reverse proxy.

It serves the landing page and redirects the legacy `/download/` URLs to the
CDN, which serves the files the builder publishes (GeoTIFF, bill of materials,
`datapackage.json`). `/download/osmviews.tiff` is a `302` to the current dated
object — the current version comes from polling `datapackage.json`; every other
`/download/` path is a permanent `301`. The webserver keeps no local state. See
[`docs/downloads.md`](../../docs/downloads.md) and
[issue #110](https://github.com/brawer/osmviews/issues/110).

Under `/beta/` it serves an embedded single-page app (built from
[`../../frontend/`](../../frontend/) into `internal/webui/dist`, see
[`internal/webui`](../../internal/webui)). This is work in progress —
[issue #100](https://github.com/brawer/osmviews/issues/100) — and is `noindex` /
`Disallow`ed for now.
