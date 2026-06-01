#!/bin/bash
# Create Windows installer using NSIS
# This script should run on Windows or with Wine + NSIS

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$PROJECT_DIR/bin"
WINDOWS_DIR="$PROJECT_DIR/build/windows"
APP_NAME="axons"
VERSION="${VERSION:-1.0.0}"

echo "Creating Windows installer..."

# Check if exe exists
if [ ! -f "$BIN_DIR/$APP_NAME.exe" ]; then
    echo "Error: $BIN_DIR/$APP_NAME.exe not found"
    echo "Run 'make build-windows' first"
    exit 1
fi

# Check for NSIS
if ! command -v makensis &> /dev/null; then
    echo "Error: NSIS not found"
    echo "Install NSIS from: https://nsis.sourceforge.io/"
    echo "On macOS: brew install nsis"
    echo "On Windows: choco install nsis"
    exit 1
fi

# Check for icon
if [ ! -f "$WINDOWS_DIR/appicon.ico" ]; then
    echo "Warning: appicon.ico not found, creating from PNG..."
    # Try to convert using ImageMagick if available
    if command -v convert &> /dev/null; then
        convert "$SCRIPT_DIR/appicon.png" "$WINDOWS_DIR/appicon.ico"
    else
        echo "Error: Cannot create .ico file. Please provide appicon.ico manually."
        exit 1
    fi
fi

# Update version in installer script
sed -i.bak "s/APP_VERSION \"[^\"]*\"/APP_VERSION \"$VERSION\"/" "$WINDOWS_DIR/installer.nsi"
rm -f "$WINDOWS_DIR/installer.nsi.bak"

# Sign the executable before packaging (optional)
if [ -n "$SIGN_CERT" ] && [ -n "$SIGN_KEY" ]; then
    echo "🔐 Signing $APP_NAME.exe..."
    signtool sign /f "$SIGN_CERT" /p "$SIGN_KEY" /tr http://timestamp.digicert.com /td sha256 /fd sha256 "$BIN_DIR/$APP_NAME.exe"
    echo "✅ Executable signed."
else
    echo "⚠️  SIGN_CERT/SIGN_KEY not set — skipping code signing."
    echo "   Windows SmartScreen may flag this application."
fi

# Build installer
cd "$WINDOWS_DIR"
makensis installer.nsi

# Sign the installer (optional)
if [ -n "$SIGN_CERT" ] && [ -n "$SIGN_KEY" ]; then
    echo "🔐 Signing installer..."
    signtool sign /f "$SIGN_CERT" /p "$SIGN_KEY" /tr http://timestamp.digicert.com /td sha256 /fd sha256 "$BIN_DIR/Axons-Setup-$VERSION.exe"
    echo "✅ Installer signed."
fi

if [ -f "$BIN_DIR/Axons-Setup-$VERSION.exe" ]; then
    echo ""
    echo "✅ Installer created: $BIN_DIR/Axons-Setup-$VERSION.exe"
    ls -lh "$BIN_DIR/Axons-Setup-$VERSION.exe"
else
    echo "❌ Failed to create installer"
    exit 1
fi