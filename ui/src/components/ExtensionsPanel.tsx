/**
 * ExtensionsPanel — 插件管理面板
 *
 * 展示已安装插件列表，支持启动/停止/卸载操作
 */

import React, { useState, useRef, useEffect, useCallback } from 'react';
import { Puzzle, Play, Square, Trash2, RefreshCw, Upload, Download, Terminal } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { usePluginList, type PluginCard } from '../hooks/usePluginRegistry';
import { startPlugin, stopPlugin, uninstallPlugin, installPlugin, scanPlugins, importPlugin } from '../services/api';
import { useEventStream, type PluginInstallProgressEvent } from '../hooks/useEventStream';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { ConfirmDialog, type ConfirmDialogCheckbox } from './ConfirmDialog';

const STATUS_STYLES: Record<string, string> = {
  running: 'text-green-400',
  starting: 'text-yellow-400',
  stopped: 'text-text-muted',
  crashed: 'text-red-400',
  installed: 'text-text-muted',
  imported: 'text-blue-400',
};

const CATEGORY_LABELS: Record<string, string> = {
  analysis: 'Analysis',
  visualization: 'Visualization',
  search: 'Search',
  productivity: 'Productivity',
};

export const ExtensionsPanel = React.memo(function ExtensionsPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('extensions');
  const { plugins, loading, refresh } = usePluginList();
  const [filter, setFilter] = useState<string>('all');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  const categories = ['all', ...new Set(plugins.map(p => p.category).filter(Boolean))];

  const filtered = filter === 'all'
    ? plugins
    : plugins.filter(p => p.category === filter);

  // Track which plugins are currently installing (async — waiting for SSE events)
  const [installingIds, setInstallingIds] = useState<Set<string>>(new Set());

  // Install logs: pluginId → array of log lines
  const [installLogs, setInstallLogs] = useState<Map<string, string[]>>(new Map());
  const installLogsRef = useRef(installLogs);
  installLogsRef.current = installLogs;

  // Which plugin's log panel is expanded
  const [expandedLogId, setExpandedLogId] = useState<string | null>(null);

  const appendLog = useCallback((pluginId: string, line: string) => {
    setInstallLogs(prev => {
      const next = new Map(prev);
      const lines = next.get(pluginId) || [];
      next.set(pluginId, [...lines, line]);
      return next;
    });
  }, []);

  const clearLog = useCallback((pluginId: string) => {
    setInstallLogs(prev => {
      const next = new Map(prev);
      next.delete(pluginId);
      return next;
    });
  }, []);

  const handleInstall = async (id: string) => {
    setActionLoading(id);
    // Clear previous logs for this plugin
    clearLog(id);
    appendLog(id, 'Starting installation...');
    try {
      await installPlugin(id);
      // Backend returned 202 — install is now running asynchronously.
      setInstallingIds(prev => new Set(prev).add(id));
      // Auto-expand log panel
      setExpandedLogId(id);
    } catch (err) {
      console.error('Failed to install plugin:', err);
      appendLog(id, `Error: ${err}`);
    } finally {
      setActionLoading(null);
    }
  };

  // Listen for install progress, completion, and failure events
  useEventStream({
    onPluginInstallProgress: useCallback((data: PluginInstallProgressEvent) => {
      const prefix = data.stream === 'stderr' ? '[stderr] ' : '';
      appendLog(data.pluginId, prefix + data.line);
    }, [appendLog]),
    onPluginInstalled: useCallback((data: import('../hooks/useEventStream').PluginLifecycleEvent) => {
      setInstallingIds(prev => {
        const next = new Set(prev);
        next.delete(data.pluginId);
        return next;
      });
      appendLog(data.pluginId, 'Installation completed successfully.');
      refresh();
    }, [refresh, appendLog]),
    onPluginInstallFailed: useCallback((data: import('../hooks/useEventStream').PluginLifecycleEvent & { error?: string }) => {
      setInstallingIds(prev => {
        const next = new Set(prev);
        next.delete(data.pluginId);
        return next;
      });
      appendLog(data.pluginId, `Installation failed: ${data.error || 'unknown error'}`);
      refresh();
    }, [refresh, appendLog]),
  });

  const handleStart = async (id: string) => {
    setActionLoading(id);
    try {
      await startPlugin(id);
      refresh();
    } catch (err) {
      console.error('Failed to start plugin:', err);
    } finally {
      setActionLoading(null);
    }
  };

  const handleStop = async (id: string) => {
    setActionLoading(id);
    try {
      await stopPlugin(id);
      refresh();
    } catch (err) {
      console.error('Failed to stop plugin:', err);
    } finally {
      setActionLoading(null);
    }
  };

  const handleUninstall = async (id: string, argValues: Record<string, boolean> = {}) => {
    setActionLoading(id);
    setConfirmDeleteId(null);
    try {
      await uninstallPlugin(id, argValues);
      refresh();
    } catch (err) {
      console.error('Failed to uninstall plugin:', err);
    } finally {
      setActionLoading(null);
    }
  };

  const handleScan = async () => {
    try {
      await scanPlugins();
      refresh();
    } catch (err) {
      console.error('Failed to scan plugins:', err);
    }
  };

  // Get uninstall checkboxes from manifest backend.uninstall.args
  const getUninstallCheckboxes = (pluginId: string): ConfirmDialogCheckbox[] | undefined => {
    const plugin = plugins.find(p => p.id === pluginId);
    if (!plugin?.backend?.uninstall?.args) return undefined;
    return (plugin.backend.uninstall.args as Array<{ name: string; type: string; default?: boolean; description?: string }>)
      .filter(a => a.type === 'boolean')
      .map(a => ({
        id: a.name,
        label: a.description || a.name,
        defaultChecked: a.default ?? false,
      }));
  };

  const fileInputRef = useRef<HTMLInputElement>(null);
  const [importing, setImporting] = useState(false);

  const handleImport = () => {
    fileInputRef.current?.click();
  };

  const handleFileSelected = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (!file.name.endsWith('.tar.gz') && !file.name.endsWith('.tgz')) {
      alert('Only .tar.gz archives are supported');
      return;
    }
    setImporting(true);
    try {
      await importPlugin(file);
      refresh();
    } catch (err: any) {
      console.error('Failed to import plugin:', err);
      alert(`Import failed: ${err.message || 'Unknown error'}`);
    } finally {
      setImporting(false);
      // Reset input so the same file can be selected again
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  return (
    <div
      className="w-full h-full bg-surface flex flex-col overflow-hidden relative"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 h-[38px] border-b border-border-subtle">
        <h2 className="text-sm font-semibold text-text-primary flex items-center gap-2">
          <Puzzle className="w-4 h-4 text-text-secondary flex-shrink-0" />
          {t('title')}
        </h2>
        <div className="flex items-center gap-1">
          <button
            onClick={handleImport}
            disabled={importing}
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded disabled:opacity-50"
            title="Import plugin from .tar.gz"
          >
            {importing ? (
              <div className="w-3.5 h-3.5 border-2 border-accent border-t-transparent rounded-full animate-spin" />
            ) : (
              <Upload className="w-3.5 h-3.5" />
            )}
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept=".tgz,.tar.gz,application/gzip"
            className="hidden"
            onChange={handleFileSelected}
          />
          <button
            onClick={handleScan}
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded"
            title="Scan for new plugins"
          >
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={onClose}
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded text-xs"
          >
            ✕
          </button>
        </div>
      </div>

      {/* Category filters */}
      <div className="flex items-center gap-1 px-4 border-b border-border-subtle overflow-x-auto" style={{ height: 31 }}>
        {categories.map(cat => (
          <button
            key={cat}
            onClick={() => setFilter(cat)}
            className={`px-2 py-0.5 text-[11px] rounded-full whitespace-nowrap ${filter === cat
              ? 'bg-accent/20 text-accent'
              : 'text-text-muted hover:text-text-primary hover:bg-hover'
              }`}
          >
            {cat === 'all' ? 'All' : CATEGORY_LABELS[cat] || cat}
          </button>
        ))}
      </div>

      {/* Plugin list */}
      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <div className="flex items-center justify-center h-32">
            <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-32 text-text-muted text-sm">
            <Puzzle className="w-8 h-8 mb-2 opacity-50" />
              <p>{t('noExtensions')}</p>
              <p className="text-xs mt-1">{t('placePlugins')}</p>
          </div>
        ) : (
          <div className="divide-y divide-border-subtle">
            {filtered.map(plugin => (
              <PluginCardItem
                key={plugin.id}
                plugin={plugin}
                isLoading={actionLoading === plugin.id || installingIds.has(plugin.id)}
                isInstalling={installingIds.has(plugin.id)}
                logs={installLogs.get(plugin.id)}
                logExpanded={expandedLogId === plugin.id}
                onToggleLog={() => setExpandedLogId(expandedLogId === plugin.id ? null : plugin.id)}
                onInstall={() => handleInstall(plugin.id)}
                onStart={() => handleStart(plugin.id)}
                onStop={() => handleStop(plugin.id)}
                onUninstall={() => setConfirmDeleteId(plugin.id)}
              />
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        isOpen={confirmDeleteId !== null}
        title={t('uninstallTitle')}
        message={t('uninstallMessage')}
        confirmLabel={t('uninstallConfirm')}
        variant="danger"
        checkboxes={confirmDeleteId !== null ? getUninstallCheckboxes(confirmDeleteId) : undefined}
        onConfirm={(argValues) => { if (confirmDeleteId !== null) handleUninstall(confirmDeleteId, argValues); }}
        onCancel={() => setConfirmDeleteId(null)}
      />
    </div>
  );
});

/** Single plugin card with optional install log panel */
function PluginCardItem({
  plugin,
  isLoading,
  isInstalling,
  logs,
  logExpanded,
  onToggleLog,
  onInstall,
  onStart,
  onStop,
  onUninstall,
}: {
  plugin: PluginCard;
  isLoading: boolean;
    isInstalling: boolean;
    logs?: string[];
    logExpanded: boolean;
    onToggleLog: () => void;
    onInstall: () => void;
  onStart: () => void;
  onStop: () => void;
  onUninstall: () => void;
}) {
  const hasLogs = logs && logs.length > 0;
  const logEndRef = useRef<HTMLDivElement>(null);

  // Auto-scroll log panel to bottom on new lines
  useEffect(() => {
    if (logExpanded && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, logExpanded]);

  return (
    <div>
      <div className="px-4 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium text-text-primary truncate">{plugin.name}</span>
              <span className="text-[10px] text-text-muted">v{plugin.version}</span>
            </div>
            <p className="text-xs text-text-secondary mt-0.5 line-clamp-2">{plugin.description}</p>
            <div className="flex items-center gap-2 mt-1">
              {plugin.author && <span className="text-[10px] text-text-muted">by {plugin.author}</span>}
              <span className={`text-[10px] ${STATUS_STYLES[plugin.status] || 'text-text-muted'}`}>
                {isInstalling ? 'installing' : plugin.status}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-1 shrink-0">
            {/* Log toggle button — show when there are logs or installing */}
            {(hasLogs || isInstalling) && (
              <button
                onClick={onToggleLog}
                className={`p-1 rounded ${logExpanded ? 'text-accent bg-accent/10' : 'text-text-muted hover:text-accent hover:bg-hover'}`}
                title="Toggle install log"
              >
                <Terminal className="w-3.5 h-3.5" />
              </button>
            )}
            {isLoading && !isInstalling ? (
              <div className="w-5 h-5 border-2 border-accent border-t-transparent rounded-full animate-spin" />
            ) : (
              <>
                {plugin.status === 'running' ? (
                  <button
                    onClick={onStop}
                    className="p-1 text-text-muted hover:text-yellow-400 hover:bg-hover rounded"
                    title="Stop"
                  >
                    <Square className="w-3.5 h-3.5" />
                  </button>
                  ) : plugin.status === 'imported' ? (
                    <button
                      onClick={onInstall}
                      className="p-1 text-text-muted hover:text-blue-400 hover:bg-hover rounded"
                      title="Install"
                    >
                      <Download className="w-3.5 h-3.5" />
                    </button>
                ) : (
                  <button
                    onClick={onStart}
                    className="p-1 text-text-muted hover:text-green-400 hover:bg-hover rounded"
                    title="Start"
                  >
                    <Play className="w-3.5 h-3.5" />
                  </button>
                )}
                <button
                  onClick={onUninstall}
                  className="p-1 text-text-muted hover:text-red-400 hover:bg-hover rounded"
                  title="Uninstall"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Install log panel */}
      {logExpanded && hasLogs && (
        <div className="border-t border-border-subtle bg-deep mx-2 mb-2 rounded overflow-hidden">
          <div className="flex items-center gap-1 px-2 py-1 border-b border-border-subtle/50">
            <Terminal className="w-3 h-3 text-text-muted" />
            <span className="text-[10px] text-text-muted font-medium">Install Log</span>
            {isInstalling && (
              <div className="w-2.5 h-2.5 border border-accent border-t-transparent rounded-full animate-spin ml-auto" />
            )}
          </div>
          <div className="max-h-40 overflow-y-auto px-2 py-1 font-mono text-[11px] leading-relaxed">
            {logs.map((line, i) => (
              <div
                key={i}
                className={
                  line.startsWith('[stderr]') ? 'text-yellow-600 dark:text-yellow-400/80' :
                    line.startsWith('Error:') || line.startsWith('Installation failed') ? 'text-red-600 dark:text-red-400' :
                      line === 'Installation completed successfully.' ? 'text-green-600 dark:text-green-400' :
                        line === 'Starting installation...' ? 'text-blue-600 dark:text-blue-400' :
                          'text-text-secondary'
                }
              >
                {line}
              </div>
            ))}
            <div ref={logEndRef} />
          </div>
        </div>
      )}
    </div>
  );
}