/**
 * Axons Desktop — Electron Main Process
 *
 * Architecture:
 *   - Go daemon runs as a child process (HTTP server on 127.0.0.1:PORT)
 *   - Main BrowserWindow loads the daemon's URL (same-origin, no CORS)
 *   - Terminal uses <webview> tag for process isolation (guest process)
 *   - IPC via contextBridge + ipcRenderer
 */

import {
  app,
  BrowserWindow,
  Menu,
  ipcMain,
  shell,
  nativeImage,
} from 'electron';
import * as path from 'path';
import * as http from 'http';
import { spawn, ChildProcess } from 'child_process';

// ═══════════════════════════════════════════════════════════════
//  Constants
// ═══════════════════════════════════════════════════════════════

const APP_NAME = 'Axons';
const WEBSITE_URL = 'https://www.axons.chat';
const ISSUES_URL = 'https://github.com/mengshi02/axons/issues';
const RELEASES_URL = 'https://github.com/mengshi02/axons/releases';

// Override app name — without this, macOS shows "Electron" in the menu bar
// during development mode. In packaged builds, electron-builder sets this
// from productName in package.json.
app.setName(APP_NAME);

let daemonProcess: ChildProcess | null = null;
let daemonPort = 0;
let mainWindow: BrowserWindow | null = null;

// ═══════════════════════════════════════════════════════════════
//  Daemon Management
// ═══════════════════════════════════════════════════════════════

/** Resolve the daemon binary path (dev vs production) */
function getDaemonPath(): string {
  // In production: daemon is bundled as extraResource
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'axons-daemon');
  }
  // In development: look in desktop/bin/
  return path.join(__dirname, '..', 'bin', 'axons-daemon');
}

