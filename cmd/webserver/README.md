<!--
SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Webserver

The webserver handles requests for
[osmviews.toolforge.org](https://osmviews.toolforge.org/).
It runs on the Wikimedia Toolforge infrastructure behind a reverse proxy.


## Release instructions

We should set up a fully automatic release process, but are blocked on
[Wikimedia T194332](https://phabricator.wikimedia.org/T194332). Currently,
a GitHub action automatically builds and tests a release candidate
whenever a git tag gets pushed to the repository. However, to become
live, the binary still needs to be deployed to production. Note that
this will update both the backend pipeline and the web server.

```bash
$ ssh bastion.toolforge.org
$ become osmviews
$ python3 prod/deploy_release.py latest  # either 'latest' or tag like '0.0.2'
```
