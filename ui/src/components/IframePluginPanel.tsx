import React, { useEffect, useCallback, useRef, useState } from 'react';
import { useTheme } from '../hooks/useTheme';

import type { PanelDef } from '../lib/panelRegistry';

/** Plugin panel resizable width constants — matches built-in panels */
const MIN_WIDTH = 280;
const MAX_WIDTH = 720;
const DEFAULT_WIDTH = 384; // w-96

/**
 * IframePluginPanel — renders plugin UI inside an isolated iframe.
 *
 * Front-thin-back-thick: the host side only manages iframe lifecycle
 * and theme notification. All data channels go through HTTP/SSE directly
 * to the daemon — no Bridge, no EventBus relay, no API proxying.
 *
 * Width management strategy:
 *   - location === 'right': panel floats on the right edge of the app. It owns its width
 *     (self-managed resize handle on the left edge, drag-left to enlarge).
 *   - other locations (left / left-top / center-bottom / modal): the panel fills its
 *     parent container (w-full / h-full). Width is controlled by the parent column
 *     (e.g. App.tsx's leftPanelWidth), so the panel must NOT render its own resize
 *     handle — doing so causes a double-source-of-truth conflict and visible jank.
 */
interface IframePluginPanelProps {
  def: PanelDef;
  onClose: () => void;
}

