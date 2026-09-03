<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Webserver

The webserver handles requests for
[osmviews.toolforge.org](https://osmviews.toolforge.org/).
It runs on the Wikimedia Toolforge infrastructure behind a reverse proxy.

It serves the landing page and, under `/download/`, the files the builder
publishes: the GeoTIFF at a stable URL and the dated auxiliary files, with a
`Link` header from the GeoTIFF to its bill of materials. See
[`docs/downloads.md`](../../docs/downloads.md).
