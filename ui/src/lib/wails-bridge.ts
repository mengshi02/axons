/**
 * Wails v3 Bridge (daemon-first architecture)
 *
 * Provides IPC helpers that use the Wails message bridge
 * (window._wails.invoke → WKScriptMessageHandler "external" → RawMessageHandler).
 *
 * In daemon-first mode, Wails only injects runtime.Core() after
 * WebViewDidFinishNavigation, so window._wails.invoke is available
 * but the full runtime.js (/wails/runtime HTTP endpoint) is not.
 * We bypass HTTP entirely and use the native message bridge.
 */

import { getRuntimeMode } from './config';

/**
 * Open a URL in the system's default browser.
 *
 * Desktop mode: sends "open-external:<url>" via _wails.invoke,
 * which is handled by RawMessageHandler in desktop/main.go →
 * wailsApp.Browser.OpenURL() (Wails native cross-platform API).
 *
 * Web mode: falls back to window.open in a new tab.
 */
export function openExternal(url: string): void {
    if (getRuntimeMode() === 'desktop') {
        try {
            (window as any)._wails?.invoke(`open-external:${url}`);
            return;
        } catch {
            // _wails.invoke not available, fall through
        }
    }
    // Web fallback
    window.open(url, '_blank', 'noopener');
}