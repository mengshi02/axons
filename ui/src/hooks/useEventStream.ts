import { useEffect, useCallback, useRef } from 'react';
import { getBaseURL } from '../lib/config';

// Event types matching backend
export type EventType =
  | 'file_change'
  | 'build_progress'
  | 'build_complete'
  | 'build_error'
  | 'search_step'
  | 'rag_chunk'
  | 'watch_status'
  | 'embed_progress'
  | 'embed_complete'
  | 'embed_error'
  | 'config_change'
  | 'plugin.started'
  | 'plugin.stopped'
  | 'plugin.crashed'
  | 'plugin.installed'
  | 'plugin.installProgress'
  | 'plugin.installFailed'
  | 'plugin.imported'
  | 'plugin.uninstalled'
  | 'plugin.cleaned'
  | 'locale.available'
  | 'locale.unavailable'
  | 'notification';

export interface Event {
  type: EventType;
  timestamp: string;
  data: Record<string, unknown>;
}

export interface FileChangeEvent {
  project_id: string;
  file_path: string;
  change_type: string;
}

export interface BuildProgressEvent {
  task_id: string;
  progress: number;
  message: string;
  phase: string;
  project_id: string;
}

export interface BuildCompleteEvent {
  task_id: string;
  project_id: string;
  files_parsed: number;
  nodes_created: number;
  edges_created: number;
  changed_files?: string[];
  removed_files?: string[];
  changed_file_old_node_ids?: string[];
  changed_file_old_edge_ids?: string[];
}

export interface BuildErrorEvent {
  task_id: string;
  project_id: string;
  error: string;
}

export interface BuildDeltaEvent {
  task_id: string;
  project_id: string;
  stage: string;
  added_nodes?: Array<{
    id: string;
    name: string;
    kind: string;
    file: string;
    line: number;
    qualified_name: string;
    exported: boolean;
  }>;
  added_edges?: Array<{
    id: string;
    source: string;
    target: string;
    kind: string;
  }>;
}

export interface SearchStepEvent {
  query_id: string;
  step: string;
  status: string;
  duration_ms: number;
}

export interface RAGChunkEvent {
  query_id: string;
  content: string;
  done: boolean;
}

export interface WatchStatusEvent {
  project_id: string;
  status: string;
  root_dir: string;
}

export interface EmbedProgressEvent {
  task_id: string;
  project_id: string;
  current: number;
  total: number;
  status: string;
}

export interface EmbedCompleteEvent {
  task_id: string;
  project_id: string;
  total_nodes: number;
  new_embeddings: number;
  updated_embeddings: number;
}

export interface EmbedErrorEvent {
  task_id: string;
  project_id: string;
  error: string;
}

/** 插件生命周期事件数据 */
export interface PluginLifecycleEvent {
  pluginId: string;
  name?: string;
  version?: string;
  endpoint?: string;
  panels?: Array<{
    id: string;
    title: string;
    icon: string;
    location: string;
    activator: string;
    footerSlot?: string;
  }>;
  commands?: Array<{
    id: string;
    title: string;
    shortcut?: string;
  }>;
  restarts?: number;
  lastError?: string;
  argValues?: Record<string, boolean>;
}

/** 插件安装进度事件 — 安装脚本每一行输出 */
export interface PluginInstallProgressEvent {
  pluginId: string;
  stream: 'stdout' | 'stderr';
  line: string;
}

/** Locale available event — language plugin installed */
export interface LocaleAvailableEvent {
  locale: string;
  pluginId: string;
  nativeName: string;
  englishName: string;
}

/** Locale unavailable event — language plugin uninstalled */
export interface LocaleUnavailableEvent {
  locale: string;
  pluginId: string;
  fallback: string;
}

/** Notification event — created or updated */
export interface NotificationEvent {
  action: 'created' | 'updated';
  id: string;
  source: string;
  type: 'info' | 'warning' | 'error' | 'success' | 'progress';
  title: string;
  message: string;
  actions: Array<{ id: string; label: string; url: string }>;
  read: boolean;
  timestamp: string;
}

