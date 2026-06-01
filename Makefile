# Axons Makefile
# Build and development commands for the axons project

# Binary name
BINARY_NAME=axons
BINARY_PATH=bin/$(BINARY_NAME)

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Frontend directory
FRONTEND_DIR=ui

# Build flags
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-X github.com/mengshi02/axons/internal/version.Version=$(VERSION) -X github.com/mengshi02/axons/internal/version.Commit=$(COMMIT) -X github.com/mengshi02/axons/internal/version.Date=$(DATE) -s -w"

# Cross-compilation settings
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
DIST_DIR=dist

.PHONY: all build clean test run daemon dev install deps frontend-build

# Default target
all: deps frontend-build build

## build: Build the binary with embedded frontend for current platform
build: frontend-build
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/axons
	@echo "Built: $(BINARY_PATH)"

## build-linux-amd64: Build for Linux AMD64
build-linux-amd64: frontend-build
	@echo "Building $(BINARY_NAME) for linux/amd64..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-linux-amd64.bin ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-linux-amd64.bin"

## build-linux-arm64: Build for Linux ARM64
build-linux-arm64: frontend-build
	@echo "Building $(BINARY_NAME) for linux/arm64..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-linux-arm64.bin ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-linux-arm64.bin"

## build-darwin-amd64: Build for macOS AMD64
build-darwin-amd64: frontend-build
	@echo "Building $(BINARY_NAME) for darwin/amd64..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-darwin-amd64.bin ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-darwin-amd64.bin"

## build-darwin-arm64: Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64: frontend-build
	@echo "Building $(BINARY_NAME) for darwin/arm64..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-darwin-arm64.bin ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-darwin-arm64.bin"

## build-windows-amd64: Build for Windows AMD64
build-windows-amd64: frontend-build
	@echo "Building $(BINARY_NAME) for windows/amd64..."
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-windows-amd64.exe ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-windows-amd64.exe"

## build-windows-arm64: Build for Windows ARM64
build-windows-arm64: frontend-build
	@echo "Building $(BINARY_NAME) for windows/arm64..."
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-web-windows-arm64.exe ./cmd/axons
	@echo "Built: $(DIST_DIR)/$(BINARY_NAME)-web-windows-arm64.exe"

## build-all: Build for all platforms and architectures
build-all: frontend-build build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64 build-windows-arm64
	@echo "All builds completed in $(DIST_DIR)/"

## build-linux: Build for Linux (amd64 and arm64)
build-linux: build-linux-amd64 build-linux-arm64
	@echo "Linux builds completed."

## build-darwin: Build for macOS (amd64 and arm64)
build-darwin: build-darwin-amd64 build-darwin-arm64
	@echo "macOS builds completed."

## build-windows: Build for Windows (amd64 and arm64)
build-windows: build-windows-amd64 build-windows-arm64
	@echo "Windows builds completed."

## dist: Create distribution archives for all platforms
dist: build-all
	@echo "Creating distribution archives..."
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-linux-amd64.tar.gz $(BINARY_NAME)-web-linux-amd64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-linux-arm64.tar.gz $(BINARY_NAME)-web-linux-arm64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-darwin-amd64.tar.gz $(BINARY_NAME)-web-darwin-amd64.bin
	cd $(DIST_DIR) && tar -czvf $(BINARY_NAME)-web-darwin-arm64.tar.gz $(BINARY_NAME)-web-darwin-arm64.bin
	cd $(DIST_DIR) && zip -q $(BINARY_NAME)-web-windows-amd64.zip $(BINARY_NAME)-web-windows-amd64.exe
	cd $(DIST_DIR) && zip -q $(BINARY_NAME)-web-windows-arm64.zip $(BINARY_NAME)-web-windows-arm64.exe
	@echo "Distribution archives created in $(DIST_DIR)/"

## clean-dist: Clean distribution directory
clean-dist:
	rm -rf $(DIST_DIR)/
	@echo "Distribution directory cleaned."

## clean: Clean build artifacts and distribution
clean: clean-dist
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf bin/
	rm -rf $(FRONTEND_DIR)/node_modules/
	rm -rf internal/api/static/dist/
	@echo "Cleaned."

