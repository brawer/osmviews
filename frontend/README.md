<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Frontend

The single-page app that `cmd/webserver` serves under `/beta/`. Vite + React;
the `package.json`, lockfile and `vite.config.js` live at the repo root so the
Toolforge Node buildpack detects them.

```sh
npm ci            # install (uses the committed package-lock.json)
npm run dev       # Vite dev server with HMR (serves /beta/ on :5173)
npm run build     # → internal/webui/dist/, embedded into the Go binary
```

To see it served by the actual Go webserver (cache headers, SPA fallback, the
untouched `/` page) without S3 credentials:

```sh
npm run build
go run ./cmd/webserver --dev --port 8080   # /download/ 404s; /, /beta/, /robots.txt work
```

`npm run build` output is **not** checked in (see
[`../internal/webui/dist/.gitignore`](../internal/webui/dist/.gitignore)); it is
rebuilt in CI and, on deploy, by the Toolforge buildpack before the Go build. A
`go build` without a prior `npm run build` still works — `cmd/webserver` then
serves a short "not built" page at `/beta/`.

Tracked by [issue #100](https://github.com/brawer/osmviews/issues/100). While the
app is a moving target it is `noindex` and `Disallow`ed in `robots.txt`; `/` and
`/download/` are untouched.
