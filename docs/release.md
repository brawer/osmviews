<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Release

Release automation is blocked on
[T380127](https://phabricator.wikimedia.org/T380127) and
[T194332](https://phabricator.wikimedia.org/T194332). Meanwhile,
we have a manual release process, which is not great.

```sh
ssh login.toolforge.org
become osmviews
toolforge build start --use-latest-versions https://github.com/brawer/osmviews.git
toolforge webservice --mount=none buildservice restart
toolforge jobs flush
toolforge jobs run --image tool-osmviews/tool-osmviews:latest --mem 3G --cpu 2 --mount none --schedule @daily --command osmviews-builder osmviews-builder
```

## Version stamping

`osmviews-builder` writes its version into the `Software` tag of every
output GeoTIFF, and `webserver` returns it in the `Server` HTTP header.

If the buildpack keeps the `.git` directory, both binaries fill this in
from the source revision automatically (`OSMViews/git-<commit>`). To
embed a proper release tag instead, tag the commit and tell the build
service to pass a linker flag, e.g.:

```sh
# in a local checkout
git tag 0.0.8 && git push origin 0.0.8

# on Toolforge, before `toolforge build start`
toolforge envvars create GO_LINKER_SYMBOL main.SoftwareVersion
toolforge envvars create GO_LINKER_VALUE  OSMViews/0.0.8
```

(`GO_LINKER_SYMBOL` / `GO_LINKER_VALUE` set a single symbol; the webserver
build uses `main.ServerVersion`. Check the current buildpack docs — newer
ones take `BP_GO_BUILD_LDFLAGS` with full `-ldflags` instead.)

After deploying, verify what landed:

```sh
toolforge jobs run --image tool-osmviews/tool-osmviews:latest --mount none \
  --command 'osmviews-builder --version' check-version
toolforge jobs logs check-version
```
