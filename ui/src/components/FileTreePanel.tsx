import React, { useState, useCallback, useEffect, useRef } from 'react';
import {
  ChevronRight, ChevronDown, Folder, FolderOpen,
  FileCode, File, FileJson, FileText, Braces, Settings,
  FilePlus, FolderPlus, ChevronsUpDown, RefreshCw,
  Copy, Scissors, Clipboard, Trash2, Pencil, Terminal,
  Image, Video,
} from 'lucide-react';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { ConfirmDialog } from './ConfirmDialog';
import {
  listFileTree, createFile, deleteFile, createFolder,
  deleteFolder, renameEntry, copyEntry, type FileTreeEntry,
} from '../services/api';
import { ContextMenu, type ContextMenuEntry } from './ContextMenu';
import { useTranslation } from 'react-i18next';
import { insertEntryToTree, removeEntryFromTree, moveEntryInTree, isValidTreeStructure } from '../utils/fileTreeUpdate';
import { useFileTreeUndoRedo } from '../hooks/useFileTreeUndoRedo';

// ─── File icon helper ────────────────────────────────────────────────────────

function getFileIcon(name: string) {
  const ext = name.split('.').pop()?.toLowerCase();
  switch (ext) {
    case 'ts': case 'tsx': case 'js': case 'jsx':
      return <FileCode className="w-4 h-4 text-node-function flex-shrink-0" />;
    case 'json':
      return <FileJson className="w-4 h-4 text-node-variable flex-shrink-0" />;
    case 'go':
      return <FileCode className="w-4 h-4 text-node-class flex-shrink-0" />;
    case 'md':
      return <FileText className="w-4 h-4 text-text-muted flex-shrink-0" />;
    case 'css': case 'scss':
      return <Braces className="w-4 h-4 text-node-interface flex-shrink-0" />;
    case 'yaml': case 'yml': case 'toml':
      return <Settings className="w-4 h-4 text-text-muted flex-shrink-0" />;
    case 'png': case 'jpg': case 'jpeg': case 'gif': case 'bmp': case 'webp': case 'svg': case 'ico': case 'tiff': case 'tif': case 'avif':
      return <Image className="w-4 h-4 text-node-variable flex-shrink-0" />;
    case 'mp4': case 'webm': case 'ogg': case 'm4v':
      return <Video className="w-4 h-4 text-node-variable flex-shrink-0" />;
    default:
      return <File className="w-4 h-4 text-text-muted flex-shrink-0" />;
  }
}

// ─── System clipboard file-path reader ──────────────────────────────────────

/**
 * Try to read file paths from the system clipboard (e.g. files copied in
 * Finder / Explorer).  Returns an empty array when the Clipboard API is
 * unavailable, permission is denied, or the clipboard contains no file refs.
 *
 * Supported MIME types:
 *   - "public.file-url"  (macOS Finder)
 *   - "text/uri-list"    (Linux / some Windows apps)
 */
/**
 * Read file paths from the system clipboard via the daemon backend.
 * The daemon uses osascript (macOS), PowerShell (Windows), or xclip (Linux)
 * to read file references that WebViews cannot access directly.
 */
async function readSystemClipboardFiles(): Promise<string[]> {
  try {
    const res = await fetch('/api/clipboard/files');
    if (!res.ok) return [];
    const data = await res.json();
    return data.files ?? [];
  } catch {
    return [];
  }
}

/** Extract the basename from a posix-style path (works for both / and \). */
function pathBasename(p: string): string {
  const segs = p.replace(/\\/g, '/').split('/');
  return segs[segs.length - 1] || '';
}

// ─── Types ───────────────────────────────────────────────────────────────────

interface ContextMenuState {
  x: number;
  y: number;
  entry: FileTreeEntry | null; // null = blank area context menu
}

interface ClipboardState {
  entries: FileTreeEntry[];
  mode: 'copy' | 'cut';
}

interface NewItemState {
  parentPath: string;
  isDir: boolean;
  depth: number;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Escape special regex characters in a string. */
function escapeRegex(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Resolve a unique destination name when a conflict exists.
 *  e.g. "foo.ts" → "foo copy.ts", "foo copy.ts" → "foo copy 2.ts", etc.
 *  For directories (no ext): "mydir" → "mydir copy", "mydir copy" → "mydir copy 2" */
function resolveUniqueName(existing: FileTreeEntry[], desiredName: string): string {
  if (!existing.some(e => e.name === desiredName)) return desiredName;

  const dotIdx = desiredName.lastIndexOf('.');
  const base = dotIdx > 0 ? desiredName.slice(0, dotIdx) : desiredName;
  const ext = dotIdx > 0 ? desiredName.slice(dotIdx) : '';

  let idx = 1;
  for (const e of existing) {
    const m = e.name.match(new RegExp(`^${escapeRegex(base)} copy(?: (\\d+))?${escapeRegex(ext)}$`));
    if (m) {
      const n = m[1] ? parseInt(m[1], 10) : 1;
      if (n >= idx) idx = n + 1;
    }
  }
  return idx === 1 ? `${base} copy${ext}` : `${base} copy ${idx}${ext}`;
}

/** Find the children at a given directory path in the tree. */
function findEntriesAtPath(tree: FileTreeEntry[], dirPath: string): FileTreeEntry[] {
  if (dirPath === '.' || dirPath === '') return tree;
  const parts = dirPath.split('/');
  let current = tree;
  for (const part of parts) {
    const found = current.find(e => e.name === part && e.is_dir);
    if (!found?.children) return [];
    current = found.children;
  }
  return current;
}

/** Find an entry in the tree by path. */
function findEntryInTree(tree: FileTreeEntry[], path: string): FileTreeEntry | null {
  for (const e of tree) {
    if (e.path === path) return e;
    if (e.children) {
      const found = findEntryInTree(e.children, path);
      if (found) return found;
    }
  }
  return null;
}

/** Check if `targetPath` is the same as `srcPath` or a descendant of it.
 *  e.g. isDescendantOrSelf("abc", "abc") → true
 *       isDescendantOrSelf("abc", "abc/sub") → true
 *       isDescendantOrSelf("abc", "def") → false */
function isDescendantOrSelf(srcPath: string, targetPath: string): boolean {
  const src = srcPath === '.' ? '' : srcPath + '/';
  const target = targetPath === '.' ? '' : targetPath;
  return target === srcPath || target.startsWith(src);
}

/** Sentinel array representing an unloaded directory's children.
 *  Using a stable module-level reference allows mergeChildrenAtPath to
 *  distinguish "no children loaded yet" (=== UNLOADED) from "children were
 *  fetched and the directory is genuinely empty" (!== UNLOADED, length === 0).
 *  This fixes the case where expanding an empty directory left it blank
 *  because the length-based check mistakenly treated an empty fetch result
 *  as "target not found". */
const UNLOADED_SENTINEL: FileTreeEntry[] = [];

/** Merge fetched children into the entry at the given directory path.
 *  Immutable — returns a new array. Used for lazy loading.
 *
 *  CRITICAL: when recursing into sub-directories, we must traverse into
 *  ANY directory (even one with `children: undefined`) so that deeply
 *  nested target paths can still be found and merged.  The old check
 *  `e.is_dir && e.children` would skip directories whose children
 *  hadn't been loaded yet, causing `mergeChildrenAtPath` to silently
 *  fail for deep paths — the user would see an expanded-but-empty dir.
 *
 *  When a directory's children haven't been loaded yet (`children` is
 *  undefined), we use the UNLOADED_SENTINEL as the recursion base so we
 *  can distinguish "target not found inside an unloaded dir" (sentinel
 *  reference is returned unchanged) from "target IS the dir and it's
 *  genuinely empty" (a new [] is returned for the matching node).
 */
function mergeChildrenAtPath(entries: FileTreeEntry[], dirPath: string, children: FileTreeEntry[]): FileTreeEntry[] {
  if (dirPath === '.' || dirPath === '') {
    // Root level — replace root entries
    return children;
  }
  return entries.map(e => {
    if (e.path === dirPath && e.is_dir) {
      return { ...e, children };
    }
    // Recurse into ANY directory, even if its children haven't been
    // loaded yet (children === undefined).  We pass UNLOADED_SENTINEL
    // instead of a fresh [] so the recursive call can detect whether
    // the target was actually found:
    //   - if updatedChildren === subEntries (same reference), nothing changed
    //   - if subEntries === UNLOADED_SENTINEL and updatedChildren.length === 0,
    //     the target was NOT inside this unloaded dir — don't replace undefined with []
    //   - otherwise the target was found and merged — propagate upward
    if (e.is_dir) {
      const subEntries = e.children ?? UNLOADED_SENTINEL;
      const updatedChildren = mergeChildrenAtPath(subEntries, dirPath, children);
      if (updatedChildren !== subEntries) {
        if (subEntries === UNLOADED_SENTINEL && updatedChildren.length === 0) {
          // Target was not found inside this unloaded directory —
          // don't replace undefined with []
          return e;
        }
        return { ...e, children: updatedChildren };
      }
    }
    return e;
  });
}

/** Collect all visible paths in depth-first order (only expanded directories are traversed). */
function collectVisiblePaths(tree: FileTreeEntry[], expandedPaths: Set<string>): string[] {
  const result: string[] = [];
  function walk(entries: FileTreeEntry[]) {
    for (const e of entries) {
      result.push(e.path);
      if (e.is_dir && e.children && expandedPaths.has(e.path)) {
        walk(e.children);
      }
    }
  }
  walk(tree);
  return result;
}

// ─── Inline new-item input ────────────────────────────────────────────────────

interface NewItemInputProps {
  parentPath: string;
  isDir: boolean;
  depth: number;
  onCommit: (name: string) => void;
  onCancel: () => void;
}

function NewItemInput({ parentPath: _parentPath, isDir, depth, onCommit, onCancel }: NewItemInputProps) {
  const [value, setValue] = useState('');
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => { ref.current?.focus(); }, []);

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') { const t = value.trim(); if (t) onCommit(t); else onCancel(); }
    else if (e.key === 'Escape') onCancel();
  };

