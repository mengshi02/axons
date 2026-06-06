import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { X, ChevronRight, Copy, Check, Search, View, Code2, Pencil, Save, RotateCcw, Terminal } from 'lucide-react';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { useAppState } from '../hooks/useAppState';
import { useTheme } from '../hooks/useTheme';
import type { GraphNode, GraphRelationship } from '../types/graph';
import type { PanelComponentProps } from '../lib/panelRegistry';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { VirtualCodeView } from './VirtualCodeView';
import { PrismCodeEditor } from './PrismCodeEditor';
import { useTranslation } from 'react-i18next';
import { MarkdownLink } from './chat/markdownCache';

const NODE_TYPE_COLORS: Record<string, string> = {
  Folder: '#6366f1',
  File: '#3b82f6',
  Function: '#10b981',
  Class: '#f59e0b',
  Method: '#14b8a6',
  Interface: '#ec4899',
  Variable: '#64748b',
  Import: '#475569',
  Type: '#a78bfa',
};

// Image file extensions that should be previewed instead of shown as text
const IMAGE_EXTENSIONS = new Set([
  'png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg', 'ico', 'tiff', 'tif', 'avif',
]);

// Video file extensions that should be played via <video> instead of shown as text
const VIDEO_EXTENSIONS = new Set([
  'mp4', 'webm', 'ogg', 'm4v',
]);

/** Check if a file path points to an image file */
function isImageFile(path: string): boolean {
  const ext = path.split('.').pop()?.toLowerCase() || '';
  return IMAGE_EXTENSIONS.has(ext);
}

/** Check if a file path points to a video file */
function isVideoFile(path: string): boolean {
  const ext = path.split('.').pop()?.toLowerCase() || '';
  return VIDEO_EXTENSIONS.has(ext);
}

/** Get MIME type from file extension (image) */
function getImageMimeType(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() || '';
  const mimeMap: Record<string, string> = {
    png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg',
    gif: 'image/gif', bmp: 'image/bmp', webp: 'image/webp',
    svg: 'image/svg+xml', ico: 'image/x-icon', tiff: 'image/tiff',
    tif: 'image/tiff', avif: 'image/avif',
  };
  return mimeMap[ext] || 'image/png';
}

/** Get MIME type from file extension (video) */
function getVideoMimeType(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() || '';
  const mimeMap: Record<string, string> = {
    mp4: 'video/mp4', webm: 'video/webm', ogg: 'video/ogg', m4v: 'video/mp4',
  };
  return mimeMap[ext] || 'video/mp4';
}

const MIN_WIDTH = 320;
const MAX_WIDTH = 800;
const DEFAULT_WIDTH = 384; // w-96

