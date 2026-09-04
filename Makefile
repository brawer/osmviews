# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Convenience targets for local parity with CI and the Toolforge buildpack.
# See frontend/README.md, cmd/webserver/README.md, cmd/osmviews-builder/README.md.

.PHONY: build webserver builder frontend dev test check clean

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

test:
	go test ./...

# Everything CI enforces, for a pre-push check. Mirrors the Toolforge buildpack
# sequence (npm ci -> build -> prune); the prune must not touch package-lock.json
# (an npm version skew would, making release builds stamp "-modified"). Restores
# dev dependencies afterwards.
check: frontend
	./scripts/check-frontend.sh
	NODE_ENV=production npm prune
	git diff --exit-code -- package-lock.json
	npm ci
	go vet ./...
	go build ./...
	go test ./...

clean:
	rm -rf webserver builder internal/webui/dist/assets internal/webui/dist/index.html
