import React, { createContext, useContext, useState, useCallback, useEffect, useRef, useMemo, type ReactNode } from 'react';
import type { KnowledgeGraph, RepoInfo, CodeReference, EdgeType, NodeLabel } from '../types/graph';
import { DEFAULT_VISIBLE_LABELS, DEFAULT_VISIBLE_EDGES } from '../types/graph';
import { fetchRepos, fetchGraph, fetchFile, fetchAgents, fetchProjectStats, fetchProjectBuildStatus, fetchAppState, setActiveProject, getFileTreeExpandedPaths as fetchFileTreeExpandedPaths, setFileTreeExpandedPaths as saveFileTreeExpandedPaths, type Project, type AgentProfile, type GraphDeltaResponse, type ProjectBuildStatus } from '../services/api';
import { getBaseURL } from '../lib/config';
import { useEventStream, type BuildProgressEvent } from './useEventStream';
import { PanelRegistry, type PanelDef, type PanelLocation, type PanelActivator } from '../lib/panelRegistry';
// Lightweight panels — static import (small bundle impact)
import { CodeHealthPanel } from '../components/CodeHealthPanel';
import { GraphAnalyticsPanel } from '../components/GraphAnalyticsPanel';
import { ImpactAnalysisPanel } from '../components/ImpactAnalysisPanel';
import { CfgDataflowPanel } from '../components/CfgDataflowPanel';
import { SequencePanel } from '../components/SequencePanel';
import { ArchRulesPanel } from '../components/ArchRulesPanel';
import { ProcessPanel } from '../components/ProcessPanel';
import { CodeReferencesPanel } from '../components/CodeReferencesPanel';
import { FileTreePanel } from '../components/FileTreePanel';
import { SettingsPanel } from '../components/SettingsPanel';
import { ExtensionsPanel } from '../components/ExtensionsPanel';
import { TerminalPanel } from '../components/terminal/TerminalPanel';
// Heavy panels — lazy import (code-split into separate chunks)
const LazyRightPanel = React.lazy(() => import('../components/RightPanel').then(m => ({ default: m.RightPanel })));

export interface AppState {
  // View state
  viewMode: 'onboarding' | 'loading' | 'exploring';
  setViewMode: (mode: 'onboarding' | 'loading' | 'exploring') => void;

  // Project state
  projects: Project[];
  loadProjects: () => Promise<void>;
  projectsLoaded: boolean;
  currentProject: Project | null;
  setCurrentProject: (project: Project | null) => void;
  reloadGraph: () => Promise<void>;

  // Building projects tracking
  buildingProjects: Set<string>;
  markProjectBuilding: (projectId: string, isBuilding: boolean) => void;
  isProjectBuilding: (projectId: string) => boolean;

  // Build progress (lifted from App.tsx — keyed by project_id for safe switching)
  buildProgress: { projectId: string; progress: number; phase: string; message: string } | null;
  updateBuildProgress: (data: BuildProgressEvent) => void;
  clearBuildProgress: () => void;
  syncBuildStatus: (projectId: string) => Promise<ProjectBuildStatus>;

  // Repo state
  repos: RepoInfo[];
  setRepos: (repos: RepoInfo[]) => void;
  currentRepo: RepoInfo | null;
  setCurrentRepo: (repo: RepoInfo | null) => void;
  loading: boolean;
  isLoading: boolean;
  error: string | null;
  setLoading: (loading: boolean) => void;

  // Graph state
  graph: KnowledgeGraph | null;
  setGraph: (graph: KnowledgeGraph | null) => void;
  /** File cache version — incremented on every mutation so consumers can re-read from ref */
  fileCacheVersion: number;

  // Graph delta state for incremental updates
  pendingDelta: GraphDeltaResponse | null;
  applyDelta: (delta: GraphDeltaResponse | null) => void;
  applyDeltaToKnowledgeGraph: (delta: GraphDeltaResponse) => void;

  // File cache management
  getCachedFile: (filePath: string) => string | undefined;
  setCachedFile: (filePath: string, content: string) => void;
  clearFileCache: (filePath: string) => void;
  clearAllFileCache: () => void;

  // Selection state - using string ID for selectedNode
  selectedNode: string | null;
  setSelectedNode: (nodeId: string | null) => void;

  // FileTree expanded paths - persisted across panel switches
  fileTreeExpandedPaths: Set<string>;
  setFileTreeExpandedPaths: (paths: Set<string> | ((prev: Set<string>) => Set<string>)) => void;
  fileTreeExpandedPathsReady: boolean;

  // Panel registry — 统一面板注册表
  panelRegistry: PanelRegistry;
  openPanels: Set<string>;
  isPanelOpen: (id: string) => boolean;
  openPanel: (id: string) => void;
  closePanel: (id: string) => void;
  togglePanel: (id: string) => void;
  registerPanel: (def: PanelDef) => void;
  unregisterPanel: (id: string) => void;
  getPanelsByLocation: (location: PanelLocation) => PanelDef[];
  getPanelsByActivator: (activator: PanelActivator) => PanelDef[];
  registryVersion: number;

  // Panel-specific state (不属于 panelRegistry，有独立逻辑)
  codeContent: string | null;
  setCodeContent: (content: string | null) => void;
  codeLoading: boolean;
  setCodeLoading: (loading: boolean) => void;
  // Active file path — set by FileTreePanel when user selects a file directly
  // (independent of graph selectedNode)
  activeFilePath: string | null;
  setActiveFilePath: (path: string | null) => void;

  // Filter state
  visibleLabels: NodeLabel[];
  setVisibleLabels: (labels: NodeLabel[]) => void;
  toggleLabelVisibility: (label: NodeLabel) => void;
  visibleEdgeTypes: EdgeType[];
  toggleEdgeVisibility: (edgeType: EdgeType) => void;
  depthFilter: number | null;
  setDepthFilter: (depth: number | null) => void;
  layoutMode: 'force' | 'tree' | 'circles';
  setLayoutMode: (mode: 'force' | 'tree' | 'circles') => void;

  // Code references
  codeReferences: CodeReference[];
  addCodeReference: (ref: CodeReference) => void;
  removeCodeReference: (id: string) => void;
  clearCodeReferences: () => void;

