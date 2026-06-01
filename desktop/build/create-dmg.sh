#!/bin/bash
# Create macOS DMG with drag-to-install interface

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BIN_DIR="$PROJECT_DIR/bin"
APP_NAME="Axons"
ARCH_SUFFIX="${ARCH_SUFFIX:-}"
SOURCE_APP="$BIN_DIR/$APP_NAME.app"
if [ -n "$ARCH_SUFFIX" ]; then
    OUTPUT_DMG="$BIN_DIR/$APP_NAME-${ARCH_SUFFIX}.dmg"
else
    OUTPUT_DMG="$BIN_DIR/$APP_NAME.dmg"
fi
TMP_DIR="/tmp/$APP_NAME-dmg-$"

echo "Creating DMG with install interface..."

# Check if app exists
if [ ! -d "$SOURCE_APP" ]; then
    echo "Error: $SOURCE_APP not found"
    echo "Run 'make build-mac' first"
    exit 1
fi

# Clean up
rm -rf "$TMP_DIR"
rm -f "$OUTPUT_DMG"

# Create temp directory
mkdir -p "$TMP_DIR"

# Copy app
echo "Copying app..."
cp -R "$SOURCE_APP" "$TMP_DIR/"

# Create Applications symlink
echo "Creating Applications link..."
ln -s /Applications "$TMP_DIR/Applications"

# Create DMG
echo "Creating DMG..."
hdiutil create -volname "$APP_NAME" -srcfolder "$TMP_DIR" -ov -format UDZO "$OUTPUT_DMG"

# Set window appearance (requires writable DMG)
echo "Setting window appearance..."

# Mount DMG and set appearance
MOUNT_DIR=$(hdiutil attach -readwrite -noverify -noautoopen "$OUTPUT_DMG" 2>/dev/null | grep "/Volumes/$APP_NAME" | awk '{print $3}')

if [ -n "$MOUNT_DIR" ]; then
    # Use AppleScript to set window properties
    osascript <<EOF 2>/dev/null || true
tell application "Finder"
    tell disk "$APP_NAME"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set the bounds of container window to {400, 100, 900, 450}
        set theViewOptions to the icon view options of container window
        set arrangement of theViewOptions to not arranged
        set icon size of theViewOptions to 100
        set position of item "$APP_NAME.app" of container window to {120, 150}
        set position of item "Applications" of container window to {380, 150}
        set background picture of theViewOptions to file ".background:background.png"
        update without registering applications
        delay 2
    end tell
end tell
EOF
    
    # Detach
    hdiutil detach "$MOUNT_DIR" 2>/dev/null || true
fi

# Convert to compressed read-only
echo "Compressing DMG..."
rm -f "${OUTPUT_DMG}.tmp" "${OUTPUT_DMG}.tmp.dmg"
hdiutil convert "$OUTPUT_DMG" -format UDZO -imagekey zlib-level=9 -o "${OUTPUT_DMG}.compressed"
rm -f "$OUTPUT_DMG"
mv "${OUTPUT_DMG}.compressed.dmg" "$OUTPUT_DMG"

# Clean up
rm -rf "$TMP_DIR"

echo ""
echo "✅ DMG created: $OUTPUT_DMG"
ls -lh "$OUTPUT_DMG"