  return (
    <div
      className="flex items-center gap-1.5 px-2 py-1"
      style={{ paddingLeft: `${depth * 12 + 8}px` }}
    >
      <span className="w-4 h-4 flex-shrink-0" />
      {isDir
        ? <Folder className="w-4 h-4 text-node-folder flex-shrink-0" />
        : <File className="w-4 h-4 text-text-muted flex-shrink-0" />}
      <input
        ref={ref}
        className="flex-1 bg-surface border border-accent rounded px-1 text-sm text-text-primary outline-none"
        value={value}
        placeholder={isDir ? 'Folder name' : 'File name'}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={handleKey}
        onBlur={onCancel}
      />
    </div>
  );
}

// ─── TreeNode component ──────────────────────────────────────────────────────

interface TreeNodeProps {
  entry: FileTreeEntry;
  depth: number;
  expandedPaths: Set<string>;
  selectedPaths: Set<string>;
  renamingPath: string | null;
  newItem: NewItemState | null;
  dragOverTargetPath: string | null;
  onToggle: (path: string) => void;
  onSelect: (entry: FileTreeEntry, e: React.MouseEvent) => void;
  onContextMenu: (e: React.MouseEvent, entry: FileTreeEntry) => void;
  onRenameCommit: (entry: FileTreeEntry, newName: string) => void;
  onRenameCancel: () => void;
  onNewItemCommit: (name: string) => void;
  onNewItemCancel: () => void;
  onDragStart: (entry: FileTreeEntry, e: React.DragEvent) => void;
  onDragOver: (entry: FileTreeEntry | null, e: React.DragEvent) => void;
  onDragLeave: () => void;
  onDrop: (targetEntry: FileTreeEntry | null, e: React.DragEvent) => void;
  onDragEnd: () => void;
}

