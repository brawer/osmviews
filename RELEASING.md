<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Releasing

Releases are automated with
[release-please](https://github.com/googleapis/release-please). Merging a
`feat:`/`fix:`/`perf:` pull request updates an open release PR; merging that PR
cuts a `vX.Y.Z` tag and a GitHub release, and the tag push redeploys the service
to Toolforge. Nothing is published to a package registry.

## How it works

1. Every pull request has a
   [Conventional Commits](https://www.conventionalcommits.org) title (`feat:`,
   `fix:`, `perf:`, `docs:`, `refactor:`, `test:`, `build:`, `ci:`, `chore:`).
   PRs are squash-merged, so the title becomes the commit on `main`. The
   `pr-title` check enforces this.
2. [`.github/workflows/release-please.yml`](.github/workflows/release-please.yml)
   watches `main` and keeps a single open **"chore(main): release X.Y.Z"** pull
   request. It updates `CHANGELOG.md` from the commit history, bumps `version:`
   in `CITATION.cff`, and updates `.release-please-manifest.json`. It runs as the
   **`brawer-release-bot`** GitHub App (see [One-time setup](#one-time-setup)) —
   a workflow run started by the built-in `GITHUB_TOKEN` cannot itself start
   further runs, so the App identity is what lets the tag push trigger the
   deploy.
3. Review that PR and squash-merge it when you want to cut the release.
   release-please then pushes the `vX.Y.Z` tag and creates the GitHub release.
   (Tags `0.0.2`…`0.1.1`, cut before release-please, have no `v` prefix.)
   Immutable releases are enabled, so once published the tag, commit and assets
   are frozen — a botched release can only be superseded by a new version, never
   re-tagged.

> **Housekeeping:** `release-please-config.json` carries a `bootstrap-sha`
> pinned to the `0.1.1` commit, because the pre-release-please tags are bare
> (`0.1.1`, no `v`) and release-please can't anchor to them. Once `v0.1.2` or
> later exists as a real anchor tag, delete that line.

## Choosing the version number

release-please picks the bump from the commit types since the last release:
`fix:`/`perf:` → patch, `feat:` → minor, and a `!` after the type or a
`BREAKING CHANGE:` footer → a breaking bump. While OSMViews is pre-1.0,
`bump-minor-pre-major` maps a breaking change to a **minor** bump (`0.1.z` →
`0.2.0`) and everything else to a **patch** bump.

Only `feat:`, `fix:` and `perf:` commits cut a release and appear in
`CHANGELOG.md`. `docs:`, `refactor:`, `test:`, `build:`, `ci:` and `chore:`
(including Dependabot's `chore(deps):`) are silent — they ride along with the
next real release.

To ship a silent change on its own, or to force a specific number, put
`Release-As: X.Y.Z` in a commit body (e.g. an empty commit on `main`).

## What the tag does

Pushing the `vX.Y.Z` tag triggers CI
([`.github/workflows/build-test.yml`](.github/workflows/build-test.yml)). After
the build and tests pass, the `deploy` job calls the Toolforge components API,
which rebuilds the image from `main` and recreates both components — the
`webserver` and the daily `osmviews-builder` job (see
[`toolforge.yaml`](toolforge.yaml)). Watch it at
<https://github.com/brawer/osmviews/actions>.

**Do not merge anything to `main` until the deploy job finishes.** The
deployment builds `main` at `HEAD` (`toolforge.yaml` pins `ref: main`), so
`main` must still point at the tagged commit when the build runs.

## Verify

```sh
# Server header carries the deployed revision:
curl -sI https://osmviews.toolforge.org/ | grep -i '^server:'
# → OSMViews/git-<commit>, where <commit> is the tagged commit
git tag --points-at "$(curl -sI https://osmviews.toolforge.org/ \
  | sed -n 's#.*OSMViews/git-##p' | tr -d '\r')"

# The daily job produces a fresh GeoTIFF within a day:
curl -sI https://osmviews.toolforge.org/download/osmviews.tiff | grep -i last-modified
```

The buildpack build does not pass linker flags, so the `Software` GeoTIFF
tag and the `Server` header read `OSMViews/git-<commit>` rather than the
tag; the commit is what carries the tag.

## If the automatic deploy fails

Deploy from the bastion by hand:

```sh
ssh login.toolforge.org
become osmviews
toolforge components deployment create --description "release vX.Y.Z"
```

`toolforge components config show` prints the currently registered config;
`toolforge components config create toolforge.yaml` re-registers it from a
local copy of the file if it has drifted.

## Changing the deployment shape

Component resources, the schedule, the health check, and so on live in
[`toolforge.yaml`](toolforge.yaml). Because it declares
`source_url: …/main/toolforge.yaml`, the components API re-fetches it from
GitHub on every deployment, so a merged change to that file takes effect on
the next release with no bastion step.

## One-time setup

Already configured on this repository (listed here in case it needs rebuilding):

- The `main` ruleset requires the `build-test` and `pr-title` checks and
  squash-only merges.
- **Immutable releases** are enabled
  (`gh api -X PUT repos/brawer/osmviews/immutable-releases`).
- **`brawer-release-bot` GitHub App** — `release-please.yml` authenticates as
  this App so its tag push can trigger the deploy workflow:
  1. Create the App at <https://github.com/settings/apps/new> (a personal App is
     fine). Uncheck **Webhook → Active**. **Repository permissions**:
     `Contents: Read and write`, `Pull requests: Read and write`; nothing else.
  2. On the App page: **Generate a private key** (downloads a `.pem`) and note
     the **Client ID** (`Iv23…`).
  3. **Install App** → select `brawer/osmviews` only.
  4. In the repo, **Settings → Secrets and variables → Actions**: add a
     **variable** `RELEASE_PLEASE_APP_CLIENT_ID` (the Client ID) and a **secret**
     `RELEASE_PLEASE_APP_PRIVATE_KEY` (the full `.pem` contents). Until the
     variable exists, the `release-please` workflow is skipped.
  5. Delete the local `.pem`. To rotate, generate a new key and update the
     secret; App tokens themselves are short-lived and refreshed per run.
