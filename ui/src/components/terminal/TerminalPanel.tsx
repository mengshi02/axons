import React, { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import { wsBaseURL } from '../../lib/config';
import { X, Plus, Maximize2, Minimize2, RotateCcw, Terminal as TerminalIcon, Copy, Clipboard } from 'lucide-react';
import { useTheme } from '../../hooks/useTheme';
import type { PanelComponentProps } from '../../lib/panelRegistry';
import { useAppState } from '../../hooks/useAppState';
import { useTranslation } from 'react-i18next';

// ═══════════════════════════════════════════════════════════════
//  IDE 框架能力：getXtermScaledDimensions
//
//  IDE 的终端 resize 不依赖 fitAddon.proposeDimensions()
//  （该函数读 DOM computedStyle，在快速拖拽时有滞后），
//  而是用 xterm 内部 renderer 的 cell 尺寸 + 容器像素尺寸
//  直接算出精确的 cols/rows。
//
//  来源：vs/workbench/contrib/terminal/browser/xterm/xtermTerminal.ts
//        getXtermScaledDimensions()
// ═══════════════════════════════════════════════════════════════

/** xterm 内部 renderer 尺寸接口（最小所需字段） */
interface XtermCellDimensions {
  css: {
    cell: {
      width: number;
      height: number;
    };
  };
}

/**
 * 获取 xterm renderer 的 cell 像素尺寸。
 * 使用 xterm 内部 _core._renderService.dimensions，与 FitAddon.proposeDimensions()
 * 内部读取的是同一数据源，但此函数不读 DOM computedStyle——
 * 因此在 drag 期间容器 CSS height 已变但 DOM 测量滞后时，仍能得到精确的 cell 尺寸。
 */
function getXtermCellDimensions(xterm: XTerm): XtermCellDimensions | null {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const core = (xterm as any)._core;
  if (!core?._renderService?.dimensions) return null;
  const dims = core._renderService.dimensions;
  if (dims.css.cell.width === 0 || dims.css.cell.height === 0) return null;
  return dims;
}

/**
 * 获取 xterm 容器（.xterm-container）的可用像素尺寸。
 * 返回 { width, height } 或 null（容器不存在时）。
 *
 * 算法与 FitAddon.proposeDimensions() 一致：
 *   - 读 parentElement 的 computed width/height
 *   - 减去 xterm element 的 padding
 *   - 减去滚动条宽度（有 scrollback 时）
 * 但我们直接用 clientWidth/clientHeight 代替 getComputedStyle，
 * 避免浏览器 layout thrashing。
 */
function getTerminalContainerPixelSize(xterm: XTerm): { width: number; height: number } | null {
  const el = xterm.element;
  if (!el || !el.parentElement) return null;

  const parent = el.parentElement;
  const parentWidth = parent.clientWidth;
  const parentHeight = parent.clientHeight;
  if (parentWidth <= 0 || parentHeight <= 0) return null;

  // xterm element 的 padding（FitAddon 也会减去）
  const style = window.getComputedStyle(el);
  const padTop = parseInt(style.paddingTop) || 0;
  const padBottom = parseInt(style.paddingBottom) || 0;
  const padLeft = parseInt(style.paddingLeft) || 0;
  const padRight = parseInt(style.paddingRight) || 0;

  // 滚动条宽度
  // xterm v6 自研滚动条：如果 scrollback=0 则无滚动条；
  // 否则取 scrollbar.width（默认 14px，即 ViewportConstants.DEFAULT_SCROLL_BAR_WIDTH），
  // 但 scrollbar.showScrollbar=false 时宽度为 0。
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const scrollbarOpts = (xterm.options as any).scrollbar;
  const showScrollbar = typeof scrollbarOpts === 'object' ? (scrollbarOpts.showScrollbar !== false) : true;
  const scrollbarWidth = (xterm.options.scrollback === 0 || !showScrollbar)
    ? 0
    : (typeof scrollbarOpts === 'object' && scrollbarOpts.width ? scrollbarOpts.width : 14);

  const availableWidth = parentWidth - padLeft - padRight - scrollbarWidth;
  const availableHeight = parentHeight - padTop - padBottom;

  if (availableWidth <= 0 || availableHeight <= 0) return null;
  return { width: availableWidth, height: availableHeight };
}

/**
 * IDE getXtermScaledDimensions：
 *   基于 xterm renderer 的 cell 尺寸 + 容器可用像素尺寸，
 *   直接算出精确的 { cols, rows }。
 *
 * 关键优势：不需要 fitAddon.proposeDimensions() 读 DOM computedStyle
 * （在快速拖拽时 computedStyle 有滞后），而是用 clientWidth/clientHeight
 * （同步反映 CSS height 变化）+ renderer 的 cell 尺寸（稳定的，不随容器大小变）。
 */
function getXtermScaledDimensions(xterm: XTerm): { cols: number; rows: number } | null {
  const cellDims = getXtermCellDimensions(xterm);
  if (!cellDims) return null;

  const containerSize = getTerminalContainerPixelSize(xterm);
  if (!containerSize) return null;

  const cols = Math.max(2, Math.floor(containerSize.width / cellDims.css.cell.width));
  const rows = Math.max(1, Math.floor(containerSize.height / cellDims.css.cell.height));

  return { cols, rows };
}

interface TerminalSession {
  id: string;
  pid: number;
  cwd: string;
  shell: string;
  created_at: string;
  status: string;
}

interface TerminalTab {
  id: string;
  session: TerminalSession;
  title: string;
}

// Terminal themes
const TERMINAL_THEMES = {
  moon: { // Dark theme
    background: '#0a0a10', // --color-deep
    foreground: '#e4e4ed', // --color-text-primary
    cursor: '#7c3aed', // --color-accent (紫色)
    cursorAccent: '#0a0a10',
    selectionBackground: '#7c3aed40', // 半透明紫色（~25%透明度，暗色主题下可视且不遮挡文字）
    selectionForeground: '#e4e4ed',
    scrollbarSliderBackground: '#79797966', // 滚动条滑块（半透明灰）
    scrollbarSliderHoverBackground: '#79797999', // 悬停
    scrollbarSliderActiveBackground: '#bfbfbf99', // 拖拽/激活
    black: '#06060a',
    red: '#f14c4c',
    green: '#10b981',
    yellow: '#f59e0b',
    blue: '#3b82f6',
    magenta: '#7c3aed',
    cyan: '#14b8a6',
    white: '#e4e4ed',
    brightBlack: '#5a5a70',
    brightRed: '#f14c4c',
    brightGreen: '#23d18b',
    brightYellow: '#f5f543',
    brightBlue: '#60a5fa',
    brightMagenta: '#a78bfa',
    brightCyan: '#22d3ee',
    brightWhite: '#e4e4ed',
  },
  sun: { // Light theme
    background: '#ffffff',
    foreground: '#1a1a2e',
    cursor: '#2563eb', // Blue cursor for light theme
    cursorAccent: '#ffffff',
    selectionBackground: '#2563eb55', // 半透明蓝色（~33%透明度，亮色主题下清晰可见）
    selectionForeground: '#1a1a2e',
    scrollbarSliderBackground: '#64646466', // 滚动条滑块（半透明灰）
    scrollbarSliderHoverBackground: '#64646499', // 悬停
    scrollbarSliderActiveBackground: '#00000099', // 拖拽/激活
    black: '#000000',
    red: '#dc2626',
    green: '#16a34a',
    yellow: '#d97706',
    blue: '#2563eb',
    magenta: '#9333ea',
    cyan: '#0891b2',
    white: '#f5f5f5',
    brightBlack: '#6b7280',
    brightRed: '#ef4444',
    brightGreen: '#22c55e',
    brightYellow: '#f59e0b',
    brightBlue: '#3b82f6',
    brightMagenta: '#a855f7',
    brightCyan: '#06b6d4',
    brightWhite: '#ffffff',
  },
};

export const TerminalPanel = React.memo(function TerminalPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('activitybar');
  const { currentProject } = useAppState();
  const cwd = currentProject?.root_path || '/';
  const projectName = currentProject?.name;
  const { theme } = useTheme();
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [panelHeight, setPanelHeight] = useState(320);
  const isDraggingRef = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(320);
  const pendingHeightRef = useRef(320);
  // drag 结束时设为 true，让 panelHeight useEffect 知道本次 height 变化来自
  // drag mouseup，跳过自己的 fit（由 colsDebounceTimer 统一处理，避免两次 fit）。
  const heightFromDragRef = useRef(false);
  // Always up-to-date ref for activeTabId — safe to read inside event handler
  // closures without stale-closure issues.
  const activeTabIdRef = useRef<string | null>(null);
  activeTabIdRef.current = activeTabId;
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null);

  // Auto-dismiss error after 8 seconds
  useEffect(() => {
    if (!error) return;
    const timer = setTimeout(() => setError(null), 8000);
    return () => clearTimeout(timer);
  }, [error]);

  // Store terminal instances per tab
  const terminalInstancesRef = useRef<Map<string, { xterm: XTerm; fitAddon: FitAddon; ws: WebSocket | null }>>(new Map());

  // Track tabs for cleanup (use ref to avoid triggering cleanup on tabs change)
  const tabsRef = useRef<TerminalTab[]>(tabs);
  tabsRef.current = tabs;

  // Track previous project name to detect changes
  const prevProjectNameRef = useRef<string | undefined>(undefined);

  // Refs for functions used in useEffect before their declaration
  const switchTabRef = useRef<(tabId: string) => void>(() => { });
  const handleAddNewTabRef = useRef<(title?: string) => void>(() => { });

  // Track reconnect attempts per tab (persists across connectWebSocket calls)
  const reconnectAttemptsRef = useRef<Map<string, number>>(new Map());

  // Track last received sequence number per tab (persists across reconnects)
  const lastSeqRef = useRef<Map<string, number>>(new Map());

  // Pending input buffer per tab (persists across reconnects)
  const pendingInputRef = useRef<Map<string, string[]>>(new Map());

  // Cleanup all terminals on unmount
  useEffect(() => {
    return () => {
      // Cleanup all terminal instances and kill backend sessions when component unmounts
      terminalInstancesRef.current.forEach((instance, _tabId) => {
        if (instance.ws) {
          // Send close message to distinguish from accidental disconnect
          if (instance.ws.readyState === WebSocket.OPEN) {
            instance.ws.send(JSON.stringify({ type: 'close' }));
          }
          instance.ws.close(1000, 'user_close');
        }
        if (instance.xterm) {
          instance.xterm.dispose();
        }
      });
      terminalInstancesRef.current.clear();

      // Kill all backend sessions
      tabsRef.current.forEach(tab => {
        if (tab.session?.id) {
          fetch(`/api/terminal/sessions/${tab.session.id}`, { method: 'DELETE' }).catch(console.error);
        }
      });
    };
  }, []);

  // Initialize terminal instance for a tab
  const initTerminal = useCallback((tabId: string) => {
    // Create terminal instance with theme colors
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const xterm = new XTerm({
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
      fontSize: 12,
      lineHeight: 1,
      cursorBlink: true,
      cursorStyle: 'bar',
      theme: TERMINAL_THEMES[theme],
      allowTransparency: true,
      scrollback: 10000,
      convertEol: true, // Convert \n to \r\n for proper line endings
      scrollOnUserInput: true, // Auto-scroll to bottom on user input
      // 启用 xterm 内置自研滚动条
      // 宽度 14px 与 IDE 终端一致，overviewRuler 用于 shell integration 装饰标记
      // xterm 6.0.0 类型定义不含 scrollbar 选项，但运行时支持
      scrollbar: {
        width: 14,
      },
      // resize 路径不能有平滑动画——否则 drag 期间每次 xterm.resize() 都触发 125ms
      // 动画，下一帧 rows 又变，动画被打断重启，导致滚动条持续抖动。
      smoothScrollDuration: 0,
      // WebGL 启用后，对可能与下一格重叠的字形做保守缩放，CJK / Powerline 字符更紧凑
      rescaleOverlappingGlyphs: true,
    } as any);

    // Load addons
    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon((_event, uri) => {
      // Handle file path clicks
      // Support both Unix paths (/path/to/file:10:5) and Windows paths (C:\path\to\file:10:5)
      const unixFileMatch = uri.match(/^([a-zA-Z0-9_\-/.]+):(\d+):?(\d+)?$/);
      const windowsFileMatch = uri.match(/^([A-Za-z]:[\\/][a-zA-Z0-9_\-/\\ .]+):(\d+):?(\d+)?$/);

      const fileMatch = unixFileMatch || windowsFileMatch;
      if (fileMatch) {
        // TODO: Open file in code panel
      }
    });

    xterm.loadAddon(fitAddon);
    xterm.loadAddon(webLinksAddon);
    // Don't open here - will open when tab becomes active

    // Store instance
    const instance = { xterm, fitAddon, ws: null };
    terminalInstancesRef.current.set(tabId, instance);

    // Update active refs
    xtermRef.current = xterm;
    fitAddonRef.current = fitAddon;

    return { xterm, fitAddon };
  }, [theme]);

  // Create new session
  const createSession = useCallback(async (cwdPath: string, cols?: number, rows?: number): Promise<TerminalSession | null> => {
    try {
      const response = await fetch('/api/terminal/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          cwd: cwdPath,
          cols: cols || 120,
          rows: rows || 40,
        }),
      });

      if (!response.ok) {
        let errorMsg = `Failed to create terminal session (${response.status})`;
        try {
          const errorData = await response.json();
          errorMsg = errorData.error || errorData.message || errorMsg;
        } catch {
          const errorText = await response.text().catch(() => '');
          if (errorText) errorMsg = errorText;
        }
        throw new Error(errorMsg);
      }

      setError(null); // Clear previous error on success
      return await response.json();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create session');
      return null;
    }
  }, []);

  // Connect WebSocket
  const connectWebSocket = useCallback((sessionId: string, tabId: string) => {
    const wsUrlStr = `${wsBaseURL()}/api/terminal/sessions/${sessionId}/ws`;

    const ws = new WebSocket(wsUrlStr);
    wsRef.current = ws;

    // Store WebSocket in terminal instances map
    const instance = terminalInstancesRef.current.get(tabId);
    if (instance) {
      instance.ws = ws;
    }

    // Heartbeat to keep connection alive
    let heartbeatInterval: number | null = null;
    let reconnectTimeout: number | null = null;
    const baseReconnectDelay = 1000; // 1 second
    const maxReconnectDelay = 30000; // 30 seconds cap

    // Track last received sequence number for resume on reconnect
    // (use ref to persist across connectWebSocket calls)

    const startHeartbeat = () => {
      // Send ping every 25 seconds to keep connection alive
      heartbeatInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 25000);
    };

    const stopHeartbeat = () => {
      if (heartbeatInterval) {
        clearInterval(heartbeatInterval);
        heartbeatInterval = null;
      }
    };

    const stopReconnect = () => {
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
      }
    };

    // Check if session is still alive on the server
    const checkSessionAlive = async (): Promise<boolean> => {
      try {
        const response = await fetch(`/api/terminal/sessions/${sessionId}`);
        if (!response.ok) return false;
        const data = await response.json();
        return data.status === 'running';
      } catch {
        return false;
      }
    };

    // Attempt reconnection with exponential backoff (no max attempts limit)
    const attemptReconnect = (attempt: number) => {
      const delay = Math.min(baseReconnectDelay * Math.pow(2, attempt), maxReconnectDelay);

      console.log(`[Terminal] WebSocket closed, attempting reconnect #${attempt + 1} in ${delay}ms`);

      reconnectTimeout = window.setTimeout(async () => {
        const currentInstance = terminalInstancesRef.current.get(tabId);
        if (!currentInstance || !currentInstance.xterm) return;

        // Check if session is still alive before reconnecting
        const alive = await checkSessionAlive();
        if (!alive) {
          console.log('[Terminal] Session no longer exists on server');
          if (currentInstance.xterm) {
            currentInstance.xterm.write('\r\n\x1b[31m[Session expired - terminal process has ended]\x1b[0m\r\n');
          }
          stopHeartbeat();
          return;
        }

        // Session is alive, reconnect
        connectWebSocket(sessionId, tabId);
      }, delay);
    };

    ws.onopen = () => {
      setError(null);
      // Reset reconnect attempts on successful connection
      reconnectAttemptsRef.current.set(tabId, 0);
      startHeartbeat();

      // If we have a lastReceivedSeq, this is a reconnection - request replay
      const lastSeq = lastSeqRef.current.get(tabId) || 0;
      if (lastSeq > 0) {
        ws.send(JSON.stringify({ type: 'resume', seq: lastSeq }));
      }

      // Send any pending input that was buffered during disconnection
      const pendingInput = pendingInputRef.current.get(tabId) || [];
      if (pendingInput.length > 0) {
        for (const data of pendingInput) {
          ws.send(JSON.stringify({ type: 'input', data }));
        }
        pendingInputRef.current.set(tabId, []);
      }
    };

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      const currentInstance = terminalInstancesRef.current.get(tabId);

      if (msg.type === 'output' && currentInstance?.xterm) {
        // In alternate buffer (full-screen TUI like top/vim), don't force
        // scrollToBottom — the program manages its own cursor position.
        const isAltBuffer = currentInstance.xterm.buffer.active.type === 'alternate';
        // 阅读位置（粘性滚动条）。这是 axons 之前"滚动条总被还原"的根因。
        const buf = currentInstance.xterm.buffer.active;
        const wasAtBottom = buf.viewportY >= buf.baseY;
        currentInstance.xterm.write(msg.data, () => {
          if (!isAltBuffer && wasAtBottom) currentInstance.xterm.scrollToBottom();
        });
        // Update sequence tracking
        if (msg.seq) lastSeqRef.current.set(tabId, msg.seq);
      } else if (msg.type === 'replay' && currentInstance?.xterm) {
        // Replayed historical output from server
        const isAltBuffer = currentInstance.xterm.buffer.active.type === 'alternate';
        const buf = currentInstance.xterm.buffer.active;
        const wasAtBottom = buf.viewportY >= buf.baseY;
        currentInstance.xterm.write(msg.data, () => {
          if (!isAltBuffer && wasAtBottom) currentInstance.xterm.scrollToBottom();
        });
      } else if (msg.type === 'sync') {
        // Server sent current sequence number after replay
        if (msg.seq) lastSeqRef.current.set(tabId, msg.seq);
      } else if (msg.type === 'exit') {
        currentInstance?.xterm?.write(`\r\n\x1b[33m[Process exited with code ${msg.code}]\x1b[0m\r\n`);
        stopHeartbeat();
        stopReconnect();
      } else if (msg.type === 'error') {
        setError(msg.data);
      } else if (msg.type === 'pong') {
        // Heartbeat response - connection is alive
      }
    };

    ws.onerror = () => {
      // Don't set error here - onclose will handle reconnection
      stopHeartbeat();
    };

    ws.onclose = (event) => {
      stopHeartbeat();

      // Check if this was a user-initiated close (normal closure with code 1000)
      const wasUserInitiated = event.code === 1000 && event.reason === 'user_close';

      if (!wasUserInitiated) {
        // Get current reconnect attempt count from ref
        const currentAttempts = reconnectAttemptsRef.current.get(tabId) || 0;
        attemptReconnect(currentAttempts);
        reconnectAttemptsRef.current.set(tabId, currentAttempts + 1);
      }
    };

    // Handle terminal input
    if (instance?.xterm) {
      instance.xterm.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({
            type: 'input',
            data,
          }));
        } else {
          // Buffer input while disconnected
          const pending = pendingInputRef.current.get(tabId) || [];
          pending.push(data);
          pendingInputRef.current.set(tabId, pending);
        }
      });
    }
  }, []);

  // Connect WebSocket when terminal is opened (xterm.element exists)
  useEffect(() => {
    tabs.forEach(tab => {
      const instance = terminalInstancesRef.current.get(tab.id);
      // Connect WebSocket when xterm is opened but WebSocket not yet connected
      if (instance?.xterm?.element && !instance.ws) {
        connectWebSocket(tab.session.id, tab.id);
      }
    });
  }, [tabs, connectWebSocket]);

  // Create first tab on mount
  useEffect(() => {
    if (tabs.length === 0) {
      // Use project name for first terminal
      handleAddNewTab(projectName);
    }
    // Reset prev project name on mount
    prevProjectNameRef.current = undefined;
  }, []);

  // Auto-create new terminal when project changes
  useEffect(() => {
    // Skip if this is the initial mount (no previous project)
    if (prevProjectNameRef.current === undefined) {
      prevProjectNameRef.current = projectName;
      return;
    }

    // Check if project changed
    if (projectName && projectName !== prevProjectNameRef.current) {
      console.log('[TerminalPanel] Project changed from', prevProjectNameRef.current, 'to', projectName);
      prevProjectNameRef.current = projectName;

      // Check if a tab for this project already exists
      const existingTab = tabs.find(t => t.title === projectName);
      if (existingTab) {
        // Switch to existing tab instead of creating a new one
        switchTabRef.current(existingTab.id);
      } else {
        // Auto-create a new terminal for the new project
        handleAddNewTabRef.current(projectName);
      }
    }
  }, [projectName, tabs]);

  // IDE TerminalInstance.layout(width, height) 策略：
  // resize 完全由框架用精确像素尺寸驱动，不依赖 fitAddon.fit()。
  //
  // IDE 的做法：
  //   - Panel sash / window resize → SplitView.layout(size) →
  //     view.layout(size) → instance.layout({width, height}) →
  //     _evaluateColsAndRows(w, h) → getXtermScaledDimensions →
  //     _resizeDebouncer.resize(cols, rows)
  //   - 没有 ResizeObserver 监听面板容器本身
  //
  // axons 等价做法：
  //   - window.resize → 用 getXtermScaledDimensions 算精确 cols/rows → resize
  //   - ResizeObserver 观察 xterm 的 screen 元素（.xterm-screen）：
  //     该元素只有在 xterm.resize() 真正完成后才改变尺寸，
  //     不会被 panel height 的 CSS 修改误触发
  //   - drag 期间两条路径都不触发（isDraggingRef 守卫 + screen 元素不变）
  useEffect(() => {
    const doResize = () => {
      const ai = terminalInstancesRef.current.get(activeTabId || '');
      if (!ai?.xterm) return;
      const xterm = ai.xterm;
      const distFromBottom = xterm.buffer.active.baseY - xterm.buffer.active.viewportY;
      try {
        // IDE 策略：用精确像素尺寸算 cols/rows，直接 resize
        const dims = getXtermScaledDimensions(xterm);
        if (!dims) return;
        if (dims.cols !== xterm.cols || dims.rows !== xterm.rows) {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const core = (xterm as any)._core;
          core?._renderService?.clear();
          xterm.resize(dims.cols, dims.rows);
        }
        if (ai.ws?.readyState === WebSocket.OPEN) {
          ai.ws.send(JSON.stringify({ type: 'resize', cols: xterm.cols, rows: xterm.rows }));
        }
        if (distFromBottom === 0) xterm.scrollToBottom();
      } catch { /* ignore */ }
    };

    // window.resize：Wails 应用窗口大小变化（drag 期间跳过）
    const onWindowResize = () => {
      if (isDraggingRef.current) return;
      doResize();
    };
    window.addEventListener('resize', onWindowResize);

    // ResizeObserver 观察 xterm screen 元素（不是 panelRef）。
    // .xterm-screen 只在 xterm 内部 renderer 完成 resize 后才改尺寸，
    // 因此 drag 期间改 panel CSS height 不会误触发这里。
    // 用于捕获侧边栏开合等导致终端容器真实宽度变化的场景。
    let resizeTimer: ReturnType<typeof setTimeout> | null = null;
    let observedScreenEl: Element | null = null;
    const resizeObserver = new ResizeObserver(() => {
      if (isDraggingRef.current) return;
      if (resizeTimer) clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => { resizeTimer = null; doResize(); }, 100);
    });

    const startObserving = () => {
      const ai = terminalInstancesRef.current.get(activeTabId || '');
      const screenEl = ai?.xterm?.element?.querySelector('.xterm-screen');
      if (screenEl && screenEl !== observedScreenEl) {
        if (observedScreenEl) resizeObserver.unobserve(observedScreenEl);
        observedScreenEl = screenEl;
        resizeObserver.observe(screenEl);
      }
    };

    // xterm 可能还未 open，轮询直到 screen 元素出现
    const pollTimer = setInterval(startObserving, 200);
    startObserving();

    // ResizeObserver 观察终端内容容器的宽度变化。
    // 当右面板（CodeReferencesPanel）被拖拽变宽/变窄时，
    // 中间列宽度变化，终端面板容器宽度随之变化，
    // 但 .xterm-screen 不会因此触发 ResizeObserver（它只在 xterm.resize() 后才变）。
    // 此观察器检测容器宽度变化，主动触发 xterm 重新计算 cols/rows。
    let lastObservedWidth = 0;
    const widthObserver = new ResizeObserver((entries) => {
      if (isDraggingRef.current) return;
      for (const entry of entries) {
        const width = entry.contentBoxSize?.[0]?.inlineSize
          ?? entry.contentRect.width;
        // 仅在宽度实际变化时触发，避免高度变化（如终端自身拖拽）误触发
        if (width !== lastObservedWidth) {
          lastObservedWidth = width;
          if (resizeTimer) clearTimeout(resizeTimer);
          resizeTimer = setTimeout(() => { resizeTimer = null; doResize(); }, 100);
        }
      }
    });
    if (contentRef.current) {
      lastObservedWidth = contentRef.current.clientWidth;
      widthObserver.observe(contentRef.current);
    }

    return () => {
      window.removeEventListener('resize', onWindowResize);
      resizeObserver.disconnect();
      widthObserver.disconnect();
      clearInterval(pollTimer);
      if (resizeTimer) { clearTimeout(resizeTimer); resizeTimer = null; }
    };
  }, [activeTabId]);

  // IDE TerminalInstance.layout(width, height) 模式：
  // 用精确像素尺寸直接算出 cols/rows，调 xterm.resize()，
  // 而不是 fitAddon.fit()（内部读 DOM computedStyle，有滞后）。
  //
  // 触发场景：isFullscreen 切换、panelHeight state 变化（非 drag 来源）。
  useEffect(() => {
    // 如果本次 panelHeight 变化来自 drag mouseup，跳过此处的 fit。
    // drag 结束后 cols resize 由 handleMouseUp 内的 flushResize 统一处理，
    // 避免两个路径几乎同时调用 xterm.resize() 导致滚动条抖动。
    if (heightFromDragRef.current) {
      heightFromDragRef.current = false;
      return;
    }
    const activeInstance = terminalInstancesRef.current.get(activeTabId || '');
    if (!activeInstance?.xterm) return;

    const xterm = activeInstance.xterm;
    const distFromBottom = xterm.buffer.active.baseY - xterm.buffer.active.viewportY;

    setTimeout(() => {
      try {
        // IDE 策略：先算精确 cols/rows，再调 xterm.resize()
        const dims = getXtermScaledDimensions(xterm);
        if (!dims) return;

        // 仅在尺寸真的变化时才 resize（避免无谓的 reflow）
        if (dims.cols !== xterm.cols || dims.rows !== xterm.rows) {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const core = (xterm as any)._core;
          core?._renderService?.clear();
          xterm.resize(dims.cols, dims.rows);
        }

        // Send resize to backend
        if (activeInstance.ws?.readyState === WebSocket.OPEN) {
          activeInstance.ws.send(JSON.stringify({
            type: 'resize',
            cols: xterm.cols,
            rows: xterm.rows,
          }));
        }

        // 只有用户 fit 前就贴底，才在 fit 后恢复到底部
        if (distFromBottom === 0) {
          xterm.scrollToBottom();
        }
      } catch (e) {
        // Ignore resize errors
      }
    }, 100);
  }, [isFullscreen, panelHeight, activeTabId]);

  // Update terminal theme when theme changes
  useEffect(() => {
    terminalInstancesRef.current.forEach((instance) => {
      if (instance.xterm) {
        instance.xterm.options.theme = TERMINAL_THEMES[theme];
      }
    });
  }, [theme]);

  // Listen for "Open in Terminal" from FileTreePanel
  useEffect(() => {
    const handler = (e: Event) => {
      const { dir } = (e as CustomEvent<{ dir: string }>).detail;
      if (!dir) return;
      // Find the active tab's WebSocket and send a cd command
      const instance = terminalInstancesRef.current.get(activeTabId || '');
      if (instance?.ws && instance.ws.readyState === WebSocket.OPEN) {
        instance.ws.send(JSON.stringify({ type: 'input', data: `cd "${dir}"\n` }));
      }
    };
    window.addEventListener('filetree:open-in-terminal', handler);
    return () => window.removeEventListener('filetree:open-in-terminal', handler);
  }, [activeTabId]);

  // Handle panel drag resize.
  //
  //   - buffer.normal.length < 200（小 buffer）：立即 resize cols+rows
  //   - buffer.normal.length >= 200（大 buffer）：
  //       rows 立即更新（cheap，只扩展 viewport 到 scrollback，不触发 reflow）
  //       cols debounce 100ms（expensive，触发全文本 reflow）
  //   - 不可见时：通过 requestIdleCallback 延迟执行
  //   - flush()：mouseup 时强制立即同步最终 cols+rows
  //
  // smoothScrollDuration 已设为 0，resize 路径不再触发平滑滚动动画。

  useEffect(() => {
    const START_DEBOUNCING_THRESHOLD = 200; // buffer.normal.length 阈值
    const DEBOUNCE_RESIZE_X_DELAY = 100;    // cols debounce 延迟 ms

    let latestCols = 0;
    let latestRows = 0;
    let resizeXJobId: number | null = null;   // requestIdleCallback id
    let resizeYJobId: number | null = null;   // requestIdleCallback id
    let debounceXTimer: ReturnType<typeof setTimeout> | null = null;

    const sendResize = (ws: WebSocket | null, xterm: XTerm) => {
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols: xterm.cols, rows: xterm.rows }));
      }
    };

    // 移植 _resizeBothCallback
    // resizeBoth 用于 flush 和小 buffer 立即路径——完整 resize 后清除 CSS 拉伸
    const resizeBoth = (cols: number, rows: number) => {
      const ai = terminalInstancesRef.current.get(activeTabIdRef.current || '');
      if (!ai?.xterm) return;
      const buf = ai.xterm.buffer.active;
      const distFromBottom = buf.baseY - buf.viewportY;
      ai.xterm.resize(cols, rows);
      sendResize(ai.ws, ai.xterm);
      if (distFromBottom === 0) ai.xterm.scrollToBottom();
    };

    // 移植 _resizeXCallback
    const resizeX = (cols: number) => {
      const ai = terminalInstancesRef.current.get(activeTabIdRef.current || '');
      if (!ai?.xterm) return;
      const buf = ai.xterm.buffer.active;
      const distFromBottom = buf.baseY - buf.viewportY;
      ai.xterm.resize(cols, ai.xterm.rows);
      sendResize(ai.ws, ai.xterm);
      if (distFromBottom === 0) ai.xterm.scrollToBottom();
    };

    // 移植 _resizeYCallback
    // IDE 策略：rows resize 时用 CSS 拉伸 .xterm-viewport 和 .xterm-screen
    // 填满容器剩余像素（不足一行的高度），使滚动条自然拉伸不抖动。
    const resizeY = (rows: number) => {
      const ai = terminalInstancesRef.current.get(activeTabIdRef.current || '');
      if (!ai?.xterm) return;
      ai.xterm.resize(ai.xterm.cols, rows);
      sendResize(ai.ws, ai.xterm);
    };

    // 移植 TerminalResizeDebouncer.resize(cols, rows, immediate)
    const resizeDebounced = (cols: number, rows: number, immediate: boolean) => {
      latestCols = cols;
      latestRows = rows;

      const ai = terminalInstancesRef.current.get(activeTabIdRef.current || '');
      if (!ai?.xterm) return;
      const bufferLength = ai.xterm.buffer.normal.length;

      // immediate 或 buffer 较小：立即 resizeBoth，取消所有 pending 任务
      if (immediate || bufferLength < START_DEBOUNCING_THRESHOLD) {
        if (resizeXJobId !== null) { cancelIdleCallback(resizeXJobId); resizeXJobId = null; }
        if (resizeYJobId !== null) { cancelIdleCallback(resizeYJobId); resizeYJobId = null; }
        if (debounceXTimer !== null) { clearTimeout(debounceXTimer); debounceXTimer = null; }
        resizeBoth(cols, rows);
        return;
      }

      const xtermEl = ai.xterm.element;
      const isVisible = xtermEl ? xtermEl.offsetParent !== null : true;
      if (!isVisible) {
        if (resizeXJobId === null) {
          resizeXJobId = requestIdleCallback(() => {
            resizeXJobId = null;
            resizeX(latestCols);
          });
        }
        if (resizeYJobId === null) {
          resizeYJobId = requestIdleCallback(() => {
            resizeYJobId = null;
            resizeY(latestRows);
          });
        }
        return;
      }

      // 正常路径：rows 立即，cols debounce 100ms
      resizeY(rows);
      latestCols = cols;
      if (debounceXTimer !== null) clearTimeout(debounceXTimer);
      debounceXTimer = setTimeout(() => {
        debounceXTimer = null;
        resizeX(latestCols);
      }, DEBOUNCE_RESIZE_X_DELAY);
    };

    // 移植 TerminalResizeDebouncer.flush()：mouseup 时强制立即同步
    const flushResize = () => {
      if (debounceXTimer !== null) {
        clearTimeout(debounceXTimer);
        debounceXTimer = null;
        resizeBoth(latestCols, latestRows);
      }
      if (resizeXJobId !== null) { cancelIdleCallback(resizeXJobId); resizeXJobId = null; }
      if (resizeYJobId !== null) { cancelIdleCallback(resizeYJobId); resizeYJobId = null; }
    };

    // IDE getXtermScaledDimensions 策略：
    // 用 xterm renderer 的 cell 尺寸 + 容器像素尺寸直接算出 cols/rows，
    // 而不是 fitAddon.proposeDimensions() 读 DOM computedStyle
    // （快速拖拽时 computedStyle 有滞后，导致底部行溢出/消失）。
    const getTargetDimensions = (): { cols: number; rows: number } | null => {
      const ai = terminalInstancesRef.current.get(activeTabIdRef.current || '');
      if (!ai?.xterm) return null;
      return getXtermScaledDimensions(ai.xterm);
    };

    const handleMouseMove = (e: MouseEvent) => {
      if (!isDraggingRef.current || !panelRef.current) return;

      const deltaY = dragStartY.current - e.clientY;
      const newHeight = Math.min(
        Math.max(dragStartHeight.current + deltaY, 200),
        window.innerHeight - 100,
      );
      pendingHeightRef.current = newHeight;
      // 同步设置 CSS height（立即生效）
      panelRef.current.style.height = `${newHeight}px`;

      // ── 同步布局（SplitView 模式）──
      // CSS height 已改，getTargetDimensions() 内部读 clientHeight
      // 会同步拿到新值，然后立即 xterm.resize()，使
      // Viewport._sync() 在同一 JS task 内用一致的
      // height/scrollHeight 更新 scrollbar，消除抖动。
      // NOTE: 这里不能用 rAF 延迟——rAF 会让 CSS height 写入和
      // xterm.resize() 之间出现一帧空隙，导致 scrollbar 闪动。
      const dims = getTargetDimensions();
      if (!dims) return;
      // immediate=false：走 TerminalResizeDebouncer 正常路径
      // （rows 立即，cols debounce 100ms）
      resizeDebounced(dims.cols, dims.rows, false);
    };

    const finishDrag = () => {
      if (!isDraggingRef.current) return;
      isDraggingRef.current = false;
      // Ensure the final height is painted
      if (panelRef.current) {
        panelRef.current.style.height = `${pendingHeightRef.current}px`;
      }
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      document.body.classList.remove('axons-resizing');
      if (panelRef.current) panelRef.current.style.willChange = '';

      // 移植 sash.onDidEnd → resizeDebouncer.flush()
      flushResize();

      // 同步 React state（触发 panelHeight useEffect，但我们标记跳过其 fit）
      heightFromDragRef.current = true;
      setPanelHeight(pendingHeightRef.current);
    };

    const handleMouseUp = () => finishDrag();
    // Fallback: abort drag if window loses focus (e.g. Alt+Tab while dragging)
    const handleBlur = () => finishDrag();
    // Fallback: abort drag if pointer leaves the document entirely
    const handleMouseLeave = (e: MouseEvent) => {
      if (e.relatedTarget === null) finishDrag();
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    window.addEventListener('blur', handleBlur);
    document.addEventListener('mouseleave', handleMouseLeave);
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      window.removeEventListener('blur', handleBlur);
      document.removeEventListener('mouseleave', handleMouseLeave);
      if (debounceXTimer !== null) clearTimeout(debounceXTimer);
      if (resizeXJobId !== null) cancelIdleCallback(resizeXJobId);
      if (resizeYJobId !== null) cancelIdleCallback(resizeYJobId);
    };
  }, []); // registered once — reads activeTabId via ref, no stale closure

  // Add new tab
  const handleAddNewTab = useCallback(async (title?: string) => {
    // Calculate terminal size based on container
    const containerWidth = panelRef.current?.clientWidth || 800;
    const containerHeight = panelRef.current?.clientHeight || 400;
    // Estimate cols and rows based on font size (12px)
    const cols = Math.floor((containerWidth - 16) / 8); // 8px per char (approximate)
    const rows = Math.floor((containerHeight - 40) / 16); // 16px per line (approximate)

    const session = await createSession(cwd, cols, rows);
    if (!session) return;

    const tabId = `tab-${Date.now()}`;
    const newTab: TerminalTab = {
      id: tabId,
      session,
      title: title || `Terminal ${tabs.length + 1}`,
    };

    // Initialize terminal instance
    initTerminal(tabId);

    setTabs(prev => [...prev, newTab]);
    setActiveTabId(tabId);
  }, [cwd, createSession, initTerminal, tabs.length]);

  // Keep ref in sync
  handleAddNewTabRef.current = handleAddNewTab;

  // Close tab
  const closeTab = useCallback(async (tabId: string, e?: React.MouseEvent) => {
    e?.stopPropagation();

    const tab = tabs.find(t => t.id === tabId);
    if (tab) {
      // Close WebSocket connection
      const instance = terminalInstancesRef.current.get(tabId);
      if (instance?.ws) {
        // Send close message to backend to distinguish from accidental disconnect
        if (instance.ws.readyState === WebSocket.OPEN) {
          instance.ws.send(JSON.stringify({ type: 'close' }));
        }
        instance.ws.close(1000, 'user_close'); // Use 1000 for normal closure
      }

      // Dispose terminal instance
      if (instance?.xterm) {
        instance.xterm.dispose();
      }

      // Remove from map
      terminalInstancesRef.current.delete(tabId);

      // Kill session on backend (with error handling)
      try {
        await fetch(`/api/terminal/sessions/${tab.session.id}`, { method: 'DELETE' });
      } catch (err) {
        console.error('Failed to kill terminal session:', err);
      }
    }

    setTabs(prev => prev.filter(t => t.id !== tabId));

    if (activeTabId === tabId) {
      const remainingTabs = tabs.filter(t => t.id !== tabId);
      if (remainingTabs.length > 0) {
        setActiveTabId(remainingTabs[0].id);
        // Switch to the remaining tab
        const remainingInstance = terminalInstancesRef.current.get(remainingTabs[0].id);
        if (remainingInstance) {
          xtermRef.current = remainingInstance.xterm;
          fitAddonRef.current = remainingInstance.fitAddon;
          wsRef.current = remainingInstance.ws;
        }
      } else {
        setActiveTabId(null);
      }
    }
  }, [tabs, activeTabId]);

  // Switch tab
  const switchTab = useCallback((tabId: string) => {
    setActiveTabId(tabId);

    // Switch to the tab's terminal instance
    const instance = terminalInstancesRef.current.get(tabId);
    if (instance) {
      xtermRef.current = instance.xterm;
      fitAddonRef.current = instance.fitAddon;
      wsRef.current = instance.ws;

      // Fit the terminal
      setTimeout(() => {
        instance.fitAddon.fit();
      }, 0);
    }
  }, []);

  // Keep ref in sync
  switchTabRef.current = switchTab;

  // Clear terminal
  const handleClear = useCallback(() => {
    if (xtermRef.current) {
      xtermRef.current.clear();
    }
  }, []);

  // Copy selected text
  const handleCopy = useCallback(() => {
    if (xtermRef.current) {
      const selection = xtermRef.current.getSelection();
      if (selection) {
        navigator.clipboard.writeText(selection);
      }
    }
    setContextMenu(null);
  }, []);

  // Paste from clipboard
  const handlePaste = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(JSON.stringify({
          type: 'input',
          data: text,
        }));
        if (xtermRef.current) {
          xtermRef.current.write(text);
        }
      }
    } catch (err) {
      console.error('Failed to paste:', err);
    }
    setContextMenu(null);
  }, []);

  // Handle context menu
  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setContextMenu({ x: e.clientX, y: e.clientY });
  }, []);

  // Close context menu on click outside
  useEffect(() => {
    const handleClick = () => setContextMenu(null);
    if (contextMenu) {
      document.addEventListener('click', handleClick);
      return () => document.removeEventListener('click', handleClick);
    }
  }, [contextMenu]);

  if (error && tabs.length === 0) {
    return (
      <div className="bg-deep text-text-primary p-4">
        <div className="flex items-center justify-between mb-2">
          <span className="text-red-400">Error: {error}</span>
          <button onClick={onClose} className="hover:bg-hover p-1 rounded transition-colors">
            <X size={18} />
          </button>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={panelRef}
      className={`flex flex-col ${isFullscreen ? 'fixed inset-0 z-50' : 'absolute bottom-0 left-0 right-0 z-10'}`}
      style={{
        height: isFullscreen ? '100vh' : panelHeight,
        minHeight: isFullscreen ? undefined : 200,
        backgroundColor: TERMINAL_THEMES[theme].background,
        // Stabilize the GPU compositing layer from the very first frame:
        // promoting the panel to its own layer up front (via translateZ +
        // isolation + contain) prevents the browser from creating a *new*
        // layer boundary mid-mount against the WebGL canvas above, which
        // was the source of the flashing black band at the sash position.
        // Because the layer exists from frame 0 with a solid background,
        // there is no "未绘制黑色像素" leaking through at the seam.
        transform: isFullscreen ? undefined : 'translateZ(0)',
        contain: isFullscreen ? undefined : 'layout paint',
        isolation: isFullscreen ? undefined : 'isolate',
        // NOTE: no `border-top` here — the visible sash line is rendered as
        // an *inside* 1px div (see below). Drawing it as a border on this
        // wrapper meant it lived on the layer boundary, where the compositor
        // could briefly paint a much thicker dark band on first paint.
      }}
    >
      {/* VS Code sash style: 4px transparent hit area, full 4px accent bar on hover.
          The hit area sits at the top of the panel (inside the panel boundary,
          so it never overlaps the WebGL canvas above and cannot flash black on
          first paint). On hover, the entire 4px bar lights up in accent color. */}
      {!isFullscreen && (
        <div
          className="cursor-row-resize group"
          style={{
            position: 'absolute',
            left: 0,
            right: 0,
            top: 0,
            height: '4px',
            zIndex: 50,
            background: 'transparent',
          }}
          onMouseDown={(e) => {
            e.preventDefault();
            isDraggingRef.current = true;
            dragStartY.current = e.clientY;
            dragStartHeight.current = panelHeight;
            pendingHeightRef.current = panelHeight;
            document.body.style.userSelect = 'none';
            document.body.style.cursor = 'row-resize';
            document.body.classList.add('axons-resizing');
            // `will-change: height` is added during drag only as a hint;
            // the panel is already a stable compositing layer thanks to
            // translateZ(0) on the wrapper, so toggling will-change no
            // longer introduces a new layer boundary mid-interaction.
            if (panelRef.current) panelRef.current.style.willChange = 'height';
          }}
        >
          {/* Hover accent bar — full 4px height, accent color on hover. */}
          <div
            className="absolute left-0 right-0 top-0 h-full opacity-0 group-hover:opacity-100 transition-opacity bg-accent"
            style={{ pointerEvents: 'none' }}
          />
        </div>
      )}

      {/* Error toast (auto-dismiss) */}
      {error && tabs.length > 0 && (
        <div className="flex items-center justify-between px-4 py-1.5 bg-red-500/10 border-b border-red-500/20 text-sm flex-shrink-0">
          <span className="text-red-400">{error}</span>
          <button onClick={() => setError(null)} className="text-red-400 hover:text-red-300 p-0.5 rounded transition-colors">
            <X size={14} />
          </button>
        </div>
      )}

      {/* Header with tabs */}
      <div className="flex items-center justify-between px-4 py-1 bg-surface border-b border-border-subtle flex-shrink-0">
        <div className="flex items-center gap-2 flex-1 overflow-x-auto">
          {/* Tabs */}
          {tabs.map(tab => (
            <div
              key={tab.id}
              onClick={() => switchTab(tab.id)}
              className={`flex items-center gap-2 px-3 py-1 rounded-t text-sm cursor-pointer transition-colors ${activeTabId === tab.id
                ? 'bg-elevated text-text-primary'
                : 'bg-surface text-text-muted hover:text-text-secondary'
                }`}
            >
              <TerminalIcon size={14} />
              <span className="max-w-[120px] truncate">{tab.title}</span>
              <button
                onClick={(e) => closeTab(tab.id, e)}
                className="hover:bg-hover rounded p-0.5 transition-colors"
              >
                <X size={12} />
              </button>
            </div>
          ))}

          {/* Add tab button */}
          <button
            onClick={() => handleAddNewTab()}
            className="p-1.5 hover:bg-hover rounded text-text-muted hover:text-text-primary transition-colors"
            title={t('terminal.newTerminal')}
          >
            <Plus size={16} />
          </button>
        </div>

        {/* Status & controls */}
        <div className="flex items-center gap-2 ml-2">
          <button
            onClick={handleClear}
            className="p-1.5 hover:bg-hover rounded text-text-muted hover:text-text-primary transition-colors"
            title={t('terminal.clear')}
          >
            <RotateCcw size={16} />
          </button>

          <button
            onClick={() => setIsFullscreen(!isFullscreen)}
            className="p-1.5 hover:bg-hover rounded text-text-muted hover:text-text-primary transition-colors"
            title={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
          >
            {isFullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>

          <button
            onClick={onClose}
            className="p-1.5 hover:bg-hover rounded text-text-muted hover:text-text-primary transition-colors"
            title={t('common:action.close')}
          >
            <X size={16} />
          </button>
        </div>
      </div>

      {/* Terminal container - one per tab */}
      <div 
        ref={contentRef}
        className="flex-1 w-full px-2 relative overflow-hidden"
        onContextMenu={handleContextMenu}
      >
        {tabs.map(tab => (
          <div
            key={tab.id}
            ref={el => {
              if (el && tab.id === activeTabId) {
                const instance = terminalInstancesRef.current.get(tab.id);
                if (instance && !instance.xterm.element) {
                  // Open terminal in this container
                  instance.xterm.open(el);

                  // Fit after a short delay to ensure proper sizing
                  setTimeout(() => {
                    try {
                      instance.fitAddon.fit();
                      instance.xterm.refresh(0, instance.xterm.rows - 1);
                      instance.xterm.scrollToBottom();
                    } catch (e) {
                      // Ignore fit errors
                    }
                  }, 50);
                } else if (instance && instance.xterm.element) {
                  // Already opened (e.g. switching tabs), just fit.
                  // Do NOT unconditionally scrollToBottom here — the user may
                  // have scrolled up to read history, and switching tabs should
                  // preserve that position. Only scroll to bottom if the terminal
                  // was already at the bottom before the fit.
                  setTimeout(() => {
                    try {
                      const bufBefore = instance.xterm.buffer.active;
                      const distFromBottom = bufBefore.baseY - bufBefore.viewportY;
                      instance.fitAddon.fit();
                      if (distFromBottom === 0) {
                        instance.xterm.scrollToBottom();
                      }
                    } catch (e) {
                      // Ignore
                    }
                  }, 0);
                }
              }
            }}
            className="absolute inset-0 xterm-container"
            data-tab-id={tab.id}
            style={{
              display: tab.id === activeTabId ? 'block' : 'none',
              pointerEvents: tab.id === activeTabId ? 'auto' : 'none'
            }}
          />
        ))}
      </div>

      {/* Context menu */}
      {contextMenu && (
        <div
          className="fixed bg-elevated border border-border-subtle rounded-lg shadow-lg py-1 z-50"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          <button
            onClick={handleCopy}
            className="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-text-secondary hover:bg-hover hover:text-text-primary transition-colors"
          >
            <Copy size={14} />
            Copy
          </button>
          <button
            onClick={handlePaste}
            className="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-text-secondary hover:bg-hover hover:text-text-primary transition-colors"
          >
            <Clipboard size={14} />
            Paste
          </button>
          <div className="border-t border-border-subtle my-1" />
          <button
            onClick={() => { handleClear(); setContextMenu(null); }}
            className="flex items-center gap-2 w-full px-3 py-1.5 text-sm text-text-secondary hover:bg-hover hover:text-text-primary transition-colors"
          >
            <RotateCcw size={14} />
            Clear
          </button>
        </div>
      )}
    </div>
  );
});