  // Legacy panel aliases — 向后兼容，内部走 panelRegistry
  isRightPanelOpen: boolean;
  setRightPanelOpen: (open: boolean) => void;
  openRightPanel: () => void;
  closeRightPanel: () => void;
  isFilePanelOpen: boolean;
  setFilePanelOpen: (open: boolean) => void;
  isCodePanelOpen: boolean;
  setCodePanelOpen: (open: boolean) => void;
  openCodePanel: () => void;
  isSettingsPanelOpen: boolean;
  openSettingsPanel: () => void;
  closeSettingsPanel: () => void;
  isCodeHealthOpen: boolean;
  openCodeHealth: () => void;
  closeCodeHealth: () => void;
  isGraphAnalyticsOpen: boolean;
  openGraphAnalytics: () => void;
  closeGraphAnalytics: () => void;
  isImpactAnalysisOpen: boolean;
  openImpactAnalysis: () => void;
  closeImpactAnalysis: () => void;
  isCfgPanelOpen: boolean;
  openCfgPanel: () => void;
  closeCfgPanel: () => void;
  isSequencePanelOpen: boolean;
  openSequencePanel: () => void;
  closeSequencePanel: () => void;
  isArchRulesPanelOpen: boolean;
  openArchRulesPanel: () => void;
  closeArchRulesPanel: () => void;
  isProcessPanelOpen: boolean;
  openProcessPanel: () => void;
  closeProcessPanel: () => void;
  isTerminalOpen: boolean;
  openTerminal: () => void;
  closeTerminal: () => void;

  // Actions
  loadRepos: () => Promise<void>;
  selectRepo: (repo: RepoInfo) => Promise<void>;
  loadFileContent: (filePath: string) => Promise<void>;

  // Agent profiles
  agents: AgentProfile[];
  currentAgentId: string;
  setCurrentAgentId: (id: string) => void;
  loadAgents: () => Promise<void>;

  // Config change version - incremented on every config_change SSE event
  configVersion: number;

  // File backup management (removed - now handled by backend)
}

