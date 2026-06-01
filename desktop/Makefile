# Axons Desktop Application Makefile
# Build and development commands for the desktop app (Wails v3)

# Binary name
BINARY_NAME=axons

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Wails v3 command
WAILS?=$(shell which wails3 2>/dev/null || echo "$(HOME)/go/bin/wails3")

.PHONY: all dev build build-mac build-mac-arm64 build-mac-amd64 build-mac-dmg build-mac-intel-dmg build-linux build-linux-appimage build-windows build-windows-installer clean help

# Default target
all: build

## dev: Run in development mode with hot reload
dev:
	@echo "Starting development mode..."
	$(WAILS) dev

## build: Build for current platform
build:
	@echo "Building desktop app for current platform..."
	$(WAILS) build
	@echo "Build complete."

## build-mac: Build for macOS (current architecture)
build-mac:
	@echo "Building for macOS..."
	$(WAILS) build
	@echo "Build complete."

## build-mac-arm64: Build for macOS ARM64 (Apple Silicon)
build-mac-arm64:
	@echo "Building for macOS ARM64..."
	GOARCH=arm64 $(WAILS) build
	@echo "Build complete."

## build-mac-amd64: Build for macOS AMD64 (Intel)
build-mac-amd64:
	@echo "Building for macOS AMD64..."
	GOARCH=amd64 $(WAILS) build
	@echo "Build complete."

## build-mac-dmg: Build macOS DMG installer (current architecture)
build-mac-dmg:
	@echo "Packaging macOS app and creating DMG..."
	$(WAILS) task package
	./build/create-dmg.sh

## build-mac-intel-dmg: Build macOS DMG installer for Intel (AMD64)
build-mac-intel-dmg:
	@echo "Packaging macOS Intel app and creating DMG..."
	ARCH=amd64 $(WAILS) task darwin:package
	ARCH_SUFFIX=intel ./build/create-dmg.sh

## build-windows: Build for Windows
build-windows:
	@echo "Building for Windows..."
	GOOS=windows $(WAILS) task windows:build
	@echo "Build complete."

## build-windows-signed: Build for Windows with code signing
build-windows-signed:
	@echo "Building and signing for Windows..."
	GOOS=windows $(WAILS) task windows:sign
	@echo "Build and signing complete."

## build-windows-installer: Build Windows installer with NSIS
build-windows-installer:
	@echo "Building Windows installer..."
	SIGN_CERT="$(SIGN_CERT)" SIGN_KEY="$(SIGN_KEY)" $(WAILS) task windows:package
	@echo "Build complete."

## build-linux: Build for Linux
build-linux:
	@echo "Building for Linux..."
	$(WAILS) task linux:build
	@echo "Build complete."

## build-linux-appimage: Build Linux AppImage package
build-linux-appimage:
	@echo "Building Linux AppImage..."
	$(WAILS) task linux:package
	@echo "Build complete."

## clean: Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	@echo "Cleaned."

## doctor: Check Wails dependencies
doctor:
	$(WAILS) doctor

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