/** Start the Go daemon as a child process and wait for it to be ready */
async function startDaemon(): Promise<string> {
  const daemonBin = getDaemonPath();

  return new Promise((resolve, reject) => {
    // Spawn daemon: daemon start --fork --tcp :0
    // --fork: run in foreground (don't double-fork)
    // --tcp :0: listen on random port
    // AXONS_DESKTOP_MODE=1: enable desktop mode (plugin CSP, etc.)
    daemonProcess = spawn(daemonBin, ['daemon', 'start', '--fork', '--tcp', ':0'], {
      env: { ...process.env, AXONS_DESKTOP_MODE: '1' },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    daemonProcess.stdout?.on('data', (data: Buffer) => {
      const line = data.toString().trim();
      console.log('[daemon]', line);

      // Daemon prints its listen address when ready (supports IPv4 and IPv6 like [::])
      const addrMatch = line.match(/Web UI available at http:\/\/\[[\d:]+\]:(\d+)/)
        ?? line.match(/Web UI available at http:\/\/[\d.]+:(\d+)/)
        ?? line.match(/"url":"http:\/\/\[[\d:]+\]:(\d+)/)
        ?? line.match(/"url":"http:\/\/[\d.]+:(\d+)/)
        ?? line.match(/listening on\s+(?:127\.0\.0\.1|0\.0\.0\.0):(\d+)/i)
        ?? line.match(/addr=127\.0\.0\.1:(\d+)/)
        ?? line.match(/port=(\d+)/);
      if (addrMatch) {
        daemonPort = parseInt(addrMatch[1], 10);
      }
    });

    daemonProcess.stderr?.on('data', (data: Buffer) => {
      console.error('[daemon:stderr]', data.toString().trim());

      // Also check stderr for port info (supports both text and zap JSON formats, IPv4 and IPv6)
      const addrMatch = data.toString().match(/Web UI available at http:\/\/\[[\d:]+\]:(\d+)/)
        ?? data.toString().match(/Web UI available at http:\/\/[\d.]+:(\d+)/)
        ?? data.toString().match(/"url":"http:\/\/\[[\d:]+\]:(\d+)/)
        ?? data.toString().match(/"url":"http:\/\/[\d.]+:(\d+)/)
        ?? data.toString().match(/listening on\s+(?:127\.0\.0\.1|0\.0\.0\.0):(\d+)/i)
        ?? data.toString().match(/addr=127\.0\.0\.1:(\d+)/)
        ?? data.toString().match(/port=(\d+)/);
      if (addrMatch) {
        daemonPort = parseInt(addrMatch[1], 10);
      }
    });

    daemonProcess.on('error', (err) => {
      console.error('Failed to start daemon:', err);
      reject(err);
    });

    daemonProcess.on('exit', (code) => {
      console.log(`Daemon exited with code ${code}`);
      daemonProcess = null;
    });

    // Poll the daemon health endpoint until ready
    const pollHealth = async () => {
      for (let i = 0; i < 100; i++) {
        if (daemonPort > 0) {
          const ready = await checkHealth(daemonPort);
          if (ready) {
            console.log(`Daemon ready at 127.0.0.1:${daemonPort}`);
            resolve(`127.0.0.1:${daemonPort}`);
            return;
          }
        }
        await sleep(100);
      }
      reject(new Error('Daemon failed to start within timeout'));
    };

    pollHealth();
  });
}

/** Check daemon health endpoint */
function checkHealth(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const req = http.get(`http://127.0.0.1:${port}/health`, (res) => {
      res.resume();
      resolve(res.statusCode === 200);
    });
    req.on('error', () => resolve(false));
    req.setTimeout(2000, () => { req.destroy(); resolve(false); });
  });
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

/** Stop the Go daemon */
function stopDaemon() {
  if (daemonProcess && !daemonProcess.killed) {
    console.log('Stopping daemon...');
    daemonProcess.kill('SIGTERM');
    // Force kill after 5s
    setTimeout(() => {
      if (daemonProcess && !daemonProcess.killed) {
        daemonProcess.kill('SIGKILL');
      }
    }, 5000);
  }
}

// ═══════════════════════════════════════════════════════════════
//  Window Management
// ═══════════════════════════════════════════════════════════════

/** Create the main application window */
function createMainWindow(daemonAddr: string): BrowserWindow {
  // Icon path for Windows/Linux (PNG or ICO).
  // macOS gets the icon from the .app bundle's Info.plist at build time;
  // in dev mode we set the dock icon separately below.
  const iconPath = process.platform === 'win32'
    ? path.join(__dirname, '..', 'build', 'windows', 'icon.ico')
    : path.join(__dirname, '..', 'build', 'appicon.png');
  const appIcon = nativeImage.createFromPath(iconPath);

  const win = new BrowserWindow({
    title: APP_NAME,
    width: 1280,
    height: 800,
    minWidth: 1024,
    minHeight: 600,
    icon: appIcon,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: false,
      webviewTag: true,
    },
    titleBarStyle: 'hiddenInset',
    backgroundColor: '#1a1a2e',
    show: false,
  });

  win.loadURL(`http://${daemonAddr}`);

  // Show window when ready to avoid white flash
  win.once('ready-to-show', () => {
    win.show();
  });

  // Handle main window close
  win.on('closed', () => {
    mainWindow = null;
    app.quit();
  });

  return win;
}

// ═══════════════════════════════════════════════════════════════
//  IPC Handlers
// ═══════════════════════════════════════════════════════════════

function registerIpcHandlers(daemonAddr: string) {
  // Open external URL in system browser
  ipcMain.handle('open-external', (_event, url: string) => {
    if (typeof url === 'string' && url.length > 0) {
      shell.openExternal(url);
    }
  });

  // Navigate main window to a path (for menu shortcuts)
  ipcMain.handle('navigate', (_event, navPath: string) => {
    if (mainWindow && !mainWindow.isDestroyed()) {
      mainWindow.loadURL(`http://${daemonAddr}${navPath}`);
    }
  });

  // Open DevTools for the terminal webview (for debugging)
  ipcMain.handle('open-webview-devtools', (_event) => {
    if (!mainWindow || mainWindow.isDestroyed()) return;
    const webcontents = mainWindow.webContents;
    // Find the webview's webContents by scanning all attached webContents
    const allWebContents = require('electron').webContents.getAllWebContents();
    for (const wc of allWebContents) {
      // webview webContents have a hostWebContents pointing to the main window
      if (wc.hostWebContents === webcontents && wc !== webcontents) {
        wc.openDevTools();
        return;
      }
    }
  });
}

// ═══════════════════════════════════════════════════════════════
//  Application Menu
// ═══════════════════════════════════════════════════════════════

function createAppMenu(daemonAddr: string) {
  const template: Electron.MenuItemConstructorOptions[] = [
    // App menu (macOS)
    {
      label: APP_NAME,
      submenu: [
        { role: 'about', label: `About ${APP_NAME}` },
        { type: 'separator' },
        {
          label: 'Preferences...',
          accelerator: 'CmdOrCtrl+,',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.loadURL(`http://${daemonAddr}/settings`);
            }
          },
        },
        { type: 'separator' },
        { role: 'services' },
        { type: 'separator' },
        { role: 'hide' },
        { role: 'hideOthers' },
        { role: 'unhide' },
        { type: 'separator' },
        { role: 'quit' },
      ],
    },
    // File menu
    {
      label: 'File',
      submenu: [
        {
          label: 'New Project',
          accelerator: 'CmdOrCtrl+N',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.loadURL(`http://${daemonAddr}/new`);
            }
          },
        },
        {
          label: 'Open Project...',
          accelerator: 'CmdOrCtrl+O',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.loadURL(`http://${daemonAddr}/open`);
            }
          },
        },
        { type: 'separator' },
        { role: 'close' },
      ],
    },
    // Edit menu
    {
      label: 'Edit',
      submenu: [
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        { role: 'pasteAndMatchStyle' },
        { role: 'delete' },
        { role: 'selectAll' },
        { type: 'separator' },
      ],
    },
    // View menu
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { role: 'toggleDevTools' },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    // Window menu
    {
      role: 'window',
      submenu: [{ role: 'minimize' }, { role: 'zoom' }, { type: 'separator' }, { role: 'front' }],
    },
    // Help menu
    {
      role: 'help',
      submenu: [
        {
          label: 'Official Website',
          click: () => shell.openExternal(WEBSITE_URL),
        },
        { type: 'separator' },
        {
          label: 'Report an Issue',
          click: () => shell.openExternal(ISSUES_URL),
        },
        {
          label: 'Release Notes',
          click: () => shell.openExternal(RELEASES_URL),
        },
      ],
    },
  ];

  const menu = Menu.buildFromTemplate(template);
  Menu.setApplicationMenu(menu);
}

