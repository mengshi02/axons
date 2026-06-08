/**
 * Electron Bridge (daemon-first architecture)
 *
 * Provides IPC helpers that use the Electron contextBridge API
 * (window.electronAPI → ipcRenderer → main process handlers).
 *
 * In daemon-first mode, the Electron BrowserWindow loads the daemon's
 * HTTP server URL. The preload script exposes window.electronAPI
 * with typed methods for desktop-specific operations.
 */

import { getRuntimeMode } from './config';

/**
 * Open a URL in the system's default browser.
 *
 * Desktop mode: uses window.electronAPI.openExternal() →
 *   ipcRenderer.invoke('open-external', url) →
 *   main process shell.openExternal() (native cross-platform API).
 *
 * Web mode: falls back to window.open in a new tab.
 */
export function openExternal(url: string): void {
    if (getRuntimeMode() === 'desktop') {
        try {
            window.electronAPI?.openExternal(url);
            return;
        } catch {
            // electronAPI not available, fall through
        }
    }
    // Web fallback
    window.open(url, '_blank', 'noopener');
}

