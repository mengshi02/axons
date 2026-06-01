/**
 * useFileTreeUndoRedo.ts — Undo/Redo hook for FileTree operations.
 *
 * This hook manages the undo/redo stacks and provides functions to:
 * - Track operations (push to undo stack)
 * - Execute undo (⌘Z)
 * - Execute redo (⌘Shift+Z)
 *
 * IMPORTANT SAFETY CONSTRAINTS:
 * - All undo/redo operations fall back to loadTree() for state refresh
 * - isUndoRedoing flag prevents new operations from being pushed during undo/redo
 * - isOperating flag prevents concurrent operations
 * - projectId change clears both stacks
 */

import { useState, useEffect, useCallback } from 'react';
import {
  deleteFile, deleteFolder, renameEntry, createFile, createFolder,
  copyEntry, statEntry, type FileTreeEntry,
} from '../services/api';

// ─── Types ────────────────────────────────────────────────────────────────────

type ValidateResult = 'valid' | 'modified' | 'missing';

type FileOperation =
  | { type: 'create'; path: string; isDir: boolean; modTime: string }
  | { type: 'copy'; srcPath: string; dstPath: string; isDir: boolean; modTime: string }
  | { type: 'move'; oldPath: string; newPath: string }
  | { type: 'delete'; path: string; isDir: boolean; trashId: string };

/** Batch operations are treated as a single undo unit */
interface CompoundOperation {
  type: 'compound';
  ops: FileOperation[];
}

type UndoableOperation = FileOperation | CompoundOperation;

// ─── Constants ────────────────────────────────────────────────────────────────

const MAX_UNDO_STACK_SIZE = 100;

// ─── Hook ────────────────────────────────────────────────────────────────────