export const AppContext = createContext<AppState | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  // View state
  const [viewMode, setViewMode] = useState<'onboarding' | 'loading' | 'exploring'>('onboarding');

  // Project state
  const [projects, setProjects] = useState<Project[]>([]);
  const [currentProject, setCurrentProjectRaw] = useState<Project | null>(null);
  const [projectsLoaded, setProjectsLoaded] = useState(false);

  // Wrap setCurrentProject so every call also persists the id to server-side
  // settings via the dedicated /v1/app/state/active-project endpoint.
  // This survives browser refresh, incognito mode, and multiple tabs/windows.
  // Atomically clears stale state when switching to a different project
  // to prevent rendering old graph / old progress on the new project.
  const currentProjectRef = useRef(currentProject);
  currentProjectRef.current = currentProject;

  const setCurrentProject = useCallback((project: Project | null) => {
    setActiveProject(project?.id ?? ''); // fire-and-forget

    if (project?.id !== currentProjectRef.current?.id) {
      setGraph(null);
      setBuildProgress(null);
      // Synchronously clear expanded-paths state so that FileTreePanel
      // sees the cleared state in the SAME render batch as the project
      // change.  Without this, React's child-before-parent effect order
      // causes FileTreePanel's restore effect to run with the OLD
      // project's expanded paths and ready=true before AppProvider's
      // effect has a chance to clear them.
      setFileTreeExpandedPathsRaw(new Set());
      setFileTreeExpandedPathsReady(false);
    }

    setCurrentProjectRaw(project);
  }, []);

  // Building projects tracking - prevents loading incomplete graph during build
  const [buildingProjects, setBuildingProjects] = useState<Set<string>>(new Set());

  // Build progress state — keyed by project_id so it survives project switches
  const [buildProgress, setBuildProgress] = useState<{
    projectId: string;
    progress: number;
    phase: string;
    message: string;
  } | null>(null);

  // Only update build progress when the SSE event matches the current project
  const updateBuildProgress = useCallback((data: BuildProgressEvent) => {
    if (currentProject && data.project_id === currentProject.id) {
      setBuildProgress({
        projectId: data.project_id,
        progress: data.progress,
        phase: data.phase || '',
        message: data.message,
      });
    }
    // Silently discard events for other projects — don't overwrite current progress
  }, [currentProject]);

  const clearBuildProgress = useCallback(() => {
    setBuildProgress(null);
  }, []);

  // Building projects management — must be before loadProjects/syncBuildStatus
  const markProjectBuilding = useCallback((projectId: string, isBuilding: boolean) => {
    setBuildingProjects(prev => {
      const next = new Set(prev);
      if (isBuilding) {
        next.add(projectId);
      } else {
        next.delete(projectId);
      }
      return next;
    });
  }, []);

  const isProjectBuilding = useCallback((projectId: string) => {
    const result = buildingProjects.has(projectId);
    return result;
  }, [buildingProjects]);

  // Sync build status from backend — the authoritative source for whether
  // a project is currently building. Used after page refresh or project switch.
  // If the frontend already knows the project is building (via markProjectBuilding),
  // skip the backend query — the build task may not exist yet on the backend side
  // (e.g. between markProjectBuilding and startBuildForProject).
  const syncBuildStatus = useCallback(async (projectId: string): Promise<ProjectBuildStatus> => {
    // Frontend already knows this project is building — trust it
    if (buildingProjects.has(projectId)) {
      return { is_building: true };
    }

    try {
      const status = await fetchProjectBuildStatus(projectId);
      markProjectBuilding(projectId, status.is_building);
      if (status.is_building) {
        setBuildProgress({
          projectId: projectId,
          progress: status.progress || 0,
          phase: status.phase || '',
          message: status.message || '',
        });
      }
      return status;
    } catch {
      // Don't clear buildingProjects on network error — may be transient
      return { is_building: false };
    }
  }, [buildingProjects, markProjectBuilding]);

  // Repo state
  const [repos, setRepos] = useState<RepoInfo[]>([]);
  const [currentRepo, setCurrentRepo] = useState<RepoInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Graph state
  const [graph, setGraph] = useState<KnowledgeGraph | null>(null);

  // File cache — stored in a ref to avoid triggering React re-renders on every
  // cache write.  Consumers that need to react to cache changes subscribe to
  // `fileCacheVersion` (incremented on every mutation) instead of the Map itself.
  const fileContentsRef = useRef<Map<string, string>>(new Map());
  const [fileCacheVersion, setFileCacheVersion] = useState(0);

  // Graph delta state for incremental updates
  const [pendingDelta, setPendingDelta] = useState<GraphDeltaResponse | null>(null);
  const applyDelta = useCallback((delta: GraphDeltaResponse | null) => {
    setPendingDelta(delta);
  }, []);

  // Apply delta changes to the KnowledgeGraph state (so file tree, search, etc. see updates)
  const applyDeltaToKnowledgeGraph = useCallback((delta: GraphDeltaResponse) => {
    setGraph(prevGraph => {
      if (!prevGraph) return prevGraph;

      // Remove nodes by ID
      const removedNodeIds = new Set(delta.removed_node_ids);
      let updatedNodes = prevGraph.nodes.filter(n => !removedNodeIds.has(n.id));

      // Build a map of delta nodes by ID for quick lookup (update existing + add new)
      const deltaNodeMap = new Map(delta.added_nodes.map(n => [n.id, n]));
      const existingNodeIds = new Set(updatedNodes.map(n => n.id));

      // Update existing nodes with delta data (e.g., startLine/endLine may have changed)
      updatedNodes = updatedNodes.map(n => {
        const deltaNode = deltaNodeMap.get(n.id);
        return deltaNode ? deltaNode : n;  // Use delta version if available (fresh data)
      });

      // Add brand new nodes (not in existing set)
      let addedNodeCount = 0;
      for (const node of delta.added_nodes) {
        if (!existingNodeIds.has(node.id)) {
          updatedNodes = [...updatedNodes, node];
          addedNodeCount++;
        }
      }

      // Remove edges by ID
      const removedEdgeIds = new Set(delta.removed_edge_ids);
      let updatedRelationships = prevGraph.relationships.filter(r => !removedEdgeIds.has(r.id));

      // Add new edges (skip if already exists by ID)
      const existingEdgeIds = new Set(updatedRelationships.map(r => r.id));
      let addedEdgeCount = 0;
      for (const edge of delta.added_edges) {
        if (!existingEdgeIds.has(edge.id)) {
          updatedRelationships = [...updatedRelationships, edge];
          addedEdgeCount++;
        }
      }

      return {
        ...prevGraph,
        nodes: updatedNodes,
        relationships: updatedRelationships,
      };
    });

    // Invalidate file cache for changed files so CodeReferencesPanel re-fetches latest content
    const changedFilePaths = new Set<string>();
    for (const node of delta.added_nodes) {
      const fp = node.properties?.filePath || node.properties?.path;
      if (fp && typeof fp === 'string') changedFilePaths.add(fp);
    }
    if (changedFilePaths.size > 0) {
      for (const fp of changedFilePaths) {
        fileContentsRef.current.delete(fp);
      }
      setFileCacheVersion(v => v + 1);
    }
  }, []);

  // Selection state - using string ID
  const [selectedNode, setSelectedNode] = useState<string | null>(null);

  // FileTree expanded paths - persisted across panel switches and browser restarts
  const [fileTreeExpandedPaths, setFileTreeExpandedPathsRaw] = useState<Set<string>>(new Set());
  // Whether the persisted expanded paths have been loaded from the backend.
  // Used by FileTreePanel to avoid the race between entries loading and
  // expanded-paths arriving.
  const [fileTreeExpandedPathsReady, setFileTreeExpandedPathsReady] = useState(false);

  // Debounced save to backend (500ms delay)
  const saveExpandedPathsTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Capture the latest value in a ref so the debounced save can read it
  // without calling the function-updater a second time (which would cause
  // a redundant setState and, for direct values, could overwrite newer state).
  const latestExpandedPathsRef = useRef<Set<string>>(new Set());
  const setFileTreeExpandedPaths = useCallback((value: Set<string> | ((prev: Set<string>) => Set<string>)) => {
    setFileTreeExpandedPathsRaw(value);

    // Debounce save to backend
    if (saveExpandedPathsTimeoutRef.current) {
      clearTimeout(saveExpandedPathsTimeoutRef.current);
    }
    saveExpandedPathsTimeoutRef.current = setTimeout(() => {
      // Read the *current* state directly instead of re-applying the updater.
      // This avoids a second setState that could overwrite concurrent changes
      // (e.g. when the user expands another directory within the 500ms window).
      setFileTreeExpandedPathsRaw((prev) => {
        latestExpandedPathsRef.current = prev;
        // Fire-and-forget save to backend (scoped by current project)
        const pid = currentProjectRef.current?.id;
        saveFileTreeExpandedPaths(Array.from(prev), pid).catch(() => {
          // Silently ignore save errors
        });
        // Return prev unchanged — no extra re-render
        return prev;
      });
    }, 500);
  }, []);

  // Load persisted expanded paths on mount and when project changes
  useEffect(() => {
    const pid = currentProject?.id;
    // Mark as NOT ready while fetching — this prevents the FileTreePanel
    // restore effect from running with stale/empty expanded paths when the
    // project changes (the old `ready=true` would still be in effect until
    // the new fetch completes, causing the restore to either use the old
    // project's paths or skip loading children entirely).
    setFileTreeExpandedPathsReady(false);
    setFileTreeExpandedPathsRaw(new Set());
    fetchFileTreeExpandedPaths(pid).then((paths) => {
      if (paths && Array.isArray(paths)) {
        setFileTreeExpandedPathsRaw(new Set(paths));
      } else {
        setFileTreeExpandedPathsRaw(new Set());
      }
    }).catch(() => {
      // Silently ignore load errors
      setFileTreeExpandedPathsRaw(new Set());
    }).finally(() => {
      setFileTreeExpandedPathsReady(true);
    });
  }, [currentProject?.id]);

  // Panel registry — 统一面板注册与开关
  const panelRegistryRef = useRef(new PanelRegistry());
  const panelRegistry = panelRegistryRef.current;
  const [openPanels, setOpenPanels] = useState<Set<string>>(new Set());
  // Registry version counter — 每次 register/unregister 递增，触发 React 重新渲染
  const [registryVersion, setRegistryVersion] = useState(0);

  const isPanelOpen = useCallback((id: string) => openPanels.has(id), [openPanels]);
  const openPanel = useCallback((id: string) => {
    setOpenPanels(prev => {
      if (prev.has(id)) return prev;
      const next = new Set(prev);
      next.add(id);
      const def = panelRegistry.get(id);

      // ActivityBar panel mutual exclusion: opening one activityBar panel closes
      // all other activityBar panels AND any extensions panel currently open.
      // This ensures only one left-side full-height panel is visible at a time.
      if (def && def.activator === 'activityBar' && def.id !== 'home') {
        const activityBarPanels = panelRegistry.getByActivator('activityBar');
        for (const p of activityBarPanels) {
          if (p.id !== id && p.id !== 'home' && next.has(p.id)) {
            next.delete(p.id);
          }
        }
        // Also close extensions panel (gearMenu → left) if open
        if (next.has('extensions')) {
          next.delete('extensions');
        }
      }

      // Extensions panel (gearMenu → left) should also close activityBar panels
      if (def && def.id === 'extensions') {
        const activityBarPanels = panelRegistry.getByActivator('activityBar');
        for (const p of activityBarPanels) {
          if (p.id !== 'home' && next.has(p.id)) {
            next.delete(p.id);
          }
        }
      }

      // Footer-driven left panels (analysis): when opening a footer panel in the
      // left column while an activityBar panel (AI, plugin) occupies the left
      // column full-height, auto-open fileTree and close the activityBar panel
      // so the footer panel can stack below fileTree as intended.
      if (def && def.activator === 'footer' && def.location === 'left') {
        const activityBarPanels = panelRegistry.getByActivator('activityBar');
        let hadActivityBarPanel = false;
        for (const p of activityBarPanels) {
          if (p.id !== 'home' && next.has(p.id)) {
            next.delete(p.id);
            hadActivityBarPanel = true;
          }
        }
        if (hadActivityBarPanel && !next.has('fileTree')) {
          next.add('fileTree');
        }
      }

      return next;
    });
  }, [panelRegistry]);
  const closePanel = useCallback((id: string) => {
    setOpenPanels(prev => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, []);
  const togglePanel = useCallback((id: string) => {
    const def = panelRegistry.get(id);

    // Standalone panels: no longer used
    if (def?.standalone) {
      setOpenPanels(prev => {
        const next = new Set(prev);
        if (next.has(id)) {
          next.delete(id);
        } else {
          next.add(id);
        }
        return next;
      });
      return;
    }

    setOpenPanels(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
        // Same mutual exclusion logic as openPanel
        const def = panelRegistry.get(id);
        if (def && def.activator === 'activityBar' && def.id !== 'home') {
          const activityBarPanels = panelRegistry.getByActivator('activityBar');
          for (const p of activityBarPanels) {
            if (p.id !== id && p.id !== 'home' && next.has(p.id)) {
              next.delete(p.id);
            }
          }
          // Also close extensions panel if open
          if (next.has('extensions')) {
            next.delete('extensions');
          }
        }
        if (def && def.id === 'extensions') {
          const activityBarPanels = panelRegistry.getByActivator('activityBar');
          for (const p of activityBarPanels) {
            if (p.id !== 'home' && next.has(p.id)) {
              next.delete(p.id);
            }
          }
        }
        // Footer-driven left panels: same auto-switch logic as openPanel
        if (def && def.activator === 'footer' && def.location === 'left') {
          const activityBarPanels = panelRegistry.getByActivator('activityBar');
          let hadActivityBarPanel = false;
          for (const p of activityBarPanels) {
            if (p.id !== 'home' && next.has(p.id)) {
              next.delete(p.id);
              hadActivityBarPanel = true;
            }
          }
          if (hadActivityBarPanel && !next.has('fileTree')) {
            next.add('fileTree');
          }
        }
      }
      return next;
    });
  }, [panelRegistry]);
  const registerPanel = useCallback((def: PanelDef) => {
    panelRegistry.register(def);
    setRegistryVersion(v => v + 1); // 触发重新渲染
  }, [panelRegistry]);
  const unregisterPanel = useCallback((id: string) => {
    panelRegistry.unregister(id);
    setRegistryVersion(v => v + 1); // 触发重新渲染
    // 同时关闭面板
    setOpenPanels(prev => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, [panelRegistry]);
  const getPanelsByLocation = useCallback((location: PanelLocation) => {
    return panelRegistry.getByLocation(location);
  }, [panelRegistry, registryVersion]); // 依赖 registryVersion，版本变化时返回新结果
  const getPanelsByActivator = useCallback((activator: PanelActivator) => {
    return panelRegistry.getByActivator(activator);
  }, [panelRegistry, registryVersion]);

  // Panel-specific state
  const [codeContent, setCodeContent] = useState<string | null>(null);
  const [codeLoading, setCodeLoading] = useState(false);
  const [activeFilePath, setActiveFilePath] = useState<string | null>(null);

  // Filter state
  const [visibleLabels, setVisibleLabels] = useState<NodeLabel[]>(DEFAULT_VISIBLE_LABELS);
  const [visibleEdgeTypes, setVisibleEdgeTypes] = useState<EdgeType[]>(DEFAULT_VISIBLE_EDGES);
  const [depthFilter, setDepthFilter] = useState<number | null>(null);
  const [layoutMode, setLayoutMode] = useState<'force' | 'tree' | 'circles'>('force');

  // Code references
  const [codeReferences, setCodeReferences] = useState<CodeReference[]>([]);

  // Settings panel state — 走 panelRegistry
  // (旧 isSettingsPanelOpen useState 已移除，由 openPanels.has('settings') 替代)

  // Agent profiles state
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [currentAgentId, setCurrentAgentId] = useState<string>('default');

  // Config version - incremented on config_change events to notify panels
  const [configVersion, setConfigVersion] = useState(0);

  // 分析面板状态 — 走 panelRegistry
  // (旧 6 个 useState 已移除，由 openPanels 统一管理)

  // Load projects — uses GET /v1/app/state which returns projects +
  // active_project_id atomically, avoiding any render window between
  // setProjects and setCurrentProject that would flash the DropZone.
  const loadProjects = useCallback(async () => {
    console.log('[useAppState] Loading projects...');
    try {
      const appState = await fetchAppState();
      const projectList = appState.projects || [];
      const savedId = appState.active_project_id;
      console.log('[useAppState] Fetched projects:', projectList.length, 'activeId:', savedId);

      if (projectList.length > 0 && !currentProject) {
        const matched = savedId ? projectList.find(p => p.id === savedId) : null;
        const selected = matched || projectList[0];
        console.log(
          '[useAppState] Auto-selecting project:',
          selected.name,
          matched ? '(restored from server)' : '(default first)'
        );
        setProjects(projectList);
        setCurrentProjectRaw(selected);

        // Restore build status from backend — handles page refresh while building
        const buildStatus = await syncBuildStatus(selected.id);
        if (buildStatus.is_building) {
          console.log('[useAppState] Restored building state for project:', selected.id);
        }
      } else {
        setProjects(projectList);
      }
    } catch (err) {
      console.error('[useAppState] Failed to load projects:', err);
    } finally {
      setProjectsLoaded(true);
    }
  }, [currentProject, syncBuildStatus]);

  // Load repos on mount
  const loadRepos = useCallback(async () => {
    console.log('[useAppState] Loading repos...');
    setLoading(true);
    setError(null);
    try {
      const fetchedRepos = await fetchRepos();
      console.log('[useAppState] Fetched repos:', fetchedRepos.length);
      setRepos(fetchedRepos);
      // Don't auto-select repo here - let the initialization effect handle it
      // This ensures project is restored from URL first
    } catch (err) {
      console.error('[useAppState] Failed to load repos:', err);
      setError(err instanceof Error ? err.message : 'Failed to load repos');
    } finally {
      setLoading(false);
    }
  }, []);

  // Select repo and load graph
  const selectRepo = useCallback(async (repo: RepoInfo) => {
    console.log('[useAppState] Selecting repo:', repo.name);

    // Sync build status from backend to handle page refresh
    if (currentProject) {
      const buildStatus = await syncBuildStatus(currentProject.id);
      if (buildStatus.is_building) {
        console.log('[useAppState] Project is currently building, showing BuildingState');
        setCurrentRepo(repo);
        setViewMode('exploring');
        setGraph(null);
        return;
      }
    }

    // Check if project has graph data before loading
    if (currentProject) {
      try {
        const stats = await fetchProjectStats(currentProject.id);
        if (stats.node_count === 0) {
          console.log('[useAppState] Project has no graph data yet, setting empty graph');
          setCurrentRepo(repo);
          setViewMode('exploring');
          // Set an empty KnowledgeGraph so GraphCanvas shows "No Graph Data"
          setGraph({ nodes: [], relationships: [] });
          return;
        }
        console.log('[useAppState] Project has graph data:', stats);
      } catch (err) {
        console.warn('[useAppState] Failed to fetch project stats, proceeding with graph load:', err);
      }
    }

    setLoading(true);
    setError(null);
    try {
      console.log('[useAppState] Fetching graph...');
      const graphData = await fetchGraph({
        repo: repo.name,
        projectId: currentProject?.id,
        filterConnected: true,
        includeStats: true,
      });
      console.log('[useAppState] Graph loaded:', {
        nodes: graphData.nodes?.length || 0,
        relationships: graphData.relationships?.length || 0
      });
      setGraph(graphData);
      setCurrentRepo(repo);
      setViewMode('exploring');

      // Reset state
      setSelectedNode(null);
      setCodeReferences([]);
      fileContentsRef.current.clear();
      setFileCacheVersion(v => v + 1);
    } catch (err) {
      console.error('[useAppState] Failed to load graph:', err);
      setError(err instanceof Error ? err.message : 'Failed to load graph');
      // On failure, set empty graph so UI doesn't get stuck on "Loading graph..."
      setGraph({ nodes: [], relationships: [] });
    } finally {
      setLoading(false);
    }
  }, [currentProject, syncBuildStatus]);

  // Reload graph for current project/repo
  const reloadGraph = useCallback(async () => {
    if (!currentProject) {
      console.log('[useAppState] No current project, skipping reload');
      return;
    }

    // Sync build status from backend — the authoritative source.
    // This handles page refresh (where buildingProjects is lost) and
    // project switch (where we need to check if the target project is building).
    const buildStatus = await syncBuildStatus(currentProject.id);

    if (buildStatus.is_building) {
      // Project is building — keep graph=null so App.tsx renders BuildingState
      console.log('[useAppState] Project is currently building, showing BuildingState');
      setGraph(null);
      return;
    }

    // Check if project has graph data before loading
    try {
      const stats = await fetchProjectStats(currentProject.id);
      if (stats.node_count === 0) {
        console.log('[useAppState] Project has no graph data yet, setting empty graph');
        // Set an empty KnowledgeGraph so GraphCanvas shows "No Graph Data"
        // instead of getting stuck on "Loading graph..."
        setGraph({ nodes: [], relationships: [] });
        return;
      }
      console.log('[useAppState] Project has graph data:', stats);
    } catch (err) {
      console.warn('[useAppState] Failed to fetch project stats, proceeding with graph load:', err);
      // Continue to try loading graph even if stats check fails
    }

    console.log('[useAppState] Reloading graph for project:', currentProject.id);
    setLoading(true);
    try {
      // Use project root path as repo name for API call
      const repoName = currentRepo?.name || currentProject.name;
      const graphData = await fetchGraph({
        repo: repoName,
        projectId: currentProject.id,
        filterConnected: true,
        includeStats: true,
      });
      console.log('[useAppState] Graph reloaded:', {
        nodes: graphData.nodes?.length || 0,
        relationships: graphData.relationships?.length || 0
      });
      setGraph(graphData);
    } catch (err) {
      console.error('[useAppState] Failed to reload graph:', err);
      // On failure, set empty graph so UI doesn't get stuck on "Loading graph..."
      setGraph({ nodes: [], relationships: [] });
    } finally {
      setLoading(false);
    }
  }, [currentProject, currentRepo, syncBuildStatus]);

  // Load file content
  const loadFileContent = useCallback(async (filePath: string) => {
    if (fileContentsRef.current.has(filePath) || !currentRepo) return;

    try {
      const content = await fetchFile(filePath, currentRepo.name);
      fileContentsRef.current.set(filePath, content);
      setFileCacheVersion(v => v + 1);
    } catch (err) {
      console.error('Failed to load file:', err);
    }
  }, [currentRepo]);

  // Toggle label visibility
  const toggleLabelVisibility = useCallback((label: NodeLabel) => {
    setVisibleLabels(prev => {
      if (prev.includes(label)) {
        return prev.filter(l => l !== label);
      }
      return [...prev, label];
    });
  }, []);

  // Toggle edge visibility
  const toggleEdgeVisibility = useCallback((edgeType: EdgeType) => {
    setVisibleEdgeTypes(prev => {
      if (prev.includes(edgeType)) {
        return prev.filter(e => e !== edgeType);
      }
      return [...prev, edgeType];
    });
  }, []);

  // Code references
  const addCodeReference = useCallback((ref: CodeReference) => {
    setCodeReferences(prev => {
      // Avoid duplicates
      if (prev.some(r => r.id === ref.id)) return prev;
      return [...prev, ref];
    });
  }, []);

  const removeCodeReference = useCallback((id: string) => {
    setCodeReferences(prev => prev.filter(r => r.id !== id));
  }, []);

  const clearCodeReferences = useCallback(() => {
    setCodeReferences([]);
  }, []);

  // File cache management
  const getCachedFile = useCallback((filePath: string): string | undefined => {
    return fileContentsRef.current.get(filePath);
  }, []);

  const setCachedFile = useCallback((filePath: string, content: string) => {
    fileContentsRef.current.set(filePath, content);
    setFileCacheVersion(v => v + 1);
  }, []);

  const clearFileCache = useCallback((filePath: string) => {
    fileContentsRef.current.delete(filePath);
    setFileCacheVersion(v => v + 1);
  }, []);

  const clearAllFileCache = useCallback(() => {
    fileContentsRef.current.clear();
    setFileCacheVersion(v => v + 1);
  }, []);

  // Legacy panel helpers — 转发到 panelRegistry
  const openRightPanel = useCallback(() => openPanel('rightPanel'), [openPanel]);
  const closeRightPanel = useCallback(() => closePanel('rightPanel'), [closePanel]);
  const openCodePanel = useCallback(() => openPanel('codePanel'), [openPanel]);
  const openSettingsPanel = useCallback(() => openPanel('settings'), [openPanel]);
  const closeSettingsPanel = useCallback(() => closePanel('settings'), [closePanel]);

  // Legacy 分析面板 helpers — 转发到 panelRegistry
  const openCodeHealth = useCallback(() => openPanel('codeHealth'), [openPanel]);
  const closeCodeHealth = useCallback(() => closePanel('codeHealth'), [closePanel]);
  const openGraphAnalytics = useCallback(() => openPanel('graphAnalytics'), [openPanel]);
  const closeGraphAnalytics = useCallback(() => closePanel('graphAnalytics'), [closePanel]);
  const openImpactAnalysis = useCallback(() => openPanel('impactAnalysis'), [openPanel]);
  const closeImpactAnalysis = useCallback(() => closePanel('impactAnalysis'), [closePanel]);
  const openCfgPanel = useCallback(() => openPanel('cfgPanel'), [openPanel]);
  const closeCfgPanel = useCallback(() => closePanel('cfgPanel'), [closePanel]);
  const openSequencePanel = useCallback(() => openPanel('sequencePanel'), [openPanel]);
  const closeSequencePanel = useCallback(() => closePanel('sequencePanel'), [closePanel]);
  const openArchRulesPanel = useCallback(() => openPanel('archRulesPanel'), [openPanel]);
  const closeArchRulesPanel = useCallback(() => closePanel('archRulesPanel'), [closePanel]);
  const openProcessPanel = useCallback(() => openPanel('processPanel'), [openPanel]);
  const closeProcessPanel = useCallback(() => closePanel('processPanel'), [closePanel]);
  const openTerminal = useCallback(() => openPanel('terminal'), [openPanel]);
  const closeTerminal = useCallback(() => closePanel('terminal'), [closePanel]);
  const loadAgents = useCallback(async () => {
    try {
      const result = await fetchAgents();
      setAgents(result.agents || []);
    } catch (err) {
      console.error('[useAppState] Failed to load agents:', err);
    }
  }, []);

  // Legacy 分析面板 helpers — 已由上面的 panelRegistry helpers 替代
  // (openCodeHealth, closeCodeHealth 等已在前面的 "Legacy panel helpers" 段定义)

  // Auto-load repos and projects on mount
  useEffect(() => {
    loadProjects();
    loadRepos();
    loadAgents();
  }, []);

  // On mount, load already-running plugin panels from backend registry.
  // This handles the case where plugins auto-started (onStartup) before the
  // frontend SSE connection was established, or the page was refreshed.
  useEffect(() => {
    fetch(`${getBaseURL()}/v1/plugins/registry/panels`)
      .then(r => r.ok ? r.json() : [])
      .then((entries: Array<{ pluginId: string; id: string; def: any; endpoint: string; status: string }>) => {
        for (const entry of entries) {
          if (panelRegistry.has(entry.id)) continue;
          if (entry.status !== 'running') continue;
          registerPanel({
            id: entry.id,
            title: entry.def.title || entry.id,
            icon: entry.def.icon || 'Puzzle',
            // Plugin panels always render in the left column — map 'right' → 'left'
            location: (entry.def.location === 'right') ? 'left' : (entry.def.location || 'left'),
            activator: entry.def.activator || 'activityBar',
            footerSlot: entry.def.footerSlot,
            order: typeof entry.def.order === 'number' ? entry.def.order : 10,
            isPlugin: true,
            pluginId: entry.pluginId,
            endpoint: entry.endpoint,
            asyncLoader: () => {
              const entryPath = `/plugins/${entry.pluginId}/ui/index.js`;
              return import(/* @vite-ignore */ entryPath);
            },
          });
        }
      })
      .catch(err => console.warn('[useAppState] Failed to load running plugin panels on mount:', err));
  }, []);

  // Register built-in panels (synchronous — before first render)
  // useRef 防止重复注册（React StrictMode 下 AppProvider 会渲染两次）
  //
  // order 排序约定：
  //   - 内置 activityBar 按钮：0~9（Home=0, FolderTree=1, AI=2）
  //   - 插件 activityBar 按钮：10+（由 manifest.json 自声明，默认 10）
  //   - footer / gearMenu / node-select 等其他 activator 的 order 各自独立排序
  const panelsRegisteredRef = useRef(false);
  if (!panelsRegisteredRef.current) {
    panelsRegisteredRef.current = true;
    // --- activityBar 内置按钮 (order 0~9) ---
    registerPanel({ id: 'home', title: 'activitybar:projectsTitle', icon: 'Home', location: 'left-top', activator: 'activityBar', action: 'popup', order: 0 });
    registerPanel({ id: 'fileTree', title: 'activitybar:files', icon: 'FolderTree', location: 'left-top', activator: 'activityBar', component: FileTreePanel, order: 1 });
    registerPanel({ id: 'rightPanel', title: 'panels:chat.newConversation', icon: 'Sparkles', location: 'left', activator: 'activityBar', component: LazyRightPanel, order: 2 });

    // --- footer 面板 ---
    registerPanel({ id: 'codeHealth', title: 'panels:codeHealth.title', icon: 'Activity', location: 'left', activator: 'footer', component: CodeHealthPanel, order: 0 });
    registerPanel({ id: 'graphAnalytics', title: 'panels:graphAnalytics.title', icon: 'BarChart3', location: 'left', activator: 'footer', component: GraphAnalyticsPanel, order: 1 });
    registerPanel({ id: 'impactAnalysis', title: 'panels:impact.title', icon: 'Radar', location: 'left', activator: 'footer', component: ImpactAnalysisPanel, order: 2 });
    registerPanel({ id: 'cfgPanel', title: 'panels:cfg.title', icon: 'Route', location: 'left', activator: 'footer', component: CfgDataflowPanel, order: 3 });
    registerPanel({ id: 'sequencePanel', title: 'panels:sequence.title', icon: 'ArrowLeftRight', location: 'left', activator: 'footer', component: SequencePanel, order: 4 });
    registerPanel({ id: 'archRulesPanel', title: 'panels:rules.title', icon: 'Shield', location: 'modal', activator: 'footer', component: ArchRulesPanel, order: 5 });
    registerPanel({ id: 'processPanel', title: 'panels:flow.title', icon: 'Workflow', location: 'modal', activator: 'footer', component: ProcessPanel, order: 6 });
    registerPanel({ id: 'terminal', title: 'activitybar:terminal.title', icon: 'Terminal', location: 'center-bottom', activator: 'command', standalone: false, component: TerminalPanel, order: 7 });

    // --- node-select ---
    registerPanel({ id: 'codePanel', title: 'activitybar:code', icon: 'Code2', location: 'right', activator: 'node-select', component: CodeReferencesPanel, order: 1 });

    // --- gearMenu ---
    registerPanel({ id: 'settings', title: 'settings:title', icon: 'Settings', location: 'modal', activator: 'gearMenu', component: SettingsPanel, order: 0 });
    registerPanel({ id: 'extensions', title: 'extensions:title', icon: 'Puzzle', location: 'left', activator: 'gearMenu', component: ExtensionsPanel, order: 1 });
  }

  // Listen for SSE events: config changes + plugin lifecycle
  useEventStream({
    onConfigChange: useCallback(() => {
      setConfigVersion(v => v + 1);
    }, []),
    onPluginStarted: useCallback((data: import('./useEventStream').PluginLifecycleEvent) => {
      console.log('[useAppState] Plugin started:', data.pluginId);
      // Fetch plugin details from registry and register panels
      fetch(`${getBaseURL()}/v1/plugins/registry/panels`)
        .then(r => r.ok ? r.json() : [])
        .then((entries: Array<{ pluginId: string; id: string; def: any; endpoint: string; status: string }>) => {
          // Only process panels belonging to the started plugin
          const pluginPanels = entries.filter(e => e.pluginId === data.pluginId);
          for (const entry of pluginPanels) {
            const def = entry.def;
            // Skip if already registered (avoid duplicates)
            if (panelRegistry.has(entry.id)) continue;
            registerPanel({
              id: entry.id,
              title: def.title || entry.id,
              icon: def.icon || 'Puzzle',
              // Plugin panels always render in the left column — map 'right' → 'left'
              location: (def.location === 'right') ? 'left' : (def.location || 'left'),
              activator: def.activator || 'activityBar',
              footerSlot: def.footerSlot,
              order: typeof def.order === 'number' ? def.order : 10,
              isPlugin: true,
              pluginId: data.pluginId,
              endpoint: data.endpoint || entry.endpoint,
              asyncLoader: () => {
                // 动态 import 插件 UI 组件 — 走 axons 静态路由 /plugins/:id/*
                const entryPath = `/plugins/${data.pluginId}/ui/index.js`;
                return import(/* @vite-ignore */ entryPath);
              },
            });
          }
        })
        .catch(err => console.warn('[useAppState] Failed to fetch plugin panels:', err));
    }, [registerPanel, panelRegistry]),
    onPluginStopped: useCallback((data: import('./useEventStream').PluginLifecycleEvent) => {
      console.log('[useAppState] Plugin stopped:', data.pluginId);
      // Unregister all panels belonging to this plugin
      const allPanels = panelRegistry.getAll();
      for (const p of allPanels) {
        if (p.pluginId === data.pluginId) {
          unregisterPanel(p.id);
        }
      }
    }, [unregisterPanel, panelRegistry]),
    onPluginCrashed: useCallback((data: import('./useEventStream').PluginLifecycleEvent) => {
      console.warn('[useAppState] Plugin crashed:', data.pluginId, 'restarts:', data.restarts);
      // Same as stopped — remove panels
      const allPanels = panelRegistry.getAll();
      for (const p of allPanels) {
        if (p.pluginId === data.pluginId) {
          unregisterPanel(p.id);
        }
      }
    }, [unregisterPanel, panelRegistry]),
    onPluginUninstalled: useCallback((data: import('./useEventStream').PluginLifecycleEvent) => {
      console.log('[useAppState] Plugin uninstalled:', data.pluginId);
      const allPanels = panelRegistry.getAll();
      for (const p of allPanels) {
        if (p.pluginId === data.pluginId) {
          unregisterPanel(p.id);
        }
      }
    }, [unregisterPanel, panelRegistry]),
    onLocaleAvailable: useCallback((data: import('./useEventStream').LocaleAvailableEvent) => {
      // Update the locale→pluginId mapping so i18next http-backend can resolve loadPath
      const map = (window as any).__localePluginMap as Record<string, string> || {};
      map[data.locale] = data.pluginId;
      (window as any).__localePluginMap = map;

      // When loadPath previously returned '' (missing mapping), i18next-http-backend
      // calls callback(null, {}) — an empty bundle is stored for that namespace.
      // Even after the mapping is fixed, changeLanguage skips reloading because
      // hasResourceBundle returns true. Use switchLocale to remove stale empty
      // bundles and force a fresh fetch.
      import('../i18n').then(({ switchLocale, default: i18n }) => {
        const lng = typeof i18n.language === 'string' ? i18n.language : i18n.language?.[0];
        if (lng && (lng === data.locale || lng.startsWith(data.locale.split('-')[0]))) {
          switchLocale(data.locale);
        }
      });

      // Dispatch custom event so SettingsPanel can refresh locale list
      window.dispatchEvent(new CustomEvent('locale-available'));
    }, []),
    onLocaleUnavailable: useCallback((data: import('./useEventStream').LocaleUnavailableEvent) => {
      // Remove from locale→pluginId mapping
      const map = (window as any).__localePluginMap as Record<string, string> || {};
      delete map[data.locale];
      (window as any).__localePluginMap = map;

      window.dispatchEvent(new CustomEvent('locale-unavailable'));
    }, []),
  });

  // Track if initial load has been done
  const initialLoadDoneRef = React.useRef(false);

  // Auto-select repo and load graph after repos and project are ready
  useEffect(() => {
    // Only run once after repos are loaded
    if (repos.length === 0 || currentRepo || initialLoadDoneRef.current) {
      return;
    }

    // Wait for currentProject to be set before loading graph
    if (!currentProject) {
      console.log('[useAppState] Waiting for currentProject to be set...');
      return;
    }

    // Mark initial load as done
    initialLoadDoneRef.current = true;

    // Now select the first repo and load graph with the correct project
    console.log('[useAppState] Auto-selecting first repo with project:', currentProject.id);
    selectRepo(repos[0]);
  }, [repos, currentRepo, currentProject, selectRepo]);

  // Reload graph when project changes (skip initial mount)
  const prevProjectIdRef = React.useRef<string | undefined>(undefined);
  useEffect(() => {
    const projectId = currentProject?.id;
    // Skip if it's the initial mount or project ID hasn't changed
    if (prevProjectIdRef.current !== undefined && prevProjectIdRef.current !== projectId) {
      console.log('[useAppState] Project changed from', prevProjectIdRef.current, 'to', projectId);
      reloadGraph();
    }
    prevProjectIdRef.current = projectId;
  }, [currentProject?.id, reloadGraph]);

  // Legacy panel computed booleans & setters — extracted as stable callbacks so
  // useMemo value does not change on every render due to inline closures.
  const setRightPanelOpen = useCallback((open: boolean) => open ? openPanel('rightPanel') : closePanel('rightPanel'), [openPanel, closePanel]);
  const setFilePanelOpen = useCallback((open: boolean) => open ? openPanel('fileTree') : closePanel('fileTree'), [openPanel, closePanel]);
  const setCodePanelOpen = useCallback((open: boolean) => open ? openPanel('codePanel') : closePanel('codePanel'), [openPanel, closePanel]);

  const value: AppState = useMemo(() => ({
    viewMode,
    setViewMode,
    projects,
    loadProjects,
    projectsLoaded,
    currentProject,
    setCurrentProject,
    reloadGraph,
    buildingProjects,
    markProjectBuilding,
    isProjectBuilding,

    // Build progress (lifted from App.tsx)
    buildProgress,
    updateBuildProgress,
    clearBuildProgress,
    syncBuildStatus,
    repos,
    setRepos,
    currentRepo,
    setCurrentRepo,
    loading,
    isLoading: loading,
    error,
    setLoading,
    graph,
    setGraph,
    fileCacheVersion,
    pendingDelta,
    applyDelta,
    applyDeltaToKnowledgeGraph,
    selectedNode,
    setSelectedNode,

    // FileTree expanded paths
    fileTreeExpandedPaths,
    setFileTreeExpandedPaths,
    fileTreeExpandedPathsReady,

    // Panel registry — 新接口
    panelRegistry,
    openPanels,
    isPanelOpen,
    openPanel,
    closePanel,
    togglePanel,
    registerPanel,
    unregisterPanel,
    getPanelsByLocation,
    getPanelsByActivator,
    registryVersion,

    // Panel-specific state
    codeContent,
    setCodeContent,
    codeLoading,
    setCodeLoading,
    activeFilePath,
    setActiveFilePath,

    // Filter state
    visibleLabels,
    setVisibleLabels,
    toggleLabelVisibility,
    visibleEdgeTypes,
    toggleEdgeVisibility,
    depthFilter,
    setDepthFilter,
    layoutMode,
    setLayoutMode,

    // Code references
    codeReferences,
    addCodeReference,
    removeCodeReference,
    clearCodeReferences,

    // File cache
    getCachedFile,
    setCachedFile,
    clearFileCache,
    clearAllFileCache,

    // Legacy panel aliases — computed from openPanels
    isRightPanelOpen: openPanels.has('rightPanel'),
    setRightPanelOpen,
    openRightPanel,
    closeRightPanel,
    isFilePanelOpen: openPanels.has('fileTree'),
    setFilePanelOpen,
    isCodePanelOpen: openPanels.has('codePanel'),
    setCodePanelOpen,
    openCodePanel,
    isSettingsPanelOpen: openPanels.has('settings'),
    openSettingsPanel,
    closeSettingsPanel,
    isCodeHealthOpen: openPanels.has('codeHealth'),
    openCodeHealth,
    closeCodeHealth,
    isGraphAnalyticsOpen: openPanels.has('graphAnalytics'),
    openGraphAnalytics,
    closeGraphAnalytics,
    isImpactAnalysisOpen: openPanels.has('impactAnalysis'),
    openImpactAnalysis,
    closeImpactAnalysis,
    isCfgPanelOpen: openPanels.has('cfgPanel'),
    openCfgPanel,
    closeCfgPanel,
    isSequencePanelOpen: openPanels.has('sequencePanel'),
    openSequencePanel,
    closeSequencePanel,
    isArchRulesPanelOpen: openPanels.has('archRulesPanel'),
    openArchRulesPanel,
    closeArchRulesPanel,
    isProcessPanelOpen: openPanels.has('processPanel'),
    openProcessPanel,
    closeProcessPanel,
    isTerminalOpen: openPanels.has('terminal'),
    openTerminal,
    closeTerminal,

    // Actions
    loadRepos,
    selectRepo,
    loadFileContent,

    // Agent profiles
    agents,
    currentAgentId,
    setCurrentAgentId,
    loadAgents,
    configVersion,
  }), [
    viewMode, setViewMode, projects, loadProjects, projectsLoaded, currentProject,
    setCurrentProject, reloadGraph, buildingProjects, markProjectBuilding, isProjectBuilding,
    buildProgress, updateBuildProgress, clearBuildProgress, syncBuildStatus,
    repos, setRepos, currentRepo, setCurrentRepo, loading, error, setLoading,
    graph, setGraph, fileCacheVersion, pendingDelta, applyDelta, applyDeltaToKnowledgeGraph,
    selectedNode, setSelectedNode,
    fileTreeExpandedPaths, setFileTreeExpandedPaths, fileTreeExpandedPathsReady,
    panelRegistry, openPanels, isPanelOpen, openPanel, closePanel, togglePanel,
    registerPanel, unregisterPanel, getPanelsByLocation, getPanelsByActivator, registryVersion,
    codeContent, setCodeContent, codeLoading, setCodeLoading, activeFilePath, setActiveFilePath,
    visibleLabels, setVisibleLabels, toggleLabelVisibility, visibleEdgeTypes, toggleEdgeVisibility,
    depthFilter, setDepthFilter, layoutMode, setLayoutMode,
    codeReferences, addCodeReference, removeCodeReference, clearCodeReferences,
    getCachedFile, setCachedFile, clearFileCache, clearAllFileCache,
    setRightPanelOpen, openRightPanel, closeRightPanel,
    setFilePanelOpen, setCodePanelOpen, openCodePanel,
    openSettingsPanel, closeSettingsPanel,
    openCodeHealth, closeCodeHealth,
    openGraphAnalytics, closeGraphAnalytics,
    openImpactAnalysis, closeImpactAnalysis,
    openCfgPanel, closeCfgPanel,
    openSequencePanel, closeSequencePanel,
    openArchRulesPanel, closeArchRulesPanel,
    openProcessPanel, closeProcessPanel,
    openTerminal, closeTerminal,
    loadRepos, selectRepo, loadFileContent,
    agents, currentAgentId, setCurrentAgentId, loadAgents, configVersion,
  ]);

  return React.createElement(AppContext.Provider, { value }, children);
}

export function useAppState() {
  const context = useContext(AppContext);
  if (!context) {
    throw new Error('useAppState must be used within AppStateProvider');
  }
  return context;
}