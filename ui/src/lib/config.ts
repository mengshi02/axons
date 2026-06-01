// API base URL configuration.
//
// In desktop mode (Wails v3), the webview loads the daemon's URL directly
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
    // Detect runtime mode: desktop (Wails WebView) vs web (browser)
    const isLocalhost = window.location.hostname === '127.0.0.1'
        || window.location.hostname === 'localhost';
    const hasWails = !!(window as any)._wails;

    // Wails v3 injects window._wails after WebView loads, but there may be a delay.
    // If we're on localhost and _wails isn't set yet, wait briefly and retry.
    if (isLocalhost && !hasWails) {
        await new Promise(r => setTimeout(r, 100));
    }

    runtimeMode = isLocalhost && !!(window as any)._wails ? 'desktop' : 'web';
}