## test: Run tests
test:
	$(GOTEST) -v $(shell go list ./cmd/... ./internal/... ./pkg/...)

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out $(shell go list ./cmd/... ./internal/... ./pkg/...)
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## run: Run the binary directly
run: build
	./$(BINARY_PATH) --help

## daemon: Start the daemon with TCP listener for web UI (port 8080)
daemon: build
	./$(BINARY_PATH) daemon start --tcp :8080

## daemon-stop: Stop the daemon
daemon-stop:
	./$(BINARY_PATH) daemon stop

## daemon-ps: Show daemon status
daemon-ps:
	./$(BINARY_PATH) daemon ps

## dev: Development mode - build frontend and run daemon
dev: frontend-build daemon

## install: Install the binary to GOPATH/bin
install: build
	cp $(BINARY_PATH) $(GOPATH)/bin/$(BINARY_NAME)
	@echo "Installed: $(GOPATH)/bin/$(BINARY_NAME)"

## deps: Install Go dependencies
deps:
	@echo "Installing Go dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Go dependencies installed."

## frontend-deps: Install frontend dependencies
frontend-deps:
	@echo "Installing frontend dependencies..."
	cd $(FRONTEND_DIR) && npm install
	@echo "Frontend dependencies installed."

## frontend-build: Build the frontend and embed into Go
frontend-build:
	@echo "Building frontend..."
	@if [ ! -d "$(FRONTEND_DIR)/node_modules" ]; then \
		echo "Node modules not found, installing..."; \
		cd $(FRONTEND_DIR) && npm install; \
	fi
	cd $(FRONTEND_DIR) && npm run build
	@echo "Frontend built and embedded."

## frontend-dev: Start frontend development server
frontend-dev:
	cd $(FRONTEND_DIR) && npm run dev

## lint: Run linter
lint:
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

## fmt: Format code
fmt:
	$(GOCMD) fmt ./...
	cd $(FRONTEND_DIR) && npx prettier --write "src/**/*.{ts,tsx,css}" || true

## check: Run all checks (lint, test, build)
check: lint test build
	@echo "All checks passed."

## docker-build: Build Docker image
docker-build:
	docker build -t axons:$(VERSION) .

## docker-run: Run Docker container
docker-run:
	docker run -p 8080:8080 axons:$(VERSION)

## version: Show version
version:
	@echo "Version: $(VERSION)"

# ============================================================================
# Desktop Application Targets (Wails)
# ============================================================================

## desktop-dev: Run desktop app in development mode
desktop-dev:
	@echo "Starting desktop app in development mode..."
	cd desktop && $(MAKE) dev

## desktop-build: Build desktop app for current platform
desktop-build:
	@echo "Building desktop app..."
	cd desktop && $(MAKE) build

## desktop-build-mac: Build macOS desktop app (universal)
desktop-build-mac:
	@echo "Building macOS desktop app..."
	cd desktop && $(MAKE) build-mac

## desktop-build-mac-dmg: Build macOS DMG installer
desktop-build-mac-dmg:
	@echo "Building macOS DMG installer..."
	cd desktop && $(MAKE) build-mac-dmg

## desktop-build-windows: Build Windows desktop app
desktop-build-windows:
	@echo "Building Windows desktop app..."
	cd desktop && $(MAKE) build-windows

## desktop-build-windows-installer: Build Windows installer
desktop-build-windows-installer:
	@echo "Building Windows installer..."
	cd desktop && $(MAKE) build-windows-installer

## desktop-build-all: Build desktop app for all platforms
desktop-build-all:
	@echo "Building desktop app for all platforms..."
	cd desktop && $(MAKE) build-all

## desktop-clean: Clean desktop app build artifacts
desktop-clean:
	@echo "Cleaning desktop app build artifacts..."
	cd desktop && $(MAKE) clean

## desktop-install-wails: Install Wails CLI
desktop-install-wails:
	@echo "Installing Wails CLI..."
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
	@echo "Wails installed. Run 'wails doctor' to check dependencies."

## desktop-doctor: Check Wails dependencies
desktop-doctor:
	cd desktop && $(MAKE) doctor

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'