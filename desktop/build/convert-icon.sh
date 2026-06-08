#!/bin/bash
# Convert SVG icon to PNG for desktop app
# Requires sharp (npm install sharp --save-dev in web/)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$SCRIPT_DIR/../../web"

echo "Generating icons for macOS and Windows..."

cd "$WEB_DIR"

node -e "
const sharp = require('sharp');
const fs = require('fs');

const svg = fs.readFileSync('public/favicon.svg');
const buildDir = '../desktop/build';
const windowsDir = buildDir + '/windows';

(async () => {
  // Main icon for macOS (electron-builder will convert to .icns)
  await sharp(svg).resize(1024, 1024).png().toFile(buildDir + '/appicon.png');
  console.log('Created appicon.png');
  
  // Windows icons (various sizes for .ico)
  const sizes = [16, 32, 48, 64, 128, 256];
  for (const size of sizes) {
    await sharp(svg).resize(size, size).png().toFile(windowsDir + '/icon-' + size + '.png');
    console.log('Created icon-' + size + '.png');
  }
  console.log('All icons generated successfully!');
})();
"

echo "✅ Icons ready for desktop build!"