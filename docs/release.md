<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Release

Releases deploy automatically. To cut one:

```sh
git switch main && git pull
git tag X.Y.Z            # matching the existing 0.0.x scheme
git push origin X.Y.Z
```

Pushing the tag triggers CI (`.github/workflows/build-test.yml`). After the
build and tests pass, the `deploy` job calls the Toolforge components API,
which rebuilds the image from `main` and recreates both components — the
`webserver` and the daily `osmviews-builder` job (see
[`toolforge.yaml`](../toolforge.yaml)). Watch it at
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
toolforge components deployment create --description "release X.Y.Z"
```

`toolforge components config show` prints the currently registered config;
`toolforge components config create toolforge.yaml` re-registers it from a
local copy of the file if it has drifted.

## Changing the deployment shape

Component resources, the schedule, the health check, and so on live in
[`toolforge.yaml`](../toolforge.yaml). Because it declares
`source_url: …/main/toolforge.yaml`, the components API re-fetches it from
GitHub on every deployment, so a merged change to that file takes effect on
the next release with no bastion step.