export function useFileTreeUndoRedo(
  _entries: FileTreeEntry[],
  _setEntries: (entries: FileTreeEntry[]) => void,
  projectId: string,
  loadTree: () => Promise<void>,
) {
  const [undoStack, setUndoStack] = useState<UndoableOperation[]>([]);
  const [redoStack, setRedoStack] = useState<UndoableOperation[]>([]);
  const [isUndoRedoing, setIsUndoRedoing] = useState(false);
  const [isOperating, setIsOperating] = useState(false);

  // Clear stacks on project change
  useEffect(() => {
    setUndoStack([]);
    setRedoStack([]);
  }, [projectId]);

  // ─── Push to undo stack ──────────────────────────────────────────────────

  const pushUndoStack = useCallback((op: UndoableOperation) => {
    setUndoStack(prev => {
      const next = [...prev, op];
      if (next.length > MAX_UNDO_STACK_SIZE) next.shift();
      return next;
    });
    setRedoStack([]); // new operation clears redo
  }, []);

  // ─── Pre-validate ────────────────────────────────────────────────────────

  const preValidateUndo = useCallback(async (op: FileOperation): Promise<ValidateResult> => {
    switch (op.type) {
      case 'create':
      case 'copy': {
        const checkPath = op.type === 'create' ? op.path : op.dstPath;
        const stat = await statEntry(checkPath, projectId);
        if (!stat.exists) return 'missing';
        if (!op.isDir && stat.mod_time !== op.modTime) return 'modified';
        return 'valid';
      }
      case 'move': {
        const stat = await statEntry(op.newPath, projectId);
        if (!stat.exists) return 'missing';
        return 'valid';
      }
      case 'delete':
        return 'valid'; // trash restore validates server-side
    }
  }, [projectId]);

  // ─── Undo single operation ───────────────────────────────────────────────

  const undoSingle = useCallback(async (op: FileOperation): Promise<boolean> => {
    try {
      switch (op.type) {
        case 'create':
          op.isDir ? await deleteFolder(op.path, projectId) : await deleteFile(op.path, projectId);
          break;
        case 'copy':
          op.isDir ? await deleteFolder(op.dstPath, projectId) : await deleteFile(op.dstPath, projectId);
          break;
        case 'move':
          await renameEntry(op.newPath, op.oldPath, projectId);
          break;
        case 'delete':
          // Phase 2: trash restore not yet implemented
          console.warn('[UndoRedo] delete undo not yet implemented (Phase 2)');
          return false;
      }
      return true;
    } catch (e) {
      console.error('[UndoRedo] undoSingle failed:', (e as Error).message);
      return false;
    }
  }, [projectId]);

  // ─── Execute undo ────────────────────────────────────────────────────────

  const executeUndo = useCallback(async () => {
    if (undoStack.length === 0 || isUndoRedoing || isOperating) return;
    const op = undoStack[undoStack.length - 1];
    setIsUndoRedoing(true);
    try {
      if (op.type === 'compound') {
        // Reverse order for compound operations
        for (const sub of [...op.ops].reverse()) {
          const result = await preValidateUndo(sub);
          if (result === 'missing') continue; // skip missing, continue with rest
          if (result === 'modified') {
            // TODO: show warning toast (file was modified since operation)
            console.warn('[UndoRedo] file was modified since operation, proceeding with undo');
          }
          await undoSingle(sub);
        }
      } else {
        const result = await preValidateUndo(op);
        if (result === 'missing') {
          console.warn('[UndoRedo] undo target is missing, skipping');
        } else {
          if (result === 'modified') {
            console.warn('[UndoRedo] file was modified since operation, proceeding with undo');
          }
          await undoSingle(op);
        }
      }
      // Always use loadTree as fallback for safety
      await loadTree();
      setUndoStack(prev => prev.slice(0, -1));
      setRedoStack(prev => [...prev, op]);
    } catch (e) {
      console.error('[UndoRedo] executeUndo failed:', (e as Error).message);
      await loadTree(); // safety fallback
    } finally {
      setIsUndoRedoing(false);
    }
  }, [undoStack, isUndoRedoing, isOperating, preValidateUndo, undoSingle, loadTree]);

  // ─── Redo single operation ───────────────────────────────────────────────

  const redoSingle = useCallback(async (op: FileOperation): Promise<boolean> => {
    try {
      switch (op.type) {
        case 'create':
          op.isDir ? await createFolder(op.path, projectId) : await createFile(op.path, projectId);
          break;
        case 'copy':
          await copyEntry(op.srcPath, op.dstPath, projectId);
          break;
        case 'move':
          await renameEntry(op.oldPath, op.newPath, projectId);
          break;
        case 'delete':
          // Phase 2: trash not yet implemented
          console.warn('[UndoRedo] delete redo not yet implemented (Phase 2)');
          return false;
      }
      return true;
    } catch (e) {
      console.error('[UndoRedo] redoSingle failed:', (e as Error).message);
      return false;
    }
  }, [projectId]);

  // ─── Execute redo ────────────────────────────────────────────────────────

  const executeRedo = useCallback(async () => {
    if (redoStack.length === 0 || isUndoRedoing || isOperating) return;
    const op = redoStack[redoStack.length - 1];
    setIsUndoRedoing(true);
    try {
      if (op.type === 'compound') {
        for (const sub of op.ops) {
          await redoSingle(sub);
        }
      } else {
        await redoSingle(op);
      }
      // Always use loadTree as fallback for safety
      await loadTree();
      setRedoStack(prev => prev.slice(0, -1));
      setUndoStack(prev => [...prev, op]);
    } catch (e) {
      console.error('[UndoRedo] executeRedo failed:', (e as Error).message);
      await loadTree(); // safety fallback
    } finally {
      setIsUndoRedoing(false);
    }
  }, [redoStack, isUndoRedoing, isOperating, redoSingle, loadTree]);

  return {
    undoStack,
    redoStack,
    isUndoRedoing,
    isOperating,
    setIsOperating,
    pushUndoStack,
    executeUndo,
    executeRedo,
  };
}