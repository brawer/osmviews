# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Local dev targets, also the single source of truth for what CI runs
# (.github/workflows/build-test.yml calls these directly, one per step, so the
# two can't drift). See cmd/webserver/README.md,
# cmd/osmviews-builder/README.md.

.PHONY: build webserver builder dev test lint vulncheck check-datapackage \
	ci clean

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

# Validate the data package golden file, which TestWriteDatapackage_Golden
# keeps identical to the builder's output, against the Frictionless v2 profile
# it declares as its $schema. check-jsonschema rather than a Go validator,
# because the profile's path patterns use ECMAScript lookaheads that Go's
# regexp package cannot compile. Needs pipx (preinstalled on GitHub runners).
CHECK_JSONSCHEMA = pipx run check-jsonschema==0.38.2
check-datapackage:
	$(CHECK_JSONSCHEMA) \
		--schemafile https://datapackage.org/profiles/2.0/datapackage.json \
		cmd/osmviews-builder/testdata/datapackage.golden.json

# Everything CI enforces, for a pre-push check.
ci:
	$(MAKE) lint
	$(MAKE) vulncheck
	go build ./...
	$(MAKE) test
	$(MAKE) check-datapackage

clean:
	rm -f webserver builder
