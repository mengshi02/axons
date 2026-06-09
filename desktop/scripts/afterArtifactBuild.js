/**
 * electron-builder afterArtifactBuild hook
 *
 * Renames the built artifacts to match our desired naming convention:
 *   axons-desktop-macos-arm64.dmg
 *   axons-desktop-macos-intel.dmg  (x64 -> intel)
 *   axons-desktop-macos-arm64-mac.zip
 *   axons-desktop-macos-intel-mac.zip
 *   axons-desktop-windows-x64.exe
 *   axons-desktop-linux-x64.AppImage
 *
 * electron-builder's artifactName template uses ${arch} which outputs
 * "x64" for Intel builds, but we want "intel" for macOS specifically.
 * Also ${os} outputs "mac" but we want "macos".
 */

const fs = require('fs');
const path = require('path');

function getDesiredName(filename) {
  let newName = filename;

  // macOS: mac -> macos, x64 -> intel
  newName = newName.replace(/axons-desktop-mac-x64/g, 'axons-desktop-macos-intel');
  newName = newName.replace(/axons-desktop-mac-arm64/g, 'axons-desktop-macos-arm64');

  return newName;
}

exports.default = async function afterArtifactBuild(context) {
  const { artifactPaths } = context;

  const renamedPaths = [];

  for (const artifactPath of artifactPaths) {
    const dir = path.dirname(artifactPath);
    const filename = path.basename(artifactPath);
    const newName = getDesiredName(filename);

    if (newName !== filename) {
      const newPath = path.join(dir, newName);
      if (fs.existsSync(artifactPath)) {
        fs.renameSync(artifactPath, newPath);
        console.log(`[afterArtifactBuild] Renamed: ${filename} -> ${newName}`);
      }
      renamedPaths.push(newPath);
    } else {
      renamedPaths.push(artifactPath);
    }
  }

  // Update the artifact paths so electron-builder publishes the correct names
  context.artifactPaths = renamedPaths;
};