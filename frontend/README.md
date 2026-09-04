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

The repo-root `Makefile` wraps the common flows:

```sh
make build        # webserver + builder — everything CI's "Build" step builds
make webserver    # npm ci + npm run build + go build -o webserver ./cmd/webserver
make dev          # build the frontend, then run the webserver with --dev on :8080
make test         # go test ./... — what CI's "Test" step runs
make lint         # go vet ./...
make ci           # everything CI enforces (see below) — build+test+lint plus
                   # the frontend guards; a pre-push check
```

`make dev` / `--dev` runs the actual Go webserver without S3 credentials
(`/download/` 404s; `/`, `/beta/`, `/robots.txt` work), so you can check cache
headers, the SPA fallback and the untouched `/` page.

CI (`.github/workflows/build-test.yml`) runs one Makefile target per step —
`frontend`, `check-frontend`, `lockfile-check`, `build`, `lint`, `test` — rather
than duplicating their commands in the workflow, so local (`make ci`) and CI
can't drift apart.

`make check-frontend` runs `scripts/check-frontend.sh` after the build:

- a gzipped JS size budget,
- `npm audit --audit-level=high` (known vulnerabilities),
- `npm audit signatures` (registry signatures + provenance attestations) and a
  check that every dependency resolves from `registry.npmjs.org`,
- a dependency-licence denylist (no GPL/AGPL/LGPL/SSPL/unlicensed).

`npm run build` output is **not** checked in (see
[`../internal/webui/dist/.gitignore`](../internal/webui/dist/.gitignore)); it is
rebuilt in CI and, on deploy, by the Toolforge buildpack before the Go build. A
`go build` without a prior `npm run build` still works — `cmd/webserver` then
serves a short "not built" page at `/beta/`.

Regenerate `package-lock.json` with **npm 10** (`npx npm@10 install`). The
Toolforge Node buildpack bundles npm 10, and its `npm prune` step rewrites a
lockfile that a newer npm wrote (dropping `libc` hints), which would dirty the
tree and stamp the release binary `-modified`. CI runs the full
`npm ci && npm run build && npm prune` and fails on any resulting diff.

Tracked by [issue #100](https://github.com/brawer/osmviews/issues/100). While the
app is a moving target it is `noindex` and `Disallow`ed in `robots.txt`; `/` and
`/download/` are untouched.
