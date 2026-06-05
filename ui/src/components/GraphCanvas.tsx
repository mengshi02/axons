import React, { useEffect, useRef, useMemo } from 'react';
import { useAppState } from '../hooks/useAppState';
import { useSigma } from '../hooks/useSigma';
import { useFPSMonitor } from '../hooks/useFPSMonitor';
import { knowledgeGraphToGraphology, getAutoDegradationLevel, assignCommunityClusterPositions, applyTreeLayout, applyCirclesLayout } from '../lib/graph-adapter';
import { NODE_COLORS, type NodeLabel, type KnowledgeGraph } from '../types/graph';
import { pluginEventBus } from '../lib/pluginEventBus';

interface GraphCanvasProps {
  onNodeClick?: (nodeId: string) => void;
}

export const GraphCanvas = React.memo(function GraphCanvas({ onNodeClick }: GraphCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { graph: graphData, isLoading, selectedNode, setSelectedNode, openCodePanel, pendingDelta, applyDelta, applyDeltaToKnowledgeGraph, setVisibleLabels, layoutMode, setLayoutMode } = useAppState();

  // FPS monitoring — only enabled when graph is displayed
  const hasGraph = !!graphData && graphData.nodes.length > 0;
  const { fps, isLowFPS, dismissWarning } = useFPSMonitor(15, 3000, hasGraph);

  const {
    sigma,
    graph: graphologyGraph,
    setGraph,
    mergeGraph,
    initSigma,
    destroySigma,
    startLayout,
    stopLayout,
    killLayout,
    highlightNode,
    clearHighlight,
    showAllEdges,
    setShowAllEdges,
  } = useSigma({
    onNodeClick: (nodeId) => {
      setSelectedNode(nodeId);
      openCodePanel(); // 打开代码面板
      pluginEventBus.emit('node:selected', { nodeId }, 'builtin:graph'); // 通知插件
      onNodeClick?.(nodeId);
    },
    onStageClick: () => {
      setSelectedNode(null);
    },
  });

  // Compute legend items based on actual node types in the graph data
  const legendItems = useMemo(() => {
    if (!graphData || !graphData.nodes || graphData.nodes.length === 0) return [];

    // Extract unique labels from graph data
    const labelSet = new Set<NodeLabel>();
    graphData.nodes.forEach((node: { label: NodeLabel }) => {
      labelSet.add(node.label);
    });
    const uniqueLabels = Array.from(labelSet);

    // Sort labels by a predefined order for consistent display
    const labelOrder: NodeLabel[] = [
      'Project', 'Package', 'Module', 'Folder', 'File',
      'Class', 'Interface', 'Struct', 'Enum', 'Trait', 'Type',
      'Function', 'Method', 'Variable', 'Constant', 'Property', 'Field',
      'Import', 'Decorator', 'CodeElement', 'Community', 'Process'
    ];

    const sortedLabels = uniqueLabels.sort((a, b) => {
      const indexA = labelOrder.indexOf(a);
      const indexB = labelOrder.indexOf(b);
      if (indexA === -1 && indexB === -1) return a.localeCompare(b);
      if (indexA === -1) return 1;
      if (indexB === -1) return -1;
      return indexA - indexB;
    });

    return sortedLabels.map(label => ({
      label,
      color: NODE_COLORS[label] || '#9ca3af',
    }));
  }, [graphData]);

  // Initialize Sigma when container is ready
  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    // Check if sigma is already initialized (via the sigma state)
    if (sigma) {
      return;
    }

    try {
      initSigma(container);
    } catch (error) {
      console.error('[GraphCanvas] Failed to initialize Sigma:', error);
    }
  }, [sigma, initSigma]);

  // Cleanup on unmount only
  useEffect(() => {
    return () => {
      try {
        destroySigma();
      } catch (error) {
        console.error('[GraphCanvas] Error destroying Sigma:', error);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Track previous graph data to detect actual changes
  const prevGraphDataRef = useRef<KnowledgeGraph | null>(null);
  // Track pending graph data that needs to be rendered when sigma initializes
  const pendingGraphDataRef = useRef<KnowledgeGraph | null>(null);
  // Flag to skip full rebuild when KnowledgeGraph is updated by delta sync
  const skipFullRebuildRef = useRef(false);

  // Update graph data when graphData changes
  useEffect(() => {
    // Skip if no graph data
    if (!graphData || !graphData.nodes || graphData.nodes.length === 0) {
      return;
    }

    // Skip if graph data hasn't actually changed (compare by reference)
    if (prevGraphDataRef.current === graphData) {
      return;
    }

    // Skip full rebuild if this update was triggered by delta sync
    if (skipFullRebuildRef.current) {
      skipFullRebuildRef.current = false;
      prevGraphDataRef.current = graphData;
      return;
    }

    // If sigma is not initialized yet, store the graph data for later
    if (!sigma) {
      pendingGraphDataRef.current = graphData;
      prevGraphDataRef.current = graphData; // Mark as seen to avoid duplicate processing
      return;
    }

    try {
      // Convert knowledge graph to graphology format
      const graphologyGraph = knowledgeGraphToGraphology(graphData);

      // Apply layout based on selected mode
      if (layoutMode === 'tree') {
        applyTreeLayout(graphologyGraph);
      } else if (layoutMode === 'circles') {
        applyCirclesLayout(graphologyGraph);
      } else {
        // Force layout: apply community-based cluster initial positions for faster FA2 convergence
        if (graphologyGraph.order > 500) {
          assignCommunityClusterPositions(graphologyGraph);
        }
      }

      // Auto-degradation: automatically reduce visible labels for large graphs
      const nodeCount = graphologyGraph.order;
      const degradation = getAutoDegradationLevel(nodeCount);
      if (degradation.level !== 'full') {
        setVisibleLabels(degradation.visibleLabels);
      }

      // Use setGraph to properly update sigma with new graph
      setGraph(graphologyGraph);

      // Update ref to track current graph data
      prevGraphDataRef.current = graphData;
      pendingGraphDataRef.current = null;
    } catch (error) {
      console.error('[GraphCanvas] Failed to update graph:', error);
    }
  }, [sigma, graphData, setGraph]);

  // Process incremental delta updates (non-blocking, preserves camera/layout)
  useEffect(() => {
    if (!pendingDelta || !sigma) return;

    const result = mergeGraph(pendingDelta);

    if (result === null) {
      // Delta signals full rebuild needed - fall back to full graph reload
      // Clear delta and let the existing graphData flow handle it
      applyDelta(null);
      return;
    }

    // Sync the delta to KnowledgeGraph state so file tree, search, etc. see updates
    // Set flag to prevent the graphData change from triggering a full rebuild
    skipFullRebuildRef.current = true;
    applyDeltaToKnowledgeGraph(pendingDelta);

    // Clear the pending delta after processing
    applyDelta(null);
  }, [pendingDelta, sigma, mergeGraph, applyDelta, applyDeltaToKnowledgeGraph]);

  // Process pending graph data when sigma initializes
  useEffect(() => {
    if (!sigma) {
      return;
    }

    if (!pendingGraphDataRef.current) {
      return;
    }

    const pendingData = pendingGraphDataRef.current;

    try {
      const graphologyGraph = knowledgeGraphToGraphology(pendingData);

      setGraph(graphologyGraph);

      prevGraphDataRef.current = pendingData;
      pendingGraphDataRef.current = null;
    } catch (error) {
      console.error('[GraphCanvas] Failed to update pending graph:', error);
    }
  }, [sigma, setGraph]);

  // Switch layout when layoutMode changes (without rebuilding the graph)
  useEffect(() => {
    const currentGraph = graphologyGraph;
    if (!currentGraph || currentGraph.order === 0 || !sigma) return;

    // Kill any running FA2 layout (skip expensive noverlap since new layout will reposition all nodes)
    killLayout();

    if (layoutMode === 'tree') {
      applyTreeLayout(currentGraph);
      sigma.refresh();
    } else if (layoutMode === 'circles') {
      applyCirclesLayout(currentGraph);
      sigma.refresh();
    } else {
      // Force: restart FA2 layout
      // After tree/circles layouts, nodes sit in very regular positions where
      // FA2 forces are nearly zero, making it appear stuck.
      // Re-scatter nodes randomly so FA2 has meaningful forces to converge from.
      const scale = Math.sqrt(currentGraph.order) * 40;
      currentGraph.forEachNode((nodeId) => {
        currentGraph.setNodeAttribute(nodeId, 'x', (Math.random() - 0.5) * scale);
        currentGraph.setNodeAttribute(nodeId, 'y', (Math.random() - 0.5) * scale);
      });
      startLayout();
    }
  }, [layoutMode]); // eslint-disable-line react-hooks/exhaustive-deps

  // Highlight selected node
  useEffect(() => {
    if (selectedNode) {
      highlightNode(selectedNode);
    } else {
      clearHighlight();
    }
  }, [selectedNode, highlightNode, clearHighlight]);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full bg-deep">
        <div className="flex flex-col items-center gap-4">
          <div className="w-12 h-12 border-4 border-accent border-t-transparent rounded-full animate-spin" />
          <span className="text-text-secondary">Loading graph...</span>
        </div>
      </div>
    );
  }

  if (!graphData || graphData.nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-full bg-deep">
        <div className="text-center">
          <div className="text-6xl mb-4">📊</div>
          <h3 className="text-lg font-medium text-text-primary mb-2">No Graph Data</h3>
          <p className="text-text-secondary text-sm">
            Import a repository to visualize its code structure
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="relative h-full w-full bg-deep">
      <div ref={containerRef} className="h-full w-full" />

      {/* Toolbar: controls in one horizontal row */}
      {hasGraph && (
        <div className="absolute top-4 left-4 flex items-center gap-1.5">
          {/* Reset view */}
          <button
            onClick={() => sigma?.getCamera().animatedReset()}
            className="p-2 bg-surface border border-border-subtle rounded-lg text-text-secondary hover:text-text-primary hover:bg-hover transition-colors"
            title="Reset view"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
            </svg>
          </button>

          {/* Stop layout */}
          <button
            onClick={() => stopLayout()}
            className="p-2 bg-surface border border-border-subtle rounded-lg text-text-secondary hover:text-text-primary hover:bg-hover transition-colors"
            title="Stop layout"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 10a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1v-4z" />
            </svg>
          </button>

          {/* Start layout */}
          <button
            onClick={() => startLayout()}
            className="p-2 bg-surface border border-border-subtle rounded-lg text-text-secondary hover:text-text-primary hover:bg-hover transition-colors"
            title="Start layout"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </button>

          {/* Divider */}
          <div className="w-px h-5 bg-border-subtle mx-0.5" />

          {/* Force layout - network/nodes icon */}
          <button
            onClick={() => setLayoutMode('force')}
            className={`p-2 rounded-lg transition-colors ${layoutMode === 'force' ? 'bg-accent text-white' : 'bg-surface border border-border-subtle text-text-secondary hover:text-text-primary hover:bg-hover'}`}
            title="Force layout"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <circle cx="12" cy="5" r="2" strokeWidth={2} />
              <circle cx="5" cy="19" r="2" strokeWidth={2} />
              <circle cx="19" cy="19" r="2" strokeWidth={2} />
              <line x1="12" y1="7" x2="5" y2="17" strokeWidth={1.5} />
              <line x1="12" y1="7" x2="19" y2="17" strokeWidth={1.5} />
              <line x1="7" y1="19" x2="17" y2="19" strokeWidth={1.5} />
            </svg>
          </button>

          {/* Tree layout - hierarchy icon */}
          <button
            onClick={() => setLayoutMode('tree')}
            className={`p-2 rounded-lg transition-colors ${layoutMode === 'tree' ? 'bg-accent text-white' : 'bg-surface border border-border-subtle text-text-secondary hover:text-text-primary hover:bg-hover'}`}
            title="Tree layout"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v4m0 0H8m4 0h4M6 12h4m4 0h4M12 8v4m0 0v4m0 0H8m4 0h4" />
              <circle cx="12" cy="4" r="1.5" fill="currentColor" />
              <circle cx="4" cy="12" r="1.5" fill="currentColor" />
              <circle cx="20" cy="12" r="1.5" fill="currentColor" />
              <circle cx="8" cy="20" r="1.5" fill="currentColor" />
              <circle cx="16" cy="20" r="1.5" fill="currentColor" />
            </svg>
          </button>

          {/* Circles layout - concentric circles icon */}
          <button
            onClick={() => setLayoutMode('circles')}
            className={`p-2 rounded-lg transition-colors ${layoutMode === 'circles' ? 'bg-accent text-white' : 'bg-surface border border-border-subtle text-text-secondary hover:text-text-primary hover:bg-hover'}`}
            title="Circles layout"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <circle cx="12" cy="12" r="9" strokeWidth={1.5} />
              <circle cx="12" cy="12" r="5.5" strokeWidth={1.5} />
              <circle cx="12" cy="12" r="2" fill="currentColor" />
            </svg>
          </button>

          {/* Divider */}
          <div className="w-px h-5 bg-border-subtle mx-0.5" />

          {/* Edge rendering mode toggle */}
          <button
            onClick={() => setShowAllEdges(!showAllEdges)}
            className={`p-2 rounded-lg transition-colors ${showAllEdges ? 'bg-accent text-white' : 'bg-surface border border-border-subtle text-text-secondary hover:text-text-primary hover:bg-hover'}`}
            title={showAllEdges ? 'Show all edges (click to show only selected node edges)' : 'Show only selected node edges (click to show all)'}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              {showAllEdges ? (
                <>
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.172 13.828a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.102 1.101" />
                </>
              ) : (
                <>
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101" />
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.172 13.828a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.102 1.101" />
                  <line x1="4" y1="4" x2="20" y2="20" strokeWidth={2} />
                </>
              )}
            </svg>
          </button>
        </div>
      )}

      {/* FPS indicator + Legend - top right */}
      <div className="absolute top-4 right-4 flex flex-col gap-2 items-end">
        {/* FPS indicator */}
        {hasGraph && (
          <div className="px-2 py-1 bg-surface/80 border border-border-subtle rounded text-xs text-text-muted tabular-nums">
            {fps} FPS
          </div>
        )}

        {/* Legend - Dynamic based on actual node types in graph */}
        {legendItems.length > 0 && (
          <div className="p-3 bg-surface border border-border-subtle rounded-lg shadow-lg">
            <div className="text-xs font-medium text-text-primary mb-2">Legend</div>
            <div className="flex flex-col gap-1.5 text-xs">
              {legendItems.map(({ label, color }) => (
                <div key={label} className="flex items-center gap-2">
                  <span className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
                  <span className="text-text-secondary">{label}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>



      {/* Low-FPS degradation warning */}
      {isLowFPS && (
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 p-4 bg-surface border border-warning rounded-lg shadow-xl z-50 max-w-md">
          <div className="text-sm font-medium text-text-primary mb-2">
            Performance Warning
          </div>
          <div className="text-xs text-text-secondary mb-3">
            The graph is rendering slowly ({fps} FPS). Consider switching to a lower granularity view
            for better performance.
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => {
                // Switch to class-level view
                const classLabels: NodeLabel[] = ['Project', 'Package', 'Module', 'Folder', 'File', 'Class', 'Interface', 'Enum', 'Struct', 'Trait', 'Record', 'Type'];
                setVisibleLabels(classLabels);
                dismissWarning();
              }}
              className="px-3 py-1.5 bg-accent text-white rounded text-xs hover:bg-accent/80 transition-colors"
            >
              Switch to Class View
            </button>
            <button
              onClick={() => {
                // Switch to structure view
                const structLabels: NodeLabel[] = ['Project', 'Package', 'Module', 'Folder', 'File'];
                setVisibleLabels(structLabels);
                dismissWarning();
              }}
              className="px-3 py-1.5 bg-warning text-white rounded text-xs hover:bg-warning/80 transition-colors"
            >
              Switch to Structure View
            </button>
            <button
              onClick={dismissWarning}
              className="px-3 py-1.5 bg-bg-secondary text-text-secondary rounded text-xs hover:bg-hover transition-colors"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}
    </div>
  );
});