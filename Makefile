# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Local dev targets, also the single source of truth for what CI runs
# (.github/workflows/build-test.yml calls these directly, one per step, so the
# two can't drift). See frontend/README.md, cmd/webserver/README.md,
# cmd/osmviews-builder/README.md.

.PHONY: build webserver builder frontend check-frontend lockfile-check \
	dev test lint ci clean

# Build both binaries, matching CI's "Build" step.
build: webserver builder

# Build the webserver the way the deploy does: frontend first, then embed it
# into the Go binary via //go:embed.
webserver: frontend
	go build -o webserver ./cmd/webserver

builder:
	go build -o builder ./cmd/osmviews-builder

frontend:
	npm ci
	npm run build
	test -f internal/webui/dist/index.html

# Size budget + npm audit + dependency-licence/signature checks. Assumes
# "frontend" already ran, with dev dependencies still installed.
check-frontend:
	./scripts/check-frontend.sh

# The Toolforge Node buildpack prunes dev deps as part of every deploy build;
# do the same and check package-lock.json comes out unchanged. An npm version
# skew between whoever generated the lockfile and the buildpack's bundled npm
# would otherwise rewrite it, dirtying the tree and stamping release builds
# "-modified" (see the fix in #103). Also assumes "frontend" already ran.
lockfile-check:
	NODE_ENV=production npm prune
	git diff --exit-code -- package-lock.json

# Run the webserver locally without object storage (/download/ returns 404).
dev: frontend
	go run ./cmd/webserver --dev --port 8080

# Go tests only — what CI's "Test" step runs. Works on a fresh checkout even
# without a prior "npm run build" (cmd/webserver then tests against the
# internal/webui/dist placeholder).
test:
	go test -v ./...

lint:
	go vet ./...

# Everything CI enforces, for a pre-push check. Restores the dev dependencies
# that lockfile-check's "npm prune" removed.
ci: frontend check-frontend lockfile-check
	npm ci
	$(MAKE) lint
	go build ./...
	$(MAKE) test

clean:
	rm -rf webserver builder internal/webui/dist/assets internal/webui/dist/index.html