export function IframePluginPanel({ def, onClose }: IframePluginPanelProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [iframeReady, setIframeReady] = useState(false);
  const [iframeError, setIframeError] = useState<string | null>(null);

  // Container position semantics
  const isRight = def.location === 'right';
  // Only right-docked panels self-manage width. Other locations defer to parent.
  const selfManagesWidth = isRight;

  // --- Resize state (only used when selfManagesWidth === true) ---
  const [panelWidth, setPanelWidth] = useState(DEFAULT_WIDTH);
  const isResizing = useRef(false);
  const resizeStartX = useRef(0);
  const resizeStartWidth = useRef(DEFAULT_WIDTH);
  // rAF scheduling — coalesce mousemove events into one paint per frame.
  const rafIdRef = useRef<number | null>(null);
  const pendingWidthRef = useRef<number>(DEFAULT_WIDTH);
  // Disable iframe pointer-events while dragging via a global body class
  // (see index.css → body.axons-resizing iframe { pointer-events: none }).
  // Using a class rather than inline style keeps a single mechanism shared
  // with App.tsx's left-panel resize handler.

  useEffect(() => {
    if (!selfManagesWidth) return;

    const flush = () => {
      rafIdRef.current = null;
      setPanelWidth(pendingWidthRef.current);
    };

    const finishDrag = () => {
      if (!isResizing.current) return;
      isResizing.current = false;
      // Ensure final width is committed even if a frame was pending.
      if (rafIdRef.current != null) {
        cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      setPanelWidth(pendingWidthRef.current);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      document.body.classList.remove('axons-resizing');
    };

    const onMouseMove = (e: MouseEvent) => {
      if (!isResizing.current) return;
      // Right-docked panel: handle is on the LEFT edge. Drag LEFT (clientX decreases)
      // should INCREASE width → delta = startX - currentX.
      const delta = resizeStartX.current - e.clientX;
      const next = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, resizeStartWidth.current + delta));
      pendingWidthRef.current = next;
      if (rafIdRef.current == null) {
        rafIdRef.current = requestAnimationFrame(flush);
      }
    };
    const onMouseUp = () => finishDrag();
    // Fallback: abort drag if window loses focus (e.g. Alt+Tab while dragging)
    const onBlur = () => finishDrag();
    // Fallback: abort drag if pointer leaves the document entirely
    const onMouseLeave = (e: MouseEvent) => {
      if (e.relatedTarget === null) finishDrag();
    };
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
    window.addEventListener('blur', onBlur);
    document.addEventListener('mouseleave', onMouseLeave);
    return () => {
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
      window.removeEventListener('blur', onBlur);
      document.removeEventListener('mouseleave', onMouseLeave);
      if (rafIdRef.current != null) {
        cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      // Defensive: if component unmounts mid-drag, clear the global flag.
      document.body.classList.remove('axons-resizing');
    };
  }, [selfManagesWidth]);

  const handleResizeMouseDown = (e: React.MouseEvent) => {
    if (!selfManagesWidth) return;
    e.preventDefault();
    isResizing.current = true;
    resizeStartX.current = e.clientX;
    resizeStartWidth.current = panelWidth;
    pendingWidthRef.current = panelWidth;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    document.body.classList.add('axons-resizing');
  };

  // Listen for iframe postMessage (UI control signals only: ready/close)
  useEffect(() => {
    const handleMessage = (event: MessageEvent) => {
      // Sandbox iframe without allow-same-origin sends events with origin "null"
      if (event.origin !== 'null' && event.origin !== window.location.origin) return;
      const msg = event.data;
      if (msg?.protocol !== 'axons-plugin-iframe' || msg.version !== 1) return;
      if (msg.pluginId !== (def.pluginId || '')) return;

      switch (msg.type) {
        case 'plugin:ready':
          setIframeReady(true);
          break;
        case 'plugin:close':
          onClose();
          break;
      }
    };
    window.addEventListener('message', handleMessage);
    return () => window.removeEventListener('message', handleMessage);
  }, [def.pluginId, onClose]);

  // Theme change notification (only active postMessage from host)
  const { theme } = useTheme();
  useEffect(() => {
    if (!iframeRef.current?.contentWindow || !iframeReady) return; // Only send after iframe is ready
    iframeRef.current.contentWindow.postMessage({
      protocol: 'axons-plugin-iframe',
      version: 1,
      source: 'host',
      pluginId: def.pluginId || '',
      type: 'plugin:theme',
      payload: { theme },
    }, '*');
  }, [def.pluginId, theme, iframeReady]);

  // Send plugin:init after iframe loads — provides pluginId + theme to the iframe adapter
  const handleIframeLoad = useCallback(() => {
    if (!iframeRef.current?.contentWindow) return;
    // Use '*' as targetOrigin because sandbox iframe without allow-same-origin has origin "null"
    iframeRef.current.contentWindow.postMessage({
      protocol: 'axons-plugin-iframe',
      version: 1,
      source: 'host',
      pluginId: def.pluginId || '',
      type: 'plugin:init',
      payload: { pluginId: def.pluginId || '', theme },
    }, '*');
  }, [def.pluginId, theme]);

  // Container styles vary by panel location.
  // - Right-docked: fixed shrink-0 column, owns width, border on left.
  // - Other locations: fill parent (w-full h-full), no own width or resize handle.
  const containerClass = selfManagesWidth
    ? 'h-full shrink-0 bg-surface flex flex-col overflow-hidden relative border-l border-border-subtle'
    : 'w-full h-full bg-surface flex flex-col overflow-hidden relative';
  const containerStyle: React.CSSProperties | undefined = selfManagesWidth
    ? { width: panelWidth }
    : undefined;

  // Lock the iframe src at first mount.
  // Theme is passed as a query parameter so the daemon can SSR the correct
  // <html class="moon-theme|sun-theme"> on the very first paint, avoiding the
  // dark→light flicker that occurred when the template hardcoded moon-theme.
  // We deliberately DO NOT include `theme` in the dependency list — subsequent
  // theme changes are delivered via plugin:theme postMessage, so reloading
  // the iframe on every theme toggle would needlessly destroy plugin state.
  const initialSrcRef = useRef<string>(
    `/v1/plugins/${def.pluginId}/iframe-host?theme=${theme}`
  );

  return (
    <div className={containerClass} style={containerStyle}>
      {/* Resize handle — only rendered for self-managed (right-docked) panels.
          VS Code sash style: 4px hit area, transparent by default, full accent on hover.
          For left/left-top panels, the parent column owns the resize handle. */}
      {selfManagesWidth && (
        <div
          className="absolute left-0 top-0 bottom-0 cursor-col-resize z-10 group"
          style={{ width: '4px' }}
          onMouseDown={handleResizeMouseDown}
        >
          <div className="absolute left-0 top-0 bottom-0 opacity-0 group-hover:opacity-100 transition-opacity bg-accent" style={{ width: '4px' }} />
        </div>
      )}
      {!iframeReady && !iframeError && (
        <div className="flex-1 flex items-center justify-center">
          <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
        </div>
      )}
      {iframeError && (
        <div className="flex-1 flex flex-col items-center justify-center text-text-muted p-4">
          <p className="text-sm text-red-400">Plugin failed to load</p>
          <p className="text-xs mt-1">{iframeError}</p>
          <button
            onClick={() => {
              setIframeError(null);
              setIframeReady(false);
              // Force iframe reload by toggling src
              const iframe = iframeRef.current;
              if (iframe) {
                const src = iframe.src;
                iframe.src = '';
                setTimeout(() => { iframe.src = src; }, 0);
              }
            }}
            className="text-xs text-accent mt-2"
          >
            Retry
          </button>
        </div>
      )}
      <iframe
        ref={iframeRef}
        src={initialSrcRef.current}
        sandbox="allow-scripts allow-forms allow-modals"
        className={iframeReady ? 'w-full h-full border-0' : 'hidden'}
        onLoad={handleIframeLoad}
        onError={() => setIframeError('Plugin failed to load')}
      />
    </div>
  );
}