import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { X, Radar, RefreshCw, Search, ChevronRight, ArrowRight, GitBranch } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useVirtualizer } from '@tanstack/react-virtual';
import {
  fetchImpact, fetchCallChain,
  type ImpactResponse, type CallChainResponse, type NodeRef,
} from '../services/analysis';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';

type Tab = 'impact' | 'callchain';

export const ImpactAnalysisPanel = React.memo(function ImpactAnalysisPanel({ onClose, onSelectNode }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { selectedNode, graph, currentProject } = useAppState();
  const [activeTab, setActiveTab] = useState<Tab>('impact');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Impact state
  const [impactData, setImpactData] = useState<ImpactResponse | null>(null);
  const [depth, setDepth] = useState(3);
  const [activeDepth, setActiveDepth] = useState<string>('all');

  // Call chain state
  const [fromNodeId, setFromNodeId] = useState('');
  const [toNodeId, setToNodeId] = useState('');
  const [callChainData, setCallChainData] = useState<CallChainResponse | null>(null);
  const [fromSearch, setFromSearch] = useState('');
  const [toSearch, setToSearch] = useState('');

  // Auto-load impact when selected node changes
  useEffect(() => {
    if (selectedNode && activeTab === 'impact') {
      const numId = parseInt(selectedNode, 10);
      if (!isNaN(numId)) {
        loadImpact(numId);
      }
    }
  }, [selectedNode, depth]); // eslint-disable-line react-hooks/exhaustive-deps

  const loadImpact = useCallback(async (nodeId: number) => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchImpact(nodeId, depth, currentProject?.id);
      setImpactData(data);
      setActiveDepth('all');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load impact');
    } finally {
      setLoading(false);
    }
  }, [depth, currentProject?.id]);

  const loadCallChain = useCallback(async () => {
    const from = parseInt(fromNodeId, 10);
    const to = parseInt(toNodeId, 10);
    if (isNaN(from) || isNaN(to)) {
      setError(t('impact.enterValidNodeIds'));
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const data = await fetchCallChain(from, to, 5, currentProject?.id);
      setCallChainData(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to find call chain');
    } finally {
      setLoading(false);
    }
  }, [fromNodeId, toNodeId, currentProject?.id]);

  const selectedNodeName = selectedNode && graph
    ? graph.nodes.find(n => n.id === selectedNode)?.properties.name
    : undefined;

  const shortPath = (file: string) => {
    const parts = file.split('/');
    return parts.length > 3 ? '…/' + parts.slice(-2).join('/') : file;
  };

  const getDisplayedNodes = (): NodeRef[] => {
    if (!impactData) return [];
    if (activeDepth === 'all') return impactData.impacted_nodes;
    return impactData.by_depth[activeDepth] || [];
  };

  // Use ref for onSelectNode so virtualized rows don't need it as dependency
  const onSelectNodeRef = useRef(onSelectNode);
  onSelectNodeRef.current = onSelectNode;

  const displayedNodes = useMemo(() => getDisplayedNodes(), [impactData, activeDepth]);

  // ─── Virtual list for impact nodes ──────────────────────────────────────
  const impactScrollRef = useRef<HTMLDivElement>(null);
  const impactVirtualizer = useVirtualizer({
    count: displayedNodes.length,
    getScrollElement: () => impactScrollRef.current,
    estimateSize: () => 52,
    overscan: 10,
    measureElement: (el) => el.getBoundingClientRect().height,
  });

  // Simple node search for call chain
  const searchNodes = (query: string) => {
    if (!graph || !query.trim()) return [];
    return graph.nodes
      .filter(n => n.properties.name.toLowerCase().includes(query.toLowerCase()))
      .slice(0, 5);
  };

  return (
    <div className="w-full bg-surface flex flex-col overflow-hidden" style={{ contain: 'layout style' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <Radar className="w-4 h-4 text-orange-400" />
          <span className="text-sm font-semibold text-text-primary">{t('impact.title')}</span>
        </div>
        <button onClick={onClose} className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors">
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border-subtle">
        {([
          { key: 'impact', label: 'Change Impact', icon: Radar },
          { key: 'callchain', label: 'Call Chain', icon: GitBranch },
        ] as { key: Tab; label: string; icon: React.FC<{ className?: string }> }[]).map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => setActiveTab(key)}
            className={`flex-1 flex items-center justify-center gap-1.5 px-4 py-2.5 text-xs font-medium transition-colors ${activeTab === key
              ? 'text-accent border-b-2 border-accent bg-accent/5'
              : 'text-text-muted hover:text-text-secondary'
              }`}
          >
            <Icon className="w-3.5 h-3.5" />
            {label}
          </button>
        ))}
      </div>

      {/* Error */}
      {error && (
        <div className="mx-4 mt-3 p-3 bg-red-500/10 border border-red-500/30 rounded-lg text-xs text-red-400">
          {error}
        </div>
      )}

      {/* Content */}
      <div>
        {/* Impact Tab */}
        {activeTab === 'impact' && (
          <div ref={impactScrollRef} className="overflow-y-auto max-h-80 p-4 space-y-4">
            {/* Context & controls */}
            <div className="space-y-2">
              {selectedNodeName ? (
                <div className="px-3 py-2 bg-orange-500/10 border border-orange-500/20 rounded-lg text-xs">
                  <span className="text-text-muted">Analyzing impact of: </span>
                  <span className="font-medium text-orange-300">{selectedNodeName}</span>
                </div>
              ) : (
                <div className="px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-xs text-text-muted">
                  Select a node in the graph to analyze its impact
                </div>
              )}
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-muted">Depth:</span>
                {[1, 2, 3, 5, 8].map((d) => (
                  <button
                    key={d}
                    onClick={() => setDepth(d)}
                    className={`px-2 py-0.5 text-xs rounded border transition-colors ${depth === d
                      ? 'bg-accent text-white border-accent'
                      : 'bg-elevated text-text-muted border-border-subtle hover:border-accent/50'
                      }`}
                  >
                    {d}
                  </button>
                ))}
                {selectedNode && (
                  <button
                    onClick={() => loadImpact(parseInt(selectedNode, 10))}
                    disabled={loading}
                    className="ml-auto p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors disabled:opacity-40"
                  >
                    <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
                  </button>
                )}
              </div>
            </div>

            {loading && (
              <div className="flex items-center justify-center h-24">
                <RefreshCw className="w-5 h-5 text-accent animate-spin" />
              </div>
            )}

            {!loading && impactData && (
              <>
                {/* Summary */}
                <div className="flex gap-3">
                  <div className="flex-1 px-3 py-2 bg-elevated rounded border border-border-subtle text-center">
                    <div className="text-lg font-bold text-orange-300">{impactData.total_affected}</div>
                    <div className="text-xs text-text-muted">Affected</div>
                  </div>
                  <div className="flex-1 px-3 py-2 bg-elevated rounded border border-border-subtle text-center">
                    <div className="text-lg font-bold text-text-primary">{impactData.impact_radius}</div>
                    <div className="text-xs text-text-muted">Max Depth</div>
                  </div>
                </div>

                {/* Depth filter */}
                <div>
                  <div className="text-xs font-medium text-text-secondary mb-2">Filter by depth</div>
                  <div className="flex flex-wrap gap-1.5">
                    <button
                      onClick={() => setActiveDepth('all')}
                      className={`px-2.5 py-1 text-xs rounded border transition-colors ${activeDepth === 'all'
                        ? 'bg-accent text-white border-accent'
                        : 'bg-elevated text-text-muted border-border-subtle hover:border-accent/50'
                        }`}
                    >
                      All ({impactData.total_affected})
                    </button>
                    {Object.entries(impactData.by_depth)
                      .sort(([a], [b]) => parseInt(a) - parseInt(b))
                      .map(([d, nodes]) => (
                        <button
                          key={d}
                          onClick={() => setActiveDepth(d)}
                          className={`px-2.5 py-1 text-xs rounded border transition-colors ${activeDepth === d
                            ? 'bg-orange-500/80 text-white border-orange-500'
                            : 'bg-elevated text-text-muted border-border-subtle hover:border-orange-500/50'
                            }`}
                        >
                          Depth {d} ({nodes.length})
                        </button>
                      ))}
                  </div>
                </div>

                {/* Node list — VIRTUALIZED */}
                <div>
                  {displayedNodes.length === 0 ? (
                    <div className="text-xs text-text-muted text-center py-4">No impacted nodes at this depth</div>
                  ) : (
                      <div
                        style={{
                          height: `${impactVirtualizer.getTotalSize()}px`,
                          width: '100%',
                          position: 'relative',
                        }}
                      >
                        {impactVirtualizer.getVirtualItems().map((virtualRow) => {
                          const n = displayedNodes[virtualRow.index];
                          return (
                            <div
                              key={n.id}
                              data-index={virtualRow.index}
                              ref={impactVirtualizer.measureElement}
                              className="pb-1.5"
                              style={{
                                position: 'absolute',
                                top: 0,
                                left: 0,
                                width: '100%',
                                transform: `translateY(${virtualRow.start}px)`,
                              }}
                            >
                              <div
                                className="flex items-center gap-2 px-3 py-2 bg-elevated rounded border border-border-subtle hover:border-orange-400/40 cursor-pointer transition-colors"
                                onClick={() => onSelectNodeRef.current?.(String(n.id))}
                              >
                                <ChevronRight className="w-3 h-3 text-orange-400 shrink-0" />
                                <div className="min-w-0 flex-1">
                                  <div className="flex items-center gap-1.5">
                                    <span className="text-xs font-medium text-text-primary truncate">{n.name}</span>
                                    <span className="text-xs px-1 bg-accent/10 text-accent rounded shrink-0">{n.kind}</span>
                                  </div>
                                  <div className="text-xs text-text-muted truncate">{shortPath(n.file)}{n.line ? `:${n.line}` : ''}</div>
                                </div>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                  )}
                </div>
              </>
            )}
          </div>
        )}

        {/* Call Chain Tab */}
        {activeTab === 'callchain' && (
          <div className="overflow-y-auto max-h-80 p-4 space-y-4">
            <div className="text-xs text-text-muted">
              Find all call paths between two functions
            </div>

            {/* From input */}
            <NodeSearchInput
              label="From"
              value={fromSearch}
              nodeId={fromNodeId}
              onChange={setFromSearch}
              onSelect={(id, name) => { setFromNodeId(id); setFromSearch(name); }}
              searchResults={searchNodes(fromSearch)}
            />

            {/* Arrow */}
            <div className="flex items-center gap-2 text-text-muted">
              <div className="h-px flex-1 bg-border-subtle" />
              <ArrowRight className="w-4 h-4" />
              <div className="h-px flex-1 bg-border-subtle" />
            </div>

            {/* To input */}
            <NodeSearchInput
              label="To"
              value={toSearch}
              nodeId={toNodeId}
              onChange={setToSearch}
              onSelect={(id, name) => { setToNodeId(id); setToSearch(name); }}
              searchResults={searchNodes(toSearch)}
            />

            <button
              onClick={loadCallChain}
              disabled={loading || !fromNodeId || !toNodeId}
              className="w-full py-2 bg-accent text-white text-xs rounded-lg font-medium disabled:opacity-40 disabled:cursor-not-allowed hover:bg-accent/90 transition-colors"
            >
              {loading ? 'Searching…' : 'Find Call Paths'}
            </button>

            {callChainData && (
              <div className="space-y-3">
                {!callChainData.found ? (
                  <div className="text-center text-xs text-text-muted py-4">No paths found between these functions</div>
                ) : (
                  <>
                    <div className="text-xs font-medium text-text-secondary">{callChainData.count} path(s) found</div>
                    {callChainData.paths.map((path, idx) => (
                      <div key={idx} className="p-3 bg-elevated rounded-lg border border-border-subtle">
                        <div className="text-xs text-text-muted mb-2">Path {idx + 1}</div>
                        <div className="flex flex-col gap-1">
                          {path.nodes.map((n, ni) => (
                            <div key={n.id} className="flex items-center gap-2">
                              <div
                                className="flex items-center gap-1.5 flex-1 cursor-pointer hover:text-accent transition-colors"
                                onClick={() => onSelectNode?.(String(n.id))}
                              >
                                <span className="text-xs font-medium text-text-primary truncate">{n.name}</span>
                                <span className="text-xs text-text-muted shrink-0">{n.kind}</span>
                              </div>
                              {ni < path.nodes.length - 1 && (
                                <ChevronRight className="w-3 h-3 text-text-muted shrink-0" />
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    ))}
                  </>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
});

// ─── NodeSearchInput ──────────────────────────────────────────────────────────

interface NodeSearchInputProps {
  label: string;
  value: string;
  nodeId: string;
  onChange: (v: string) => void;
  onSelect: (id: string, name: string) => void;
  searchResults: Array<{ id: string; properties: { name: string }; label: string }>;
}

function NodeSearchInput({ label, value, nodeId, onChange, onSelect, searchResults }: NodeSearchInputProps) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <div className="text-xs text-text-muted mb-1.5 font-medium">{label}</div>
      <div className={`flex items-center gap-2 px-3 py-2 bg-elevated border rounded-lg transition-colors ${nodeId ? 'border-accent/50' : 'border-border-subtle'
        }`}>
        <Search className="w-3.5 h-3.5 text-text-muted shrink-0" />
        <input
          type="text"
          value={value}
          placeholder="Search symbol name…"
          onChange={e => { onChange(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          className="flex-1 text-xs bg-transparent text-text-primary placeholder:text-text-muted focus:outline-none"
        />
        {nodeId && <span className="text-xs text-accent shrink-0">#{nodeId}</span>}
      </div>
      {open && searchResults.length > 0 && (
        <div className="absolute top-full left-0 right-0 z-10 mt-1 bg-elevated border border-border-subtle rounded-lg shadow-lg overflow-hidden">
          {searchResults.map(n => (
            <div
              key={n.id}
              className="flex items-center gap-2 px-3 py-2 hover:bg-hover cursor-pointer text-xs"
              onMouseDown={() => { onSelect(n.id, n.properties.name); setOpen(false); }}
            >
              <span className="font-medium text-text-primary truncate">{n.properties.name}</span>
              <span className="text-accent shrink-0">{n.label}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}