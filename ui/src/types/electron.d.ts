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

  /** Update the project list in the native File menu */
  updateProjectsMenu: (projects: Array<{ id: string; name: string; root_path: string }>) => Promise<void>;

  /** Listen for menu actions from the native menu bar */
  onMenuAction: (callback: (action: string, data?: string) => void) => void;

  /** Listen for open-panel events from the native menu bar */
  onOpenPanel: (callback: (panelId: string) => void) => void;
}

interface Window {
  electronAPI?: ElectronAPI;
}