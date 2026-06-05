import React, { useState, useEffect, useCallback } from 'react';
import { X, Route, RefreshCw, ArrowRight, ArrowDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { fetchCfg, type CfgData, type CfgBlock } from '../services/analysis';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';

// Block type → display color
const BLOCK_COLORS: Record<string, string> = {
  condition: 'border-yellow-500/60 bg-yellow-500/10 text-yellow-300',
  loop: 'border-blue-500/60 bg-blue-500/10 text-blue-300',
  switch: 'border-purple-500/60 bg-purple-500/10 text-purple-300',
  return: 'border-green-500/60 bg-green-500/10 text-green-300',
  break: 'border-orange-500/60 bg-orange-500/10 text-orange-300',
  continue: 'border-orange-500/60 bg-orange-500/10 text-orange-300',
  throw: 'border-red-500/60 bg-red-500/10 text-red-300',
  try: 'border-cyan-500/60 bg-cyan-500/10 text-cyan-300',
  catch: 'border-pink-500/60 bg-pink-500/10 text-pink-300',
  entry: 'border-accent/60 bg-accent/10 text-accent',
  statement: 'border-border-subtle bg-elevated text-text-secondary',
  block: 'border-border-subtle bg-elevated text-text-secondary',
  sequential: 'border-border-subtle bg-elevated text-text-secondary',
};

const EDGE_COLORS: Record<string, string> = {
  true: 'text-green-400',
  false: 'text-red-400',
  loop: 'text-blue-400',
  enter: 'text-blue-300',
  exit: 'text-text-muted',
  case: 'text-purple-400',
  sequential: 'text-text-muted',
};

export const CfgDataflowPanel = React.memo(function CfgDataflowPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { selectedNode, graph, currentProject } = useAppState();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cfgData, setCfgData] = useState<CfgData | null>(null);
  const [nodeName, setNodeName] = useState<string>('');
  const [selectedBlock, setSelectedBlock] = useState<number | null>(null);

  const load = useCallback(async (nodeId: number) => {
    setLoading(true);
    setError(null);
    setCfgData(null);
    try {
      const data = await fetchCfg(nodeId, currentProject?.id);
      setCfgData(data.cfg);
      setNodeName(data.node.name);
    } catch {
      setError('No CFG data for this node (AST not included in build)');
    } finally {
      setLoading(false);
    }
  }, [currentProject?.id]);

  useEffect(() => {
    if (selectedNode) {
      const numId = parseInt(selectedNode, 10);
      if (!isNaN(numId)) {
        const node = graph?.nodes.find(n => n.id === selectedNode);
        const kind = node?.label;
        if (kind === 'Function' || kind === 'Method') {
          load(numId);
        } else {
          setCfgData(null);
          setNodeName(node?.properties.name || '');
          setError(null);
        }
      }
    }
  }, [selectedNode, load, graph]);

  const getBlockColor = (block: CfgBlock) => BLOCK_COLORS[block.type] || BLOCK_COLORS.statement;

  // Find outgoing edges for a block
  const outEdges = (idx: number) =>
    (cfgData?.edges || []).filter(e => e.source === idx);

  // Find incoming edges for a block
  const inEdges = (idx: number) =>
    (cfgData?.edges || []).filter(e => e.target === idx);

  return (
    <div className="w-full bg-surface flex flex-col overflow-hidden" style={{ contain: 'layout style' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
        <div className="flex items-center gap-2">
          <Route className="w-4 h-4 text-cyan-400" />
          <span className="text-sm font-semibold text-text-primary">{t('cfg.title')}</span>
          {nodeName && (
            <span className="text-xs text-text-muted truncate max-w-[160px]">— {nodeName}</span>
          )}
        </div>
        <div className="flex items-center gap-1">
          {selectedNode && (
            <button
              onClick={() => load(parseInt(selectedNode, 10))}
              disabled={loading}
              className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors disabled:opacity-40"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            </button>
          )}
          <button onClick={onClose} className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors">
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="overflow-y-auto max-h-80 p-4">
        {!selectedNode && (
          <div className="flex items-center justify-center text-xs text-text-muted py-8">
            Select a function or method node to view its control flow graph
          </div>
        )}

        {error && (
          <div className="p-3 bg-elevated border border-border-subtle rounded-lg text-xs text-text-muted text-center">
            {error}
            <div className="mt-2 text-text-muted/60">
              Tip: Include AST when building the graph (enable "Include AST" in build settings)
            </div>
          </div>
        )}

        {loading && (
          <div className="flex items-center justify-center py-8">
            <RefreshCw className="w-5 h-5 text-accent animate-spin" />
          </div>
        )}

        {!loading && cfgData && (
          <div className="space-y-4">
            {/* Legend */}
            <div className="flex flex-wrap gap-2 pb-3 border-b border-border-subtle">
              {Object.entries({
                condition: 'If/Else',
                loop: 'Loop',
                return: 'Return',
                entry: 'Entry',
                try: 'Try',
              }).map(([type, label]) => (
                <div key={type} className={`text-xs px-2 py-0.5 rounded border ${BLOCK_COLORS[type]}`}>
                  {label}
                </div>
              ))}
            </div>

            {/* Summary */}
            <div className="flex gap-3 text-xs">
              <div className="flex-1 text-center p-2 bg-elevated rounded border border-border-subtle">
                <div className="font-bold text-text-primary">{cfgData.blocks.length}</div>
                <div className="text-text-muted">Blocks</div>
              </div>
              <div className="flex-1 text-center p-2 bg-elevated rounded border border-border-subtle">
                <div className="font-bold text-text-primary">{cfgData.edges.length}</div>
                <div className="text-text-muted">Edges</div>
              </div>
              <div className="flex-1 text-center p-2 bg-elevated rounded border border-border-subtle">
                <div className="font-bold text-text-primary">
                  {cfgData.edges.length - cfgData.blocks.length + 2}
                </div>
                <div className="text-text-muted">Cyclomatic</div>
              </div>
            </div>

            {cfgData.blocks.length === 0 ? (
              <div className="text-xs text-text-muted text-center py-8">No CFG blocks available</div>
            ) : (
              /* Block list */
              <div className="space-y-2">
                {cfgData.blocks.map((block) => {
                  const outs = outEdges(block.index);
                  const ins = inEdges(block.index);
                  const isSelected = selectedBlock === block.index;

                  return (
                    <div key={block.index}>
                      <div
                        className={`p-2.5 rounded-lg border cursor-pointer transition-all ${getBlockColor(block)} ${isSelected ? 'ring-1 ring-accent' : ''
                          }`}
                        onClick={() => setSelectedBlock(isSelected ? null : block.index)}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            <span className="text-xs font-mono opacity-50 shrink-0">#{block.index}</span>
                            <span className="text-xs font-medium capitalize">{block.type}</span>
                            {block.start_line > 0 && (
                              <span className="text-xs opacity-60">
                                L{block.start_line}
                                {block.end_line > block.start_line ? `–${block.end_line}` : ''}
                              </span>
                            )}
                          </div>
                          <div className="flex items-center gap-1.5 text-xs opacity-60 shrink-0">
                            {ins.length > 0 && <span>↓{ins.length}</span>}
                            {outs.length > 0 && <span>↑{outs.length}</span>}
                          </div>
                        </div>

                        {/* Text preview */}
                        {block.text && (
                          <div className="mt-1.5 text-xs font-mono opacity-60 truncate">{block.text.slice(0, 60)}</div>
                        )}

                        {/* Expanded: show edges */}
                        {isSelected && outs.length > 0 && (
                          <div className="mt-2 pt-2 border-t border-current/20 space-y-1">
                            {outs.map((e, i) => (
                              <div key={i} className="flex items-center gap-1.5 text-xs">
                                <ArrowDown className="w-3 h-3 shrink-0" />
                                <ArrowRight className={`w-3 h-3 shrink-0 ${EDGE_COLORS[e.kind] || 'text-text-muted'}`} />
                                <span className={`font-medium ${EDGE_COLORS[e.kind] || 'text-text-muted'}`}>{e.kind}</span>
                                <span className="opacity-60">→ block #{e.target}</span>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>

                      {/* Edge connector lines between blocks */}
                      {block.index < cfgData.blocks.length - 1 && outs.some(e => e.target === block.index + 1 && e.kind === 'sequential') && (
                        <div className="flex justify-center">
                          <div className="w-px h-3 bg-border-subtle" />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
});