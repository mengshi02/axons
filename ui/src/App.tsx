import React, { useEffect, useCallback, useState, useRef, useMemo, Suspense, lazy } from 'react';
import { TopSearchBar } from './components/TopSearchBar';
import { ActivityBar } from './components/ActivityBar';
import { Footer } from './components/Footer';
const GraphCanvas = lazy(() => import('./components/GraphCanvas').then(m => ({ default: m.GraphCanvas })));
import { DropZone } from './components/DropZone';
import { BuildingState } from './components/BuildingState';
import { useAppState } from './hooks/useAppState';
import { useEventStream, type BuildCompleteEvent, type FileChangeEvent, type NotificationEvent } from './hooks/useEventStream';
import { useRecentPaths } from './hooks/useRecentPaths';
import { useNotifications } from './hooks/useNotifications';
import { NotificationToast } from './components/NotificationToast';
import { fetchGraph, fetchGraphDelta, createProject, startBuild, fetchTaskStatus } from './services/api';
import { IframePluginPanel } from './components/IframePluginPanel';
import { ErrorBoundary } from './components/ErrorBoundary';

function App() {
  const {
    graph,
    loading,
    setLoading,
    setSelectedNode,
    setActiveFilePath,
    loadProjects,
    projects,
    projectsLoaded,
    setCurrentProject,
    setGraph,
    currentProject,
    markProjectBuilding,
    buildingProjects,
    applyDelta,
    buildProgress,
    updateBuildProgress,
    clearBuildProgress,
    // Panel registry
    panelRegistry,
    openPanels,
    openPanel,
    closePanel,
    getPanelsByLocation,
    registryVersion,
  } = useAppState();

  const { addRecentPath } = useRecentPaths();

  // Notification state (shared between TopSearchBar and NotificationToast)
  const {
    notifications: notificationList,
    unreadCount,
    markAsRead,
    markAllAsRead,
    deleteNotification: deleteNotificationAction,
    handleNewNotification,
  } = useNotifications();
  const [isNotificationPanelOpen, setIsNotificationPanelOpen] = useState(false);

  // Debounce timer ref for file change events - accumulates changed files during debounce period
  const fileChangeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingChangedFilesRef = useRef<Set<string>>(new Set());
  const pendingRemovedFilesRef = useRef<Set<string>>(new Set());

  // Listen for build complete events to refresh graph
  useEventStream({
    onBuildProgress: updateBuildProgress,
    onFileChange: useCallback((data: FileChangeEvent) => {
      // Only process events for the current project
      if (currentProject && data.project_id !== currentProject.id) {
        return;
      }

      // Accumulate file changes — do NOT request delta here because
      // the backend incremental build may not have finished yet (edges
      // won't exist in the DB until BuildEdges completes).  The actual
      // delta request is triggered by the build_complete event.
      if (data.change_type === 'removed') {
        pendingRemovedFilesRef.current.add(data.file_path);
      } else {
        pendingChangedFilesRef.current.add(data.file_path);
      }
    }, [currentProject]),
    onBuildComplete: useCallback((data: BuildCompleteEvent) => {
      // Clear build progress (only if it belongs to current project)
      clearBuildProgress();

      // Mark project as no longer building
      markProjectBuilding(data.project_id, false);

      // Only process events for the current project
      if (currentProject && data.project_id !== currentProject.id) {
        return;
      }

      // If build_complete provides changed_files, use incremental delta
      const hasChangedFiles = data.changed_files && data.changed_files.length > 0;
      const hasRemovedFiles = data.removed_files && data.removed_files.length > 0;

      if (hasChangedFiles || hasRemovedFiles) {
        const fetchDelta = async () => {
          try {
            const delta = await fetchGraphDelta({
              projectId: data.project_id,
              changedFiles: data.changed_files || [],
              removedFiles: data.removed_files || [],
            });

            if (delta.is_full_rebuild) {
              // Fall back to full reload
              const graphData = await fetchGraph({
                projectId: data.project_id,
                filterConnected: true,
                includeStats: true,
              });
              setGraph(graphData);
              return;
            }

            // Merge old node/edge IDs from changed files into the delta's removed lists
            // The backend sends these via SSE because the old IDs are deleted from the DB
            // before the delta API can query them
            if (data.changed_file_old_node_ids?.length || data.changed_file_old_edge_ids?.length) {
              const oldNodeIds = data.changed_file_old_node_ids || [];
              const oldEdgeIds = data.changed_file_old_edge_ids || [];
              const existingRemovedNodeIds = new Set(delta.removed_node_ids);
              const existingRemovedEdgeIds = new Set(delta.removed_edge_ids);
              for (const id of oldNodeIds) {
                if (!existingRemovedNodeIds.has(id)) {
                  delta.removed_node_ids.push(id);
                }
              }
              for (const id of oldEdgeIds) {
                if (!existingRemovedEdgeIds.has(id)) {
                  delta.removed_edge_ids.push(id);
                }
              }
            }

            applyDelta(delta);
          } catch (err) {
            console.error('[App] Failed to fetch graph delta, falling back to full reload:', err);
            try {
              const graphData = await fetchGraph({
                projectId: data.project_id,
                filterConnected: true,
                includeStats: true,
              });
              setGraph(graphData);
            } catch (fetchErr) {
              console.error('[App] Full reload also failed:', fetchErr);
            }
          }
        };
        fetchDelta();
      } else {
        // No changed_files info - fall back to full reload (backward compatible)
        const fetchGraphData = async () => {
          try {
            setLoading(true);
            const graphData = await fetchGraph({
              projectId: data.project_id,
              filterConnected: true,
              includeStats: true,
            });
            setGraph(graphData);
          } catch (err) {
            console.error('[App] Failed to fetch graph after build complete:', err);
          } finally {
            setLoading(false);
          }
        };
        fetchGraphData();
      }
    }, [setGraph, setLoading, currentProject, markProjectBuilding, clearBuildProgress, applyDelta]),
    onNotification: useCallback((data: NotificationEvent) => {
      handleNewNotification(data);
    }, [handleNewNotification]),
    enabled: true,
  });

  // Cleanup file change debounce timer on unmount
  useEffect(() => {
    return () => {
      // No longer used for delta requests, but clean up for safety
      if (fileChangeTimerRef.current) {
        clearTimeout(fileChangeTimerRef.current);
      }
    };
  }, []);

  // NOTE: loadRepos, loadProjects, loadAgents are all called by useAppState's
  // own mount effect. No need to call them here again.

  // Listen for open-settings event dispatched from other components
  const openSettingsPanelHandler = useCallback(() => openPanel('settings'), [openPanel]);
  useEffect(() => {
    const handler = () => openSettingsPanelHandler();
    window.addEventListener('open-settings', handler);
    return () => window.removeEventListener('open-settings', handler);
  }, [openSettingsPanelHandler]);

  const handleImport = useCallback(async (path: string) => {
    const trimmed = path.trim();
    if (!trimmed) return;

    // Warn if not absolute path
    if (!trimmed.startsWith('/') && !trimmed.match(/^[A-Za-z]:\\/)) {
      alert('Please enter an absolute path (e.g. /Users/you/myproject)');
      return;
    }

    setLoading(true);
    try {
      // Extract project name from last path segment
      const name = trimmed.replace(/\\/g, '/').split('/').filter(Boolean).pop() || 'project';

      // Create project record
      console.log('[App] Creating project:', name, trimmed);
      const project = await createProject(name, trimmed);
      console.log('[App] Project created:', project);

      // Add to recent paths
      addRecentPath(trimmed, name);

      // Clear old graph immediately so we don't show stale data while building
      setGraph(null);

      // Mark project as building BEFORE setting as current
      // This ensures the UI shows BuildingState immediately
      console.log('[App] Marking project as building:', project.id);
      markProjectBuilding(project.id, true);

      // Set current project BEFORE loadProjects so there is never a render
      // window where projects.length > 0 but currentProject is null.
      setCurrentProject(project);

      // Refresh projects list (currentProject is already set above so
      // loadProjects won't try to auto-select and overwrite it)
      await loadProjects();

      // Start build
      console.log('[App] Starting build for project:', project.id);
      const task = await startBuild({
        root_dir: project.root_path,
        full_build: true,
        project_id: project.id,
      });

      const pollInterval = setInterval(async () => {
        try {
          const status = await fetchTaskStatus(task.task_id);
          if (status.status === 'complete' || status.status === 'error') {
            clearInterval(pollInterval);
            markProjectBuilding(project.id, false);
            if (status.status === 'complete') {
              // Fetch graph directly — SSE onBuildComplete may miss the event
              // when currentProject was just set and the closure still holds a
              // stale value.
              try {
                const graphData = await fetchGraph({
                  projectId: project.id,
                  filterConnected: true,
                  includeStats: true,
                });
                setGraph(graphData);
              } catch (err) {
                console.error('[App] Failed to fetch graph after build:', err);
              }
            }
            setLoading(false);
          }
        } catch {
          clearInterval(pollInterval);
          setLoading(false);
          markProjectBuilding(project.id, false);
        }
      }, 1000);
    } catch (error) {
      console.error('Failed to import repository:', error);
      alert(error instanceof Error ? error.message : 'Failed to import project');
      setLoading(false);
    }
  }, [loadProjects, setCurrentProject, setLoading, setGraph, markProjectBuilding, addRecentPath]);

  const handleNodeFocus = useCallback((nodeId: string) => {
    setActiveFilePath(null); // clear file tree selection
    setSelectedNode(nodeId);
  }, [setSelectedNode, setActiveFilePath]);

  const handleNodeClick = useCallback((nodeId: string) => {
    setActiveFilePath(null); // clear file tree selection
    setSelectedNode(nodeId);
  }, [setSelectedNode, setActiveFilePath]);

  // Left panel resizable width - must be before early return
  const [leftPanelWidth, setLeftPanelWidth] = useState(320); // Default w-80 = 320px
  const isLeftPanelResizing = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(320);
  const leftPanelPendingWidthRef = useRef<number>(320);
  // Ref to the left panel DOM element — used for direct style writes during drag
  // to bypass React re-renders and keep dragging smooth.
  const leftPanelRef = useRef<HTMLDivElement | null>(null);
  // rAF scheduling — coalesce mousemove events into one DOM write per frame.
  const leftPanelRafIdRef = useRef<number | null>(null);

  useEffect(() => {
    const flush = () => {
      leftPanelRafIdRef.current = null;
      if (leftPanelRef.current) {
        leftPanelRef.current.style.width = `${leftPanelPendingWidthRef.current}px`;
      }
    };
    const onMouseMove = (e: MouseEvent) => {
      if (!isLeftPanelResizing.current) return;
      const delta = e.clientX - startX.current;
      const newWidth = Math.min(600, Math.max(200, startWidth.current + delta));
      leftPanelPendingWidthRef.current = newWidth;
      // Schedule at most one DOM write per animation frame
      if (leftPanelRafIdRef.current == null) {
        leftPanelRafIdRef.current = requestAnimationFrame(flush);
      }
    };
    const finishDrag = () => {
      if (!isLeftPanelResizing.current) return;
      isLeftPanelResizing.current = false;
      // Cancel any pending rAF — we'll write the final value directly below
      if (leftPanelRafIdRef.current != null) {
        cancelAnimationFrame(leftPanelRafIdRef.current);
        leftPanelRafIdRef.current = null;
      }
      // Remove GPU layer promotion after drag
      if (leftPanelRef.current) leftPanelRef.current.style.willChange = '';
      // Ensure the final width is painted
      if (leftPanelRef.current) {
        leftPanelRef.current.style.width = `${leftPanelPendingWidthRef.current}px`;
      }
      // Sync the final width back to React state so future renders use the correct value
      setLeftPanelWidth(leftPanelPendingWidthRef.current);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      // Re-enable pointer events on any iframes inside the left column.
      document.body.classList.remove('axons-resizing');
    };
    const onMouseUp = () => finishDrag();
    // Fallback: abort drag if window loses focus (e.g. Alt+Tab while dragging)
    const onBlur = () => finishDrag();
    // Fallback: abort drag if pointer leaves the document entirely
    const onMouseLeave = (e: MouseEvent) => {
      if (e.relatedTarget === null) finishDrag();
    };
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
    window.addEventListener('blur', onBlur);
    document.addEventListener('mouseleave', onMouseLeave);
    return () => {
      document.removeEventListener('mousemove', onMouseMove);
      document.removeEventListener('mouseup', onMouseUp);
      window.removeEventListener('blur', onBlur);
      document.removeEventListener('mouseleave', onMouseLeave);
      if (leftPanelRafIdRef.current != null) {
        cancelAnimationFrame(leftPanelRafIdRef.current);
        leftPanelRafIdRef.current = null;
      }
    };
  }, []);

  // Check if current project is building
  const isBuilding = currentProject
    ? buildingProjects.has(currentProject.id)
    : buildingProjects.size > 0;

  // Dynamic panel rendering helper
  // Stable notification panel callbacks — prevents TopSearchBar React.memo from
  // failing shallow comparison on every App re-render.
  const toggleNotificationPanel = useCallback(() => setIsNotificationPanelOpen(prev => !prev), []);
  const closeNotificationPanel = useCallback(() => setIsNotificationPanelOpen(false), []);

  // Stable onClose callbacks per panelId — avoids creating new arrow functions
  // on every render, which would defeat React.memo shallow comparison.
  const onCloseCache = useRef<Record<string, () => void>>({});
  const getOnClose = useCallback((panelId: string) => {
    if (!onCloseCache.current[panelId]) {
      onCloseCache.current[panelId] = () => closePanel(panelId);
    }
    return onCloseCache.current[panelId];
  }, [closePanel]);

  const renderPanel = (panelId: string) => {
    const def = panelRegistry.get(panelId);
    if (!def) return null;

    // Plugin panel with async loader
    if (def.asyncLoader) {
      return <IframePluginPanel key={panelId} def={def} onClose={getOnClose(panelId)} />;
    }

    // Built-in panel with synchronous or lazy component
    const Component = def.component;
    if (!Component) return null;
    return (
      <ErrorBoundary key={panelId}>
        <Suspense fallback={<div className="flex items-center justify-center h-full"><div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" /></div>}>
          <Component onClose={getOnClose(panelId)} panelId={panelId} onSelectNode={handleNodeClick} />
        </Suspense>
      </ErrorBoundary>
    );
  };

  // Panels by location
  const leftTopPanels = useMemo(() => getPanelsByLocation('left-top'), [getPanelsByLocation, registryVersion]);
  const leftPanels = useMemo(() => getPanelsByLocation('left'), [getPanelsByLocation, registryVersion]);
  const rightPanels = useMemo(() => getPanelsByLocation('right'), [getPanelsByLocation, registryVersion]);
  const centerBottomPanels = useMemo(() => getPanelsByLocation('center-bottom'), [getPanelsByLocation, registryVersion]);
  const modalPanels = useMemo(() => getPanelsByLocation('modal'), [getPanelsByLocation, registryVersion]);

  // Determine which left-side panel is currently active
  // ActivityBar panels (fileTree, rightPanel/AI, plugins) are mutually exclusive,
  // so only one can be active at a time. Footer analysis panels stack below fileTree.
  const isFilePanelOpen = openPanels.has('fileTree');

  // Non-footer left panels that are currently open — these take over the left
  // column full-height (activityBar panels, gearMenu panels like extensions, plugins)
  const activeLeftPanel = leftPanels.find(p => openPanels.has(p.id) && p.activator !== 'footer');

  // Track activityBar left panels that have been opened at least once.
  // We keep them mounted (hidden via CSS) so that active streams (e.g. AI chat)
  // survive panel switching. Without this, unmounting the component cancels
  // the fetch and loses all conversation state.
  //
  // Cleanup rule: a panel is removed from the mounted set only when it is
  // NOT in openPanels AND it was closed by the user (not by mutual-exclusion),
  // so that switching between activityBar panels never unmounts the previous one.
  // In practice, we always keep the panel mounted once opened — the user can
  // always close the whole left column to free resources.
  const mountedActivityBarPanelsRef = useRef<Set<string>>(new Set());
  if (activeLeftPanel) {
    mountedActivityBarPanelsRef.current.add(activeLeftPanel.id);
  }
  // Cleanup: if a panel is unregistered (e.g. plugin stopped), remove from mounted set
  const mountedActivityBarPanels = useMemo(() => {
    const all = leftPanels.filter(p => p.activator !== 'footer');
    const mounted = new Set<string>();
    for (const id of mountedActivityBarPanelsRef.current) {
      if (all.some(p => p.id === id)) {
        mounted.add(id);
      }
    }
    return mounted;
  }, [leftPanels, openPanels]); // openPanels triggers recalc so UI stays in sync

  // Footer-driven left panels (analysis panels stacked below fileTree)
  const hasFooterLeftPanel = leftPanels.some(p => openPanels.has(p.id) && p.activator === 'footer');

  // Is any left-side content visible (fileTree, activityBar panel, or footer panel)?
  const hasLeftContent = isFilePanelOpen || !!activeLeftPanel || hasFooterLeftPanel;

  // When a non-footer left panel (AI, plugin, extensions) is active, it takes
  // over the entire left column. Otherwise, fileTree + footer panels use the
  // traditional split layout.
  const isLeftPanelFullHeight = !!activeLeftPanel;

  const hasCenterBottomPanel = centerBottomPanels.some(p => openPanels.has(p.id));

  // Projects still being fetched on initial load: render a light-weight
  // placeholder so DropZone doesn't flash while the list is empty.
  if (!projectsLoaded) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-deep text-text-primary">
        <div className="w-10 h-10 border-4 border-accent border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  // Show drop zone ONLY when projectsLoaded is true AND project list is
  // confirmed empty AND no currentProject. This is the only safe gate:
  // projectsLoaded guarantees the async fetch finished, so projects === []
  // truly means no projects exist — not a transient loading state.
  if (projectsLoaded && projects.length === 0 && !currentProject) {
    return <DropZone onImport={handleImport} />;
  }

  // If projects exist but no current project yet (still initialising), show spinner
  if (projects.length > 0 && !currentProject) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-deep text-text-primary">
        <div className="w-10 h-10 border-4 border-accent border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  // hasAnalysisPanel is now derived from panelRegistry (see below)

  const handleLeftPanelResizeMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    isLeftPanelResizing.current = true;
    startX.current = e.clientX;
    startWidth.current = leftPanelWidth;
    leftPanelPendingWidthRef.current = leftPanelWidth;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    // Promote to GPU layer only during drag
    if (leftPanelRef.current) leftPanelRef.current.style.willChange = 'width';
    // Disable pointer events on iframes in the left column while resizing so
    // mousemove keeps firing on the host document (otherwise the iframe
    // swallows the events and dragging stutters / drops frames).
    document.body.classList.add('axons-resizing');
  };

  return (
    <div className="h-screen w-screen flex flex-col bg-deep text-text-primary overflow-hidden">
      {/* Top Search Bar */}
      <TopSearchBar
        onFocusNode={handleNodeFocus}
        notifications={notificationList}
        unreadCount={unreadCount}
        isPanelOpen={isNotificationPanelOpen}
        onTogglePanel={toggleNotificationPanel}
        onMarkRead={markAsRead}
        onMarkAllRead={markAllAsRead}
        onDeleteNotification={deleteNotificationAction}
        onClosePanel={closeNotificationPanel}
        onOpenPanel={openPanel}
      />

      {/* Main content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Activity Bar - left sidebar icons */}
        <ActivityBar />

        {/* Left column: File tree on top, analysis panels below - collapsible */}
        {hasLeftContent && (
        <div
            ref={leftPanelRef}
            style={{ width: leftPanelWidth, contain: 'layout style' }}
          className="h-full shrink-0 bg-surface border-r border-border-subtle flex flex-col overflow-hidden relative"
        >
            {/* Resize handle on right side — VS Code sash style: 4px hit area, transparent by default, full accent on hover */}
          <div
              className="absolute right-0 top-0 bottom-0 cursor-col-resize z-20 group"
              style={{ width: '4px' }}
            onMouseDown={handleLeftPanelResizeMouseDown}
            >
              <div className="absolute right-0 top-0 bottom-0 opacity-0 group-hover:opacity-100 transition-opacity bg-accent" style={{ width: '4px' }} />
            </div>

            {/* ActivityBar non-footer left panels: always mounted, CSS controls visibility.
                This keeps components like the AI chat alive when user switches to fileTree,
                so active SSE/fetch streams are not interrupted by React unmounting. */}
            {leftPanels
              .filter(p => p.activator !== 'footer' && mountedActivityBarPanels.has(p.id))
              .map(p => {
                const isActive = p.id === activeLeftPanel?.id;
                return (
                  <div
                    key={p.id}
                    className={isActive
                      ? 'w-full h-full'
                      : 'invisible h-0 overflow-hidden pointer-events-none absolute'}
                  >
                    {renderPanel(p.id)}
                  </div>
                );
              })}

            {/* File tree + footer panels (normal flow, only when no activityBar panel is active) */}
            {!isLeftPanelFullHeight && (
              <>
                {/* Left-top panels (File tree): fills space when no footer panels, shrinks when footer panels open */}
                <div className={`w-full overflow-hidden ${hasFooterLeftPanel ? 'flex-none h-2/5' : 'flex-1'}`}>
                  {leftTopPanels.filter(p => openPanels.has(p.id)).map(p => renderPanel(p.id))}
                </div>

                  {/* Footer-driven left panels (analysis) stacked, scrollable */}
                  {hasFooterLeftPanel && (
                    <div className="w-full flex-1 overflow-y-auto border-t border-border-subtle divide-y divide-border-subtle">
                      {leftPanels.filter(p => openPanels.has(p.id) && p.activator === 'footer').map(p => renderPanel(p.id))}
                    </div>
                  )}
              </>
            )}
        </div>
        )}

        {/* Center column: Graph canvas + center-bottom panels (overlaid) */}
        <div className="flex-1 flex flex-col min-w-0 overflow-hidden relative" style={{ contain: 'layout style' }}>
          {/* Graph canvas — always takes full height; terminal overlays it */}
          <div className={`flex-1 relative ${hasCenterBottomPanel ? '' : 'min-h-0'}`}>
            {graph ? (
              <ErrorBoundary>
                <Suspense fallback={<div className="flex items-center justify-center h-full"><div className="w-12 h-12 border-4 border-accent border-t-transparent rounded-full animate-spin" /></div>}>
                  <GraphCanvas onNodeClick={handleNodeClick} />
                </Suspense>
              </ErrorBoundary>
            ) : isBuilding ? (
                <BuildingState projectName={currentProject?.name} progress={buildProgress?.progress} phase={buildProgress?.phase} message={buildProgress?.message} />
            ) : loading ? (
              <div className="flex items-center justify-center h-full">
                <div className="flex flex-col items-center gap-4">
                  <div className="w-12 h-12 border-4 border-accent border-t-transparent rounded-full animate-spin" />
                  <span className="text-text-secondary">Loading graph...</span>
                </div>
              </div>
                ) : currentProject ? (
                  // Has a project but graph not loaded yet (initial load in progress) — show spinner
                  <div className="flex items-center justify-center h-full">
                    <div className="flex flex-col items-center gap-4">
                      <div className="w-12 h-12 border-4 border-accent border-t-transparent rounded-full animate-spin" />
                      <span className="text-text-secondary">Loading graph...</span>
                    </div>
                  </div>
            ) : (
                      // No project at all — show DropZone for onboarding
              <DropZone onImport={handleImport} />
            )}
          </div>

          {/* Center-bottom panels (Terminal) — absolute positioned to overlay graph, not push it */}
          {centerBottomPanels.filter(p => openPanels.has(p.id)).map(p => renderPanel(p.id))}
        </div>

        {/* Right panels (Code references) */}
        {rightPanels.filter(p => openPanels.has(p.id)).map(p => renderPanel(p.id))}
      </div>

      {/* Modal panels (Settings, ArchRules, Process) */}
      {modalPanels.filter(p => openPanels.has(p.id)).map(p => renderPanel(p.id))}

      {/* Notification Toast (bottom-right, only when panel is closed) */}
      <NotificationToast
        notifications={notificationList}
        isPanelOpen={isNotificationPanelOpen}
        onMarkRead={markAsRead}
      />

      {/* Footer - IDE style status bar */}
      <Footer />
    </div>
  );
}

export default App;