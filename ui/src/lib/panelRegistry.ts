/**
 * Panel Registry — 统一面板注册表
 *
 * 所有面板（内置 + 将来的插件面板）通过 registerPanel 注册，
 * 通过 openPanel / closePanel / togglePanel / isPanelOpen 管理开关状态，
 * 通过 getPanelsByLocation / getPanelsByActivator 查询面板列表。
 *
 * 设计原则：新增面板只需 registerPanel 一行，不需要改 useAppState / App.tsx / Footer.tsx / ActivityBar.tsx。
 */

import type { ComponentType } from 'react';

/** 面板在布局中的位置 */
export type PanelLocation = 'left-top' | 'left' | 'right' | 'center-bottom' | 'modal';

/** 面板的触发方式 */
export type PanelActivator = 'footer' | 'activityBar' | 'node-select' | 'gearMenu' | 'command';

/** Footer 按钮放置位置 — 仅 activator='footer' 时生效 */
export type FooterSlot = 'left' | 'center' | 'right';

/** 面板触发行为 */
export type PanelAction = 'toggle' | 'popup';

/** 面板定义 */
export interface PanelDef {
  /** 面板唯一 ID，如 "codeHealth"、"graphAnalytics" */
  id: string;
  /** 显示标题 */
  title: string;
  /** lucide icon 名称，如 "Activity"、"BarChart3"；插件面板可为 URL */
  icon: string;
  /** 面板在布局中的位置 */
  location: PanelLocation;
  /** 面板的触发方式 */
  activator: PanelActivator;
  /** Footer 按钮放置位置（仅 activator='footer' 时生效，默认 'left'） */
  footerSlot?: FooterSlot;
  /** React 组件（静态 import，内置面板使用） */
  component?: ComponentType<PanelComponentProps>;
  /** 异步组件加载器（插件面板使用，动态 import()） */
  asyncLoader?: () => Promise<{ default: ComponentType<PluginPanelProps> }>;
  /** 面板排序权重，越小越靠前；内置 0~9，插件推荐 10~99，默认 0 */
  order?: number;
  /** 面板触发行为：'toggle' 点击切换面板，'popup' 点击弹出浮层；默认 'toggle' */
  action?: PanelAction;
  /** 是否为插件面板 */
  isPlugin?: boolean;
  /** 是否为独立窗口面板（不在主窗口内渲染） */
  standalone?: boolean;
  /** 插件 ID（仅插件面板有值） */
  pluginId?: string;
  /** 插件后端 endpoint（仅插件面板有值，如 http://127.0.0.1:18080） */
  endpoint?: string;
}

/** 统一面板组件 Props 协议 */
export interface PanelComponentProps {
  /** 关闭面板回调 */
  onClose: () => void;
  /** 面板 ID */
  panelId: string;
  /** 节点选择回调（可选，统一注入） */
  onSelectNode?: (nodeId: string) => void;
}

/** 插件面板组件 Props 协议 — 插件开发者使用此接口 */
export interface PluginPanelProps {
  /** pluginApi 对象 — 插件与 axons 平台通信的唯一入口 */
  pluginApi: import('./pluginApi').PluginApi;
  /** 关闭面板回调 */
  onClose: () => void;
  /** 面板 ID */
  panelId: string;
}

/** 面板注册表 */
export class PanelRegistry {
  private panels = new Map<string, PanelDef>();

  /** 注册一个面板 */
  register(def: PanelDef): void {
    if (this.panels.has(def.id)) {
      console.warn(`[PanelRegistry] Panel "${def.id}" already registered, skipping`);
      return;
    }
    this.panels.set(def.id, def);
  }

  /** 注销一个面板 */
  unregister(id: string): void {
    this.panels.delete(id);
  }

  /** 获取面板定义 */
  get(id: string): PanelDef | undefined {
    return this.panels.get(id);
  }

  /** 获取所有已注册面板 */
  getAll(): PanelDef[] {
    return Array.from(this.panels.values()).sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  }

  /** 按位置获取面板 */
  getByLocation(location: PanelLocation): PanelDef[] {
    return this.getAll().filter(p => p.location === location);
  }

  /** 按触发方式获取面板 */
  getByActivator(activator: PanelActivator): PanelDef[] {
    return this.getAll().filter(p => p.activator === activator);
  }

  /** 检查面板是否已注册 */
  has(id: string): boolean {
    return this.panels.has(id);
  }
}