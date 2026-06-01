/**
 * usePluginRegistry — 从后端拉取插件注册表数据
 *
 * 用法：
 *   const pluginPanels = usePluginRegistry<PanelEntry>('panels');
 *   const pluginCommands = usePluginRegistry<CommandEntry>('commands');
 *   const pluginSkills = usePluginRegistry('skills');
 */

import { useState, useEffect } from 'react';
import { getBaseURL } from '../lib/config';

/** 插件注册表条目 */
export interface PluginRegistryEntry {
  pluginId: string;
  type: string;
  id: string;
  def: any;
  endpoint: string;
  status: string;
  updatedAt: string;
}

/** 插件卡片数据 — GET /v1/plugins 返回 */
export interface PluginCard {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  icon: string;
  category: 'analysis' | 'visualization' | 'search' | 'productivity';
  status: 'imported' | 'starting' | 'running' | 'stopped' | 'crashed' | 'installed';
  endpoint: string;
  port: number;
  frontend?: {
    entry: string;
    panels: Array<{
      id: string;
      title: string;
      icon: string;
      location: string;
      activator: string;
      footerSlot?: string;
    }>;
    commands: Array<{
      id: string;
      title: string;
      shortcut?: string;
    }>;
  };
  backend?: any;
}

/**
 * 按类型拉取插件注册表条目
 */
export function usePluginRegistry<T = PluginRegistryEntry>(type: string): T[] {
  const [items, setItems] = useState<T[]>([]);

  useEffect(() => {
    let cancelled = false;

    fetch(`${getBaseURL()}/v1/plugins/registry/${type}`)
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(data => {
        if (!cancelled) {
          setItems(Array.isArray(data) ? data : []);
        }
      })
      .catch(err => {
        console.warn(`[usePluginRegistry] Failed to fetch ${type}:`, err);
        if (!cancelled) setItems([]);
      });

    return () => { cancelled = true; };
  }, [type]);

  return items;
}

/**
 * 拉取所有已安装插件列表
 */
export function usePluginList(): { plugins: PluginCard[]; loading: boolean; refresh: () => void } {
  const [plugins, setPlugins] = useState<PluginCard[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchPlugins = () => {
    setLoading(true);
    fetch(`${getBaseURL()}/v1/plugins`)
      .then(r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(data => {
        setPlugins(Array.isArray(data) ? data : []);
        setLoading(false);
      })
      .catch(err => {
        console.warn('[usePluginList] Failed to fetch plugins:', err);
        setPlugins([]);
        setLoading(false);
      });
  };

  useEffect(() => {
    fetchPlugins();
  }, []);

  return { plugins, loading, refresh: fetchPlugins };
}