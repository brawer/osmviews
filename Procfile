# SPDX-FileCopyrightText: 2025 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Heroku-like Procfile for Wikimedia Toolforge Build Service
# https://wikitech.wikimedia.org/wiki/Help:Toolforge/Build_Service

# Work around https://phabricator.wikimedia.org/T363417 by passing
# absolute paths to our binaries in the buildpack-generated container image.
# Workaround recommended on mailing list (cloud@lists.wikimedia.org)
# on April 30, 2024. Phabricator ticket T363417 still open in April 2025.
osmviews-builder: /layers/heroku_go/go_target/bin/osmviews-builder
web: /layers/heroku_go/go_target/bin/webserver