interface UseEventStreamOptions {
  onFileChange?: (data: FileChangeEvent) => void;
  onBuildProgress?: (data: BuildProgressEvent) => void;
  onBuildComplete?: (data: BuildCompleteEvent) => void;
  onBuildError?: (data: BuildErrorEvent) => void;
  onBuildDelta?: (data: BuildDeltaEvent) => void;
  onSearchStep?: (data: SearchStepEvent) => void;
  onRAGChunk?: (data: RAGChunkEvent) => void;
  onWatchStatus?: (data: WatchStatusEvent) => void;
  onEmbedProgress?: (data: EmbedProgressEvent) => void;
  onEmbedComplete?: (data: EmbedCompleteEvent) => void;
  onEmbedError?: (data: EmbedErrorEvent) => void;
  onConfigChange?: (data: { config_type: string; message: string }) => void;
  onPluginStarted?: (data: PluginLifecycleEvent) => void;
  onPluginStopped?: (data: PluginLifecycleEvent) => void;
  onPluginCrashed?: (data: PluginLifecycleEvent) => void;
  onPluginInstalled?: (data: PluginLifecycleEvent) => void;
  onPluginInstallProgress?: (data: PluginInstallProgressEvent) => void;
  onPluginInstallFailed?: (data: PluginLifecycleEvent & { error?: string }) => void;
  onPluginUninstalled?: (data: PluginLifecycleEvent) => void;
  onLocaleAvailable?: (data: LocaleAvailableEvent) => void;
  onLocaleUnavailable?: (data: LocaleUnavailableEvent) => void;
  onNotification?: (data: NotificationEvent) => void;
  onConnect?: () => void;
  onDisconnect?: () => void;
  enabled?: boolean;
}

