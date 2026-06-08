/**
 * Preload script for main BrowserWindow
 *
 * Exposes safe IPC methods via contextBridge.
 */

import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('electronAPI', {
  /** Open a URL in the system's default browser */
  openExternal: (url: string) => ipcRenderer.invoke('open-external', url),

  /** Navigate main window to a path */
  navigate: (path: string) => ipcRenderer.invoke('navigate', path),

  /** Open DevTools for the terminal webview (debugging) */
  openWebviewDevTools: () => ipcRenderer.invoke('open-webview-devtools'),

  /** Check if running in Electron (always true when this preload is loaded) */
  isElectron: true,
});