import { useState, useEffect, useCallback } from 'react';
import {
  X, BarChart3, RefreshCw, Network, ListOrdered,
  CheckCircle, XCircle, VectorSquare, ChartScatter, Waypoints, 
  CircleSmall, Infinity,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  fetchGraphMetrics, fetchPageRank, fetchCommunities, fetchCycles,
  type GraphMetrics, type PageRankItem, type Community, type CycleItem,
} from '../services/analysis';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';

type Tab = 'metrics' | 'pagerank' | 'communities' | 'cycles';

export function GraphAnalyticsPanel({ onClose, onSelectNode }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { currentProject } = useAppState();
  const [activeTab, setActiveTab] = useState<Tab>('metrics');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [metrics, setMetrics] = useState<GraphMetrics | null>(null);
  const [rankings, setRankings] = useState<PageRankItem[]>([]);
  const [communities, setCommunities] = useState<Community[]>([]);
  const [cycles, setCycles] = useState<CycleItem[]>([]);

  const load = useCallback(async (tab: Tab) => {
    setLoading(true);
    setError(null);
    const projectId = currentProject?.id;
    try {
      if (tab === 'metrics') {
        const data = await fetchGraphMetrics(projectId);
        setMetrics(data);
      } else if (tab === 'pagerank') {
        const data = await fetchPageRank(50, projectId);
        setRankings(data.rankings);
      } else if (tab === 'communities') {
        const data = await fetchCommunities(1.0, projectId);
        setCommunities(data.communities);
      } else if (tab === 'cycles') {
        const data = await fetchCycles(projectId);
        setCycles(data.cycles);
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

  const shortPath = (file: string) => {
    const parts = file.split('/');
    return parts.length > 2 ? '…/' + parts.slice(-2).join('/') : file;
  };

  const maxRank = rankings.length > 0 ? rankings[0].page_rank : 1;

  return (
    <div className="w-full bg-surface flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <BarChart3 className="w-4 h-4 text-purple-400" />
          <span className="text-sm font-semibold text-text-primary">{t('graphAnalytics.title')}</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => load(activeTab)}
            disabled={loading}
            className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors disabled:opacity-40"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button onClick={onClose} className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="grid grid-cols-4 border-b border-border-subtle">
        {([
          { key: 'metrics', label: t('graphAnalytics.tab.metrics'), icon: ChartScatter },
          { key: 'pagerank', label: t('graphAnalytics.tab.pagerank'), icon: ListOrdered },
          { key: 'communities', label: t('graphAnalytics.tab.communities'), icon: VectorSquare },
          { key: 'cycles', label: t('graphAnalytics.tab.cycles'), icon: Infinity },
        ] as { key: Tab; label: string; icon: React.FC<{ className?: string }> }[]).map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => setActiveTab(key)}
            className={`flex flex-col items-center gap-1 py-2.5 text-xs font-medium transition-colors ${activeTab === key
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
          <div className="m-4 p-3 bg-red-500/10 border border-red-500/30 rounded-lg text-xs text-red-400">{error}</div>
        )}
        {loading && (
          <div className="flex items-center justify-center h-32">
            <RefreshCw className="w-5 h-5 text-accent animate-spin" />
          </div>
        )}

        {!loading && !error && (
          <>
            {/* Metrics Tab */}
            {activeTab === 'metrics' && metrics && (
              <div className="p-4 space-y-4">
                {/* Summary cards */}
                <div className="grid grid-cols-2 gap-3">
                  <MetricCard label={t('graphAnalytics.nodes')} value={metrics.total_nodes.toLocaleString()} icon={<CircleSmall className="w-4 h-4 text-blue-400" />} />
                  <MetricCard label={t('graphAnalytics.edges')} value={metrics.total_edges.toLocaleString()} icon={<Waypoints className="w-4 h-4 text-purple-400" />} />
                  <MetricCard label={t('graphAnalytics.communities')} value={metrics.num_communities.toString()} icon={<Network className="w-4 h-4 text-green-400" />} />
                  <MetricCard label={t('graphAnalytics.cyclesScc')} value={metrics.num_sccs.toString()} icon={<Infinity className="w-4 h-4 text-orange-400" />} />
                </div>

                {/* Detail rows */}
                <div className="space-y-2">
                  <SectionTitle>{t('graphAnalytics.degree')}</SectionTitle>
                  <DetailRow label={t('graphAnalytics.avgInDegree')} value={metrics.avg_in_degree.toFixed(2)} />
                  <DetailRow label={t('graphAnalytics.avgOutDegree')} value={metrics.avg_out_degree.toFixed(2)} />
                  <DetailRow label={t('graphAnalytics.maxInDegree')} value={String(metrics.max_in_degree)} />
                  <DetailRow label={t('graphAnalytics.maxOutDegree')} value={String(metrics.max_out_degree)} />

                  <SectionTitle>Structure</SectionTitle>
                  <DetailRow label={t('graphAnalytics.density')} value={metrics.density.toFixed(4)} />
                  <DetailRow label="Largest SCC" value={String(metrics.largest_scc_size)} />
                  <DetailRow label={t('graphAnalytics.modularity')} value={metrics.modularity.toFixed(3)} />
                  <div className="flex items-center justify-between px-3 py-2 bg-elevated rounded border border-border-subtle">
                    <span className="text-xs text-text-secondary">Is DAG</span>
                    {metrics.is_dag
                      ? <CheckCircle className="w-4 h-4 text-green-400" />
                      : <XCircle className="w-4 h-4 text-red-400" />}
                  </div>
                </div>
              </div>
            )}

            {/* PageRank Tab */}
            {activeTab === 'pagerank' && (
              <div className="p-4 space-y-2">
                <div className="text-xs text-text-muted mb-3">
                  Most important nodes by random-walk probability (higher = more central)
                </div>
                {rankings.length === 0 ? (
                  <EmptyState message="No data. Build the graph first." />
                ) : (
                  rankings.map((item, idx) => (
                    <div
                      key={item.id}
                      className="flex items-center gap-3 px-3 py-2.5 bg-elevated rounded border border-border-subtle hover:border-accent/40 cursor-pointer transition-colors"
                      onClick={() => onSelectNode?.(String(item.id))}
                    >
                      <span className="text-xs text-text-muted w-5 text-right shrink-0">{idx + 1}</span>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="text-xs font-medium text-text-primary truncate">{item.name}</span>
                          <span className="text-xs px-1 bg-accent/10 text-accent rounded shrink-0">{item.kind}</span>
                        </div>
                        <div className="text-xs text-text-muted truncate">{shortPath(item.file)}</div>
                      </div>
                      <div className="shrink-0 flex flex-col items-end gap-1">
                        <span className="text-xs font-mono text-purple-300">{(item.page_rank * 100).toFixed(3)}%</span>
                        <div className="w-16 h-1 bg-border-subtle rounded-full overflow-hidden">
                          <div
                            className="h-full bg-purple-400 rounded-full"
                            style={{ width: `${(item.page_rank / maxRank) * 100}%` }}
                          />
                        </div>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {/* Communities Tab */}
            {activeTab === 'communities' && (
              <div className="p-4 space-y-3">
                <div className="text-xs text-text-muted mb-2">
                  Louvain community detection — groups of tightly coupled nodes
                </div>
                {communities.length === 0 ? (
                  <EmptyState message="No communities detected." />
                ) : (
                  communities
                    .sort((a, b) => b.size - a.size)
                    .map((comm, idx) => (
                      <CommunityCard key={comm.id} community={comm} index={idx} onSelectNode={onSelectNode} />
                    ))
                )}
              </div>
            )}

            {/* Cycles Tab */}
            {activeTab === 'cycles' && (
              <div className="p-4 space-y-3">
                <div className="text-xs text-text-muted mb-2">
                  Strongly Connected Components with 2+ nodes — potential circular dependencies
                </div>
                {cycles.length === 0 ? (
                  <div className="flex flex-col items-center py-12 gap-2">
                    <CheckCircle className="w-8 h-8 text-green-400" />
                    <span className="text-xs text-text-muted">No circular dependencies detected</span>
                  </div>
                ) : (
                  cycles.map((cycle, idx) => (
                    <div key={idx} className="p-3 bg-elevated rounded-lg border border-red-500/20">
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-xs font-medium text-red-400">Cycle #{idx + 1}</span>
                        <span className="text-xs text-text-muted">{cycle.length} nodes</span>
                      </div>
                      <div className="space-y-1">
                        {cycle.nodes.slice(0, 5).map((n) => (
                          <div
                            key={n.id}
                            className="flex items-center gap-2 text-xs cursor-pointer hover:text-accent transition-colors"
                            onClick={() => onSelectNode?.(String(n.id))}
                          >
                            <span className="w-1.5 h-1.5 rounded-full bg-red-400 shrink-0" />
                            <span className="font-medium truncate">{n.name}</span>
                            <span className="text-text-muted shrink-0">{n.kind}</span>
                          </div>
                        ))}
                        {cycle.nodes.length > 5 && (
                          <div className="text-xs text-text-muted pl-3.5">+{cycle.nodes.length - 5} more nodes</div>
                        )}
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

// ─── Sub-components ──────────────────────────────────────────────────────────

const COMMUNITY_COLORS = [
  'bg-blue-500/20 border-blue-500/30 text-blue-300',
  'bg-purple-500/20 border-purple-500/30 text-purple-300',
  'bg-green-500/20 border-green-500/30 text-green-300',
  'bg-orange-500/20 border-orange-500/30 text-orange-300',
  'bg-pink-500/20 border-pink-500/30 text-pink-300',
  'bg-cyan-500/20 border-cyan-500/30 text-cyan-300',
];

function CommunityCard({
  community, index, onSelectNode,
}: {
  community: Community;
  index: number;
  onSelectNode?: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const colorClass = COMMUNITY_COLORS[index % COMMUNITY_COLORS.length];
  const shortPath = (file: string) => {
    const parts = file.split('/');
    return parts.length > 2 ? '…/' + parts[parts.length - 1] : file;
  };

  return (
    <div className={`rounded-lg border p-3 ${colorClass}`}>
      <div
        className="flex items-center justify-between cursor-pointer"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="text-xs font-medium">Module {index + 1}</span>
        <div className="flex items-center gap-3 text-xs">
          <span>{community.size} nodes</span>
          <span>density {community.density.toFixed(2)}</span>
          <span>{expanded ? '▲' : '▼'}</span>
        </div>
      </div>
      {expanded && (
        <div className="mt-2 space-y-1 max-h-40 overflow-y-auto">
          {community.nodes.map((n) => (
            <div
              key={n.id}
              className="flex items-center gap-1.5 text-xs cursor-pointer hover:opacity-70 transition-opacity"
              onClick={() => onSelectNode?.(String(n.id))}
            >
              <span className="font-medium truncate">{n.name}</span>
              <span className="opacity-60 shrink-0">{shortPath(n.file)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function MetricCard({ label, value, icon }: { label: string; value: string; icon: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 p-3 bg-elevated rounded-lg border border-border-subtle">
      {icon}
      <div>
        <div className="text-lg font-bold text-text-primary">{value}</div>
        <div className="text-xs text-text-muted">{label}</div>
      </div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between px-3 py-2 bg-elevated rounded border border-border-subtle">
      <span className="text-xs text-text-secondary">{label}</span>
      <span className="text-xs font-mono text-text-primary">{value}</span>
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return <div className="text-xs font-semibold text-text-muted uppercase tracking-wider mt-3 mb-1">{children}</div>;
}

function EmptyState({ message }: { message: string }) {
  return <div className="py-12 text-center text-text-muted text-xs">{message}</div>;
}