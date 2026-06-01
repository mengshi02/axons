/**
 * Wails v3 Window Drag Runtime (daemon-first architecture)
 *
 * The daemon-first architecture loads the page from the daemon's HTTP server,
 * not Wails' AssetHandler, so the full runtime.js (including drag.ts) is never
 * loaded. Wails only injects runtime.Core() (invoke + environment) after
 * WebViewDidFinishNavigation, but the drag module is missing.
 *
 * This module provides the drag logic:
 * - Listens for mousedown on elements with CSS `--wails-draggable: drag`
 * - On mousemove, calls `window._wails.invoke("wails:drag")` to trigger native drag
 * - Elements with `--wails-draggable: no-drag` are excluded
 */

let canDrag = false;

function isDragTarget(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null;
    if (!el) return false;

    // Walk up the DOM to find a draggable ancestor, stop if we hit no-drag
    let current: HTMLElement | null = el;
    while (current && current !== document.body && current !== document.documentElement) {
        const style = window.getComputedStyle(current);
        const value = style.getPropertyValue('--wails-draggable').trim();
        if (value === 'no-drag') return false;
        if (value === 'drag') return true;
        current = current.parentElement;
    }
    return false;
}

function onMouseDown(event: MouseEvent): void {
    canDrag = false;
    if (event.button !== 0) return;
    canDrag = isDragTarget(event.target);
}

function onMouseMove(_event: MouseEvent): void {
    if (!canDrag) return;
    canDrag = false;
    try {
        (window as any)._wails?.invoke('wails:drag');
    } catch {
        // invoke not available
    }
}

function onMouseUp(): void {
    canDrag = false;
}

export function initWailsDrag(): void {
    // Always register listeners — check for _wails.invoke at call time instead.
    // initWailsDrag() runs before Wails injects runtime.Core(), so we cannot
    // check isDesktop() here; the invoke may become available later.
    window.addEventListener('mousedown', onMouseDown, { capture: true });
    window.addEventListener('mousemove', onMouseMove, { capture: true });
    window.addEventListener('mouseup', onMouseUp, { capture: true });
}