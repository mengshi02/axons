# Axons Makefile
# Build and development commands

# ── Variables ────────────────────────────────────────────────────────────────

BINARY_NAME  := axons
FRONTEND_DIR := ui
DIST_DIR     := dist

VERSION ?= $(shell cat VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
	-X github.com/mengshi02/axons/internal/version.Version=$(VERSION) \
	-X github.com/mengshi02/axons/internal/version.Commit=$(COMMIT) \
	-X github.com/mengshi02/axons/internal/version.Date=$(DATE) \
	-s -w"

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# ── PHONY ────────────────────────────────────────────────────────────────────

.PHONY: all build clean test run daemon dev install deps \
        frontend-deps frontend-build frontend-dev \
        lint fmt check version \
        rebuild desktop-rebuild \
        dist clean-dist \
        build-linux build-darwin build-windows build-all \
        desktop-dev desktop-build desktop-dist desktop-clean desktop-install-deps

# ── Default ──────────────────────────────────────────────────────────────────

all: deps frontend-build build

# ══════════════════════════════════════════════════════════════════════════════
#  Core Build
# ══════════════════════════════════════════════════════════════════════════════

## build: Build binary with embedded frontend for current platform
build: frontend-build
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/axons
	@echo "Built: bin/$(BINARY_NAME)"

## rebuild: Quick rebuild — vite build + go build (skips npm install check)
rebuild:
	@echo ">>> Rebuilding frontend..."
	cd $(FRONTEND_DIR) && npm run build
	@echo ">>> Rebuilding daemon..."
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/axons
	@echo ">>> Done: bin/$(BINARY_NAME)"

## desktop-rebuild: Quick rebuild — vite build + go build daemon to desktop/bin/
desktop-rebuild:
	@echo ">>> Rebuilding frontend..."
	cd $(FRONTEND_DIR) && npm run build
	@echo ">>> Rebuilding daemon for desktop..."
	@mkdir -p desktop/bin
	go build -ldflags="-s -w" -o desktop/bin/axons-daemon ./cmd/axons
	@echo ">>> Done: desktop/bin/axons-daemon"

# ══════════════════════════════════════════════════════════════════════════════
#  Cross-Compilation
# ══════════════════════════════════════════════════════════════════════════════

# Pattern: build-<os>-<arch>
# Example: make build-linux-amd64

## build-linux-amd64: Build for Linux AMD64
build-linux-amd64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-linux-amd64.bin ./cmd/axons

## build-linux-arm64: Build for Linux ARM64
build-linux-arm64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-linux-arm64.bin ./cmd/axons

## build-darwin-amd64: Build for macOS AMD64
build-darwin-amd64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-darwin-amd64.bin ./cmd/axons

## build-darwin-arm64: Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-darwin-arm64.bin ./cmd/axons

## build-windows-amd64: Build for Windows AMD64
build-windows-amd64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-windows-amd64.exe ./cmd/axons

## build-windows-arm64: Build for Windows ARM64
build-windows-arm64: frontend-build
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-windows-arm64.exe ./cmd/axons

## build-linux: Build for Linux (amd64 + arm64)
build-linux: build-linux-amd64 build-linux-arm64

## build-darwin: Build for macOS (amd64 + arm64)
build-darwin: build-darwin-amd64 build-darwin-arm64

## build-windows: Build for Windows (amd64 + arm64)
build-windows: build-windows-amd64 build-windows-arm64

## build-all: Build for all platforms and architectures
build-all: build-linux build-darwin build-windows

# ══════════════════════════════════════════════════════════════════════════════
#  Distribution
# ══════════════════════════════════════════════════════════════════════════════

## dist: Create distribution archives for all platforms
dist: build-all
	@echo "Creating distribution archives..."
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-linux-amd64.tar.gz   $(BINARY_NAME)-web-linux-amd64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-linux-arm64.tar.gz   $(BINARY_NAME)-web-linux-arm64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-darwin-amd64.tar.gz  $(BINARY_NAME)-web-darwin-amd64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-darwin-arm64.tar.gz  $(BINARY_NAME)-web-darwin-arm64.bin
	cd $(DIST_DIR) && zip -q   $(BINARY_NAME)-web-windows-amd64.zip     $(BINARY_NAME)-web-windows-amd64.exe
	cd $(DIST_DIR) && zip -q   $(BINARY_NAME)-web-windows-arm64.zip     $(BINARY_NAME)-web-windows-arm64.exe
	@echo "Distribution archives created in $(DIST_DIR)/"

# ══════════════════════════════════════════════════════════════════════════════
#  Daemon
# ══════════════════════════════════════════════════════════════════════════════

## daemon: Start daemon with TCP listener on port 8080
daemon: build
	./bin/$(BINARY_NAME) daemon start --tcp :8080

## daemon-stop: Stop the daemon
daemon-stop:
	./bin/$(BINARY_NAME) daemon stop

## daemon-ps: Show daemon status
daemon-ps:
	./bin/$(BINARY_NAME) daemon ps

## dev: Development mode — build frontend and run daemon
dev: frontend-build daemon

## run: Build and show help
run: build
	./bin/$(BINARY_NAME) --help

# ══════════════════════════════════════════════════════════════════════════════
#  Frontend
# ══════════════════════════════════════════════════════════════════════════════

## frontend-deps: Install frontend npm dependencies
frontend-deps:
	cd $(FRONTEND_DIR) && npm install

## frontend-build: Build frontend (auto-installs deps if missing)
frontend-build:
	@if [ ! -d "$(FRONTEND_DIR)/node_modules" ]; then \
		echo "node_modules not found, installing..."; \
		cd $(FRONTEND_DIR) && npm install; \
	fi
	cd $(FRONTEND_DIR) && npm run build

## frontend-dev: Start frontend dev server with hot reload
frontend-dev:
	cd $(FRONTEND_DIR) && npm run dev

# ══════════════════════════════════════════════════════════════════════════════
#  Desktop (Electron)
# ══════════════════════════════════════════════════════════════════════════════

## desktop-dev: Run desktop app in development mode
desktop-dev:
	cd desktop && $(MAKE) dev

## desktop-build: Build desktop app for current platform
desktop-build:
	cd desktop && $(MAKE) build

## desktop-dist: Package desktop app for distribution
desktop-dist:
	cd desktop && $(MAKE) dist

## desktop-clean: Clean desktop build artifacts
desktop-clean:
	cd desktop && $(MAKE) clean

## desktop-install-deps: Install desktop npm dependencies
desktop-install-deps:
	cd desktop && npm install

# ══════════════════════════════════════════════════════════════════════════════
#  Testing & Quality
# ══════════════════════════════════════════════════════════════════════════════

## test: Run Go tests
test:
	go test -v $(shell go list ./cmd/... ./internal/... ./pkg/...)

## test-coverage: Run tests with coverage report
test-coverage:
	go test -v -coverprofile=coverage.out $(shell go list ./cmd/... ./internal/... ./pkg/...)
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run golangci-lint
lint:
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

## fmt: Format Go and frontend code
fmt:
	go fmt ./...
	cd $(FRONTEND_DIR) && npx prettier --write "src/**/*.{ts,tsx,css}" || true

## check: Run all checks (lint + test + build)
check: lint test build

# ══════════════════════════════════════════════════════════════════════════════
#  Dependencies
# ══════════════════════════════════════════════════════════════════════════════

## deps: Install Go dependencies
deps:
	go mod download
	go mod tidy

## install: Install binary to GOPATH/bin
install: build
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)

# ══════════════════════════════════════════════════════════════════════════════
#  Docker
# ══════════════════════════════════════════════════════════════════════════════

## docker-build: Build Docker image
docker-build:
	docker build -t axons:$(VERSION) .

## docker-run: Run Docker container
docker-run:
	docker run -p 8080:8080 axons:$(VERSION)

# ══════════════════════════════════════════════════════════════════════════════
#  Cleanup
# ══════════════════════════════════════════════════════════════════════════════

## clean-dist: Remove distribution directory
clean-dist:
	rm -rf $(DIST_DIR)/

## clean: Remove all build artifacts
clean: clean-dist
	go clean
	rm -rf bin/
	rm -rf $(FRONTEND_DIR)/node_modules/
	rm -rf internal/api/static/dist/

# ══════════════════════════════════════════════════════════════════════════════
#  Misc
# ══════════════════════════════════════════════════════════════════════════════

## version: Show current version
version:
	@echo "Version: $(VERSION)"

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' Makefile | column -t -s ':'