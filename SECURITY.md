<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: MIT
-->

# Security policy

This repository contains the data-processing pipeline and the web server behind
[osmviews.toolforge.org](https://osmviews.toolforge.org). It is pre-1.0 and
best-effort; fixes are made against `main` and the current deployment.

## Reporting a vulnerability

Please report suspected vulnerabilities privately, **not** as a public issue:

- Preferred: **[open a private report](https://github.com/brawer/osmviews/security/advisories/new)**
  via GitHub's "Report a vulnerability" (Security tab).
- Or email **sascha@brawer.ch**.

Please include a description of the issue, the affected component (pipeline or
web server), and a minimal way to reproduce it. You can expect an initial
response within about a week.

## Disclosure

Fixed vulnerabilities are published as GitHub Security Advisories for this
repository. The published OSMViews dataset is released into the public domain
([CC0-1.0](https://creativecommons.org/publicdomain/zero/1.0/)) and carries no
warranty.

## Scope

This policy covers the code in this repository. The client libraries have their
own policies:
[Python](https://github.com/brawer/osmviews-py/security/policy),
[Rust](https://github.com/brawer/osmviews-rs/security/policy).
