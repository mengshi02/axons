/**
 * Type declarations for Electron APIs exposed via contextBridge
 */

interface ElectronAPI {
  /** Open a URL in the system's default browser */
  openExternal: (url: string) => Promise<void>;

  /** Send panel event (e.g., terminal:show, terminal:hide) */
  panelEvent: (panelId: string, action: string) => Promise<void>;

  /** Send terminal event to main process */
  terminalEvent: (action: string) => Promise<void>;

  /** Notify main process of terminal height change */
  terminalResize: (height: number) => Promise<void>;

  /** Navigate main window to a path */
  navigate: (path: string) => Promise<void>;

  /** Check if running in Electron desktop mode */
  isElectron: boolean;
}

interface Window {
  electronAPI?: ElectronAPI;
}