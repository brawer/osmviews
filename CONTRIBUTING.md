<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Contributing 👋

Thanks for looking! Contributions of every size are welcome — a typo fix, a
clearer comment, a missing test case, a bug report, or a new feature. No
contribution is too small. 🙂

This repository holds the data-processing pipeline (`cmd/osmviews-builder`) and
the web server (`cmd/webserver`) behind
[osmviews.toolforge.org](https://osmviews.toolforge.org). The map with the
current data is at [osmviews.brawer.ch](https://osmviews.brawer.ch); its code
lives in [brawer/osmviews-app](https://github.com/brawer/osmviews-app). The
Python and Rust client libraries live in
[brawer/osmviews-py](https://github.com/brawer/osmviews-py) and
[brawer/osmviews-rs](https://github.com/brawer/osmviews-rs).

## Getting set up 🛠️

```sh
git clone https://github.com/brawer/osmviews.git
cd osmviews
go build ./...
go test ./...
make lint
```

`make lint` runs [golangci-lint](https://golangci-lint.run/), configured in
[`.golangci.yml`](.golangci.yml). It includes `go vet`, `staticcheck`,
unchecked-error and unused-code checks, and fails on files that aren't
`gofmt -s` formatted. Install the version CI uses with
`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`,
or the latest release with `brew install golangci-lint`.
`golangci-lint fmt` fixes the formatting.

`make vulncheck` runs [govulncheck](https://go.dev/doc/security/vuln/), which
reports known vulnerabilities that the code actually calls, including in the Go
standard library. Install it with
`go install golang.org/x/vuln/cmd/govulncheck@v1.8.0`. If it fails on the
standard library, raise the `toolchain` line in `go.mod` to a patched release.
Leave the `go` line at a version the Toolforge Go buildpack knows (it
installs that version, which then downloads the `toolchain` one).

CI (`.github/workflows/build-test.yml`) runs `make build`, `make lint`,
`make vulncheck` and `make test` and must be green before a pull request can
merge. It also runs weekly, to catch newly published vulnerabilities. Run `make ci`
to do all of them locally before pushing.

The [defensive publication](docs/defensive-publication) explains how the
pipeline works — the tile-key ordering, the streaming raster build, and the
file-size tricks. Worth reading before touching `cmd/osmviews-builder`.

## Making changes

- Keep changes focused; one topic per pull request.
- Add or update tests for any changed behaviour.
- Every file needs license info (this repo is
  [REUSE](https://reuse.software)-compliant): add the `SPDX-FileCopyrightText`
  and `SPDX-License-Identifier` header that the surrounding files use, or a
  `.license` sidecar for binaries. `reuse lint` must pass.

## Pull requests

`main` is protected: changes land through a pull request, CI must pass, and the
branch must be up to date before merging. PRs are **squash-merged**, so the
**pull request title becomes the commit message** on `main`.

Write PR titles as [Conventional Commits](https://www.conventionalcommits.org):

```
<type>[(scope)][!]: <description>
```

Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`,
`chore`, `ci`. Append `!` (or a `BREAKING CHANGE:` body) for a breaking change.
Examples:

```
fix: handle a week with no tile logs
feat(builder): scale partial weeks up to a full week
docs: refresh the release runbook
```

The `Conventional Commits` check enforces this. Individual commit messages within
a PR are not checked — only the title that gets squashed.

## Releasing and deploying

Releases are automated with
[release-please](https://github.com/googleapis/release-please): merging a
`feat:`/`fix:`/`perf:` PR updates an open release pull request, and merging that
PR tags `vX.Y.Z` and deploys to Toolforge. See [`RELEASING.md`](RELEASING.md).