export function useEventStream(options: UseEventStreamOptions = {}) {
  const {
    enabled = true,
  } = options;

  // Store callbacks in refs so the EventSource is never torn down
  // when a callback reference changes — this prevents missed events.
  const callbacksRef = useRef(options);
  callbacksRef.current = options;

  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);

  const connect = useCallback(() => {
    if (!enabled || eventSourceRef.current) return;

    const eventSource = new EventSource(`${getBaseURL()}/v1/events`);
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      callbacksRef.current.onConnect?.();
    };

    eventSource.onerror = () => {
      // Check readyState to determine the nature of the error
      if (eventSource.readyState === EventSource.CLOSED) {
        // Connection was closed, clean up and reconnect
        eventSource.close();
        eventSourceRef.current = null;
        callbacksRef.current.onDisconnect?.();

        // Reconnect after 3 seconds
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
        }
        reconnectTimeoutRef.current = window.setTimeout(() => {
          connect();
        }, 3000);
      }
      // If CONNECTING, browser handles auto-reconnect
    };

    // Handle specific event types
    eventSource.addEventListener('file_change', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onFileChange?.(event.data as unknown as FileChangeEvent);
      } catch (err) {
        console.error('Failed to parse file_change event:', err);
      }
    });

    eventSource.addEventListener('build_progress', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onBuildProgress?.(event.data as unknown as BuildProgressEvent);
      } catch (err) {
        console.error('Failed to parse build_progress event:', err);
      }
    });

    eventSource.addEventListener('build_complete', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onBuildComplete?.(event.data as unknown as BuildCompleteEvent);
      } catch (err) {
        console.error('Failed to parse build_complete event:', err);
      }
    });

    eventSource.addEventListener('build_error', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onBuildError?.(event.data as unknown as BuildErrorEvent);
      } catch (err) {
        console.error('Failed to parse build_error event:', err);
      }
    });

    eventSource.addEventListener('build_delta', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onBuildDelta?.(event.data as unknown as BuildDeltaEvent);
      } catch (err) {
        console.error('Failed to parse build_delta event:', err);
      }
    });

    eventSource.addEventListener('search_step', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onSearchStep?.(event.data as unknown as SearchStepEvent);
      } catch (err) {
        console.error('Failed to parse search_step event:', err);
      }
    });

    eventSource.addEventListener('rag_chunk', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onRAGChunk?.(event.data as unknown as RAGChunkEvent);
      } catch (err) {
        console.error('Failed to parse rag_chunk event:', err);
      }
    });

    eventSource.addEventListener('watch_status', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onWatchStatus?.(event.data as unknown as WatchStatusEvent);
      } catch (err) {
        console.error('Failed to parse watch_status event:', err);
      }
    });

    eventSource.addEventListener('embed_progress', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onEmbedProgress?.(event.data as unknown as EmbedProgressEvent);
      } catch (err) {
        console.error('Failed to parse embed_progress event:', err);
      }
    });

    eventSource.addEventListener('embed_complete', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onEmbedComplete?.(event.data as unknown as EmbedCompleteEvent);
      } catch (err) {
        console.error('Failed to parse embed_complete event:', err);
      }
    });

    eventSource.addEventListener('embed_error', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onEmbedError?.(event.data as unknown as EmbedErrorEvent);
      } catch (err) {
        console.error('Failed to parse embed_error event:', err);
      }
    });

    eventSource.addEventListener('config_change', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onConfigChange?.(event.data as unknown as { config_type: string; message: string });
      } catch (err) {
        console.error('Failed to parse config_change event:', err);
      }
    });

    // Plugin lifecycle events
    eventSource.addEventListener('plugin.started', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginStarted?.(event.data as unknown as PluginLifecycleEvent);
      } catch (err) {
        console.error('Failed to parse plugin.started event:', err);
      }
    });

    eventSource.addEventListener('plugin.stopped', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginStopped?.(event.data as unknown as PluginLifecycleEvent);
      } catch (err) {
        console.error('Failed to parse plugin.stopped event:', err);
      }
    });

    eventSource.addEventListener('plugin.crashed', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginCrashed?.(event.data as unknown as PluginLifecycleEvent);
      } catch (err) {
        console.error('Failed to parse plugin.crashed event:', err);
      }
    });

    eventSource.addEventListener('plugin.installProgress', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginInstallProgress?.(event.data as unknown as PluginInstallProgressEvent);
      } catch (err) {
        console.error('Failed to parse plugin.installProgress event:', err);
      }
    });

    eventSource.addEventListener('plugin.installed', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginInstalled?.(event.data as unknown as PluginLifecycleEvent);
      } catch (err) {
        console.error('Failed to parse plugin.installed event:', err);
      }
    });

    eventSource.addEventListener('plugin.installFailed', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginInstallFailed?.(event.data as unknown as PluginLifecycleEvent & { error?: string });
      } catch (err) {
        console.error('Failed to parse plugin.installFailed event:', err);
      }
    });

    eventSource.addEventListener('plugin.uninstalled', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onPluginUninstalled?.(event.data as unknown as PluginLifecycleEvent);
      } catch (err) {
        console.error('Failed to parse plugin.uninstalled event:', err);
      }
    });

    // Handle locale.available event — language plugin installed
    eventSource.addEventListener('locale.available', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onLocaleAvailable?.(event.data as unknown as LocaleAvailableEvent);
      } catch (err) {
        console.error('Failed to parse locale.available event:', err);
      }
    });

    // Handle locale.unavailable event — language plugin uninstalled
    eventSource.addEventListener('locale.unavailable', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onLocaleUnavailable?.(event.data as unknown as LocaleUnavailableEvent);
      } catch (err) {
        console.error('Failed to parse locale.unavailable event:', err);
      }
    });

    // Handle notification event
    eventSource.addEventListener('notification', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data) as Event;
        callbacksRef.current.onNotification?.(event.data as unknown as NotificationEvent);
      } catch (err) {
        console.error('Failed to parse notification event:', err);
      }
    });

    // Handle heartbeat event (no callback needed, just prevent warnings)
    eventSource.addEventListener('heartbeat', () => {
      // Heartbeat received, connection is alive - no action needed
    });

    // Handle connected event (already handled by onopen, but we can suppress the log)
    eventSource.addEventListener('connected', () => {
      // Initial connection event received
    });

    // Handle generic message
    eventSource.onmessage = () => {
    // Generic message handler
    };
  }, [enabled]);

  const disconnect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (enabled) {
      connect();
    } else {
      disconnect();
    }

    return () => {
      disconnect();
    };
  }, [enabled, connect, disconnect]);

  return {
    connect,
    disconnect,
    isConnected: eventSourceRef.current?.readyState === EventSource.OPEN,
  };
}