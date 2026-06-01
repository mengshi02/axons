/**
 * iframe-adapter — 在插件 iframe 内运行，封装 HTTP/SSE 为 PluginApi 接口。
 * 前薄后厚：数据通道全部直连 daemon，UI 控制走 postMessage。
 * 插件开发者无需直接使用此模块，由 daemon iframe-host 模板自动加载。
 */

/** postMessage 信封格式 */
interface PluginMessage {
  /** 消息协议标识 */
  protocol: 'axons-plugin-iframe';
  /** 协议版本号 */
  version: 1;
  /** 消息来源 */
  source: 'host' | 'plugin';
  /** 插件 ID */
  pluginId: string;
  /** 消息类型 */
  type: string;
  /** 消息载荷 */
  payload?: any;
}

/** daemon 注入到 iframe 的运行时配置 */
interface AxonsPluginRuntime {
  pluginId: string;
  endpoint: string;
  runtimeMode: 'desktop' | 'web';
  protocolVersion: number;
}

declare global {
  interface Window {
    __AXONS_PLUGIN__?: AxonsPluginRuntime;
  }
}

/**
 * IframePluginApiAdapter — 在插件 iframe 内运行，实现 PluginApi 接口。
 * 所有方法走 HTTP/SSE 直连 daemon，postMessage 只用于 UI 控制（close/theme）。
 */
export class IframePluginApiAdapter {
  private static readonly PROTOCOL_VERSION = 1 as const;
  /** Trusted origin of the host window — set from the first valid plugin:init message */
  private hostOrigin: string | null = null;
  private _pluginId = '';
  private eventSource: EventSource | null = null;
  private eventHandlers = new Map<string, Set<(payload: any) => void>>();

  constructor() {
    window.addEventListener('message', this.handleMessage);
  }

  /** Validate that a message comes from the trusted host */
  private isFromHost(event: MessageEvent): boolean {
    // Before init, accept any origin for plugin:init (the protocol identifier provides basic auth)
    // After init, strictly check against the recorded host origin
    if (!this.hostOrigin) return true;
    return event.origin === this.hostOrigin;
  }

  /** 等待主 UI 发送 init 消息，初始化 pluginId */
  init(): Promise<{ pluginId: string }> {
    return new Promise((resolve) => {
      const handler = (event: MessageEvent) => {
        const msg = event.data;
        if (msg?.protocol !== 'axons-plugin-iframe' || msg.version !== IframePluginApiAdapter.PROTOCOL_VERSION) return;
        if (msg.type !== 'plugin:init') return;
        // Record the host's origin from the first valid init message
        this.hostOrigin = event.origin;
        this._pluginId = msg.payload.pluginId;
        // Set initial theme class from host
        if (msg.payload.theme) {
          const root = document.documentElement;
          root.classList.remove('moon-theme', 'sun-theme');
          root.classList.add(msg.payload.theme === 'moon' ? 'moon-theme' : 'sun-theme');
        }
        window.removeEventListener('message', handler);
        this.send({ type: 'plugin:ready' });
        resolve({ pluginId: this._pluginId });
      };
      window.addEventListener('message', handler);
    });
  }

  // ---- PluginApi 接口实现（全部走 HTTP/SSE 直连 daemon） ----

  get pluginId(): string { return this._pluginId; }

  /** 插件后端地址 (http://127.0.0.1:PORT)，无后端时为 null */
  get endpoint(): string | null {
    const runtime = window.__AXONS_PLUGIN__;
    if (runtime?.endpoint) return runtime.endpoint;
    return null;
  }

  /** fetch → 桌面端直连插件后端，Web 端走 daemon 代理（与改造前行为一致） */
  async fetch(path: string, opts?: RequestInit): Promise<Response> {
    return globalThis.fetch(this.resolveUrl(path), opts);
  }

  /** createEventSource → 桌面端直连插件后端，Web 端走 daemon 代理 */
  createEventSource(path: string): EventSource {
    return new EventSource(this.resolveUrl(path));
  }