export const CodeReferencesPanel = React.memo(function CodeReferencesPanel({ onClose: _onClose }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const {
    graph,
    selectedNode,
    setCodePanelOpen,
    codeContent,
    setCodeContent,
    codeLoading,
    setCodeLoading,
    currentProject,
    getCachedFile,
    setCachedFile,
    activeFilePath,
    fileCacheVersion,
  } = useAppState();

  const { theme } = useTheme();

  const [activeTab, setActiveTab] = useState<'code' | 'refs'>('code');
  const [copied, setCopied] = useState(false);
  const [highlightRange, setHighlightRange] = useState<{ startLine: number; endLine: number } | null>(null);

  // Virtual scroll: target line to scroll to (1-based)
  const [scrollToLine, setScrollToLine] = useState<number | null>(null);

  // Binary preview: stores mimeType when backend indicates isBinary
  const [binaryMimeType, setBinaryMimeType] = useState<string | null>(null);

  // Markdown preview mode (for .md files)
  const [mdPreviewMode, setMdPreviewMode] = useState(true); // true = preview, false = source

  // Edit mode state
  const [isEditMode, setIsEditMode] = useState(false);
  const [editedContent, setEditedContent] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [vimMode, setVimMode] = useState(false);

  // Search state
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResultsList, setSearchResultsList] = useState<number[]>([]);
  const [currentSearchIndex, setCurrentSearchIndex] = useState(0);
  const currentSearchLine = searchResultsList.length > 0 ? searchResultsList[currentSearchIndex] : null;
  // Pre-computed Set for O(1) lookup in virtual rendering
  const searchResultSet = useMemo(() => new Set(searchResultsList), [searchResultsList]);

  // Resizable width — direct DOM writes during drag (bypass React re-renders per frame).
  // Only setState on mouseup to sync the final width back to React.
  const [panelWidth, setPanelWidth] = useState(DEFAULT_WIDTH);
  const isResizing = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(DEFAULT_WIDTH);
  const pendingWidthRef = useRef(DEFAULT_WIDTH);
  const panelRef = useRef<HTMLDivElement | null>(null);
  // rAF scheduling — coalesce mousemove events into one DOM write per frame.
  const rafIdRef = useRef<number | null>(null);

  const handleResizeMouseDown = (e: React.MouseEvent) => {
    e.preventDefault();
    isResizing.current = true;
    startX.current = e.clientX;
    startWidth.current = panelWidth;
    pendingWidthRef.current = panelWidth;
    // Promote to GPU layer only during drag
    if (panelRef.current) panelRef.current.style.willChange = 'width';
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    document.body.classList.add('axons-resizing');
  };

  useEffect(() => {
    const flush = () => {
      rafIdRef.current = null;
      if (panelRef.current) {
        panelRef.current.style.width = `${pendingWidthRef.current}px`;
      }
    };
    const finishDrag = () => {
      if (!isResizing.current) return;
      isResizing.current = false;
      // Cancel any pending rAF — we'll write the final value directly below
      if (rafIdRef.current != null) {
        cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      // Remove GPU layer promotion after drag
      if (panelRef.current) panelRef.current.style.willChange = '';
      // Ensure the final width is painted
      if (panelRef.current) {
        panelRef.current.style.width = `${pendingWidthRef.current}px`;
      }
      // Sync the final width back to React state
      setPanelWidth(pendingWidthRef.current);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      document.body.classList.remove('axons-resizing');
    };
    const onMouseMove = (e: MouseEvent) => {
      if (!isResizing.current) return;
      // drag handle on left side, drag left to increase width
      const delta = startX.current - e.clientX;
      const newWidth = Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startWidth.current + delta));
      pendingWidthRef.current = newWidth;
      // Schedule at most one DOM write per animation frame
      if (rafIdRef.current == null) {
        rafIdRef.current = requestAnimationFrame(flush);
      }
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
      if (rafIdRef.current != null) {
        cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
    };
  }, []);

  // Pinned node: the last node that was explicitly selected via the graph.
  // When selectedNode becomes null (e.g. graph reload clears selection), we
  // keep showing the pinned node's content instead of going blank.
  const pinnedNodeId = useRef<string | null>(null);
  if (selectedNode !== null) {
    pinnedNodeId.current = selectedNode;
  }
  const activeNodeId = selectedNode ?? pinnedNodeId.current;

  // Get active node data (live from graph, or null if node no longer exists)
  const liveNodeData = useMemo(() => {
    if (!graph || !activeNodeId) return null;
    return graph.nodes.find(n => n.id === activeNodeId) ?? null;
  }, [graph, activeNodeId]);

  // Keep a cached copy of the last valid node data so the panel never goes
  // blank when a graph reload temporarily removes/replaces the node.
  const pinnedNodeData = useRef<typeof liveNodeData>(null);
  if (liveNodeData !== null) {
    pinnedNodeData.current = liveNodeData;
  }
  const nodeData = liveNodeData ?? pinnedNodeData.current;

  // Get related nodes (incoming and outgoing relationships)
  const relatedNodes = useMemo(() => {
    if (!graph || !activeNodeId) return { incoming: [], outgoing: [] };

    const incoming: { rel: GraphRelationship; node: GraphNode }[] = [];
    const outgoing: { rel: GraphRelationship; node: GraphNode }[] = [];

    graph.relationships.forEach(rel => {
      // API returns sourceId/targetId (camelCase), fallback to startNode/endNode for compatibility
      const sourceId = rel.sourceId || rel.startNode;
      const targetId = rel.targetId || rel.endNode;

      if (targetId === activeNodeId) {
        const sourceNode = graph.nodes.find(n => n.id === sourceId);
        if (sourceNode) incoming.push({ rel, node: sourceNode });
      }
      if (sourceId === activeNodeId) {
        const targetNode = graph.nodes.find(n => n.id === targetId);
        if (targetNode) outgoing.push({ rel, node: targetNode });
      }
    });

    return { incoming, outgoing };
  }, [graph, activeNodeId]);

  // Get the file path and line range for the selected node
  const nodeFileInfo = useMemo(() => {
    if (!nodeData) return null;

    // For File nodes, use the file path directly
    if (nodeData.label === 'File') {
      const filePath = nodeData.properties.path || nodeData.properties.filePath;
      return filePath ? { filePath, startLine: undefined, endLine: undefined } : null;
    }

    // For other nodes (Function, Class, Method, etc.), find the containing file and line range
    const filePath = nodeData.properties.filePath || nodeData.properties.path;
    // Ensure startLine/endLine are numbers (API may return strings)
    const startLine = typeof nodeData.properties.startLine === 'string'
      ? parseInt(nodeData.properties.startLine, 10) : nodeData.properties.startLine;
    const endLine = typeof nodeData.properties.endLine === 'string'
      ? parseInt(nodeData.properties.endLine, 10) : nodeData.properties.endLine;

    if (!filePath) return null;

    return { filePath, startLine, endLine };
  }, [nodeData]);

  // Load code content when node is selected.
  // When nodeFileInfo is null it means no node has ever been selected yet —
  // don't clear content (the pinned content should stay visible).
  // When activeFilePath is set (file tree selection), skip — FileTreePanel manages loading.
  useEffect(() => {
    if (!nodeFileInfo) {
      return;
    }

    // If a file was selected from the file tree, don't override its content
    if (activeFilePath) {
      return;
    }

    const { filePath, startLine, endLine } = nodeFileInfo;

    // Set highlight range for non-File nodes
    if (startLine !== undefined && endLine !== undefined) {
      setHighlightRange({ startLine, endLine });
    } else {
      setHighlightRange(null);
    }

    // Clear binary mime type when loading a new file from graph node
    setBinaryMimeType(isImageFile(filePath) ? getImageMimeType(filePath) : isVideoFile(filePath) ? getVideoMimeType(filePath) : null);

    // Check cache first
    const cached = getCachedFile(filePath);
    if (cached) {
      setCodeContent(cached);
      return;
    }

    // Clear stale content immediately before fetching new file,
    // preventing the previous file's content from showing under the new file's path.
    setCodeContent(null);

    // Fetch from server if not cached
    setCodeLoading(true);
    const params = new URLSearchParams({ path: filePath });
    if (currentProject?.id) {
      params.append('project_id', currentProject.id);
    }
    fetch(`/api/file?${params.toString()}`)
      .then(res => res.json())
      .then(data => {
        const content = data.content ?? null;
        if (content !== null) {
          setCachedFile(filePath, content);
        }
        // If backend indicates this is a binary/image file, store MIME type
        if (data.isBinary === 'true' && data.mimeType) {
          setBinaryMimeType(data.mimeType);
        }
        setCodeContent(content);
      })
      .catch(() => setCodeContent(null))
      .finally(() => setCodeLoading(false));
  }, [nodeFileInfo, activeFilePath, setCodeContent, setCodeLoading, currentProject?.id, getCachedFile, setCachedFile, fileCacheVersion]);

  // Sync highlightRange and codeContent when activeFilePath changes.
  // Bug fix: when switching from graph node to file tree, highlightRange was stale;
  // when switching back from file tree to the same graph node, codeContent wasn't refreshed.
  const prevActiveFilePathRef = useRef<string | null>(null);
  useEffect(() => {
    const prev = prevActiveFilePathRef.current;
    prevActiveFilePathRef.current = activeFilePath;

    if (activeFilePath) {
      // File tree selection: clear highlight range from graph node
      setHighlightRange(null);
      // Set binaryMimeType based on file extension for file tree selections
      setBinaryMimeType(isImageFile(activeFilePath) ? getImageMimeType(activeFilePath) : isVideoFile(activeFilePath) ? getVideoMimeType(activeFilePath) : null);
    } else if (prev !== null && !activeFilePath && nodeFileInfo) {
      // activeFilePath went from non-null → null (switching back to graph node from file tree).
      // If nodeFileInfo hasn't changed, the main useEffect above won't re-trigger,
      // so we need to restore the node's file content and highlight range here.
      const { filePath, startLine, endLine } = nodeFileInfo;

      if (startLine !== undefined && endLine !== undefined) {
        setHighlightRange({ startLine, endLine });
      }

      const cached = getCachedFile(filePath);
      if (cached) {
        setCodeContent(cached);
      }
    }
  }, [activeFilePath, nodeFileInfo, setHighlightRange, setCodeContent, getCachedFile, fileCacheVersion]);

  // Scroll to highlighted line when code content loads or highlight range changes
  useEffect(() => {
    if (highlightRange && codeContent) {
      setScrollToLine(highlightRange.startLine);
    }
  }, [highlightRange, codeContent]);

  // Search functionality
  useEffect(() => {
    if (!searchQuery || !codeContent) {
      setSearchResultsList([]);
      setCurrentSearchIndex(0);
      return;
    }

    const lines = codeContent.split('\n');
    const results: number[] = [];
    const query = searchQuery.toLowerCase();

    lines.forEach((line, index) => {
      if (line.toLowerCase().includes(query)) {
        results.push(index + 1); // 1-based line numbers
      }
    });

    setSearchResultsList(results);
    setCurrentSearchIndex(0);
  }, [searchQuery, codeContent]);

  // Scroll to current search result (uses virtual scroll scrollToLine)
  const scrollToSearchResult = (index: number) => {
    if (searchResultsList.length === 0) return;
    const targetLine = searchResultsList[index];
    setScrollToLine(targetLine);
  };

  const handleSearchPrev = () => {
    if (searchResultsList.length === 0) return;
    const newIndex = currentSearchIndex > 0 ? currentSearchIndex - 1 : searchResultsList.length - 1;
    setCurrentSearchIndex(newIndex);
    scrollToSearchResult(newIndex);
  };

  const handleSearchNext = () => {
    if (searchResultsList.length === 0) return;
    const newIndex = currentSearchIndex < searchResultsList.length - 1 ? currentSearchIndex + 1 : 0;
    setCurrentSearchIndex(newIndex);
    scrollToSearchResult(newIndex);
  };

  const handleCopy = async () => {
    if (codeContent) {
      await navigator.clipboard.writeText(codeContent);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  // Save edited file content
  const handleSave = async () => {
    if (editedContent === null || !filePath) return;
    setIsSaving(true);
    setSaveError(null);
    try {
      const params = new URLSearchParams({ path: filePath });
      if (currentProject?.id) {
        params.append('project_id', currentProject.id);
      }
      const res = await fetch(`/api/file?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: filePath, content: editedContent }),
      });
      if (!res.ok) {
        throw new Error(`Save failed: ${res.statusText}`);
      }
      setCachedFile(filePath, editedContent);
      setCodeContent(editedContent);
      setIsEditMode(false);
      setEditedContent(null);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setIsSaving(false);
    }
  };

  const handleEnterEdit = () => {
    setEditedContent(codeContent);
    setIsEditMode(true);
    setSaveError(null);
  };

  const handleCancelEdit = () => {
    setIsEditMode(false);
    setEditedContent(null);
    setSaveError(null);
  };

  // Stable callback for editor updates
  const handleEditorUpdate = useCallback((value: string) => {
    setEditedContent(value);
  }, []);

  // CodeReferencesPanel is always rendered when open (App.tsx controls visibility via panelRegistry)
  // When no graph node is selected but a file was opened from the file tree,
  // derive filePath from activeFilePath directly.
  // activeFilePath takes priority — it means the user explicitly clicked a file
  // in the file tree and we should show that, not the last graph node's file.
  const filePath = activeFilePath || nodeFileInfo?.filePath || '';

  const getLanguage = (path: string) => {
    const ext = path.split('.').pop()?.toLowerCase();
    const fileName = path.split('/').pop()?.toLowerCase();

    const langMap: Record<string, string> = {
      // Programming languages - mapped to Prism language identifiers
      ts: 'typescript',
      tsx: 'tsx',
      js: 'javascript',
      jsx: 'jsx',
      mjs: 'javascript',
      cjs: 'javascript',
      go: 'go',
      py: 'python',
      pyi: 'python',
      rs: 'rust',
      java: 'java',
      c: 'c',
      h: 'c',
      cc: 'cpp',
      cpp: 'cpp',
      cxx: 'cpp',
      hpp: 'cpp',
      cs: 'csharp',
      // Config files
      json: 'json',
      yaml: 'yaml',
      yml: 'yaml',
      toml: 'toml',
      xml: 'markup',
      ini: 'ini',
      properties: 'ini', // Prism has no 'properties' - use ini as closest match
      // Shell scripts
      sh: 'bash',
      bash: 'bash',
      zsh: 'bash',
      // Styles
      css: 'css',
      scss: 'scss',
      less: 'less',
      sass: 'sass',
      // Templates
      html: 'markup',
      htm: 'markup',
      ejs: 'ejs',
      hbs: 'handlebars',
      // Documentation
      md: 'markdown',
      txt: 'text',
      rst: 'rest',
      adoc: 'asciidoc',
      // Data/Query
      sql: 'sql',
      graphql: 'graphql',
      gql: 'graphql',
      csv: 'csv',
      // Other
      proto: 'protobuf',
      lock: 'text',
    };

    // Special files by name
    if (fileName === 'dockerfile' || fileName?.startsWith('dockerfile.')) return 'docker';
    if (fileName === 'makefile' || fileName?.startsWith('makefile.')) return 'makefile';
    if (fileName === 'jenkinsfile') return 'groovy';
    if (fileName === 'vagrantfile') return 'ruby';
    if (fileName === 'procfile') return 'text';
    if (fileName === 'gemfile' || fileName === 'rakefile') return 'ruby';
    if (fileName === '.gitignore' || fileName === '.dockerignore') return 'bash'; // Prism has no 'gitignore' - use bash as closest match
    if (fileName === '.env' || fileName?.startsWith('.env.')) return 'bash';
    if (fileName === '.editorconfig') return 'ini';

    return langMap[ext || ''] || 'text';
  };

  // Show placeholder only when no graph node AND no file-tree file is active
  if (!nodeData && !activeFilePath && !codeLoading) {
    return (
      <div
        ref={panelRef}
        className="h-full shrink-0 bg-surface border-l border-border-subtle flex flex-col overflow-hidden relative"
        style={{ width: panelWidth, contain: 'layout style' }}
      >
        {/* Resize handle — VS Code sash style: 4px hit area, transparent by default, full accent on hover */}
        <div
          className="absolute left-0 top-0 bottom-0 cursor-col-resize z-10 group"
          style={{ width: '4px' }}
          onMouseDown={handleResizeMouseDown}
        >
          <div className="absolute left-0 top-0 bottom-0 opacity-0 group-hover:opacity-100 transition-opacity bg-accent" style={{ width: '4px' }} />
        </div>
        {/* Header */}
        <div className="flex items-center justify-between px-3 py-2 h-[38px] border-b border-border-subtle">
          <span className="text-xs font-medium text-text-muted">No node selected</span>
          <button
            onClick={() => setCodePanelOpen(false)}
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
        {/* Empty state */}
        <div className="flex-1 flex items-center justify-center text-text-muted text-sm">
          Click a node in the graph to view its code
        </div>
      </div>
    );
  }

  return (
    <div
      ref={panelRef}
      className="h-full shrink-0 bg-surface border-l border-border-subtle flex flex-col overflow-hidden relative"
      style={{ width: panelWidth, contain: 'layout style' }}
    >
      {/* Resize handle — VS Code sash style: 4px hit area, transparent by default, full accent on hover */}
      <div
        className="absolute left-0 top-0 bottom-0 cursor-col-resize z-10 group"
        style={{ width: '4px' }}
        onMouseDown={handleResizeMouseDown}
      >
        <div className="absolute left-0 top-0 bottom-0 opacity-0 group-hover:opacity-100 transition-opacity bg-accent" style={{ width: '4px' }} />
      </div>
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 h-[38px] border-b border-border-subtle">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className="w-2.5 h-2.5 rounded-full flex-shrink-0"
            style={{ backgroundColor: activeFilePath ? '#3b82f6' : nodeData ? (NODE_TYPE_COLORS[nodeData.label] || '#6b7280') : '#3b82f6' }}
          />
          <span className="text-xs font-medium text-text-primary truncate">
            {activeFilePath ? (filePath.split('/').pop() || filePath) : nodeData ? nodeData.properties.name : (filePath.split('/').pop() || filePath)}
          </span>
        </div>
        <button
          onClick={() => setCodePanelOpen(false)}
          className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border-subtle">
        <button
          onClick={() => setActiveTab('code')}
          className={`flex-1 px-4 py-1.5 text-xs font-medium transition-colors ${activeTab === 'code'
              ? 'text-accent border-b-2 border-accent bg-accent/5'
              : 'text-text-muted hover:text-text-secondary'
            }`}
        >
          Code
        </button>
        <button
          onClick={() => setActiveTab('refs')}
          className={`flex-1 px-4 py-1.5 text-xs font-medium transition-colors ${activeTab === 'refs'
              ? 'text-accent border-b-2 border-accent bg-accent/5'
              : 'text-text-muted hover:text-text-secondary'
            }`}
        >
          References ({relatedNodes.incoming.length + relatedNodes.outgoing.length})
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-hidden">
        {activeTab === 'code' && (
          <div className="h-full flex flex-col">
            {/* File path and search */}
            {filePath && (
              <div className="px-4 py-1 bg-elevated border-b border-border-subtle flex items-center gap-2">
                <span className="text-xs text-text-muted font-mono truncate flex-1">
                  {filePath}
                </span>

                {/* Markdown preview toggle (only for .md files) */}
                {filePath.toLowerCase().endsWith('.md') && (
                  <div className="flex items-center gap-0.5 bg-surface border border-border-subtle rounded p-0.5">
                    <button
                      onClick={() => setMdPreviewMode(true)}
                      className={`flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors ${mdPreviewMode
                        ? 'bg-accent text-white'
                        : 'text-text-muted hover:text-text-primary'
                        }`}
                      title={t('codeRef.preview')}
                    >
                      <View className="w-3 h-3" />
                      <span>{t('codeRef.preview')}</span>
                    </button>
                    <button
                      onClick={() => setMdPreviewMode(false)}
                      className={`flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors ${!mdPreviewMode
                        ? 'bg-accent text-white'
                        : 'text-text-muted hover:text-text-primary'
                        }`}
                      title={t('codeRef.source')}
                    >
                      <Code2 className="w-3 h-3" />
                      <span>{t('codeRef.source')}</span>
                    </button>
                  </div>
                )}

                {/* Search input */}
                <div className="flex items-center gap-1">
                  <div className="relative">
                    <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-text-muted" />
                    <input
                      type="text"
                      placeholder={t('codeRef.searchPlaceholder')}
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      className="pl-6 pr-2 py-1 w-32 text-xs bg-surface border border-border-subtle rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                    />
                  </div>
                  {searchResultsList.length > 0 && (
                    <div className="flex items-center gap-1">
                      <button
                        onClick={handleSearchPrev}
                        className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded"
                        title={t('codeRef.previous')}
                      >
                        <ChevronRight className="w-3 h-3 rotate-180" />
                      </button>
                      <span className="text-xs text-text-muted min-w-[3rem] text-center">
                        {currentSearchIndex + 1}/{searchResultsList.length}
                      </span>
                      <button
                        onClick={handleSearchNext}
                        className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded"
                        title={t('codeRef.next')}
                      >
                        <ChevronRight className="w-3 h-3" />
                      </button>
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Code content */}
            {codeLoading ? (
              <div className="flex-1 flex items-center justify-center">
                <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
              </div>
            ) : codeContent !== null ? (
                isImageFile(filePath) ? (
                  // ── Image preview ──
                  <div className="flex-1 overflow-auto flex items-center justify-center bg-elevated p-4">
                    <img
                      src={`data:${binaryMimeType || getImageMimeType(filePath)};base64,${codeContent}`}
                      alt={filePath.split('/').pop() || 'Image'}
                      className="max-w-full max-h-full object-contain rounded shadow-lg"
                      style={{ imageRendering: 'auto' }}
                    />
                  </div>
                ) : isVideoFile(filePath) ? (
                  // ── Video preview (streamed via /api/file/raw for browser compatibility) ──
                  <div className="flex-1 overflow-auto flex items-center justify-center bg-elevated p-4">
                    <video
                      src={`/api/file/raw?path=${encodeURIComponent(filePath)}&project_id=${encodeURIComponent(currentProject?.id || '')}`}
                      controls
                      className="max-w-full max-h-full rounded shadow-lg"
                      style={{ outline: 'none' }}
                    >
                      Your browser does not support video playback.
                    </video>
                  </div>
                ) : (
                <div className="flex-1 overflow-hidden relative">
                  {/* Toolbar */}
                  <div className="absolute top-2 right-2 z-10 flex items-center gap-1">
                    {isEditMode ? (
                      <>
                        <button
                          onClick={handleCancelEdit}
                          className="p-1.5 bg-surface border border-border-subtle rounded text-text-muted hover:text-text-primary transition-colors"
                          title={t('common:action.cancel')}
                        >
                          <RotateCcw className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => setVimMode(v => !v)}
                          className={`p-1.5 border rounded transition-colors ${vimMode
                            ? 'bg-accent/90 border-accent text-white hover:bg-accent'
                            : 'bg-surface border-border-subtle text-text-muted hover:text-text-primary'
                            }`}
                          title={vimMode ? 'Disable Vim mode' : 'Enable Vim mode'}
                        >
                          <Terminal className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={handleSave}
                          disabled={isSaving}
                          className="p-1.5 bg-accent/90 border border-accent rounded text-white hover:bg-accent transition-colors disabled:opacity-50"
                          title={t('common:action.save')}
                        >
                          {isSaving ? (
                            <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />
                          ) : (
                            <Save className="w-3.5 h-3.5" />
                          )}
                        </button>
                      </>
                    ) : (
                      <>
                          <button
                            onClick={handleCopy}
                            className="p-1.5 bg-surface border border-border-subtle rounded text-text-muted hover:text-text-primary transition-colors"
                            title={t('codeRef.copyCode')}
                          >
                            {copied ? <Check className="w-3.5 h-3.5 text-node-function" /> : <Copy className="w-3.5 h-3.5" />}
                          </button>
                        <button
                          onClick={handleEnterEdit}
                          className="p-1.5 bg-surface border border-border-subtle rounded text-text-muted hover:text-text-primary transition-colors"
                            title={t('codeRef.editFile')}
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                      </>
                    )}
                  </div>

                  {/* Save error banner */}
                  {saveError && (
                    <div className="absolute top-0 left-0 right-0 z-20 px-3 py-2 bg-red-500/90 text-white text-xs flex items-center justify-between">
                      <span>{saveError}</span>
                      <button onClick={() => setSaveError(null)} className="ml-2 underline">Dismiss</button>
                    </div>
                  )}

                  {/* Edit mode: PrismCodeEditor | Read-only: VirtualCodeView */}
                  {isEditMode ? (
                    <PrismCodeEditor
                      code={codeContent}
                      language={getLanguage(filePath)}
                      themeName={theme}
                      onUpdate={handleEditorUpdate}
                      scrollToLine={highlightRange?.startLine ?? null}
                      vimMode={vimMode}
                    />
                  ) : filePath.toLowerCase().endsWith('.md') && mdPreviewMode ? (
                    <div className="absolute inset-0 overflow-auto p-6 prose prose-invert prose-sm max-w-none">
                              <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ a: MarkdownLink }}>
                                {codeContent}
                              </ReactMarkdown>
                    </div>
                    ) : (
                        <VirtualCodeView
                          code={codeContent}
                          language={getLanguage(filePath)}
                          theme={theme === 'moon' ? oneDark : oneLight}
                          highlightRange={highlightRange}
                          searchResultSet={searchResultSet}
                          currentSearchLine={currentSearchLine}
                          scrollToLine={scrollToLine}
                        />
                  )}
              </div>
                    )
            ) : (
              <div className="flex-1 flex items-center justify-center text-text-muted text-sm">
                    {(nodeData?.label === 'File' || activeFilePath) ? 'No code content available' : 'Select a file to view code'}
              </div>
            )}
          </div>
        )}

        {activeTab === 'refs' && (
          <div className="h-full overflow-y-auto">
            {/* Incoming references - others referencing this node */}
            {relatedNodes.incoming.length > 0 && (
              <div className="p-4">
                <h3 className="text-xs font-semibold text-text-muted uppercase tracking-wider mb-2">
                  Used by ({relatedNodes.incoming.length})
                </h3>
                <div className="space-y-1">
                  {relatedNodes.incoming.map(({ rel, node }) => (
                    <div
                      key={`${rel.sourceId || rel.startNode}-${rel.type}-${rel.targetId || rel.endNode}`}
                      className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-hover cursor-pointer transition-colors"
                      title={`${node.properties.name} ${rel.type.toLowerCase()} ${nodeData?.properties.name}`}
                    >
                      <span
                        className="w-2 h-2 rounded-full flex-shrink-0"
                        style={{ backgroundColor: NODE_TYPE_COLORS[node.label] || '#6b7280' }}
                      />
                      <span className="text-sm text-text-primary truncate flex-1">{node.properties.name}</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-accent/10 text-accent font-medium">{rel.type}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Outgoing references - this node references others */}
            {relatedNodes.outgoing.length > 0 && (
              <div className="p-4 border-t border-border-subtle">
                <h3 className="text-xs font-semibold text-text-muted uppercase tracking-wider mb-2">
                  Uses ({relatedNodes.outgoing.length})
                </h3>
                <div className="space-y-1">
                  {relatedNodes.outgoing.map(({ rel, node }) => (
                    <div
                      key={`${rel.sourceId || rel.startNode}-${rel.type}-${rel.targetId || rel.endNode}`}
                      className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-hover cursor-pointer transition-colors"
                      title={`${nodeData?.properties.name} ${rel.type.toLowerCase()} ${node.properties.name}`}
                    >
                      <span
                        className="w-2 h-2 rounded-full flex-shrink-0"
                        style={{ backgroundColor: NODE_TYPE_COLORS[node.label] || '#6b7280' }}
                      />
                      <span className="text-sm text-text-primary truncate flex-1">{node.properties.name}</span>
                      <span className="text-xs px-1.5 py-0.5 rounded bg-accent/10 text-accent font-medium">{rel.type}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* No references */}
            {relatedNodes.incoming.length === 0 && relatedNodes.outgoing.length === 0 && (
              <div className="flex items-center justify-center h-32 text-text-muted text-sm">
                No references found
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
});