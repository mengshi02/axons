import { useState, useEffect, useCallback } from 'react';
import {
  X, Activity, AlertTriangle, Zap, GitBranch,
  TrendingUp, FileCode, RefreshCw, ChevronRight,
  ArrowUpRight,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  fetchHotspots, fetchDeadCode, fetchCoChanges,
  type HotspotItem, type DeadCodeItem, type CoChangeItem,
} from '../services/analysis';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';

type Tab = 'hotspots' | 'deadcode' | 'cochange';

export function CodeHealthPanel({ onClose, onSelectNode }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { currentProject } = useAppState();
  const [activeTab, setActiveTab] = useState<Tab>('hotspots');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Data states
  const [hotspots, setHotspots] = useState<HotspotItem[]>([]);
  const [deadNodes, setDeadNodes] = useState<DeadCodeItem[]>([]);
  const [unusedExports, setUnusedExports] = useState<DeadCodeItem[]>([]);
  const [coChanges, setCoChanges] = useState<CoChangeItem[]>([]);

  const load = useCallback(async (tab: Tab) => {
    setLoading(true);
    setError(null);
    const projectId = currentProject?.id;
    try {
      if (tab === 'hotspots') {
        const data = await fetchHotspots(30, projectId);
        setHotspots(data.hotspots);
      } else if (tab === 'deadcode') {
        const data = await fetchDeadCode(projectId);
        setDeadNodes(data.dead_nodes);
        setUnusedExports(data.unused_exports);
      } else if (tab === 'cochange') {
        const data = await fetchCoChanges(60, 2, undefined, projectId);
        setCoChanges(data.co_changes);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }, [currentProject?.id]);

  useEffect(() => {
    load(activeTab);
  }, [activeTab, load]);

  const handleTabChange = (tab: Tab) => {
    setActiveTab(tab);
  };

  const getScoreColor = (score: number) => {
    if (score >= 20) return 'text-red-400';
    if (score >= 10) return 'text-yellow-400';
    return 'text-green-400';
  };

  const getScoreBg = (score: number) => {
    if (score >= 20) return 'bg-red-500/10 border-red-500/30';
    if (score >= 10) return 'bg-yellow-500/10 border-yellow-500/30';
    return 'bg-green-500/10 border-green-500/30';
  };

  const shortPath = (file: string) => {
    const parts = file.split('/');
    return parts.length > 3 ? '…/' + parts.slice(-2).join('/') : file;
  };

  const jaccardColor = (j: number) => {
    if (j >= 0.7) return 'text-red-400';
    if (j >= 0.4) return 'text-yellow-400';
    return 'text-blue-400';
  };

  return (
    <div className="w-full bg-surface flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-green-400" />
          <span className="text-sm font-semibold text-text-primary">{t('codeHealth.title')}</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => load(activeTab)}
            disabled={loading}
            className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors disabled:opacity-40"
            title="Refresh"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            onClick={onClose}
            className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border-subtle">
        {([
          { key: 'hotspots', label: t('codeHealth.tab.hotspots'), icon: Zap },
          { key: 'deadcode', label: t('codeHealth.tab.deadcode'), icon: AlertTriangle },
          { key: 'cochange', label: t('codeHealth.tab.cochange'), icon: GitBranch },
        ] as { key: Tab; label: string; icon: React.FC<{ className?: string }> }[]).map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => handleTabChange(key)}
            className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2.5 text-xs font-medium transition-colors ${activeTab === key
              ? 'text-accent border-b-2 border-accent bg-accent/5'
              : 'text-text-muted hover:text-text-secondary'
              }`}
          >
            <Icon className="w-3.5 h-3.5" />
            {label}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="overflow-y-auto max-h-80">
        {error && (
          <div className="m-4 p-3 bg-red-500/10 border border-red-500/30 rounded-lg text-xs text-red-400">
            {error}
          </div>
        )}

        {loading && (
          <div className="flex items-center justify-center h-32">
            <RefreshCw className="w-5 h-5 text-accent animate-spin" />
          </div>
        )}

        {!loading && !error && (
          <>
            {/* Hotspots Tab */}
            {activeTab === 'hotspots' && (
              <div className="p-4 space-y-3">
                <div className="flex items-center gap-2 text-xs text-text-muted mb-2">
                  <TrendingUp className="w-3.5 h-3.5" />
                  <span>Functions with highest call coupling (fan-in × 2 + fan-out)</span>
                </div>
                {hotspots.length === 0 ? (
                  <EmptyState message={t('codeHealth.noHotspots')} />
                ) : (
                  hotspots.map((h) => (
                    <div
                      key={h.id}
                      className={`p-3 rounded-lg border cursor-pointer hover:border-accent/50 transition-colors ${getScoreBg(h.score)}`}
                      onClick={() => onSelectNode?.(String(h.id))}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-1.5">
                            <span className="text-sm font-medium text-text-primary truncate">{h.name}</span>
                            <span className="text-xs px-1.5 py-0.5 bg-accent/10 text-accent rounded shrink-0">{h.kind}</span>
                            {h.exported && <span className="text-xs px-1 py-0.5 bg-surface text-text-muted rounded border border-border-subtle shrink-0">pub</span>}
                          </div>
                          <div className="text-xs text-text-muted mt-0.5 truncate">{shortPath(h.file)}:{h.line}</div>
                        </div>
                        <div className="flex flex-col items-end shrink-0">
                          <span className={`text-sm font-bold ${getScoreColor(h.score)}`}>{h.score.toFixed(1)}</span>
                          <span className="text-xs text-text-muted">{t('codeHealth.score')}</span>
                        </div>
                      </div>
                      <div className="flex gap-4 mt-2 text-xs text-text-secondary">
                        <span className="flex items-center gap-1">
                          <ArrowUpRight className="w-3 h-3 text-blue-400" />
                          {h.fan_in} {t('common:unit.callers', { count: h.fan_in })}
                        </span>
                        <span className="flex items-center gap-1">
                          <ChevronRight className="w-3 h-3 text-purple-400" />
                          {h.fan_out} {t('common:unit.callees', { count: h.fan_out })}
                        </span>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {/* Dead Code Tab */}
            {activeTab === 'deadcode' && (
              <div className="p-4 space-y-4">
                {/* Dead nodes */}
                <div>
                  <div className="flex items-center gap-2 text-xs font-medium text-text-secondary mb-2">
                    <AlertTriangle className="w-3.5 h-3.5 text-red-400" />
                    <span>{t('codeHealth.unreachable', { count: deadNodes.length })}</span>
                  </div>
                  <div className="space-y-1.5">
                    {deadNodes.length === 0 ? (
                      <EmptyState message={t('codeHealth.noDeadCode')} compact />
                    ) : (
                      deadNodes.map((n) => (
                        <NodeRow key={n.id} node={n} onClick={() => onSelectNode?.(String(n.id))} />
                      ))
                    )}
                  </div>
                </div>

                {/* Unused exports */}
                <div>
                  <div className="flex items-center gap-2 text-xs font-medium text-text-secondary mb-2">
                    <FileCode className="w-3.5 h-3.5 text-yellow-400" />
                    <span>{t('codeHealth.unusedExports', { count: unusedExports.length })}</span>
                  </div>
                  <div className="space-y-1.5">
                    {unusedExports.length === 0 ? (
                      <EmptyState message={t('codeHealth.noUnusedExports')} compact />
                    ) : (
                      unusedExports.map((n) => (
                        <NodeRow key={n.id} node={n} onClick={() => onSelectNode?.(String(n.id))} accent="yellow" />
                      ))
                    )}
                  </div>
                </div>
              </div>
            )}

            {/* Co-Change Tab */}
            {activeTab === 'cochange' && (
              <div className="p-4 space-y-3">
                <div className="flex items-center gap-2 text-xs text-text-muted mb-2">
                  <GitBranch className="w-3.5 h-3.5" />
                  <span>Files frequently changed together (Jaccard similarity)</span>
                </div>
                {coChanges.length === 0 ? (
                  <EmptyState message="No co-change data. Run co-change analysis first." />
                ) : (
                  coChanges.map((cc, i) => (
                    <div key={i} className="p-3 bg-elevated rounded-lg border border-border-subtle hover:border-accent/40 transition-colors">
                      <div className="flex items-center justify-between mb-1.5">
                        <span className="text-xs font-medium px-1.5 py-0.5 bg-accent/10 text-accent rounded">
                          {cc.commit_count} commits
                        </span>
                        <span className={`text-xs font-bold ${jaccardColor(cc.jaccard)}`}>
                          J={cc.jaccard.toFixed(2)}
                        </span>
                      </div>
                      <div className="space-y-1">
                        <div className="text-xs text-text-primary font-mono truncate" title={cc.file_a}>{shortPath(cc.file_a)}</div>
                        <div className="flex items-center gap-1 text-xs text-text-muted">
                          <div className="h-px flex-1 bg-border-subtle" />
                          <span>co-changes with</span>
                          <div className="h-px flex-1 bg-border-subtle" />
                        </div>
                        <div className="text-xs text-text-primary font-mono truncate" title={cc.file_b}>{shortPath(cc.file_b)}</div>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────────────

function EmptyState({ message, compact }: { message: string; compact?: boolean }) {
  return (
    <div className={`${compact ? 'py-3' : 'py-12'} text-center text-text-muted text-xs`}>
      {message}
    </div>
  );
}

function NodeRow({
  node,
  onClick,
  accent = 'red',
}: {
  node: DeadCodeItem;
  onClick: () => void;
  accent?: 'red' | 'yellow';
}) {
  const shortPath = (file: string) => {
    const parts = file.split('/');
    return parts.length > 3 ? '…/' + parts.slice(-2).join('/') : file;
  };

  const accentClass = accent === 'yellow' ? 'text-yellow-400' : 'text-red-400';

  return (
    <div
      className="flex items-center gap-2 px-2.5 py-2 bg-elevated rounded border border-border-subtle hover:border-accent/40 cursor-pointer transition-colors"
      onClick={onClick}
    >
      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${accent === 'yellow' ? 'bg-yellow-400' : 'bg-red-400'}`} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium text-text-primary truncate">{node.name}</span>
          <span className={`text-xs px-1 rounded bg-surface border border-border-subtle ${accentClass}`}>{node.kind}</span>
        </div>
        <div className="text-xs text-text-muted truncate">{shortPath(node.file)}:{node.line}</div>
      </div>
    </div>
  );
}