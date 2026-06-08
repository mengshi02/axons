/**
 * Type declarations for Electron APIs exposed via contextBridge
 *
 * These types are available when the app runs in Electron desktop mode.
 * window.electronAPI is injected by the preload script's contextBridge.
 */

interface ElectronAPI {
  /** Open a URL in the system's default browser */
  openExternal: (url: string) => Promise<void>;

  /** Navigate main window to a path */
  navigate: (path: string) => Promise<void>;

  /** Check if running in Electron desktop mode */
  isElectron: boolean;
}

interface Window {
  electronAPI?: ElectronAPI;
}