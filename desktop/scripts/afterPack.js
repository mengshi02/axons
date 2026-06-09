/**
 * electron-builder afterPack hook
 *
 * Replaces the axons-daemon binary with the correct architecture-specific
 * version when building macOS universal packages.
 *
 * The Makefile builds two binaries:
 *   bin/axons-daemon-x64   (GOARCH=amd64)
 *   bin/axons-daemon-arm64 (GOARCH=arm64)
 *
 * During packaging, this hook detects which arch is being built and copies
 * the matching binary into the app's Resources directory.
 */

const fs = require('fs');
const path = require('path');

const DAEMON_NAME = 'axons-daemon';

/**
 * Map electron-builder Arch enum values to our binary suffixes.
 * electron-builder v26 uses numeric Arch enum:
 *   Arch.x64 = 1, Arch.arm64 = 3
 */
const ARCH_MAP = {
  1: 'x64',
  3: 'arm64',
};

exports.default = async function afterPack(context) {
  // Only relevant for macOS builds
  if (context.electronPlatformName !== 'darwin') {
    return;
  }

  const arch = context.arch;
  const archSuffix = ARCH_MAP[arch];
  if (!archSuffix) {
    console.log(`[afterPack] Unknown arch "${arch}", skipping daemon replacement`);
    return;
  }

  const appDir = context.appOutDir;
  const resourcesDir = path.join(appDir, context.packager.appInfo.productFilename + '.app', 'Contents', 'Resources');
  const daemonInResources = path.join(resourcesDir, DAEMON_NAME);
  const archSpecificBinary = path.join(__dirname, '..', 'bin', `${DAEMON_NAME}-${archSuffix}`);

  // Check if arch-specific binary exists
  if (!fs.existsSync(archSpecificBinary)) {
    console.log(`[afterPack] Arch-specific binary not found: ${archSpecificBinary}`);
    console.log(`[afterPack] Run 'make build-daemon-mac' to build both architectures`);
    return;
  }

  // Replace the daemon in Resources with the arch-specific one
  if (fs.existsSync(daemonInResources)) {
    fs.copyFileSync(archSpecificBinary, daemonInResources);
    fs.chmodSync(daemonInResources, 0o755);
    console.log(`[afterPack] Replaced ${DAEMON_NAME} with ${archSuffix} binary`);
  } else {
    // Daemon not in Resources yet — copy it directly
    fs.copyFileSync(archSpecificBinary, daemonInResources);
    fs.chmodSync(daemonInResources, 0o755);
    console.log(`[afterPack] Copied ${archSuffix} ${DAEMON_NAME} to Resources`);
  }
};