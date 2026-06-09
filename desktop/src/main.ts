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

    let resolved = false;
    const tryResolve = (port: number) => {
      if (resolved) return;
      resolved = true;
      daemonPort = port;
      console.log(`Daemon ready at 127.0.0.1:${port}`);
      resolve(`127.0.0.1:${port}`);
    };

    // Parse port from daemon output and health-check immediately
    const parsePort = (data: Buffer) => {
      const s = data.toString();
      console.log('[daemon]', s.trim());
      const m = s.match(/Web UI available at http:\/\/\[[\d:]+\]:(\d+)/)
        ?? s.match(/Web UI available at http:\/\/[\d.]+:(\d+)/)
        ?? s.match(/"url":"http:\/\/\[[\d:]+\]:(\d+)/)
        ?? s.match(/"url":"http:\/\/[\d.]+:(\d+)/)
        ?? s.match(/listening on\s+(?:127\.0\.0\.1|0\.0\.0\.0):(\d+)/i)
        ?? s.match(/addr=127\.0\.0\.1:(\d+)/)
        ?? s.match(/port=(\d+)/);
      if (m) {
        const port = parseInt(m[1], 10);
        // Port found — verify health then resolve immediately (no polling delay)
        checkHealth(port).then((ok) => { if (ok && !resolved) tryResolve(port); });
      }
    };

    daemonProcess.stdout?.on('data', parsePort);
    daemonProcess.stderr?.on('data', parsePort);

    daemonProcess.on('error', (err) => {
      console.error('Failed to start daemon:', err);
      if (!resolved) reject(err);
    });

    daemonProcess.on('exit', (code) => {
      console.log(`Daemon exited with code ${code}`);
      daemonProcess = null;
    });

    // Safety fallback: poll health endpoint if port parsing missed it
    const pollHealth = async () => {
      for (let i = 0; i < 200; i++) {
        if (resolved) return;
        if (daemonPort > 0) {
          const ready = await checkHealth(daemonPort);
          if (ready) { tryResolve(daemonPort); return; }
        }
        await sleep(50);
      }
      if (!resolved) reject(new Error('Daemon failed to start within timeout'));
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

/** Create the main application window (hidden, no URL loaded yet) */
function createMainWindow(): BrowserWindow {
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

  // Handle main window close
  win.on('closed', () => {
    mainWindow = null;
    app.quit();
  });

  return win;
}

/** Show a splash window while daemon is starting */
function createSplashWindow(): BrowserWindow {
  const splash = new BrowserWindow({
    width: 400,
    height: 300,
    frame: false,
    transparent: true,
    resizable: false,
    center: true,
    show: false,
    alwaysOnTop: true,
  });

  // Inline splash HTML — logo + spinner, no external files needed
  splash.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(SPLASH_HTML)}`);
  splash.once('ready-to-show', () => splash.show());

  return splash;
}

const SPLASH_HTML = `<!DOCTYPE html>
<html>
<head>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    width: 400px; height: 300px;
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    background: #1a1a2e; color: #e0e0e0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    border-radius: 12px; overflow: hidden;
  }
  .logo { font-size: 32px; font-weight: 700; letter-spacing: 2px; margin-bottom: 24px; }
  .spinner {
    width: 28px; height: 28px;
    border: 3px solid rgba(255,255,255,0.15);
    border-top-color: #6c63ff;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
  <div class="logo">Axons</div>
  <div class="spinner"></div>
</body>
</html>`;



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

  // Update project list in the File menu — renderer sends projects on change
  ipcMain.handle('update-projects-menu', (_event, projects: Array<{ id: string; name: string; root_path: string }>) => {
    cachedProjects = projects || [];
    rebuildFileMenu(daemonAddr);
  });
}

// ═══════════════════════════════════════════════════════════════
//  Application Menu
// ═══════════════════════════════════════════════════════════════

/** Cached project list for dynamic File menu updates */
let cachedProjects: Array<{ id: string; name: string; root_path: string }> = [];

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
              mainWindow.webContents.send('open-panel', 'settings');
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
    // File menu — with dynamic project list
    {
      label: 'File',
      submenu: [
        {
          label: 'New Project...',
          accelerator: 'CmdOrCtrl+N',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('menu-action', 'new-project');
            }
          },
        },
        {
          label: 'Open Project...',
          accelerator: 'CmdOrCtrl+O',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('menu-action', 'open-project');
            }
          },
        },
        { type: 'separator' },
        // Dynamic project entries are inserted here by rebuildFileMenu()
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
        {
          label: 'Files',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'fileTree');
            }
          },
        },
        {
          label: 'AI Assistant',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'rightPanel');
            }
          },
        },
        { type: 'separator' },
        {
          label: 'Terminal',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'terminal');
            }
          },
        },
        { type: 'separator' },
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

/**
 * Rebuild the File menu with the current project list.
 * Called whenever the renderer sends an updated project list via IPC.
 */
function rebuildFileMenu(daemonAddr: string) {
  // Build project submenu items
  const projectItems: Electron.MenuItemConstructorOptions[] = cachedProjects.length === 0
    ? [{ label: 'No Projects Yet', enabled: false }]
    : cachedProjects.map((p, idx) => ({
      label: p.name,
      accelerator: idx < 9 ? `CmdOrCtrl+${idx + 1}` as string : undefined,
      click: () => {
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send('menu-action', 'switch-project', p.id);
        }
      },
    }));

  const fileSubmenu: Electron.MenuItemConstructorOptions[] = [
    {
      label: 'New Project...',
      accelerator: 'CmdOrCtrl+N',
      click: () => {
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send('menu-action', 'new-project');
        }
      },
    },
    {
      label: 'Open Project...',
      accelerator: 'CmdOrCtrl+O',
      click: () => {
        if (mainWindow && !mainWindow.isDestroyed()) {
          mainWindow.webContents.send('menu-action', 'open-project');
        }
      },
    },
    { type: 'separator' },
    {
      label: 'Open Recent',
      submenu: projectItems,
    },
    { type: 'separator' },
    { role: 'close' },
  ];

  // Rebuild the entire menu from template
  const appMenu: Electron.MenuItemConstructorOptions[] = [
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
              mainWindow.webContents.send('open-panel', 'settings');
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
    { label: 'File', submenu: fileSubmenu },
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
      ],
    },
    {
      label: 'View',
      submenu: [
        {
          label: 'Files',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'fileTree');
            }
          },
        },
        {
          label: 'AI Assistant',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'rightPanel');
            }
          },
        },
        { type: 'separator' },
        {
          label: 'Terminal',
          click: () => {
            if (mainWindow && !mainWindow.isDestroyed()) {
              mainWindow.webContents.send('open-panel', 'terminal');
            }
          },
        },
        { type: 'separator' },
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
    { role: 'window', submenu: [{ role: 'minimize' }, { role: 'zoom' }, { type: 'separator' }, { role: 'front' }] },
    {
      role: 'help',
      submenu: [
        { label: 'Official Website', click: () => shell.openExternal(WEBSITE_URL) },
        { type: 'separator' },
        { label: 'Report an Issue', click: () => shell.openExternal(ISSUES_URL) },
        { label: 'Release Notes', click: () => shell.openExternal(RELEASES_URL) },
      ],
    },
  ];

  const menu = Menu.buildFromTemplate(appMenu);
  Menu.setApplicationMenu(menu);
}

// ═══════════════════════════════════════════════════════════════
//  App Lifecycle
// ═══════════════════════════════════════════════════════════════

app.whenReady().then(async () => {
  try {
    // Customize About panel
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

    // Show splash window immediately for perceived speed
    const splash = createSplashWindow();

    // Start daemon and create main window in parallel
    const [daemonAddr] = await Promise.all([
      startDaemon(),
      Promise.resolve((mainWindow = createMainWindow())),
    ]);

    // Load daemon URL into main window
    mainWindow.loadURL(`http://${daemonAddr}`);

    // Register IPC handlers
    registerIpcHandlers(daemonAddr);

    // Set application menu
    createAppMenu(daemonAddr);

    // Show main window when ready, then destroy splash
    mainWindow.once('ready-to-show', () => {
      splash.destroy();
      mainWindow?.show();
    });

    // macOS: re-create window when clicking dock icon
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) {
        mainWindow = createMainWindow();
        mainWindow.loadURL(`http://${daemonAddr}`);
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