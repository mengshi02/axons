# Axons Desktop Application Makefile
# Build and development commands for Electron desktop app

# ── Variables ────────────────────────────────────────────────────────────────

BINARY_NAME := axons-daemon
ROOT_DIR    := ..
DAEMON_BIN  := bin/$(BINARY_NAME)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
	-X github.com/mengshi02/axons/internal/version.Version=$(VERSION) \
	-X github.com/mengshi02/axons/internal/version.Commit=$(COMMIT) \
	-X github.com/mengshi02/axons/internal/version.Date=$(DATE) \
	-s -w"

# ── PHONY ────────────────────────────────────────────────────────────────────

.PHONY: all dev build build-daemon build-electron \
        dist dist-mac dist-win dist-linux \
        install clean help

# ── Default ──────────────────────────────────────────────────────────────────

all: build

# ══════════════════════════════════════════════════════════════════════════════
#  Development
# ══════════════════════════════════════════════════════════════════════════════

## dev: Run in development mode (build daemon + start Electron)
dev: build-daemon
	@echo "Starting Electron in development mode..."
	npm run dev

# ══════════════════════════════════════════════════════════════════════════════
#  Build
# ══════════════════════════════════════════════════════════════════════════════

## build: Build daemon + compile Electron main process
build: build-daemon build-electron

## build-daemon: Build the Go daemon binary for current platform
build-daemon:
	@echo "Building Go daemon..."
	@mkdir -p bin
	cd $(ROOT_DIR) && go build $(LDFLAGS) -o desktop/$(DAEMON_BIN) ./cmd/axons/
	@echo "Daemon built: $(DAEMON_BIN)"

## build-electron: Compile TypeScript Electron main process
build-electron:
	@echo "Compiling Electron main process..."
	npm run build
	@echo "Electron build complete."

# ══════════════════════════════════════════════════════════════════════════════
#  Distribution (electron-builder)
# ══════════════════════════════════════════════════════════════════════════════

## dist: Package for current platform
dist: build
	npm run dist

## dist-mac: Package for macOS (DMG + ZIP)
dist-mac: build
	npm run dist:mac

## dist-win: Package for Windows (NSIS installer)
dist-win:
	@echo "Building daemon for Windows..."
	@mkdir -p bin
	cd $(ROOT_DIR) && GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME).exe ./cmd/axons/
	npm run build
	npm run dist:win

## dist-linux: Package for Linux (AppImage + deb)
dist-linux:
	@echo "Building daemon for Linux..."
	@mkdir -p bin
	cd $(ROOT_DIR) && GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME) ./cmd/axons/
	npm run build
	npm run dist:linux

# ══════════════════════════════════════════════════════════════════════════════
#  Dependencies & Cleanup
# ══════════════════════════════════════════════════════════════════════════════

## install: Install npm dependencies
install:
	npm install

## clean: Clean build artifacts
clean:
	rm -rf bin/ dist/ release/

# ══════════════════════════════════════════════════════════════════════════════
#  Misc
# ══════════════════════════════════════════════════════════════════════════════

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' Makefile | column -t -s ':'