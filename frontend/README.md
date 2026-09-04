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
npm run dev       # local dev server with HMR
npm run build     # → internal/webui/dist/, embedded into the Go binary
```

`npm run build` output is **not** checked in (see
[`../internal/webui/dist/.gitignore`](../internal/webui/dist/.gitignore)); it is
rebuilt in CI and, on deploy, by the Toolforge buildpack before the Go build. A
`go build` without a prior `npm run build` still works — `cmd/webserver` then
serves a short "not built" page at `/beta/`.

Tracked by [issue #100](https://github.com/brawer/osmviews/issues/100). While the
app is a moving target it is `noindex` and `Disallow`ed in `robots.txt`; `/` and
`/download/` are untouched.
