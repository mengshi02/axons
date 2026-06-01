import React, { useState, useEffect } from 'react';
import { GitBranch, RotateCcw, Check, Eye, X, ChevronDown, ChevronRight } from 'lucide-react';
import { useAppState } from '../hooks/useAppState';
import { useTranslation } from 'react-i18next';
import { Modal } from './Modal';

interface Change {
  file_path: string;
  change_type: 'create' | 'modify';
  timestamp: string;
}

interface DiffLine {
  type: 'equal' | 'insert' | 'delete';
  text: string;
}

interface DiffResult {
  file_path: string;
  change_type: string;
  original_content: string;
  current_content: string;
  diff: DiffLine[];
  stats: {
    added: number;
    removed: number;
  };
}

interface ChangeListProps {
  sessionId: string;
  projectId: string;
  refreshKey?: number; // Used to trigger refresh when files are modified
}

export const ChangeList: React.FC<ChangeListProps> = ({ sessionId, projectId, refreshKey }) => {
  const { t } = useTranslation('panels');
  const { reloadGraph, clearAllFileCache } = useAppState();
  const [changes, setChanges] = useState<Change[]>([]);
  const [isReverting, setIsReverting] = useState(false);
  const [isConfirming, setIsConfirming] = useState(false);
  const [diffModal, setDiffModal] = useState<{ path: string; diff: DiffResult | null; loading: boolean } | null>(null);
  const [revertingPath, setRevertingPath] = useState<string | null>(null);
  const [isCollapsed, setIsCollapsed] = useState(false);

  useEffect(() => {
    if (!sessionId) return;

    fetch(`/api/changes?session_id=${encodeURIComponent(sessionId)}`)
      .then(r => r.json())
      .then(data => {
        console.log('[ChangeList] Fetched changes:', data);
        setChanges(data.changes || []);
      })
      .catch(err => {
        console.error('[ChangeList] Failed to fetch changes:', err);
        setChanges([]);
      });
  }, [sessionId, refreshKey]);

  const handleShowDiff = async (filePath: string) => {
    setDiffModal({ path: filePath, diff: null, loading: true });
    try {
      const response = await fetch(
        `/api/changes/diff?session_id=${encodeURIComponent(sessionId)}&path=${encodeURIComponent(filePath)}`
      );
      if (response.ok) {
        const data = await response.json();
        console.log('[ChangeList] Diff response:', data);
        console.log('[ChangeList] Diff array:', data.diff);
        console.log('[ChangeList] Diff length:', data.diff?.length);
        setDiffModal({ path: filePath, diff: data, loading: false });
      } else {
        const errorText = await response.text();
        console.error('[ChangeList] Failed to fetch diff:', response.status, errorText);
        setDiffModal(null);
      }
    } catch (err) {
      console.error('[ChangeList] Failed to fetch diff:', err);
      setDiffModal(null);
    }
  };

  const handleRevertFile = async (filePath: string) => {
    if (revertingPath) return;
    setRevertingPath(filePath);
    try {
      const response = await fetch('/api/changes/revert', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_id: sessionId,
          project_id: projectId,
          path: filePath
        }),
      });

      if (response.ok) {
        setChanges(prev => prev.filter(c => c.file_path !== filePath));
        setDiffModal(null);
        // Refresh graph and file tree
        clearAllFileCache();
        reloadGraph();
      }
    } catch (err) {
      console.error('[ChangeList] Failed to revert file:', err);
    } finally {
      setRevertingPath(null);
    }
  };

  const handleRevertAll = async () => {
    if (isReverting) return;
    setIsReverting(true);
    try {
      const response = await fetch('/api/changes/revert-all', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_id: sessionId,
          project_id: projectId
        }),
      });

      if (response.ok) {
        setChanges([]);
        // Refresh graph and file tree
        clearAllFileCache();
        reloadGraph();
      }
    } catch (err) {
      console.error('[ChangeList] Failed to revert:', err);
    } finally {
      setIsReverting(false);
    }
  };

  const handleConfirm = async () => {
    if (isConfirming) return;
    setIsConfirming(true);
    try {
      const response = await fetch(
        `/api/changes?session_id=${encodeURIComponent(sessionId)}&project_id=${encodeURIComponent(projectId)}`,
        { method: 'DELETE' }
      );

      if (response.ok) {
        setChanges([]);
        // Refresh graph and file tree to show new files
        clearAllFileCache();
        reloadGraph();
      }
    } catch (err) {
      console.error('[ChangeList] Failed to confirm:', err);
    } finally {
      setIsConfirming(false);
    }
  };

  if (changes.length === 0) return null;

  return (
    <>
      <div className="border-b border-border-subtle bg-blue-500/5">
        {/* Header */}
        <div
          className="flex items-center justify-between px-4 py-2 cursor-pointer hover:bg-hover transition-colors"
          onClick={() => setIsCollapsed(prev => !prev)}
        >
          <div className="flex items-center gap-2">
            {isCollapsed ? (
              <ChevronRight className="w-4 h-4 text-text-muted" />
            ) : (
              <ChevronDown className="w-4 h-4 text-text-muted" />
            )}
            <GitBranch className="w-4 h-4 text-blue-500" />
            <span className="font-medium text-sm text-text-primary">
              Changes ({changes.length} {changes.length === 1 ? 'file' : 'files'})
            </span>
          </div>
        </div>

        {/* Change list */}
        {!isCollapsed && (
        <div className="px-4 pb-3 space-y-1">
          {changes.map(c => {
            const fileName = c.file_path.split('/').pop() || c.file_path;
            const dirPath = c.file_path.substring(0, c.file_path.lastIndexOf('/'));

            return (
              <div
                key={c.file_path}
                className="flex items-center gap-2 py-1.5 px-2 rounded hover:bg-hover transition-colors group"
              >
                {/* Change type badge */}
                <span
                  className={`text-xs font-bold px-1.5 py-0.5 rounded ${c.change_type === 'create'
                    ? 'bg-green-500/20 text-green-600 dark:text-green-400'
                    : 'bg-yellow-500/20 text-yellow-600 dark:text-yellow-400'
                    }`}
                >
                  {c.change_type === 'create' ? 'A' : 'M'}
                </span>

                {/* File info */}
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium truncate text-text-primary">{fileName}</div>
                  {dirPath && (
                    <div className="text-xs text-text-muted truncate">{dirPath}</div>
                  )}
                </div>

                {/* File actions - show on hover */}
                <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button
                    onClick={() => handleShowDiff(c.file_path)}
                    className="p-1 text-text-muted hover:text-blue-500 hover:bg-blue-500/10 rounded transition-colors"
                    title={t('changeList.previewDiff')}
                  >
                    <Eye className="w-3.5 h-3.5" />
                  </button>
                  <button
                    onClick={() => handleRevertFile(c.file_path)}
                    disabled={revertingPath === c.file_path}
                    className="p-1 text-text-muted hover:text-red-500 hover:bg-red-500/10 rounded transition-colors disabled:opacity-50"
                    title={t('changeList.revertFile')}
                  >
                    <RotateCcw className={`w-3.5 h-3.5 ${revertingPath === c.file_path ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
        )}

        {/* Actions */}
        <div className="flex items-center gap-2 px-4 pb-3 pt-2 border-t border-border-subtle">
          <button
            onClick={handleRevertAll}
            disabled={isReverting}
            className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-red-500 hover:text-red-600 hover:bg-red-500/10 rounded transition-colors disabled:opacity-50"
          >
            <RotateCcw className={`w-3.5 h-3.5 ${isReverting ? 'animate-spin' : ''}`} />
            Revert All
          </button>
          <button
            onClick={handleConfirm}
            disabled={isConfirming}
            className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-green-500 hover:text-green-600 hover:bg-green-500/10 rounded transition-colors disabled:opacity-50"
          >
            <Check className="w-3.5 h-3.5" />
            Confirm All
          </button>
        </div>
      </div>

      {/* Diff Modal */}
      <Modal isOpen={!!diffModal} onClose={() => setDiffModal(null)} size="xl" overlayOpacity="none" backdropBlur={false} className="max-h-[80vh] flex flex-col">
        {diffModal && (
          <>
            {/* Modal Header */}
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <div className="flex items-center gap-2">
                <Eye className="w-4 h-4 text-text-muted" />
                <span className="font-medium text-sm">{diffModal.path}</span>
                {diffModal.diff && (
                  <span className="text-xs text-text-muted ml-2">
                    +{diffModal.diff.stats.added} -{diffModal.diff.stats.removed}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => handleRevertFile(diffModal.path)}
                  disabled={revertingPath === diffModal.path}
                  className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-red-500 hover:text-red-600 hover:bg-red-500/10 rounded transition-colors disabled:opacity-50"
                >
                  <RotateCcw className={`w-3.5 h-3.5 ${revertingPath === diffModal.path ? 'animate-spin' : ''}`} />
                  Revert
                </button>
                <button
                  onClick={() => setDiffModal(null)}
                  className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>

            {/* Modal Content */}
            <div className="flex-1 overflow-auto p-4">
              {diffModal.loading ? (
                <div className="flex items-center justify-center h-32 text-text-muted">
                  Loading diff...
                </div>
              ) : diffModal.diff ? (
                  Array.isArray(diffModal.diff.diff) && diffModal.diff.diff.length > 0 ? (
                    <pre className="text-xs font-mono">
                      {diffModal.diff.diff.map((line, i) => (
                        <div
                          key={i}
                          className={`${line.type === 'insert'
                            ? 'bg-green-500/20 text-green-600 dark:text-green-400'
                            : line.type === 'delete'
                              ? 'bg-red-500/20 text-red-600 dark:text-red-400'
                              : 'text-text-muted'
                            }`}
                        >
                          <span className="inline-block w-6 text-center select-none opacity-50">
                            {line.type === 'insert' ? '+' : line.type === 'delete' ? '-' : ' '}
                          </span>
                          {line.text}
                        </div>
                      ))}
                  </pre>
                ) : (
                    <div className="flex flex-col items-center justify-center h-32 text-text-muted">
                        <div>{t('changeList.noDifferences')}</div>
                      <div className="text-xs mt-2">
                        {diffModal.diff.change_type === 'create'
                          ? 'This is a newly created file'
                          : 'File content is identical to original'}
                      </div>
                    </div>
                  )
                ) : (
                <div className="flex items-center justify-center h-32 text-text-muted">
                  No diff available
                </div>
              )}
            </div>
          </>
        )}
      </Modal>
    </>
  );
};