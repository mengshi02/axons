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

  /** Update the project list in the native File menu */
  updateProjectsMenu: (projects: Array<{ id: string; name: string; root_path: string }>) =>
    ipcRenderer.invoke('update-projects-menu', projects),

  /** Listen for menu actions from the native menu bar */
  onMenuAction: (callback: (action: string, data?: string) => void) => {
    ipcRenderer.on('menu-action', (_event, action: string, data?: string) => callback(action, data));
  },

  /** Listen for open-panel events from the native menu bar */
  onOpenPanel: (callback: (panelId: string) => void) => {
    ipcRenderer.on('open-panel', (_event, panelId: string) => callback(panelId));
  },
});