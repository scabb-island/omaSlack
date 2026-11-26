# omaslack — local development targets.
#
# `make check` is what CI runs. `make local` adds the checks that need this
# machine (the Omarchy shell for QML, the omarchy CLI, node for OpenSpec); CI
# skips them on purpose to save shared-runner minutes.

GO          ?= go
PKG         := gitlab.com/scabb-island/omaslack
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
               -X $(PKG)/internal/version.Version=$(VERSION) \
               -X $(PKG)/internal/version.Commit=$(COMMIT) \
               -X $(PKG)/internal/version.Date=$(DATE)

OMARCHY_SHELL ?= $(or $(OMARCHY_PATH),/usr/share/omarchy)/shell
QMLLINT       ?= /usr/lib/qt6/bin/qmllint
QML_IMPORTS   := .cache/qml-imports

.PHONY: all build test test-race vet lint check qmllint plugin-validate specs local snapshot clean

all: check

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/omaslack ./cmd/omaslack

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

# What CI runs.
check: vet lint test

# qmllint needs the shell's own modules: the shell maps its root to the `qs`
# import prefix, so we expose it under that name. missing-property is off
# because the shell hands plugins a dynamically typed `bar` object.
qmllint:
	@mkdir -p $(QML_IMPORTS)
	@ln -sfn $(OMARCHY_SHELL) $(QML_IMPORTS)/qs
	$(QMLLINT) --missing-property disable -I $(QML_IMPORTS) plugin/*.qml

plugin-validate:
	omarchy plugin validate plugin

specs:
	npx -y @fission-ai/openspec@latest validate --all --strict

# Everything, including the checks that only run on an Omarchy machine.
local: check test-race qmllint plugin-validate specs

snapshot:
	goreleaser release --snapshot --clean

# ── ub-mode quality gates (local only, never in CI) ──────────────────────────
# crapper, dryer and mutator (github.com/unclebob) are installed by mise from
# git, pinned to the commits we reviewed (see mise.toml). mutator gets crapper
# in its own environment, which is where it looks for it first.
GO_SOURCES := cmd internal

.PHONY: crap dry mutate quality

crap:      ## CRAP per function; fails if any is >= 10
	crapper --threshold 9.99 $(GO_SOURCES)

dry:       ## structural duplication >= 0.8
	dryer --threshold 0.8 $(GO_SOURCES)

mutate:    ## mutation testing; differential after the first run
	mutator $(GO_SOURCES)

quality: crap dry mutate

clean:
	rm -rf bin dist .cache
