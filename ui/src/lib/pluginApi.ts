/**
 * pluginApi — 插件与 axons 平台通信的唯一入口
 *
 * 插件开发者通过 { pluginApi } 参数获得此对象，调用：
 *   pluginApi.fetch('/api/models')       → 桌面端直连插件后端 / Web端走 axons 代理
 *   pluginApi.createEventSource('/sse')  → SSE 连接（自动选择直连/代理）
 *   pluginApi.onEvent('node:selected')   → EventBus 订阅
 *   pluginApi.emitEvent('model:ready')   → EventBus 广播
 *   pluginApi.getState('key')            → 读取共享状态
 *   pluginApi.setState('key', value)     → 写入共享状态
 */

import { getBaseURL } from './config';
import { getRuntimeMode } from './config';
import { pluginEventBus } from './pluginEventBus';

/** PluginApi 配置 */
export interface PluginApiConfig {
  pluginId: string;
  endpoint: string; // http://127.0.0.1:PORT or '' for pure-frontend
}

/** PluginApi 对象 — 注入到每个插件面板组件 */
export interface PluginApi {
  /** 当前插件 ID */
  pluginId: string;
  /** 插件后端地址 (http://127.0.0.1:PORT)，无后端时为 null */
  endpoint: string | null;
  /** 请求插件后端 API — 自动选择直连/代理 */
  fetch(path: string, opts?: RequestInit): Promise<Response>;
  /** 创建 SSE 连接 — 自动选择直连/代理 */
  createEventSource(path: string): EventSource;
  /** 订阅 EventBus 事件 */
  onEvent(type: string, handler: (payload: any) => void): () => void;
  /** 广播 EventBus 事件 */
  emitEvent(type: string, payload: any): void;
  /** 读取共享状态 */
  getState(key: string): Promise<any>;
  /** 写入共享状态 */
  setState(key: string, value: any): Promise<void>;
}

/**
 * 解析请求 URL — 桌面端直连插件后端，Web 端走 axons 代理
 */
function resolveUrl(path: string, config: PluginApiConfig): string {
  if (getRuntimeMode() === 'desktop' && config.endpoint) {
    // 桌面端：直连插件后端
    return config.endpoint + path;
  }
  // Web 端：走 axons 代理
  return `${getBaseURL()}/v1/plugins/${config.pluginId}/proxy${path}`;
}

/**
 * 创建 PluginApi 实例
 */
export function createPluginApi(config: PluginApiConfig): PluginApi {
  return {
    pluginId: config.pluginId,
    endpoint: config.endpoint || null,

    fetch(path: string, opts?: RequestInit): Promise<Response> {
      const url = resolveUrl(path, config);
      return globalThis.fetch(url, opts);
    },

    createEventSource(path: string): EventSource {
      const url = resolveUrl(path, config);
      return new EventSource(url);
    },

    onEvent(type: string, handler: (payload: any) => void): () => void {
      return pluginEventBus.on(type, handler);
    },

    emitEvent(type: string, payload: any): void {
      pluginEventBus.emit(type, payload, `plugin:${config.pluginId}`);
    },

    async getState(key: string): Promise<any> {
      const resp = await globalThis.fetch(`${getBaseURL()}/v1/plugins/state/${config.pluginId}:${key}`);
      if (!resp.ok) return null;
      const data = await resp.json();
      return data.value;
    },

    async setState(key: string, value: any): Promise<void> {
      await globalThis.fetch(`${getBaseURL()}/v1/plugins/state/${config.pluginId}:${key}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(value),
      });
    },
  };
}