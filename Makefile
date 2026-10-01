# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Local dev targets, also the single source of truth for what CI runs
# (.github/workflows/build-test.yml calls these directly, one per step, so the
# two can't drift). See cmd/webserver/README.md,
# cmd/osmviews-builder/README.md.

.PHONY: build webserver builder dev test lint vulncheck ci clean

# Build both binaries, matching CI's "Build" step.
build: webserver builder

webserver:
	go build -o webserver ./cmd/webserver

builder:
	go build -o builder ./cmd/osmviews-builder

# Run the webserver locally without object storage (/download/ returns 404).
dev:
	go run ./cmd/webserver --dev --port 8080

# What CI's "Test" step runs.
test:
	go test -v ./...

# golangci-lint, configured in .golangci.yml. Its govet linter covers "go vet".
lint:
	golangci-lint run ./...

# Known vulnerabilities that the code actually calls, in dependencies and in
# the standard library of the Go version running the scan (pinned in go.mod).
vulncheck:
	govulncheck ./...

# Everything CI enforces, for a pre-push check.
ci:
	$(MAKE) lint
	$(MAKE) vulncheck
	go build ./...
	$(MAKE) test

clean:
	rm -f webserver builder