// ═══════════════════════════════════════════════════════════════
//  App Lifecycle
// ═══════════════════════════════════════════════════════════════

app.whenReady().then(async () => {
  try {
    // Customize About panel
    // Note: iconPath only works on Linux/Windows (not macOS).
    // On macOS, the About panel icon comes from CFBundleIconFile in Info.plist.
    // In dev mode, dev.js patches electron.icns; in packaged builds,
    // electron-builder sets the icon from the build config.
    const aboutOptions: Electron.AboutPanelOptionsOptions = {
      applicationName: APP_NAME,
      applicationVersion: app.getVersion(),
      copyright: `© 2026 Axons Team`,
      credits: 'https://www.axons.chat',
      authors: ['Axons Team'],
      website: WEBSITE_URL,
    };
    if (process.platform !== 'darwin') {
      aboutOptions.iconPath = path.join(__dirname, '..', 'build', 'appicon.png');
    }
    app.setAboutPanelOptions(aboutOptions);

    // macOS: set dock icon for dev mode (packaged builds get it from Info.plist)
    if (process.platform === 'darwin' && app.dock) {
      const dockIcon = nativeImage.createFromPath(
        path.join(__dirname, '..', 'build', 'appicon.png')
      );
      app.dock.setIcon(dockIcon);
    }

    // Start Go daemon
    const daemonAddr = await startDaemon();

    // Create main window
    mainWindow = createMainWindow(daemonAddr);

    // Register IPC handlers
    registerIpcHandlers(daemonAddr);

    // Set application menu
    createAppMenu(daemonAddr);

    // macOS: re-create window when clicking dock icon
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) {
        mainWindow = createMainWindow(daemonAddr);
      } else if (mainWindow && !mainWindow.isDestroyed()) {
        mainWindow.show();
      }
    });
  } catch (err) {
    console.error('Failed to start application:', err);
    app.quit();
  }
});

// Quit when all windows are closed (except on macOS)
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

// Clean up daemon on quit
app.on('before-quit', () => {
  stopDaemon();
});

// Prevent new window creation (open links in system browser)
app.on('web-contents-created', (_event, contents) => {
  contents.setWindowOpenHandler(({ url }) => {
    shell.openExternal(url);
    return { action: 'deny' };
  });
});