function TreeNodeItem({
  entry, depth, expandedPaths, selectedPaths, renamingPath, newItem,
  dragOverTargetPath,
  onToggle, onSelect, onContextMenu, onRenameCommit, onRenameCancel,
  onNewItemCommit, onNewItemCancel,
  onDragStart, onDragOver, onDragLeave, onDrop, onDragEnd,
}: TreeNodeProps) {
  const isExpanded = expandedPaths.has(entry.path);
  const isSelected = selectedPaths.has(entry.path);
  const isRenaming = renamingPath === entry.path;
  const isDragOver = dragOverTargetPath === entry.path && entry.is_dir;

  const [renameValue, setRenameValue] = useState(entry.name);
  const renameRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (isRenaming) {
      setRenameValue(entry.name);
      setTimeout(() => {
        renameRef.current?.select();
      }, 30);
    }
  }, [isRenaming, entry.name]);

  const handleClick = (e: React.MouseEvent) => {
    if (entry.is_dir) onToggle(entry.path);
    onSelect(entry, e);
  };

  const handleRenameKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      const trimmed = renameValue.trim();
      if (trimmed && trimmed !== entry.name) onRenameCommit(entry, trimmed);
      else onRenameCancel();
    } else if (e.key === 'Escape') {
      onRenameCancel();
    }
  };

  // Check if we should show a new item input inside this directory
  const showNewItemHere = entry.is_dir && isExpanded && newItem && newItem.parentPath === entry.path;

  return (
    <div>
      <div
        className={`flex items-center gap-1.5 px-2 py-1 cursor-pointer transition-colors hover:bg-hover rounded select-none
          ${isSelected ? 'bg-accent/20 text-accent' : 'text-text-secondary'}
          ${isDragOver ? 'bg-accent/10 ring-1 ring-inset ring-accent/40' : ''}`}
        style={{ paddingLeft: `${depth * 12 + 8}px` }}
        draggable={!isRenaming}
        onClick={handleClick}
        onContextMenu={(e) => { e.preventDefault(); onContextMenu(e, entry); }}
        onDragStart={(e) => onDragStart(entry, e)}
        onDragOver={(e) => onDragOver(entry, e)}
        onDragLeave={onDragLeave}
        onDrop={(e) => onDrop(entry, e)}
        onDragEnd={onDragEnd}
      >
        {/* Expand arrow */}
        <span className="w-4 h-4 flex items-center justify-center flex-shrink-0 text-text-muted">
          {entry.is_dir
            ? (isExpanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />)
            : null}
        </span>

        {/* Icon */}
        {entry.is_dir
          ? (isExpanded
            ? <FolderOpen className="w-4 h-4 text-node-folder flex-shrink-0" />
            : <Folder className="w-4 h-4 text-node-folder flex-shrink-0" />)
          : getFileIcon(entry.name)}

        {/* Name / rename input */}
        {isRenaming ? (
          <input
            ref={renameRef}
            className="flex-1 bg-surface border border-accent rounded px-1 text-sm text-text-primary outline-none"
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            onKeyDown={handleRenameKeyDown}
            onBlur={onRenameCancel}
            onClick={(e) => e.stopPropagation()}
          />
        ) : (
          <span className="truncate text-sm flex-1">{entry.name}</span>
        )}

        {/* Children count for folders */}
        {entry.is_dir && !isRenaming && (
          <span className="text-xs text-text-muted ml-auto flex-shrink-0">
            {entry.children?.length ?? ''}
          </span>
        )}
      </div>

      {/* Children + inline new item input */}
      {entry.is_dir && isExpanded && (
        <div>
          {/* New item input at top of expanded directory */}
          {showNewItemHere && (
            <NewItemInput
              parentPath={entry.path}
              isDir={newItem!.isDir}
              depth={depth + 1}
              onCommit={onNewItemCommit}
              onCancel={onNewItemCancel}
            />
          )}
          {entry.children?.map((child) => (
            <TreeNodeItem
              key={child.path}
              entry={child}
              depth={depth + 1}
              expandedPaths={expandedPaths}
              selectedPaths={selectedPaths}
              renamingPath={renamingPath}
              newItem={newItem}
              dragOverTargetPath={dragOverTargetPath}
              onToggle={onToggle}
              onSelect={onSelect}
              onContextMenu={onContextMenu}
              onRenameCommit={onRenameCommit}
              onRenameCancel={onRenameCancel}
              onNewItemCommit={onNewItemCommit}
              onNewItemCancel={onNewItemCancel}
              onDragStart={onDragStart}
              onDragOver={onDragOver}
              onDragLeave={onDragLeave}
              onDrop={onDrop}
              onDragEnd={onDragEnd}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Main panel ──────────────────────────────────────────────────────────────

export const FileTreePanel = React.memo(function FileTreePanel({ onSelectNode: _onSelectNode }: PanelComponentProps) {
  const { t } = useTranslation('activitybar');
  const {
    currentProject, openCodePanel,
    setCodeContent, setCodeLoading, getCachedFile, setCachedFile,
    setActiveFilePath,
    fileTreeExpandedPaths, setFileTreeExpandedPaths,
    fileTreeExpandedPathsReady,
  } = useAppState();

  const [entries, setEntries] = useState<FileTreeEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [lastClickedPath, setLastClickedPath] = useState<string | null>(null);
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [deleteTargets, setDeleteTargets] = useState<FileTreeEntry[]>([]);
  const [clipboard, setClipboard] = useState<ClipboardState | null>(null);
  const [newItem, setNewItem] = useState<NewItemState | null>(null);
  const [dragOverTargetPath, setDragOverTargetPath] = useState<string | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);

  // Track if we've restored expanded state after initial load.
  // Stores the projectId for which the restore has been completed.
  // When the project changes, the old "restored" status is automatically
  // invalidated because the stored projectId no longer matches.
  // This prevents the race where the restore effect runs before the
  // AppProvider (parent) has cleared the old project's expanded paths,
  // causing the effect to mark itself as "restored" using stale data.
  const restoredProjectIdRef = useRef<string>('');

  const projectId = currentProject?.id ?? '';

  // Use a ref for fileTreeExpandedPaths to avoid re-creating callbacks on every expand/collapse
  const expandedPathsRef = useRef(fileTreeExpandedPaths);
  expandedPathsRef.current = fileTreeExpandedPaths;

  // ── Load tree ──────────────────────────────────────────────────────────────

  const loadTree = useCallback(async () => {
    if (!projectId) return;
    setLoading(true);
    try {
      const result = await listFileTree(projectId, '.', false, true);
      // Reset lazy-load cache on full refresh
      loadedDirsRef.current = new Set();
      expandingRef.current = new Map();

      // Re-load children for all currently expanded directories so that
      // the expand state is preserved after a full tree refresh (e.g. undo/redo).
      // Without this, loadTree clears the entries but the expanded-paths remain,
      // causing the dirs to appear expanded-but-empty.
      //
      // RACE CONDITION FIX: do NOT call setEntries(result.entries) here before
      // the async loop, then setEntries(currentEntries) at the end.  The two
      // separate writes create a window where a concurrent toggleExpand can
      // interleave a setEntries(prev => merge(prev,...)) that then races with
      // the second write, corrupting the accumulated state.
      // Solution: accumulate everything in a local variable and commit once.
      const expanded = expandedPathsRef.current;
      let currentEntries = result.entries;
      if (expanded.size > 0) {
        const sortedPaths = [...expanded].sort(
          (a, b) => a.split('/').length - b.split('/').length,
        );
        const stalePaths: string[] = [];
        for (const dirPath of sortedPaths) {
          try {
            const dirResult = await listFileTree(projectId, dirPath, false, true);
            currentEntries = mergeChildrenAtPath(currentEntries, dirPath, dirResult.entries);
            loadedDirsRef.current.add(dirPath);
          } catch (e: any) {
            if (e?.status === 404 || e?.code === 'NOT_FOUND') {
              stalePaths.push(dirPath);
            } else {
              console.error('Failed to reload expanded dir:', dirPath, e);
            }
          }
        }
        if (stalePaths.length > 0) {
          setFileTreeExpandedPaths((prev) => {
            const next = new Set(prev);
            for (const p of stalePaths) next.delete(p);
            return next;
          });
        }
      }
      // Single commit — no intermediate render with stale children
      setEntries(currentEntries);
    } catch (e) {
      console.error('FileTree load error', e);
    } finally {
      setLoading(false);
    }
  }, [projectId, setFileTreeExpandedPaths]);

  useEffect(() => {
    if (projectId) loadTree();
  }, [projectId, loadTree]);

  // ── Undo/Redo ──────────────────────────────────────────────────────────────

  const {
    undoStack, redoStack,
    pushUndoStack, executeUndo, executeRedo,
  } = useFileTreeUndoRedo(entries, setEntries, projectId, loadTree);

  // ── Loaded directories tracking (for lazy loading) ─────────────────────────

  const loadedDirsRef = useRef<Set<string>>(new Set());

  // In-flight expand requests — maps dirPath → Promise<void>.
  // If the same directory is expanded again while a request is already in
  // flight (e.g. fast double-click or slow network), the second call reuses
  // the same Promise instead of issuing a second network request.  Without
  // this, two concurrent listFileTree calls for the same path race and the
  // second setEntries write can overwrite the first, leaving children empty.
  const expandingRef = useRef<Map<string, Promise<void>>>(new Map());

  // Reset loadedDirs and expandingRef when project changes
  useEffect(() => {
    loadedDirsRef.current = new Set();
    expandingRef.current = new Map();
    // Don't reset restoredProjectIdRef here — the restore effect itself
    // checks restoredProjectIdRef against the current projectId,
    // so it naturally re-runs when the project changes.
  }, [projectId]);

  // ── Restore expanded state after initial load ──────────────────────────────

  // After the tree is loaded AND we have expanded paths from backend,
  // load the children for all expanded directories.
  //
  // RACE CONDITION FIX:
  // React runs child-component effects BEFORE parent-component effects.
  // When the project changes, FileTreePanel's restore effect can run
  // BEFORE AppProvider's effect that clears the old project's expanded
  // paths and sets fileTreeExpandedPathsReady=false.  This means the
  // restore effect would see the OLD project's expanded paths with
  // ready=true, mark itself as "restored", and then never re-run when
  // the NEW project's expanded paths arrive.
  //
  // Primary fix: setCurrentProject() in AppProvider now synchronously
  // sets ready=false and clears expanded paths, so FileTreePanel sees
  // the cleared state in the same render batch as the project change.
  //
  // Defense-in-depth: use a projectId-keyed ref (`restoredProjectIdRef`)
  // instead of a simple boolean.  The effect only considers itself
  // "restored" when the stored projectId matches the current one.
  useEffect(() => {
    if (entries.length === 0 || !projectId) return;
    // Don't run until the persisted expanded paths have been fetched from backend.
    if (!fileTreeExpandedPathsReady) return;

    // Already restored for THIS project — skip.
    if (restoredProjectIdRef.current === projectId) return;

    if (fileTreeExpandedPaths.size === 0) {
      // No expanded paths for this project — mark as restored.
      restoredProjectIdRef.current = projectId;
      return;
    }

    // Mark as restored for this project to prevent re-running
    restoredProjectIdRef.current = projectId;

    // Load children for all expanded paths.
    // CRITICAL: sort paths by depth (shallow first) so that parent
    // directories are loaded before their children.  Without this,
    // `mergeChildrenAtPath` cannot find a deep target if the
    // intermediate directory's children haven't been loaded yet.
    //
    // RACE CONDITION FIX (in-memory serial merge):
    //   The old code called setEntries(prev => merge(prev, ...)) inside each
    //   loop iteration.  Because the loop is async, React may batch-schedule
    //   those setEntries calls and the `prev` seen by iteration N+1 does NOT
    //   yet include the changes from iteration N — so deep paths silently fail
    //   to merge.  The fix: accumulate all merges into a local `current`
    //   variable (same pattern already used in loadTree), then call setEntries
    //   exactly once at the end.
    const loadExpandedDirs = async () => {
      const sortedPaths = [...fileTreeExpandedPaths].sort(
        (a, b) => a.split('/').length - b.split('/').length,
      );
      const stalePaths: string[] = [];
      // Start from the current entries snapshot and merge in-memory
      let current = entries;
      for (const path of sortedPaths) {
        if (!loadedDirsRef.current.has(path)) {
          try {
            const result = await listFileTree(projectId, path, false, true);
            current = mergeChildrenAtPath(current, path, result.entries);
            loadedDirsRef.current.add(path);
          } catch (e: any) {
            if (e?.status === 404 || e?.code === 'NOT_FOUND') {
              // Directory no longer exists – remove it from expanded paths silently
              stalePaths.push(path);
            } else {
              console.error('Failed to load expanded dir:', path, e);
            }
          }
        }
      }
      // Single setEntries write — avoids the stale-prev race
      setEntries(current);
      if (stalePaths.length > 0) {
        setFileTreeExpandedPaths((prev) => {
          const next = new Set(prev);
          for (const p of stalePaths) next.delete(p);
          return next;
        });
      }
    };

    loadExpandedDirs();
  }, [entries, fileTreeExpandedPaths, fileTreeExpandedPathsReady, projectId]);

  // ── Toggle expand (lazy: fetch children on first expand) ───────────────────

  const toggleExpand = useCallback(async (path: string) => {
    if (expandedPathsRef.current.has(path)) {
      // Collapse: just remove from fileTreeExpandedPaths
      setFileTreeExpandedPaths((prev) => {
        const next = new Set(prev);
        next.delete(path);
        return next;
      });
      return;
    }

    // Expand: fetch children on first visit (lazy loading).
    // RACE CONDITION FIX (IDE-style Promise reuse):
    //   If this directory is already being fetched (e.g. fast double-click or
    //   slow network), reuse the existing Promise instead of issuing a second
    //   network request.  Without this, two concurrent listFileTree calls
    //   race: the second setEntries call uses a stale `prev` snapshot and
    //   overwrites the first result, leaving the directory visually empty.
    if (!loadedDirsRef.current.has(path) && projectId) {
      let p = expandingRef.current.get(path);
      if (!p) {
        p = listFileTree(projectId, path, false, true)
          .then((result) => {
            setEntries((prev) => mergeChildrenAtPath(prev, path, result.entries));
            loadedDirsRef.current.add(path);
          })
          .catch((e: any) => {
            if (e?.status === 404 || e?.code === 'NOT_FOUND') {
              // Directory no longer exists — remove from expanded paths silently
              setFileTreeExpandedPaths((prev) => {
                const next = new Set(prev);
                next.delete(path);
                return next;
              });
              // Re-throw so the await below catches it and skips the expand
              throw e;
            }
            console.error('FileTree lazy load error', e);
            // Other errors: still expand (will show empty), swallow error
          })
          .finally(() => expandingRef.current.delete(path));
        expandingRef.current.set(path, p);
      }
      try {
        await p;
      } catch {
        // 404 path: directory gone, already cleaned up above — don't expand
        return;
      }
    }

    setFileTreeExpandedPaths((prev) => {
      const next = new Set(prev);
      next.add(path);
      return next;
    });
  }, [projectId, setFileTreeExpandedPaths]);

  /** Ensure a directory's children are loaded before expanding it.
   *  Used by context menu and toolbar actions that force-expand a directory.
   *  Reuses expandingRef so concurrent calls for the same path share one request. */
  const ensureDirLoaded = useCallback(async (path: string): Promise<void> => {
    if (loadedDirsRef.current.has(path) || !projectId) return;
    let p = expandingRef.current.get(path);
    if (!p) {
      p = listFileTree(projectId, path, false, true)
        .then((result) => {
          setEntries((prev) => mergeChildrenAtPath(prev, path, result.entries));
          loadedDirsRef.current.add(path);
        })
        .catch((e: any) => {
          if (e?.status === 404 || e?.code === 'NOT_FOUND') {
            setFileTreeExpandedPaths((prev) => {
              const next = new Set(prev);
              next.delete(path);
              return next;
            });
          } else {
            console.error('FileTree ensureDirLoaded error', e);
          }
        })
        .finally(() => expandingRef.current.delete(path));
      expandingRef.current.set(path, p);
    }
    await p;
  }, [projectId, setFileTreeExpandedPaths]);

  // ── Select ─────────────────────────────────────────────────────────────────

  /** Open a single file in the code panel (used for the last-clicked / anchor file). */
  const openFileInCodePanel = useCallback((entry: FileTreeEntry) => {
    if (!entry.is_dir) {
      setActiveFilePath(entry.path);
      openCodePanel();
      const cached = getCachedFile(entry.path);
      if (cached !== undefined) {
        setCodeContent(cached);
        return;
      }
      setCodeContent(null);
      setCodeLoading(true);
      const params = new URLSearchParams({ path: entry.path });
      if (currentProject?.id) params.append('project_id', currentProject.id);
      fetch(`/api/file?${params}`)
        .then(r => r.json())
        .then(data => {
          const content: string = data.content ?? null;
          if (content !== null) setCachedFile(entry.path, content);
          setCodeContent(content);
        })
        .catch(() => setCodeContent(null))
        .finally(() => setCodeLoading(false));
    }
  }, [openCodePanel, setActiveFilePath, currentProject?.id, getCachedFile, setCachedFile, setCodeContent, setCodeLoading]);

  const handleSelect = useCallback((entry: FileTreeEntry, e: React.MouseEvent) => {
    const isMod = e.metaKey || e.ctrlKey;   // ⌘ on Mac, Ctrl on Windows/Linux
    const isShift = e.shiftKey;

    if (isShift && lastClickedPath) {
      // ── Shift-click: range select from lastClickedPath to current ──
      const visiblePaths = collectVisiblePaths(entries, fileTreeExpandedPaths);
      const startIdx = visiblePaths.indexOf(lastClickedPath);
      const endIdx = visiblePaths.indexOf(entry.path);
      if (startIdx !== -1 && endIdx !== -1) {
        const [lo, hi] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
        const rangePaths = visiblePaths.slice(lo, hi + 1);
        setSelectedPaths(new Set(rangePaths));
      }
      // Open the clicked file in code panel
      openFileInCodePanel(entry);
    } else if (isMod) {
      // ── Cmd/Ctrl-click: toggle selection of clicked entry ──
      setSelectedPaths((prev) => {
        const next = new Set(prev);
        if (next.has(entry.path)) {
          next.delete(entry.path);
          // If the deselected entry was the anchor, clear it
          if (lastClickedPath === entry.path) setLastClickedPath(null);
        } else {
          next.add(entry.path);
          setLastClickedPath(entry.path);
        }
        return next;
      });
      openFileInCodePanel(entry);
    } else {
      // ── Plain click: single selection ──
      setSelectedPaths(new Set([entry.path]));
      setLastClickedPath(entry.path);
      openFileInCodePanel(entry);
    }
  }, [entries, fileTreeExpandedPaths, lastClickedPath, openFileInCodePanel]);

  // ── Collapse all ───────────────────────────────────────────────────────────

  const collapseAll = () => {
    setFileTreeExpandedPaths(new Set());
    // Keep loadedDirs cache — collapsing doesn't invalidate it
  };

  // ── Paste ──────────────────────────────────────────────────────────────────

  const handlePaste = useCallback(async (targetEntry: FileTreeEntry | null) => {
    if (!projectId) return;
    const targetDir = targetEntry
      ? (targetEntry.is_dir ? targetEntry.path : targetEntry.path.split('/').slice(0, -1).join('/') || '.')
      : '.';

    // ── Priority 1: system clipboard (files copied in Finder/Explorer) ────────
    try {
      const sysFiles = await readSystemClipboardFiles();
      if (sysFiles.length > 0) {
        // Try incremental update, fall back to loadTree on failure
        const sysUndoOps: Array<{ type: 'copy'; srcPath: string; dstPath: string; isDir: boolean; modTime: string }> = [];
        try {
          let updated = entries;
          const siblings = findEntriesAtPath(entries, targetDir);
          for (const srcPath of sysFiles) {
            const name = pathBasename(srcPath);
            const newName = resolveUniqueName(siblings, name);
            const dstPath = `${targetDir}/${newName}`;
            const result = await copyEntry(srcPath, dstPath, projectId);
            if (result.entry) {
              updated = insertEntryToTree(updated, targetDir, result.entry);
              // Update siblings to avoid name collision for subsequent copies
              siblings.push(result.entry);
              sysUndoOps.push({ type: 'copy', srcPath, dstPath, isDir: result.entry.is_dir, modTime: result.entry.mod_time ?? '' });
            } else {
              // No entry returned — cannot do incremental update
              throw new Error('Missing entry in copy response');
            }
          }
          if (isValidTreeStructure(updated)) {
            setClipboard(null);
            setEntries(updated);
            // Mark targetDir as loaded so lazy-expand won't overwrite incremental inserts
            loadedDirsRef.current.add(targetDir);
            // ── Undo stack: push copy operations ──
            if (sysUndoOps.length > 0) {
              pushUndoStack(sysUndoOps.length === 1 ? sysUndoOps[0] : { type: 'compound', ops: sysUndoOps });
            }
            return;
          }
        } catch {
          // incremental update failed, fall through to loadTree
        }
        setClipboard(null);
        await loadTree(); // fallback
        return;
      }
    } catch {
      // System clipboard read failed — fall through to internal clipboard
    }

    // ── Priority 2: internal clipboard (panel copy/cut) ──────────────────────
    if (!clipboard || clipboard.entries.length === 0) return;

    // Guard: cannot copy/cut a folder into itself or any of its descendants
    for (const ce of clipboard.entries) {
      if (ce.is_dir && isDescendantOrSelf(ce.path, targetDir)) {
        alert('Cannot paste a folder into itself or its subdirectory');
        return;
      }
    }

    try {
      // Collect undo operations — available in both incremental and fallback paths
      const undoOps: Array<{ type: 'copy'; srcPath: string; dstPath: string; isDir: boolean; modTime: string } | { type: 'move'; oldPath: string; newPath: string }> = [];
      // Try incremental update, fall back to loadTree on failure
      try {
        let updated = entries;
        const siblings = findEntriesAtPath(entries, targetDir);

        if (clipboard.mode === 'cut') {
          for (const ce of clipboard.entries) {
            const newPath = `${targetDir}/${ce.name}`;
            const result = await renameEntry(ce.path, newPath, projectId);
            if (result.entry) {
              updated = moveEntryInTree(updated, ce.path, result.entry);
              undoOps.push({ type: 'move', oldPath: ce.path, newPath });
            } else {
              throw new Error('Missing entry in rename response');
            }
          }
        } else {
          // Copy mode
          for (const ce of clipboard.entries) {
            const newName = resolveUniqueName(siblings, ce.name);
            const dstPath = `${targetDir}/${newName}`;
            const result = await copyEntry(ce.path, dstPath, projectId);
            if (result.entry) {
              updated = insertEntryToTree(updated, targetDir, result.entry);
              // Update siblings to avoid name collision for subsequent copies
              siblings.push(result.entry);
              undoOps.push({ type: 'copy', srcPath: ce.path, dstPath, isDir: ce.is_dir, modTime: result.entry.mod_time ?? '' });
            } else {
              throw new Error('Missing entry in copy response');
            }
          }
        }

        if (isValidTreeStructure(updated)) {
          if (clipboard.mode === 'cut') setClipboard(null);
          setEntries(updated);
          // Mark targetDir as loaded so lazy-expand won't overwrite incremental inserts
          loadedDirsRef.current.add(targetDir);
          // ── Undo stack: push operation(s) ──
          if (undoOps.length > 0) {
            pushUndoStack(undoOps.length === 1 ? undoOps[0] : { type: 'compound', ops: undoOps });
          }
          return;
        }
      } catch {
      // incremental update failed, fall through to loadTree
      }
      if (clipboard.mode === 'cut') setClipboard(null);
      await loadTree(); // fallback
      // ── Undo stack: push whatever operations we collected before fallback ──
      if (undoOps.length > 0) {
        pushUndoStack(undoOps.length === 1 ? undoOps[0] : { type: 'compound', ops: undoOps });
      }
    } catch (e: unknown) {
      alert((e as Error).message);
    }
  }, [clipboard, projectId, entries, loadTree, pushUndoStack]);

  // ── Drag and Drop ──────────────────────────────────────────────────────────

  const handleDragStart = useCallback((entry: FileTreeEntry, e: React.DragEvent) => {
    // If the dragged entry is part of the current multi-selection, drag all selected;
    // otherwise, drag just this entry.
    const dragEntries = selectedPaths.has(entry.path)
      ? [...selectedPaths].map(p => findEntryInTree(entries, p)).filter((e): e is FileTreeEntry => e !== null)
      : [entry];

    // Store dragged paths in dataTransfer for drop handler to read
    e.dataTransfer.effectAllowed = 'copyMove';
    e.dataTransfer.setData('application/x-file-tree-paths', JSON.stringify(dragEntries.map(e => e.path)));
    e.dataTransfer.setData('text/plain', dragEntries.map(e => e.name).join(', '));
  }, [selectedPaths, entries]);

  const handleDragOver = useCallback((targetEntry: FileTreeEntry | null, e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();

    if (!targetEntry) {
      setDragOverTargetPath(null);
      return;
    }

    // Only directories are valid drop targets (files will be treated as their parent dir)
    if (targetEntry.is_dir) {
      setDragOverTargetPath(targetEntry.path);
    } else {
      // Highlight the parent directory instead
      const parentPath = targetEntry.path.includes('/') ? targetEntry.path.split('/').slice(0, -1).join('/') : null;
      setDragOverTargetPath(parentPath);
    }

    // Set drop effect based on modifier key
    const isCopy = e.altKey || (e.ctrlKey && !e.metaKey);
    e.dataTransfer.dropEffect = isCopy ? 'copy' : 'move';
  }, []);

  const handleDragLeave = useCallback(() => {
    setDragOverTargetPath(null);
  }, []);

  const handleDrop = useCallback(async (targetEntry: FileTreeEntry | null, e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragOverTargetPath(null);

    if (!projectId) return;

    // Parse dragged paths from dataTransfer
    const pathsJson = e.dataTransfer.getData('application/x-file-tree-paths');
    if (!pathsJson) return;

    let draggedPaths: string[];
    try {
      draggedPaths = JSON.parse(pathsJson);
    } catch {
      return;
    }
    if (draggedPaths.length === 0) return;

    // Resolve target directory
    const targetDir = targetEntry
      ? (targetEntry.is_dir ? targetEntry.path : targetEntry.path.split('/').slice(0, -1).join('/') || '.')
      : '.';

    // Determine copy vs move
    const isCopy = e.altKey || (e.ctrlKey && !e.metaKey);

    // Collect entries to operate on
    const dragEntries = draggedPaths
      .map(p => findEntryInTree(entries, p))
      .filter((e): e is FileTreeEntry => e !== null);

    if (dragEntries.length === 0) return;

    // Guard: cannot move/copy a folder into itself or any of its descendants
    for (const de of dragEntries) {
      if (de.is_dir && isDescendantOrSelf(de.path, targetDir)) {
        return; // silently reject — same as IDE behavior
      }
    }

    // Guard: cannot move to same parent (no-op) unless copying
    if (!isCopy) {
      const allSameParent = dragEntries.every(de => {
        const parent = de.path.includes('/') ? de.path.split('/').slice(0, -1).join('/') : '.';
        return parent === targetDir;
      });
      if (allSameParent) return;
    }

    // Execute the operations — reuse same pattern as handlePaste
    const undoOps: Array<{ type: 'copy'; srcPath: string; dstPath: string; isDir: boolean; modTime: string } | { type: 'move'; oldPath: string; newPath: string }> = [];

    try {
      let updated = entries;
      const siblings = findEntriesAtPath(entries, targetDir);

      if (isCopy) {
        for (const de of dragEntries) {
          const newName = resolveUniqueName(siblings, de.name);
          const dstPath = targetDir === '.' ? newName : `${targetDir}/${newName}`;
          const result = await copyEntry(de.path, dstPath, projectId);
          if (result.entry) {
            updated = insertEntryToTree(updated, targetDir, result.entry);
            siblings.push(result.entry);
            undoOps.push({ type: 'copy', srcPath: de.path, dstPath, isDir: de.is_dir, modTime: result.entry.mod_time ?? '' });
          } else {
            throw new Error('Missing entry in copy response');
          }
        }
      } else {
        // Move mode
        for (const de of dragEntries) {
          const newPath = targetDir === '.' ? de.name : `${targetDir}/${de.name}`;
          const result = await renameEntry(de.path, newPath, projectId);
          if (result.entry) {
            updated = moveEntryInTree(updated, de.path, result.entry);
            undoOps.push({ type: 'move', oldPath: de.path, newPath });
          } else {
            throw new Error('Missing entry in rename response');
          }
        }
      }

      if (isValidTreeStructure(updated)) {
        setEntries(updated);
        // Mark targetDir as loaded so lazy-expand won't overwrite incremental inserts
        loadedDirsRef.current.add(targetDir);
        // Update selectedPaths to reflect new locations
        if (!isCopy) {
          setSelectedPaths(prev => {
            const next = new Set<string>();
            for (const p of prev) {
              const movedOp = undoOps.find((op): op is { type: 'move'; oldPath: string; newPath: string } => op.type === 'move' && op.oldPath === p);
              next.add(movedOp ? movedOp.newPath : p);
            }
            return next;
          });
        }
        // Push undo
        if (undoOps.length > 0) {
          pushUndoStack(undoOps.length === 1 ? undoOps[0] : { type: 'compound', ops: undoOps });
        }
        return;
      }
    } catch {
      // incremental update failed, fall through to loadTree
    }
    await loadTree(); // fallback
    if (undoOps.length > 0) {
      pushUndoStack(undoOps.length === 1 ? undoOps[0] : { type: 'compound', ops: undoOps });
    }
  }, [projectId, entries, loadTree, pushUndoStack]);

  const handleDragEnd = useCallback(() => {
    setDragOverTargetPath(null);
  }, []);

  // ── Context menu ───────────────────────────────────────────────────────────

  const handleContextMenu = useCallback((e: React.MouseEvent, entry: FileTreeEntry | null) => {
    e.stopPropagation();
    // IDE behaviour: if the right-clicked entry is not in the current selection,
    // make it the sole selected item; otherwise preserve the multi-selection.
    if (entry && !selectedPaths.has(entry.path)) {
      setSelectedPaths(new Set([entry.path]));
      setLastClickedPath(entry.path);
    }
    setContextMenu({ x: e.clientX, y: e.clientY, entry });
  }, [selectedPaths]);

  const buildEntryContextMenuItems = (entry: FileTreeEntry): ContextMenuEntry[] => {
    const parentDepth = entry.path.split('/').length;

    const items: ContextMenuEntry[] = [];

    if (entry.is_dir) {
      items.push({
        label: 'New File',
        icon: <FilePlus className="w-3.5 h-3.5" />,
        onClick: async () => {
          await ensureDirLoaded(entry.path);
          setFileTreeExpandedPaths((p) => new Set(p).add(entry.path));
          setNewItem({ parentPath: entry.path, isDir: false, depth: parentDepth });
        },
      });
      items.push({
        label: 'New Folder',
        icon: <FolderPlus className="w-3.5 h-3.5" />,
        onClick: async () => {
          await ensureDirLoaded(entry.path);
          setFileTreeExpandedPaths((p) => new Set(p).add(entry.path));
          setNewItem({ parentPath: entry.path, isDir: true, depth: parentDepth });
        },
      });
      items.push({ separator: true });
    }

    // Determine which entries are affected by this context menu action:
    // if the right-clicked entry is part of the current multi-selection, operate on all;
    // otherwise operate on just the right-clicked entry.
    const targetEntries = selectedPaths.has(entry.path)
      ? [...selectedPaths].map(p => findEntryInTree(entries, p)).filter((e): e is FileTreeEntry => e !== null)
      : [entry];
    const isMultiSelected = targetEntries.length > 1;

    items.push({
      label: isMultiSelected ? `Copy (${targetEntries.length} items)` : 'Copy',
      icon: <Copy className="w-3.5 h-3.5" />,
      shortcut: '⌘C',
      onClick: () => setClipboard({ entries: targetEntries, mode: 'copy' }),
    });
    items.push({
      label: isMultiSelected ? `Cut (${targetEntries.length} items)` : 'Cut',
      icon: <Scissors className="w-3.5 h-3.5" />,
      shortcut: '⌘X',
      onClick: () => setClipboard({ entries: targetEntries, mode: 'cut' }),
    });
    if (clipboard) {
      items.push({
        label: 'Paste Here',
        icon: <Clipboard className="w-3.5 h-3.5" />,
        shortcut: '⌘V',
        onClick: () => handlePaste(entry),
      });
    }

    items.push({ separator: true });

    items.push({
      label: 'Copy Path',
      onClick: () => navigator.clipboard.writeText(entry.abs_path),
    });
    items.push({
      label: 'Copy Relative Path',
      onClick: () => navigator.clipboard.writeText(entry.path),
    });

    items.push({ separator: true });

    items.push({
      label: 'Rename',
      icon: <Pencil className="w-3.5 h-3.5" />,
      shortcut: 'F2',
      onClick: () => setRenamingPath(entry.path),
    });
    items.push({
      label: isMultiSelected
        ? `Delete ${targetEntries.length} items`
        : entry.is_dir ? 'Delete Folder' : 'Delete File',
      icon: <Trash2 className="w-3.5 h-3.5" />,
      danger: true,
      onClick: () => setDeleteTargets(targetEntries),
    });

    items.push({ separator: true });

    items.push({
      label: 'Open in Terminal',
      icon: <Terminal className="w-3.5 h-3.5" />,
      onClick: () => {
        const dir = entry.is_dir ? entry.abs_path : entry.abs_path.split('/').slice(0, -1).join('/');
        window.dispatchEvent(new CustomEvent('filetree:open-in-terminal', { detail: { dir } }));
      },
    });

    return items;
  };

  /** Build context menu for the blank area (no entry targeted). */
  const buildBlankContextMenuItems = (): ContextMenuEntry[] => {
    const items: ContextMenuEntry[] = [];

    items.push({
      label: 'New File',
      icon: <FilePlus className="w-3.5 h-3.5" />,
      onClick: () => setNewItem({ parentPath: '.', isDir: false, depth: 0 }),
    });
    items.push({
      label: 'New Folder',
      icon: <FolderPlus className="w-3.5 h-3.5" />,
      onClick: () => setNewItem({ parentPath: '.', isDir: true, depth: 0 }),
    });

    if (clipboard) {
      items.push({ separator: true });
      items.push({
        label: 'Paste',
        icon: <Clipboard className="w-3.5 h-3.5" />,
        shortcut: '⌘V',
        onClick: () => handlePaste(null),
      });
    }

    items.push({ separator: true });

    items.push({
      label: 'Refresh',
      icon: <RefreshCw className="w-3.5 h-3.5" />,
      onClick: loadTree,
    });
    items.push({
      label: 'Collapse All',
      icon: <ChevronsUpDown className="w-3.5 h-3.5" />,
      onClick: collapseAll,
    });

    return items;
  };

  // ── Keyboard shortcuts (⌘C / ⌘X / ⌘V / F2 / Delete) ─────────────────────

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Only handle when no input/textarea is focused
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;

      const isMod = e.metaKey || e.ctrlKey;
      const isShift = e.shiftKey;

      // ── ⌘Z / ⌘Shift+Z: Undo/Redo (earliest return, only when FileTree panel is active) ──
      if (isMod && e.key === 'z') {
        // Determine if FileTree is the active context:
        // 1. Mouse is hovering over the FileTree panel, OR
        // 2. The active element is inside the FileTree panel (e.g. a focused button)
        // This avoids conflicting with CodePanel's ⌘Z.
        const activeEl = document.activeElement;
        const isHovering = panelRef.current?.matches(':hover');
        const isFocusedInside = activeEl ? panelRef.current?.contains(activeEl) : false;
        const isInFileTree = isHovering || isFocusedInside;
        if (isInFileTree) {
          if (!isShift && undoStack.length > 0) {
            e.preventDefault();
            executeUndo();
            return;
          }
          if (isShift && redoStack.length > 0) {
            e.preventDefault();
            executeRedo();
            return;
          }
        }
      }

      // For keyboard shortcuts, use the first selected path as the "active" one
      const activePath = selectedPaths.size > 0 ? [...selectedPaths][0] : null;

      if (isMod && e.key === 'c' && selectedPaths.size > 0) {
        e.preventDefault();
        const selectedEntries = [...selectedPaths]
          .map(p => findEntryInTree(entries, p))
          .filter((e): e is FileTreeEntry => e !== null);
        if (selectedEntries.length > 0) setClipboard({ entries: selectedEntries, mode: 'copy' });
      } else if (isMod && e.key === 'x' && selectedPaths.size > 0) {
        e.preventDefault();
        const selectedEntries = [...selectedPaths]
          .map(p => findEntryInTree(entries, p))
          .filter((e): e is FileTreeEntry => e !== null);
        if (selectedEntries.length > 0) setClipboard({ entries: selectedEntries, mode: 'cut' });
      } else if (isMod && e.key === 'v' && selectedPaths.size > 0) {
        e.preventDefault();
        const entry = findEntryInTree(entries, activePath!);
        // When the selected entry is a directory that is the same as (or an ancestor of)
        // any clipboard source, paste should target the directory's parent — matching
        // IDE behaviour where ⌘V pastes at the same level, not inside the copied folder.
        if (entry && clipboard && entry.is_dir && clipboard.entries.some(ce => isDescendantOrSelf(ce.path, entry.path))) {
          const parentDir = entry.path.split('/').slice(0, -1).join('/') || '.';
          const parentEntry = parentDir === '.' ? null : findEntryInTree(entries, parentDir);
          handlePaste(parentEntry);
        } else {
          handlePaste(entry);
        }
      } else if (e.key === 'F2' && selectedPaths.size === 1) {
        e.preventDefault();
        setRenamingPath(activePath);
      } else if (e.key === 'Delete' && selectedPaths.size > 0) {
        e.preventDefault();
        // Batch delete: collect all selected entries
        const selectedEntries = [...selectedPaths]
          .map(p => findEntryInTree(entries, p))
          .filter((e): e is FileTreeEntry => e !== null);
        if (selectedEntries.length > 0) setDeleteTargets(selectedEntries);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [selectedPaths, entries, handlePaste, undoStack, redoStack, executeUndo, executeRedo]);

  // ── Rename commit ──────────────────────────────────────────────────────────

  const handleRenameCommit = async (entry: FileTreeEntry, newName: string) => {
    setRenamingPath(null);
    if (!projectId) return;
    const dir = entry.path.split('/').slice(0, -1).join('/');
    const newPath = dir ? `${dir}/${newName}` : newName;
    try {
      const result = await renameEntry(entry.path, newPath, projectId);

      // Try incremental update, fall back to loadTree on failure
      try {
        if (result.entry) {
          const updated = moveEntryInTree(entries, entry.path, result.entry);
          if (isValidTreeStructure(updated)) {
            setEntries(updated);
            // Update selectedPaths to reflect the new path
            setSelectedPaths(prev => {
              const next = new Set(prev);
              if (next.has(entry.path)) {
                next.delete(entry.path);
                next.add(newPath);
              }
              return next;
            });
            if (lastClickedPath === entry.path) {
              setLastClickedPath(newPath);
            }
            // ── Undo stack: push move operation ──
            pushUndoStack({ type: 'move', oldPath: entry.path, newPath });
            return;
          }
        }
      } catch {
        // incremental update failed, fall through to loadTree
      }
      await loadTree(); // fallback
      // ── Undo stack: push move operation (loadTree path) ──
      pushUndoStack({ type: 'move', oldPath: entry.path, newPath });
    } catch (e: unknown) {
      alert((e as Error).message);
    }
  };

  // ── Delete confirm ────────────────────────────────────────────────────────

  const handleDeleteConfirm = async () => {
    if (deleteTargets.length === 0 || !projectId) return;
    try {
      for (const target of deleteTargets) {
        if (target.is_dir) await deleteFolder(target.path, projectId);
        else await deleteFile(target.path, projectId);
      }
      const deletedPaths = new Set(deleteTargets.map(t => t.path));
      setDeleteTargets([]);
      setSelectedPaths((prev) => {
        const next = new Set(prev);
        for (const p of deletedPaths) next.delete(p);
        return next;
      });

      // Try incremental update, fall back to loadTree on failure
      try {
        let updated = entries;
        for (const path of deletedPaths) {
          updated = removeEntryFromTree(updated, path);
        }
        if (isValidTreeStructure(updated)) {
          setEntries(updated);
          return;
        }
      } catch {
        // incremental update failed, fall through to loadTree
      }
      await loadTree(); // fallback
    } catch (e: unknown) {
      alert((e as Error).message);
    }
  };

  // ── New item commit ────────────────────────────────────────────────────────

  const handleNewItemCommit = async (name: string) => {
    if (!newItem || !projectId) return;
    const fullPath = newItem.parentPath === '.' ? name : `${newItem.parentPath}/${name}`;
    try {
      const result = newItem.isDir
        ? await createFolder(fullPath, projectId)
        : await createFile(fullPath, projectId);
      setNewItem(null);

      // Try incremental update, fall back to loadTree on failure
      try {
        if (result.entry) {
          const updated = insertEntryToTree(entries, newItem.parentPath, result.entry);
          if (isValidTreeStructure(updated)) {
            setEntries(updated);
            // Mark the parent directory as loaded so that expanding it
            // later won't overwrite the incrementally-inserted child.
            loadedDirsRef.current.add(newItem.parentPath);
            // Auto-expand the parent directory so the new item is visible
            setFileTreeExpandedPaths(prev => new Set(prev).add(newItem.parentPath));
            // ── Undo stack: push create operation ──
            pushUndoStack({
              type: 'create',
              path: fullPath,
              isDir: newItem.isDir,
              modTime: result.entry.mod_time ?? '',
            });
            return; // incremental update success, no need for loadTree
          }
        }
      } catch {
        // incremental update failed, fall through to loadTree
      }
      await loadTree(); // fallback
      // ── Undo stack: push create operation (loadTree path) ──
      if (result.entry) {
        pushUndoStack({
          type: 'create',
          path: fullPath,
          isDir: newItem.isDir,
          modTime: result.entry.mod_time ?? '',
        });
      }
    } catch (e: unknown) {
      alert((e as Error).message);
    }
  };

  const handleNewItemCancel = () => setNewItem(null);

  // ── Toolbar actions ────────────────────────────────────────────────────────

  const resolveToolbarTarget = (): { parentPath: string; depth: number } => {
    // Use the first selected path as the active selection for toolbar operations
    const activePath = selectedPaths.size > 0 ? [...selectedPaths][0] : null;
    if (!activePath) return { parentPath: '.', depth: 0 };

    const found = findEntryInTree(entries, activePath);
    let parentPath: string;
    if (found?.is_dir) {
      parentPath = found.path;
      ensureDirLoaded(parentPath);
      setFileTreeExpandedPaths((p) => new Set(p).add(parentPath));
    } else {
      const parts = activePath.split('/');
      parentPath = parts.length > 1 ? parts.slice(0, -1).join('/') : '.';
      if (parentPath && parentPath !== '.') {
        ensureDirLoaded(parentPath);
        setFileTreeExpandedPaths((p) => new Set(p).add(parentPath));
      }
    }
    const depth = parentPath === '.' ? 0 : parentPath.split('/').length;
    return { parentPath, depth };
  };

  const handleToolbarNewFile = () => {
    const { parentPath, depth } = resolveToolbarTarget();
    setNewItem({ parentPath, isDir: false, depth });
  };
  const handleToolbarNewFolder = () => {
    const { parentPath, depth } = resolveToolbarTarget();
    setNewItem({ parentPath, isDir: true, depth });
  };

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div
      ref={panelRef}
      data-panel="filetree"
      className="h-full bg-surface flex flex-col overflow-hidden"
      onContextMenu={(e) => {
        e.preventDefault();
        // Blank area right-click — show blank-area context menu
        handleContextMenu(e, null);
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border-subtle flex-shrink-0">
        <span className="text-xs font-medium text-text-primary uppercase tracking-wide truncate">
          {currentProject?.name ?? 'FILES'}
        </span>
        <div className="flex items-center gap-0.5">
          <button
            title={t('fileTree.newFile')}
            className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover transition-colors"
            onClick={handleToolbarNewFile}
          >
            <FilePlus className="w-3.5 h-3.5" />
          </button>
          <button
            title={t('fileTree.newFolder')}
            className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover transition-colors"
            onClick={handleToolbarNewFolder}
          >
            <FolderPlus className="w-3.5 h-3.5" />
          </button>
          <button
            title={t('common:action.refresh')}
            className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover transition-colors"
            onClick={loadTree}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            title={t('fileTree.collapseAll')}
            className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-hover transition-colors"
            onClick={collapseAll}
          >
            <ChevronsUpDown className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      {/* Tree */}
      <div
        className="flex-1 overflow-y-auto py-1 scrollbar-thin"
        style={{ contain: 'content', '--wails-draggable': 'no-drag' } as React.CSSProperties}
        onClick={(e) => {
          // Only clear selection when clicking the blank area of the tree container,
          // not when clicking on a tree node (which has its own onClick handler).
          if (e.target === e.currentTarget) {
            setSelectedPaths(new Set());
          }
        }}
        onDragOver={(e) => { e.preventDefault(); setDragOverTargetPath(null); }}
        onDrop={(e) => handleDrop(null, e)}
      >
        {loading && entries.length === 0 ? (
          <div className="flex items-center justify-center h-20 text-text-muted text-xs">Loading...</div>
        ) : entries.length === 0 ? (
          <div className="flex items-center justify-center h-20 text-text-muted text-xs">No files</div>
        ) : (
          <>
            {/* Root-level new item input */}
            {newItem && newItem.parentPath === '.' && (
              <NewItemInput
                parentPath="."
                isDir={newItem.isDir}
                depth={0}
                onCommit={handleNewItemCommit}
                onCancel={handleNewItemCancel}
              />
            )}
            {entries.map((entry) => (
              <TreeNodeItem
                key={entry.path}
                entry={entry}
                depth={0}
                expandedPaths={fileTreeExpandedPaths}
                selectedPaths={selectedPaths}
                renamingPath={renamingPath}
                newItem={newItem}
                dragOverTargetPath={dragOverTargetPath}
                onToggle={toggleExpand}
                onSelect={handleSelect}
                onContextMenu={handleContextMenu}
                onRenameCommit={handleRenameCommit}
                onRenameCancel={() => setRenamingPath(null)}
                onNewItemCommit={handleNewItemCommit}
                onNewItemCancel={handleNewItemCancel}
                onDragStart={handleDragStart}
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onDrop={handleDrop}
                onDragEnd={handleDragEnd}
              />
            ))}
          </>
        )}
      </div>

      {/* Context menu */}
      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          items={contextMenu.entry
            ? buildEntryContextMenuItems(contextMenu.entry)
            : buildBlankContextMenuItems()}
          onClose={() => setContextMenu(null)}
        />
      )}

      {/* Delete confirm */}
      <ConfirmDialog
        isOpen={deleteTargets.length > 0}
        title={deleteTargets.length > 1
          ? `Delete ${deleteTargets.length} Items`
          : deleteTargets[0]?.is_dir ? 'Delete Folder' : 'Delete File'}
        message={deleteTargets.length > 1
          ? `Delete ${deleteTargets.length} selected items? This action cannot be undone.`
          : `Delete ${deleteTargets[0]?.name}${deleteTargets[0]?.is_dir ? ' and all its contents' : ''}? This action cannot be undone.`}
        confirmLabel="Delete"
        variant="danger"
        onConfirm={handleDeleteConfirm}
        onCancel={() => setDeleteTargets([])}
      />
    </div>
  );
});