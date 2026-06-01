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
    selectionBackground: '#7c3aed55', // 半透明紫色
    selectionForeground: '#e4e4ed',
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
    selectionBackground: '#2563eb33', // 半透明蓝色
    selectionForeground: '#1a1a2e',
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

export const TerminalPanel: React.FC<PanelComponentProps> = ({ onClose }) => {
  const { t } = useTranslation('activitybar');
  const { currentProject } = useAppState();
  const cwd = currentProject?.root_path || '/';
  const projectName = currentProject?.name;
  const { theme } = useTheme();
  const xtermRef = useRef<XTerm | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [panelHeight, setPanelHeight] = useState(320);
  const [isDragging, setIsDragging] = useState(false);
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
    });

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
        currentInstance.xterm.write(msg.data, () => {
          // Scroll to bottom after write completes
          currentInstance.xterm.scrollToBottom();
        });
        // Update sequence tracking
        if (msg.seq) lastSeqRef.current.set(tabId, msg.seq);
      } else if (msg.type === 'replay' && currentInstance?.xterm) {
        // Replayed historical output from server
        currentInstance.xterm.write(msg.data, () => {
          currentInstance.xterm.scrollToBottom();
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

  // Fit terminal on resize (both window and container resize)
  useEffect(() => {
    const handleResize = () => {
      // Use active tab's instance instead of refs
      const activeInstance = terminalInstancesRef.current.get(activeTabId || '');
      if (activeInstance?.fitAddon && activeInstance?.xterm) {
        try {
          activeInstance.fitAddon.fit();

          // Send resize to backend
          if (activeInstance.ws && activeInstance.ws.readyState === WebSocket.OPEN) {
            activeInstance.ws.send(JSON.stringify({
              type: 'resize',
              cols: activeInstance.xterm.cols,
              rows: activeInstance.xterm.rows,
            }));
          }
        } catch (e) {
          // Ignore fit errors during resize
        }
      }
    };

    // Listen to window resize
    window.addEventListener('resize', handleResize);

    // Use ResizeObserver to monitor container size changes (e.g., when side panel opens/closes)
    const resizeObserver = new ResizeObserver(() => {
      handleResize();
    });

    if (panelRef.current) {
      resizeObserver.observe(panelRef.current);
    }

    return () => {
      window.removeEventListener('resize', handleResize);
      resizeObserver.disconnect();
    };
  }, [activeTabId]);

  // Fit terminal on fullscreen/height change
  useEffect(() => {
    // Use active tab's fitAddon instead of the ref
    const activeInstance = terminalInstancesRef.current.get(activeTabId || '');
    if (activeInstance?.fitAddon && activeInstance?.xterm) {
      setTimeout(() => {
        try {
          activeInstance.fitAddon.fit();

          // Send resize to backend
          if (activeInstance.ws?.readyState === WebSocket.OPEN) {
            activeInstance.ws.send(JSON.stringify({
              type: 'resize',
              cols: activeInstance.xterm.cols,
              rows: activeInstance.xterm.rows,
            }));
          }
        } catch (e) {
          // Ignore fit errors
        }
      }, 100);
    }
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

  // Handle panel drag resize
  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isDragging || !panelRef.current) return;

      const newHeight = window.innerHeight - e.clientY;
      const minHeight = 200;
      const maxHeight = window.innerHeight - 100;

      setPanelHeight(Math.min(Math.max(newHeight, minHeight), maxHeight));

      // Fit terminal immediately during drag to prevent visual gap at bottom
      const activeInstance = terminalInstancesRef.current.get(activeTabId || '');
      if (activeInstance?.fitAddon && activeInstance?.xterm) {
        try {
          activeInstance.fitAddon.fit();
          // Send resize to backend
          if (activeInstance.ws && activeInstance.ws.readyState === WebSocket.OPEN) {
            activeInstance.ws.send(JSON.stringify({
              type: 'resize',
              cols: activeInstance.xterm.cols,
              rows: activeInstance.xterm.rows,
            }));
          }
        } catch (_e) {
          // Ignore fit errors during drag
        }
      }
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      // Reset user-select that was disabled during drag
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };

    if (isDragging) {
      // Disable text selection during drag to prevent accidental selections
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'row-resize';
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    }

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, [isDragging]);

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
      className={`border-t border-border-subtle flex flex-col ${isFullscreen ? 'fixed inset-0 z-50' : ''}`}
      style={{ height: isFullscreen ? '100vh' : panelHeight, minHeight: isFullscreen ? undefined : 200, backgroundColor: TERMINAL_THEMES[theme].background }}
    >
      {/* Drag handle */}
      {!isFullscreen && (
        <div
          className="h-1 bg-transparent hover:bg-accent/20 cursor-row-resize transition-colors flex-shrink-0"
          onMouseDown={() => setIsDragging(true)}
        />
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
      <div className="flex items-center justify-between px-4 py-2 bg-surface border-b border-border-subtle flex-shrink-0">
        <div className="flex items-center gap-2 flex-1 overflow-x-auto">
          {/* Tabs */}
          {tabs.map(tab => (
            <div
              key={tab.id}
              onClick={() => switchTab(tab.id)}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-t text-sm cursor-pointer transition-colors ${activeTabId === tab.id
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
                      // Force a refresh
                      instance.xterm.refresh(0, instance.xterm.rows - 1);
                      // Scroll to bottom
                      instance.xterm.scrollToBottom();
                    } catch (e) {
                      // Ignore fit errors
                    }
                  }, 50);
                } else if (instance && instance.xterm.element) {
                  // Already opened, just fit
                  setTimeout(() => {
                    try {
                      instance.fitAddon.fit();
                      // Scroll to bottom
                      instance.xterm.scrollToBottom();
                    } catch (e) {
                      // Ignore
                    }
                  }, 0);
                }
              }
            }}
            className="absolute inset-0 xterm-container"
            style={{
              display: tab.id === activeTabId ? 'block' : 'none',
              height: '100%',
              width: '100%',
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
};