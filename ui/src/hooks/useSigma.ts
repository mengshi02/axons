import { useRef, useEffect, useCallback, useState } from 'react';
import Sigma from 'sigma';
import Graph from 'graphology';
import FA2Layout from 'graphology-layout-forceatlas2/worker';
import forceAtlas2 from 'graphology-layout-forceatlas2';
import NoverlapWorker from 'graphology-layout-noverlap/worker';
import EdgeCurveProgram from '@sigma/edge-curve';
import type { SigmaNodeAttributes, SigmaEdgeAttributes, EdgeType } from '../types/graph';
import { dimColor, brightenColor, mergeGraphDelta, type MergeDeltaResult } from '../lib/graph-adapter';
import type { GraphDeltaResponse } from '../services/api';
import { useTheme } from './useTheme';

interface UseSigmaOptions {
  onNodeClick?: (nodeId: string) => void;
  onNodeHover?: (nodeId: string | null) => void;
  onStageClick?: () => void;
  highlightedNodeIds?: Set<string>;
  selectedNodeId?: string | null;
  visibleEdgeTypes?: EdgeType[];
}

interface UseSigmaReturn {
  containerRef: React.RefObject<HTMLDivElement | null>;
  sigma: Sigma | null;
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes> | null;
  setGraph: (graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>) => void;
  mergeGraph: (delta: GraphDeltaResponse) => MergeDeltaResult | null;
  zoomIn: () => void;
  zoomOut: () => void;
  resetZoom: () => void;
  focusNode: (nodeId: string) => void;
  highlightNode: (nodeId: string) => void;
  clearHighlight: () => void;
  isLayoutRunning: boolean;
  startLayout: () => void;
  stopLayout: () => void;
  killLayout: () => void;
  initSigma: (container: HTMLDivElement) => void;
  destroySigma: () => void;
  selectedNode: string | null;
  setSelectedNode: (nodeId: string | null) => void;
  refresh: () => void;
  showAllEdges: boolean;
  setShowAllEdges: (show: boolean) => void;
}

const NOVERLAP_SETTINGS = {
  maxIterations: 20,
  ratio: 1.1,
  margin: 10,
  expansion: 1.05,
};

/**
 * Run a localized noverlap pass — only adjusts positions of new nodes and
 * their immediate neighbors.  This is far cheaper than running noverlap on
 * the entire graph (O(K²) vs O(N²) where K << N).
 */
function localNoverlap(
  graph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>,
  newNodeIds: string[]
): void {
  // Collect the set of nodes that should participate: new nodes + their neighbors
  const participantIds = new Set<string>();
  for (const nodeId of newNodeIds) {
    if (!graph.hasNode(nodeId)) continue;
    participantIds.add(nodeId);
    graph.forEachNeighbor(nodeId, (neighborId) => {
      participantIds.add(neighborId);
    });
  }

  if (participantIds.size < 2) return;

  // Simple iterative repulsion: push overlapping nodes apart
  // Only among participants, keeping non-participants fixed
  const positions = new Map<string, { x: number; y: number; size: number }>();
  for (const id of participantIds) {
    const attrs = graph.getNodeAttributes(id);
    positions.set(id, { x: attrs.x, y: attrs.y, size: attrs.size || 8 });
  }

  const ratio = NOVERLAP_SETTINGS.ratio;
  const margin = NOVERLAP_SETTINGS.margin;
  const maxIter = 10; // short pass

  for (let iter = 0; iter < maxIter; iter++) {
    let moved = false;
    // Only move new nodes; neighbors act as fixed obstacles
    for (const nodeId of newNodeIds) {
      const pos = positions.get(nodeId);
      if (!pos) continue;

      let dx = 0;
      let dy = 0;

      for (const otherId of participantIds) {
        if (otherId === nodeId) continue;
        const other = positions.get(otherId);
        if (!other) continue;

        const diffX = pos.x - other.x;
        const diffY = pos.y - other.y;
        const dist = Math.sqrt(diffX * diffX + diffY * diffY) || 0.01;
        const minDist = (pos.size + other.size) * ratio + margin;

        if (dist < minDist) {
          const force = (minDist - dist) / dist * 0.5;
          dx += diffX * force;
          dy += diffY * force;
        }
      }

      if (Math.abs(dx) > 0.01 || Math.abs(dy) > 0.01) {
        pos.x += dx;
        pos.y += dy;
        moved = true;
      }
    }

    if (!moved) break; // converged
  }

  // Write positions back to the graph
  for (const nodeId of newNodeIds) {
    const pos = positions.get(nodeId);
    if (!pos || !graph.hasNode(nodeId)) continue;
    graph.mergeNodeAttributes(nodeId, { x: pos.x, y: pos.y });
  }
}