  /** getState → iframe 内直连 daemon 状态 API */
  async getState(key: string): Promise<any> {
    const resp = await globalThis.fetch(`/v1/plugins/state/${this.pluginId}:${key}`);
    if (!resp.ok) return null;
    const data = await resp.json();
    return data.value;
  }

  /** setState → iframe 内直连 daemon 状态 API */
  async setState(key: string, value: any): Promise<void> {
    await globalThis.fetch(`/v1/plugins/state/${this.pluginId}:${key}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(value),
    });
  }

  /** onEvent → iframe 内直连 daemon SSE 广播 */
  onEvent(type: string, handler: (payload: any) => void): () => void {
    if (!this.eventHandlers.has(type)) {
      this.eventHandlers.set(type, new Set());
    }
    this.eventHandlers.get(type)!.add(handler);

    // 首次订阅时创建 SSE 连接
    if (!this.eventSource) {
      this.connectSSE();
    }

    return () => {
      this.eventHandlers.get(type)?.delete(handler);
    };
  }

  /** emitEvent → iframe 内直连 daemon EventBus POST */
  async emitEvent(type: string, payload: any): Promise<void> {
    await globalThis.fetch('/v1/plugins/event', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pluginId: this.pluginId, type, payload }),
    });
  }

  /** onClose → postMessage 到主 UI（唯一 postMessage 用途） */
  onClose(): void {
    this.send({ type: 'plugin:close' });
  }

  /** destroy — 清理 SSE 连接和事件监听 */
  destroy(): void {
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
    this.eventHandlers.clear();
    window.removeEventListener('message', this.handleMessage);
  }

  // ---- SSE 连接管理 ----

  /** resolveUrl — 桌面端直连插件后端，Web 端走 daemon 代理（与改造前 pluginApi.ts 行为一致） */
  private resolveUrl(path: string): string {
    if (window.__AXONS_PLUGIN__?.runtimeMode === 'desktop' && window.__AXONS_PLUGIN__?.endpoint) {
      return window.__AXONS_PLUGIN__.endpoint + path;
    }
    return `/v1/plugins/${this.pluginId}/proxy${path}`;
  }

  private connectSSE() {
    this.eventSource = new EventSource('/v1/plugins/events/stream');
    this.eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        const handlers = this.eventHandlers.get(data.type);
        if (handlers) {
          handlers.forEach(h => {
            try { h(data.payload); } catch (e) { console.error('[Plugin] Event handler error:', e); }
          });
        }
      } catch (e) { console.error('[Plugin] SSE parse error:', e); }
    };
  }

  // ---- postMessage UI 控制 ----

  private handleMessage = (event: MessageEvent) => {
    if (!this.isFromHost(event)) return;
    const msg = event.data;
    if (msg?.protocol !== 'axons-plugin-iframe' || msg.source !== 'host') return;
    if (msg.version !== IframePluginApiAdapter.PROTOCOL_VERSION) return;

    switch (msg.type) {
      case 'plugin:theme': {
        const root = document.documentElement;
        root.classList.remove('moon-theme', 'sun-theme');
        root.classList.add(msg.payload.theme === 'moon' ? 'moon-theme' : 'sun-theme');
        break;
      }
      case 'host:click': {
        // Host document received a mousedown — dispatch a synthetic mousedown
        // on the iframe's document so plugin click-outside handlers fire.
        // The event target is the document itself, which is outside any
        // dropdown/popup ref, so existing contains()-based checks auto-close.
        document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
        break;
      }
    }
  };

  private send(msg: Omit<PluginMessage, 'protocol' | 'version' | 'source' | 'pluginId'>) {
    // Use '*' as targetOrigin because the host may be in a different origin context
    // (sandbox iframe without allow-same-origin has opaque origin)
    window.parent.postMessage({
      protocol: 'axons-plugin-iframe',
      version: IframePluginApiAdapter.PROTOCOL_VERSION,
      source: 'plugin',
      pluginId: this.pluginId,
      ...msg,
    }, '*');
  }
}