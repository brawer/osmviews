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
[osmviews.toolforge.org](https://osmviews.toolforge.org). The Python and Rust
client libraries live in
[brawer/osmviews-py](https://github.com/brawer/osmviews-py) and
[brawer/osmviews-rs](https://github.com/brawer/osmviews-rs).

## Getting set up 🛠️

```sh
git clone https://github.com/brawer/osmviews.git
cd osmviews
go build ./...
go test ./...
go vet ./...
gofmt -l .          # should print nothing
```

CI (`.github/workflows/build-test.yml`) runs `go build`, `go vet` and
`go test -v ./...` and must be green before a pull request can merge. Keep
`gofmt` clean.

[`docs/technical-design.md`](docs/technical-design.md) explains how the pipeline works —
the quad-tree ordering, the streaming raster build, and the file-size tricks.
Worth reading before touching `cmd/osmviews-builder`.

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

Releases are cut by tagging (`X.Y.Z`) and deployed to Toolforge; see
[`docs/release.md`](docs/release.md).
