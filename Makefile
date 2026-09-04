# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Convenience targets for local parity with CI and the Toolforge buildpack.
# See frontend/README.md, cmd/webserver/README.md, cmd/osmviews-builder/README.md.

.PHONY: build webserver builder frontend dev test lint ci clean

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

# Run the webserver locally without object storage (/download/ returns 404).
dev: frontend
	go run ./cmd/webserver --dev --port 8080

# Go tests only — what CI's "Test" step runs. Works on a fresh checkout even
# without a prior "npm run build" (cmd/webserver then tests against the
# internal/webui/dist placeholder).
test:
	go test ./...

lint:
	go vet ./...

# Everything CI enforces, for a pre-push check: the frontend size/supply-chain
# guards, the buildpack's npm-prune step (must not touch package-lock.json —
# an npm version skew would, making release builds stamp "-modified"), then
# lint/build/test. Restores dev dependencies pruned along the way.
ci: frontend
	./scripts/check-frontend.sh
	NODE_ENV=production npm prune
	git diff --exit-code -- package-lock.json
	npm ci
	$(MAKE) lint
	go build ./...
	$(MAKE) test

clean:
	rm -rf webserver builder internal/webui/dist/assets internal/webui/dist/index.html