const getFA2Settings = (nodeCount: number) => {
  const isSmall = nodeCount < 500;
  const isMedium = nodeCount >= 500 && nodeCount < 2000;
  const isLarge = nodeCount >= 2000 && nodeCount < 10000;

  return {
    gravity: isSmall ? 0.8 : isMedium ? 0.5 : isLarge ? 0.3 : 0.15,
    scalingRatio: isSmall ? 15 : isMedium ? 30 : isLarge ? 60 : 100,
    slowDown: isSmall ? 1 : isMedium ? 2 : isLarge ? 3 : 5,
    barnesHutOptimize: nodeCount > 200,
    barnesHutTheta: isLarge ? 0.8 : 0.6,
    strongGravityMode: false,
    outboundAttractionDistribution: true,
    linLogMode: false,
    adjustSizes: true,
    edgeWeightInfluence: 1,
  };
};

const getLayoutDuration = (nodeCount: number): number => {
  if (nodeCount > 10000) return 45000;
  if (nodeCount > 5000) return 35000;
  if (nodeCount > 2000) return 30000;
  if (nodeCount > 1000) return 30000;
  if (nodeCount > 500) return 25000;
  return 20000;
};

export function useSigma(options: UseSigmaOptions = {}): UseSigmaReturn {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const sigmaRef = useRef<Sigma | null>(null);
  const graphRef = useRef<Graph<SigmaNodeAttributes, SigmaEdgeAttributes> | null>(null);
  const layoutRef = useRef<FA2Layout | null>(null);
  const selectedNodeRef = useRef<string | null>(null);
  const highlightedRef = useRef<Set<string>>(new Set());
  const visibleEdgeTypesRef = useRef<EdgeType[] | null>(null);
  const layoutTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const noverlapWorkerRef = useRef<NoverlapWorker | null>(null);
  // Index: backend edgeId → graphology edge key, for O(1) removal lookups
  const edgeIdIndexRef = useRef<Map<string, string>>(new Map());

  // Edge rendering mode: when false, only show edges connected to the selected node
  // This dramatically reduces GPU load for large graphs by avoiding rendering all edges
  const showAllEdgesRef = useRef(false);
  const [showAllEdges, setShowAllEdgesState] = useState(false);

  // Debounce for node click events to prevent rapid clicking
  const lastClickTimeRef = useRef<number>(0);
  const CLICK_DEBOUNCE_MS = 300; // 300ms debounce

  // LOD (Level of Detail) — zoom-based rendering control
  // camera.ratio: < 0.3 = far, 0.3-1.5 = mid, > 1.5 = near
  const lodLevelRef = useRef<'far' | 'mid' | 'near'>('mid');

  const [isLayoutRunning, setIsLayoutRunning] = useState(false);
  const [selectedNode, setSelectedNodeState] = useState<string | null>(null);
  const [sigma, setSigma] = useState<Sigma | null>(null);
  const [graph, setGraphState] = useState<Graph<SigmaNodeAttributes, SigmaEdgeAttributes> | null>(null);

  // Get theme and store in ref for use in reducers
  const { theme } = useTheme();
  const isDarkThemeRef = useRef(theme === 'moon');
  isDarkThemeRef.current = theme === 'moon';

  // Update refs when options change
  useEffect(() => {
    highlightedRef.current = options.highlightedNodeIds || new Set();
    visibleEdgeTypesRef.current = options.visibleEdgeTypes || null;
    sigmaRef.current?.refresh();
  }, [options.highlightedNodeIds, options.visibleEdgeTypes]);

  // Refresh graph when theme changes
  useEffect(() => {
    sigmaRef.current?.refresh();
  }, [theme]);

  const setSelectedNode = useCallback((nodeId: string | null) => {
    // Skip if same node (avoid unnecessary updates)
    if (selectedNodeRef.current === nodeId) {
      return;
    }

    selectedNodeRef.current = nodeId;
    setSelectedNodeState(nodeId);

    const sigmaInstance = sigmaRef.current;
    if (!sigmaInstance) return;

    // Use requestAnimationFrame for smoother updates
    requestAnimationFrame(() => {
      const camera = sigmaInstance.getCamera();
      camera.animate({ ratio: camera.ratio * 1.0001 }, { duration: 50 });
      sigmaInstance.refresh();
    });
  }, []);

  const initSigma = useCallback((container: HTMLDivElement) => {
    if (sigmaRef.current) {
      return;
    }

    containerRef.current = container;

    // ── WebGL support detection ──
    // Sigma.js requires WebGL. If unavailable, show a warning.
    const canvas = document.createElement('canvas');
    let webglSupported = false;
    try {
      const gl = canvas.getContext('webgl2') || canvas.getContext('webgl');
      webglSupported = !!gl;
    } catch {
      webglSupported = false;
    }

    if (!webglSupported) {
      console.error('[useSigma] WebGL is not supported in this browser/environment.');
      container.innerHTML = `
        <div style="display:flex;align-items:center;justify-content:center;height:100%;color:#94a3b8;font-family:sans-serif;text-align:center;padding:2rem;">
          <div>
            <h3 style="margin:0 0 0.5rem 0;color:#ef4444;">WebGL Not Available</h3>
            <p style="margin:0;font-size:0.875rem;">The graph visualization requires WebGL support.<br/>
            Please try a different browser or enable hardware acceleration.</p>
          </div>
        </div>
      `;
      return;
    }

    const graphInstance = new Graph<SigmaNodeAttributes, SigmaEdgeAttributes>();
    graphRef.current = graphInstance;
    setGraphState(graphInstance);

    const sigmaInstance = new Sigma(graphInstance, container, {
      renderLabels: true,
      labelFont: 'JetBrains Mono, monospace',
      labelSize: 11,
      labelWeight: '500',
      labelColor: { attribute: 'labelColor' }, // Use node's labelColor attribute
      labelRenderedSizeThreshold: 8,
      labelDensity: 0.1,
      labelGridCellSize: 70,

      defaultNodeColor: '#6b7280',
      defaultEdgeColor: '#2a2a3a',

      defaultEdgeType: 'curved',
      edgeProgramClasses: {
        curved: EdgeCurveProgram,
      },

      minCameraRatio: 0.002,
      maxCameraRatio: 50,
      hideEdgesOnMove: true,
      zIndex: true,
      allowInvalidContainer: true, // Allow initialization even when container has no width

      nodeReducer: (node, data) => {
        const res = { ...data };

        if (data.hidden) {
          res.hidden = true;
          return res;
        }

        // LOD: hide labels for small/unimportant nodes when zoomed out
        const lod = lodLevelRef.current;
        const nodeType = data.nodeType as string;
        if (lod === 'far') {
          // Far: only show labels for Class/Interface/Struct/Module/File
          const showLabelTypes = ['Class', 'Interface', 'Struct', 'Module', 'File', 'Package', 'Project'];
          if (!showLabelTypes.includes(nodeType)) {
            res.label = ''; // Hide label
          }
        } else if (lod === 'mid') {
          // Mid: show labels for all except Variable/Parameter/Field/Constant
          const hideLabelTypes = ['Variable', 'Parameter', 'Field', 'Constant', 'Import'];
          if (hideLabelTypes.includes(nodeType)) {
            res.label = '';
          }
        }
        // Near: show all labels (default)

        // Default label color - use dark text for better contrast on hover label
        res.labelColor = '#1e293b';

        const currentSelected = selectedNodeRef.current;
        const highlighted = highlightedRef.current;
        const hasHighlights = highlighted.size > 0;
        const isHighlighted = highlighted.has(node);

        if (hasHighlights && !currentSelected) {
          if (isHighlighted) {
            res.color = '#06b6d4';
            res.size = (data.size || 8) * 1.6;
            res.zIndex = 2;
            res.highlighted = true;
            res.labelColor = '#1a1a2e'; // Dark label for highlighted node (white background)
          } else {
            res.color = dimColor(data.color, 0.2, isDarkThemeRef.current);
            res.size = (data.size || 8) * 0.5;
            res.zIndex = 0;
          }
          return res;
        }

        if (currentSelected) {
          const currentGraph = graphRef.current;
          if (currentGraph) {
            const isSelected = node === currentSelected;
            const isNeighbor = currentGraph.hasEdge(node, currentSelected) || currentGraph.hasEdge(currentSelected, node);

            if (isSelected) {
              res.color = data.color;
              res.size = (data.size || 8) * 1.8;
              res.zIndex = 2;
              res.highlighted = true;
              res.labelColor = '#1a1a2e'; // Dark label for highlighted node (white background)
            } else if (isNeighbor) {
              res.color = data.color;
              res.size = (data.size || 8) * 1.3;
              res.zIndex = 1;
            } else {
              res.color = dimColor(data.color, 0.25, isDarkThemeRef.current);
              res.size = (data.size || 8) * 0.6;
              res.zIndex = 0;
            }
          }
        }

        return res;
      },

      edgeReducer: (edge, data) => {
        const res = { ...data };

        // ── On-demand edge rendering ──
        // When showAllEdges is false (default for large graphs), only render edges
        // connected to the currently selected or highlighted node(s). This avoids
        // the massive GPU cost of drawing thousands of curved edges simultaneously.
        if (!showAllEdgesRef.current) {
          const currentSelected = selectedNodeRef.current;
          const highlighted = highlightedRef.current;
          const hasHighlights = highlighted.size > 0;
          const currentGraph = graphRef.current;

          if (!currentSelected && !hasHighlights) {
            // No selection & no highlight → hide all edges
            res.hidden = true;
            return res;
          }

          if (currentGraph) {
            const [source, target] = currentGraph.extremities(edge);
            let shouldShow = false;

            if (currentSelected) {
              shouldShow = source === currentSelected || target === currentSelected;
            }

            if (!shouldShow && hasHighlights) {
              shouldShow = highlighted.has(source) || highlighted.has(target);
            }

            if (!shouldShow) {
              res.hidden = true;
              return res;
            }
          }
        }

        // LOD: hide most edges when zoomed out for performance
        const lod = lodLevelRef.current;
        if (lod === 'far') {
          // Far: only show IMPORTS and CONTAINS edges (structural relationships)
          const relationType = data.relationType as string;
          const visibleInFar = ['IMPORTS', 'CONTAINS', 'DEFINES'];
          if (!visibleInFar.includes(relationType)) {
            res.hidden = true;
            return res;
          }
          // Simplify edge rendering in far mode — keep curved but reduce size
          res.size = Math.max(0.1, (data.size || 1) * 0.5);
        } else if (lod === 'mid') {
          // Mid: show IMPORTS, CONTAINS, DEFINES, CALLS edges
          const relationType = data.relationType as string;
          const hiddenInMid = ['DATAFLOW', 'DEPENDS_ON', 'INHERITS', 'IMPLEMENTED_BY'];
          if (hiddenInMid.includes(relationType)) {
            res.hidden = true;
            return res;
          }
        }
        // Near: show all edges (default)

        const visibleTypes = visibleEdgeTypesRef.current;
        if (visibleTypes && data.relationType) {
          if (!visibleTypes.includes(data.relationType as EdgeType)) {
            res.hidden = true;
            return res;
          }
        }

        const currentSelected = selectedNodeRef.current;
        const highlighted = highlightedRef.current;
        const hasHighlights = highlighted.size > 0;

        if (hasHighlights && !currentSelected) {
          const currentGraph = graphRef.current;
          if (currentGraph) {
            const [source, target] = currentGraph.extremities(edge);
            const bothHighlighted = highlighted.has(source) && highlighted.has(target);
            const oneHighlighted = highlighted.has(source) || highlighted.has(target);

            if (bothHighlighted) {
              res.color = '#06b6d4';
              res.size = Math.max(2, (data.size || 1) * 3);
              res.zIndex = 2;
            } else if (oneHighlighted) {
              res.color = dimColor('#06b6d4', 0.4, isDarkThemeRef.current);
              res.size = 1;
              res.zIndex = 1;
            } else {
              res.color = dimColor(data.color, 0.08, isDarkThemeRef.current);
              res.size = 0.2;
              res.zIndex = 0;
            }
          }
          return res;
        }

        if (currentSelected) {
          const currentGraph = graphRef.current;
          if (currentGraph) {
            const [source, target] = currentGraph.extremities(edge);
            const isConnected = source === currentSelected || target === currentSelected;

            if (isConnected) {
              res.color = brightenColor(data.color, 1.5);
              res.size = Math.max(3, (data.size || 1) * 4);
              res.zIndex = 2;
            } else {
              res.color = dimColor(data.color, 0.1, isDarkThemeRef.current);
              res.size = 0.3;
              res.zIndex = 0;
            }
          }
        }

        return res;
      },
    });

    sigmaRef.current = sigmaInstance;
    setSigma(sigmaInstance);

    sigmaInstance.on('clickNode', ({ node }) => {
      // Debounce: ignore clicks that happen too quickly
      const now = Date.now();
      if (now - lastClickTimeRef.current < CLICK_DEBOUNCE_MS) {
        return;
      }
      lastClickTimeRef.current = now;

      setSelectedNode(node);
      options.onNodeClick?.(node);
    });

    sigmaInstance.on('clickStage', () => {
      setSelectedNode(null);
      options.onStageClick?.();
    });

    sigmaInstance.on('enterNode', ({ node }) => {
      options.onNodeHover?.(node);
      if (containerRef.current) {
        containerRef.current.style.cursor = 'pointer';
      }
    });

    sigmaInstance.on('leaveNode', () => {
      options.onNodeHover?.(null);
      if (containerRef.current) {
        containerRef.current.style.cursor = 'grab';
      }
    });

    // LOD: listen to camera changes to update detail level
    let lodRefreshTimer: ReturnType<typeof setTimeout> | null = null;
    const camera = sigmaInstance.getCamera();
    camera.on('updated', () => {
      // Debounce LOD refresh to avoid excessive re-renders
      if (lodRefreshTimer) clearTimeout(lodRefreshTimer);
      lodRefreshTimer = setTimeout(() => {
        const ratio = camera.ratio;
        let newLevel: 'far' | 'mid' | 'near';
        if (ratio > 1.5) {
        // Zoomed out (small ratio) — show less detail for performance
          newLevel = 'far';
        } else if (ratio >= 0.3) {
          // Default zoom — moderate detail
          newLevel = 'mid';
        } else {
          // Zoomed in (large ratio) — show all detail
          newLevel = 'near';
        }
        if (newLevel !== lodLevelRef.current) {
          lodLevelRef.current = newLevel;
          sigmaInstance.refresh();
        }
      }, 150);
    });
  }, [options, setSelectedNode]);

  const destroySigma = useCallback(() => {
    if (layoutTimeoutRef.current) {
      clearTimeout(layoutTimeoutRef.current);
      layoutTimeoutRef.current = null;
    }
    if (noverlapWorkerRef.current) {
      noverlapWorkerRef.current.kill();
      noverlapWorkerRef.current = null;
    }
    if (layoutRef.current) {
      layoutRef.current.kill();
      layoutRef.current = null;
    }
    if (sigmaRef.current) {
      try {
        // Kill the sigma instance which should clean up its DOM
        sigmaRef.current.kill();
      } catch (error) {
        console.error('[useSigma] Error killing sigma:', error);
      }
      sigmaRef.current = null;
    }
    graphRef.current = null;
    setSigma(null);
    setGraphState(null);
    console.log('[useSigma] destroySigma complete');
  }, []);

  const runLayout = useCallback((graphInstance: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>) => {
    const nodeCount = graphInstance.order;
    if (nodeCount === 0) {
      return;
    }

    try {
      if (layoutRef.current) {
        layoutRef.current.kill();
        layoutRef.current = null;
      }
      if (layoutTimeoutRef.current) {
        clearTimeout(layoutTimeoutRef.current);
        layoutTimeoutRef.current = null;
      }

      const inferredSettings = forceAtlas2.inferSettings(graphInstance);
      const customSettings = getFA2Settings(nodeCount);
      const settings = { ...inferredSettings, ...customSettings };

      const layout = new FA2Layout(graphInstance, { settings });

      layoutRef.current = layout;
      layout.start();
      setIsLayoutRunning(true);

      const duration = getLayoutDuration(nodeCount);

      layoutTimeoutRef.current = setTimeout(() => {
        if (layoutRef.current) {
          layoutRef.current.stop();
          layoutRef.current = null;

          // Run noverlap in a Web Worker to avoid blocking the main thread
          if (noverlapWorkerRef.current) {
            noverlapWorkerRef.current.kill();
            noverlapWorkerRef.current = null;
          }
          const noverlapLayout = new NoverlapWorker(graphInstance, {
            settings: NOVERLAP_SETTINGS,
            onConverged: () => {
              noverlapWorkerRef.current = null;
              sigmaRef.current?.refresh();
              setIsLayoutRunning(false);
            },
          });
          noverlapWorkerRef.current = noverlapLayout;
          noverlapLayout.start();
        }
      }, duration);
    } catch (error) {
      console.error('[useSigma] Layout error:', error);
    }
  }, []);

  const setGraph = useCallback((newGraph: Graph<SigmaNodeAttributes, SigmaEdgeAttributes>) => {
    const sigmaInstance = sigmaRef.current;
    if (!sigmaInstance) return;

    if (layoutRef.current) {
      layoutRef.current.kill();
      layoutRef.current = null;
    }
    if (noverlapWorkerRef.current) {
      noverlapWorkerRef.current.kill();
      noverlapWorkerRef.current = null;
    }
    if (layoutTimeoutRef.current) {
      clearTimeout(layoutTimeoutRef.current);
      layoutTimeoutRef.current = null;
    }

    graphRef.current = newGraph;

    // Build edgeId index for O(1) edge removal lookups during delta merges
    const index = new Map<string, string>();
    newGraph.forEachEdge((edgeKey, attrs) => {
      if (attrs.edgeId) {
        index.set(attrs.edgeId, edgeKey);
      }
    });
    edgeIdIndexRef.current = index;

    setGraphState(newGraph);
    sigmaInstance.setGraph(newGraph);
    setSelectedNode(null);

    runLayout(newGraph);
    sigmaInstance.getCamera().animatedReset({ duration: 500 });
  }, [runLayout, setSelectedNode]);

  /**
   * mergeGraph - Incrementally merge delta changes into the current graph.
   * Unlike setGraph (full replacement), this:
   * - Removes deleted nodes/edges in-place
   * - Adds new nodes near their neighbors (no global re-layout)
   * - Runs a short local noverlap pass to avoid overlaps
   * - Preserves camera position and existing node positions
   */
  const mergeGraph = useCallback((delta: GraphDeltaResponse): MergeDeltaResult | null => {
    const sigmaInstance = sigmaRef.current;
    const currentGraph = graphRef.current;
    if (!sigmaInstance || !currentGraph) return null;

    // If backend signals full rebuild is needed, return null to let caller handle it
    if (delta.is_full_rebuild) return null;

    // Apply delta to the existing graphology graph in-place
    // Pass edgeId index for O(1) edge removal lookups
    const result = mergeGraphDelta(currentGraph, delta, edgeIdIndexRef.current);

    // Run a localized noverlap pass on new nodes and their neighbors only.
    // This avoids O(N²) cost on the entire graph when only a few nodes were added.
    if (result.addedNodeCount > 0 && result.addedNodeIds.length > 0) {
      try {
        localNoverlap(currentGraph, result.addedNodeIds);
      } catch (e) {
        console.warn('[useSigma] local noverlap pass failed:', e);
      }
    }

    // Refresh sigma rendering (no re-layout, no camera reset)
    sigmaInstance.refresh();

    return result;
  }, []);

  const focusNode = useCallback((nodeId: string) => {
    const sigmaInstance = sigmaRef.current;
    const currentGraph = graphRef.current;
    if (!sigmaInstance || !currentGraph || !currentGraph.hasNode(nodeId)) return;

    const alreadySelected = selectedNodeRef.current === nodeId;

    selectedNodeRef.current = nodeId;
    setSelectedNodeState(nodeId);

    if (!alreadySelected) {
      const nodeAttrs = currentGraph.getNodeAttributes(nodeId);
      sigmaInstance.getCamera().animate(
        { x: nodeAttrs.x, y: nodeAttrs.y, ratio: 0.15 },
        { duration: 400 }
      );
    }

    sigmaInstance.refresh();
  }, []);

  const highlightNode = useCallback((nodeId: string) => {
    highlightedRef.current = new Set([nodeId]);
    sigmaRef.current?.refresh();
  }, []);

  const clearHighlight = useCallback(() => {
    highlightedRef.current = new Set();
    sigmaRef.current?.refresh();
  }, []);

  const zoomIn = useCallback(() => {
    sigmaRef.current?.getCamera().animatedZoom({ duration: 200 });
  }, []);

  const zoomOut = useCallback(() => {
    sigmaRef.current?.getCamera().animatedUnzoom({ duration: 200 });
  }, []);

  const resetZoom = useCallback(() => {
    sigmaRef.current?.getCamera().animatedReset({ duration: 300 });
    setSelectedNode(null);
  }, [setSelectedNode]);

  const startLayout = useCallback(() => {
    const currentGraph = graphRef.current;
    if (!currentGraph || currentGraph.order === 0) return;
    runLayout(currentGraph);
  }, [runLayout]);

  const stopLayout = useCallback(() => {
    if (layoutTimeoutRef.current) {
      clearTimeout(layoutTimeoutRef.current);
      layoutTimeoutRef.current = null;
    }
    if (noverlapWorkerRef.current) {
      noverlapWorkerRef.current.kill();
      noverlapWorkerRef.current = null;
    }
    if (layoutRef.current) {
      layoutRef.current.stop();
      layoutRef.current = null;

      // Run noverlap in a Web Worker to avoid blocking the main thread
      const currentGraph = graphRef.current;
      if (currentGraph) {
        const noverlapLayout = new NoverlapWorker(currentGraph, {
          settings: NOVERLAP_SETTINGS,
          onConverged: () => {
            noverlapWorkerRef.current = null;
            sigmaRef.current?.refresh();
          },
        });
        noverlapWorkerRef.current = noverlapLayout;
        noverlapLayout.start();
      }

      setIsLayoutRunning(false);
    }
  }, []);

  // Kill FA2 layout without running noverlap.
  // Use this when switching layouts — noverlap is expensive for large graphs
  // and unnecessary since the new layout will reposition all nodes anyway.
  const killLayout = useCallback(() => {
    if (layoutTimeoutRef.current) {
      clearTimeout(layoutTimeoutRef.current);
      layoutTimeoutRef.current = null;
    }
    if (noverlapWorkerRef.current) {
      noverlapWorkerRef.current.kill();
      noverlapWorkerRef.current = null;
    }
    if (layoutRef.current) {
      layoutRef.current.kill();
      layoutRef.current = null;
      setIsLayoutRunning(false);
    }
  }, []);

  const refresh = useCallback(() => {
    sigmaRef.current?.refresh();
  }, []);

  const setShowAllEdges = useCallback((show: boolean) => {
    showAllEdgesRef.current = show;
    setShowAllEdgesState(show);
    sigmaRef.current?.refresh();
  }, []);

  return {
    containerRef,
    sigma,
    graph,
    setGraph,
    mergeGraph,
    zoomIn,
    zoomOut,
    resetZoom,
    focusNode,
    highlightNode,
    clearHighlight,
    isLayoutRunning,
    startLayout,
    stopLayout,
    killLayout,
    initSigma,
    destroySigma,
    selectedNode,
    setSelectedNode,
    refresh,
    showAllEdges,
    setShowAllEdges,
  };
}