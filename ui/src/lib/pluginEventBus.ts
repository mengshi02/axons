/**
 * PluginEventBus — 前薄后厚版 EventBus
 *
 * 前薄后厚设计：daemon 作为 EventBus 广播中心。
 * - emit → POST /v1/plugins/event → daemon EventBus → SSE 广播
 * - on   → EventSource /v1/plugins/events/stream → daemon SSE 推送
 *
 * 内置面板和 iframe 走同一通道，不再有前端 JS 桥接中继。
 */

type EventHandler = (payload: any) => void;

interface PluginEvent {
  pluginId: string;
  type: string;
  payload: any;
}

class PluginEventBusImpl {
  private handlers = new Map<string, Set<EventHandler>>();
  private wildcardHandlers = new Set<EventHandler>();
  private eventSource: EventSource | null = null;
  private connected = false;

  // Debounce for emit to prevent rapid event flooding
  private pendingEmits = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly EMIT_DEBOUNCE_MS = 100; // 100ms debounce for event emission

  /**
   * 订阅事件，返回 unsubscribe 函数
   */
  on(type: string, handler: EventHandler): () => void {
    if (type === '*') {
      this.wildcardHandlers.add(handler);
    } else {
      if (!this.handlers.has(type)) {
        this.handlers.set(type, new Set());
      }
      this.handlers.get(type)!.add(handler);
    }

    // 首次订阅时创建 SSE 连接
    if (!this.eventSource) {
      this.connectSSE();
    }

    return () => {
      this.off(type, handler);
    };
  }

  /**
   * 取消订阅
   */
  off(type: string, handler: EventHandler): void {
    if (type === '*') {
      this.wildcardHandlers.delete(handler);
    } else {
      const handlers = this.handlers.get(type);
      if (handlers) {
        handlers.delete(handler);
        if (handlers.size === 0) {
          this.handlers.delete(type);
        }
      }
    }
  }

  /**
   * 广播事件 — 通过 daemon EventBus POST
   * 带防抖保护,防止快速点击产生大量请求
   */
  emit(type: string, payload: any, source: string = 'unknown'): void {
    // Cancel any pending emit for this event type
    const existingTimer = this.pendingEmits.get(type);
    if (existingTimer) {
      clearTimeout(existingTimer);
    }

    // Debounce: delay the emit slightly
    const timer = setTimeout(() => {
      this.pendingEmits.delete(type);

      const event: PluginEvent = { pluginId: source, type, payload };
      fetch('/v1/plugins/event', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(event),
      }).catch(err => {
        console.error('[EventBus] Failed to emit event:', err);
      });
    }, this.EMIT_DEBOUNCE_MS);

    this.pendingEmits.set(type, timer);
  }

  /**
   * 检查是否有某个事件的订阅者
   */
  hasListeners(type: string): boolean {
    return (this.handlers.get(type)?.size ?? 0) > 0;
  }

  /**
   * 建立 SSE 连接到 daemon
   */
  private connectSSE() {
    if (this.connected) return;
    this.connected = true;

    this.eventSource = new EventSource('/v1/plugins/events/stream');
    this.eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as PluginEvent;
        // 派发到具体类型的 handler
        const handlers = this.handlers.get(data.type);
        if (handlers) {
          handlers.forEach(handler => {
            try {
              handler(data.payload);
            } catch (err) {
              console.error(`[EventBus] Error in handler for "${data.type}":`, err);
            }
          });
        }
        // 派发到通配符 handler
        this.wildcardHandlers.forEach(handler => {
          try {
            handler(data);
          } catch (err) {
            console.error(`[EventBus] Error in wildcard handler:`, err);
          }
        });
      } catch (err) {
        console.error('[EventBus] SSE parse error:', err);
      }
    };
    this.eventSource.onerror = () => {
      console.warn('[EventBus] SSE connection error, will auto-reconnect');
    };
  }
}

/** 全局单例 */
export const pluginEventBus = new PluginEventBusImpl();