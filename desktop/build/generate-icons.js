// Generate icons for macOS and Windows
// Run from web directory: node ../desktop/build/generate-icons.js

const sharp = require('./node_modules/sharp');
const fs = require('fs');
const path = require('path');

const webDir = __dirname + '/../../web';
const svgPath = path.join(webDir, 'public/favicon.svg');
const buildDir = path.join(__dirname);
const windowsDir = path.join(buildDir, 'windows');

async function generateIcons() {
    console.log('Generating icons...');
    console.log('SVG source:', svgPath);

    const svg = fs.readFileSync(svgPath);

    // Generate main icon 1024x1024 (for macOS, electron-builder will convert to .icns)
    const mainIcon = path.join(buildDir, 'appicon.png');
    await sharp(svg).resize(1024, 1024).png().toFile(mainIcon);
    console.log('✅ Created:', mainIcon);

    // Generate Windows icons (various sizes)
    const sizes = [16, 32, 48, 64, 128, 256];

    // Ensure windows directory exists
    if (!fs.existsSync(windowsDir)) {
        fs.mkdirSync(windowsDir, { recursive: true });
    }

    for (const size of sizes) {
        const iconPath = path.join(windowsDir, `icon-${size}.png`);
        await sharp(svg).resize(size, size).png().toFile(iconPath);
        console.log(`✅ Created: icon-${size}.png`);
    }

    console.log('\n✅ All icons generated successfully!');
}

generateIcons().catch(err => {
    console.error('❌ Error:', err);
    process.exit(1);
});