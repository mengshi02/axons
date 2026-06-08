#!/usr/bin/env node

/**
 * Development launcher for macOS.
 *
 * When running `electron .` in dev mode, macOS shows "Electron" as the app name
 * in the menu bar and About dialog — because it reads from the Electron
 * framework's Info.plist (CFBundleName / CFBundleDisplayName).
 *
 * This script patches those plist fields before launching Electron, and restores
 * them on exit. On non-macOS platforms it simply compiles and runs `electron .`.
 */

const { execSync, spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const APP_NAME = 'Axons';
const ROOT_DIR = path.join(__dirname, '..');
const PLISTBUDDY = '/usr/libexec/PlistBuddy';

let originalBundleName = null;
let originalDisplayName = null;
let originalIcnsPath = null;

function getElectronInfoPlist() {
    if (process.platform !== 'darwin') return null;

    try {
        const electronPath = require('electron');
        // electronPath: node_modules/electron/dist/Electron.app/Contents/MacOS/Electron
        const plistPath = path.join(path.dirname(electronPath), '..', 'Info.plist');
        return fs.existsSync(plistPath) ? plistPath : null;
    } catch {
        return null;
    }
}

function readPlistValue(plistPath, key) {
    try {
        return execSync(`"${PLISTBUDDY}" -c "Print :${key}" "${plistPath}"`, { encoding: 'utf8' }).trim();
    } catch {
        return null;
    }
}

function setPlistValue(plistPath, key, value) {
    execSync(`"${PLISTBUDDY}" -c "Set :${key} ${value}" "${plistPath}"`, { stdio: 'ignore' });
}

function patchPlist(plistPath) {
    try {
        originalBundleName = readPlistValue(plistPath, 'CFBundleName');
        originalDisplayName = readPlistValue(plistPath, 'CFBundleDisplayName');

        setPlistValue(plistPath, 'CFBundleName', APP_NAME);
        setPlistValue(plistPath, 'CFBundleDisplayName', APP_NAME);
        console.log(`[dev] Patched Electron Info.plist → ${APP_NAME}`);

        // Replace the Electron app icon so macOS About panel shows our icon.
        // CFBundleIconFile points to "electron.icns" in the Resources dir.
        const resourcesDir = path.join(path.dirname(plistPath), 'Resources');
        const electronIcns = path.join(resourcesDir, 'electron.icns');
        const axonsIcns = path.join(ROOT_DIR, 'build', 'darwin', 'icons.icns');

        if (fs.existsSync(electronIcns) && fs.existsSync(axonsIcns)) {
            // Backup original icon
            const backupPath = electronIcns + '.bak';
            fs.copyFileSync(electronIcns, backupPath);
            originalIcnsPath = backupPath;
            // Replace with our icon
            fs.copyFileSync(axonsIcns, electronIcns);
            console.log('[dev] Patched Electron app icon → Axons');
        }

        return true;
    } catch (e) {
        console.warn('[dev] Failed to patch Info.plist:', e.message);
        return false;
    }
}

function restorePlist(plistPath) {
    try {
        if (originalBundleName) setPlistValue(plistPath, 'CFBundleName', originalBundleName);
        if (originalDisplayName) setPlistValue(plistPath, 'CFBundleDisplayName', originalDisplayName);

        // Restore original electron.icns
        if (originalIcnsPath && fs.existsSync(originalIcnsPath)) {
            const resourcesDir = path.join(path.dirname(plistPath), 'Resources');
            const electronIcns = path.join(resourcesDir, 'electron.icns');
            fs.copyFileSync(originalIcnsPath, electronIcns);
            fs.unlinkSync(originalIcnsPath);
            originalIcnsPath = null;
        }

        console.log('[dev] Restored Electron Info.plist');
    } catch {
        // Ignore restore errors
    }
}

// ── Main ──────────────────────────────────────────────────────────────────────

const plistPath = getElectronInfoPlist();
let patched = false;

if (plistPath) {
    patched = patchPlist(plistPath);
}

// Compile TypeScript
console.log('[dev] Compiling TypeScript...');
execSync('npx tsc -p tsconfig.json', { cwd: ROOT_DIR, stdio: 'inherit' });

// Launch Electron
const child = spawn('electron', ['.'], { cwd: ROOT_DIR, stdio: 'inherit' });

const cleanup = () => {
    if (patched && plistPath) restorePlist(plistPath);
    process.exit(0);
};

child.on('exit', cleanup);
process.on('SIGINT', cleanup);
process.on('SIGTERM', cleanup);