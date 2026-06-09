# Axons Desktop Application Makefile
# Build and development commands for Electron desktop app

# ── Variables ────────────────────────────────────────────────────────────────

BINARY_NAME := axons-daemon
ROOT_DIR    := ..
DAEMON_BIN  := bin/$(BINARY_NAME)

VERSION ?= $(shell cat $(ROOT_DIR)/VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
	-X github.com/mengshi02/axons/internal/version.Version=$(VERSION) \
	-X github.com/mengshi02/axons/internal/version.Commit=$(COMMIT) \
	-X github.com/mengshi02/axons/internal/version.Date=$(DATE) \
	-s -w"

# ── PHONY ────────────────────────────────────────────────────────────────────

.PHONY: all dev build build-daemon build-daemon-mac build-daemon-mac-arm64 build-daemon-mac-x64 build-electron \
        dist dist-mac dist-mac-arm64 dist-mac-x64 dist-win dist-linux \
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

## build-daemon-mac: Build the Go daemon for both macOS architectures (x64 + arm64)
build-daemon-mac:
	@echo "Building Go daemon for macOS x64..."
	@mkdir -p bin
	cd $(ROOT_DIR) && GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME)-x64 ./cmd/axons/
	@echo "Daemon built: bin/$(BINARY_NAME)-x64"
	@echo "Building Go daemon for macOS arm64..."
	cd $(ROOT_DIR) && GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME)-arm64 ./cmd/axons/
	@echo "Daemon built: bin/$(BINARY_NAME)-arm64"
	@echo "Copying host-arch binary as default for development..."
	@cp "bin/$(BINARY_NAME)-$(shell uname -m | sed 's/x86_64/x64/')" "bin/$(BINARY_NAME)"
	@echo "Both architectures built."

## build-daemon-mac-arm64: Build the Go daemon for macOS arm64 only
build-daemon-mac-arm64:
	@mkdir -p bin
	cd $(ROOT_DIR) && GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME) ./cmd/axons/
	@echo "Daemon built: bin/$(BINARY_NAME) (arm64)"

## build-daemon-mac-x64: Build the Go daemon for macOS x64 only
build-daemon-mac-x64:
	@mkdir -p bin
	cd $(ROOT_DIR) && GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o desktop/bin/$(BINARY_NAME) ./cmd/axons/
	@echo "Daemon built: bin/$(BINARY_NAME) (x64)"

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

## dist-mac: Package for macOS universal (DMG + ZIP)
dist-mac: build-daemon-mac build-electron
	npx electron-builder --mac --universal
	@cd release && for f in axons-desktop-mac-*.dmg axons-desktop-mac-*.zip; do \
		[ -f "$f" ] || continue; \
		newname="$(echo "$f" | sed 's/axons-desktop-mac-x64/axons-desktop-macos-intel/;s/axons-desktop-mac-arm64/axons-desktop-macos-arm64/')"; \
		[ "$f" = "$newname" ] || mv "$f" "$newname"; \
	done && echo "Renamed macOS artifacts"

## dist-mac-arm64: Package for macOS arm64 only (smaller, Apple Silicon)
dist-mac-arm64: build-daemon-mac-arm64 build-electron
	npx electron-builder --mac --arm64
	@cd release && for f in axons-desktop-mac-arm64.*; do \
		[ -f "$f" ] || continue; \
		newname="$(echo "$f" | sed 's/axons-desktop-mac-arm64/axons-desktop-macos-arm64/')"; \
		[ "$f" = "$newname" ] || mv "$f" "$newname"; \
	done && echo "Renamed macOS arm64 artifacts"

## dist-mac-x64: Package for macOS x64 only (smaller, Intel Mac)
dist-mac-x64: build-daemon-mac-x64 build-electron
	npx electron-builder --mac --x64
	@cd release && for f in axons-desktop-mac-x64.*; do \
		[ -f "$f" ] || continue; \
		newname="$(echo "$f" | sed 's/axons-desktop-mac-x64/axons-desktop-macos-intel/')"; \
		[ "$f" = "$newname" ] || mv "$f" "$newname"; \
	done && echo "Renamed macOS x64 artifacts"

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