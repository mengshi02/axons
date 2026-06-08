// API base URL configuration.
//
// In desktop mode (Electron), the BrowserWindow loads the daemon's URL directly
// (http://127.0.0.1:PORT), so all requests are same-origin — no baseURL needed.
// In web mode, baseURL also stays empty (same-origin by default).

let baseURL = '';

export function getBaseURL(): string {
    return baseURL;
}

export function wsBaseURL(): string {
    if (!baseURL) {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${protocol}//${window.location.host}`;
    }
    return baseURL.replace(/^http/, 'ws');
}

// --- Runtime mode detection ---

let runtimeMode: 'desktop' | 'web' = 'web'; // default to web

/** Get the current runtime mode (desktop or web) */
export function getRuntimeMode(): 'desktop' | 'web' {
    return runtimeMode;
}

export async function initConfig(): Promise<void> {
    // Detect runtime mode: desktop (Electron) vs web (browser)
    const isLocalhost = window.location.hostname === '127.0.0.1'
        || window.location.hostname === 'localhost';

    // Electron injects window.electronAPI via preload script's contextBridge.
    // The preload runs before the page loads, so window.electronAPI is
    // available immediately — no retry needed.
    const hasElectron = !!window.electronAPI?.isElectron;

    runtimeMode = isLocalhost && hasElectron ? 'desktop' : 'web';
}