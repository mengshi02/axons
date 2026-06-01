/**
 * fileTreeUpdate.ts — Incremental update utilities for FileTree entries.
 *
 * These functions modify the entries tree locally (no API calls) so that
 * after a file operation the UI can update without a full `loadTree()` round-trip.
 *
 * CRITICAL: The sort function `dirFirstAlphaAsc` must match the backend
 * `listDir` sort logic exactly (dirs first → alphabetical ascending),
 * otherwise inserted nodes will appear at different positions than after
 * a `loadTree`, causing duplicate-node visual bugs in React reconciliation.
 */

import type { FileTreeEntry } from '../services/api';

// ─── Sort comparison ────────────────────────────────────────────────────────

/** Sort comparator matching backend listDir: directories first, then alphabetical.
 *  Backend uses: di != dj → return di; then strings.ToLower(name_i) < strings.ToLower(name_j)
 *  Frontend uses: localeCompare with toLowerCase, which is equivalent for ASCII names. */
export function dirFirstAlphaAsc(a: FileTreeEntry, b: FileTreeEntry): number {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1; // directories first
    return a.name.toLowerCase().localeCompare(b.name.toLowerCase()); // alphabetical ascending
}

// ─── Remove ─────────────────────────────────────────────────────────────────

/** Remove an entry (and its subtree) by path from the entries tree. Immutable — returns new arrays. */
export function removeEntryFromTree(entries: FileTreeEntry[], path: string): FileTreeEntry[] {
    const result: FileTreeEntry[] = [];
    for (const e of entries) {
        if (e.path === path) continue; // skip the removed entry
        if (e.is_dir && e.children) {
            const updatedChildren = removeEntryFromTree(e.children, path);
            if (updatedChildren !== e.children) {
                // Children changed — create new object to preserve immutability
                result.push({ ...e, children: updatedChildren });
            } else {
                result.push(e);
            }
        } else {
            result.push(e);
        }
    }
    return result;
}

// ─── Insert ──────────────────────────────────────────────────────────────────

/** Insert a new entry into a specific parent directory in the tree.
 *  If parentDir is '.' or '', insert at root level.
 *  The new entry is sorted into the correct position using dirFirstAlphaAsc. */
export function insertEntryToTree(
    entries: FileTreeEntry[],
    parentDir: string,
    newEntry: FileTreeEntry,
): FileTreeEntry[] {
    if (parentDir === '.' || parentDir === '') {
        // Insert at root level
        const result = [...entries, newEntry];
        result.sort(dirFirstAlphaAsc);
        return result;
    }
    return entries.map(e => {
        if (e.path === parentDir && e.is_dir) {
            // Found the target directory — insert child and sort
            const newChildren = [...(e.children ?? []), newEntry];
            newChildren.sort(dirFirstAlphaAsc);
            return { ...e, children: newChildren };
        }
        if (e.is_dir && e.children) {
            // Recurse into subdirectories
            const updatedChildren = insertEntryToTree(e.children, parentDir, newEntry);
            if (updatedChildren !== e.children) {
                return { ...e, children: updatedChildren };
            }
        }
        return e;
    });
}

// ─── Move ────────────────────────────────────────────────────────────────────

/** Move an entry: remove it from oldPath, then insert the newEntry (with new path/name)
 *  at its new parent directory. This is used for rename/move operations. */
export function moveEntryInTree(
    entries: FileTreeEntry[],
    oldPath: string,
    newEntry: FileTreeEntry,
): FileTreeEntry[] {
    const parentDir = newEntry.path.includes('/')
        ? newEntry.path.split('/').slice(0, -1).join('/')
        : '.';
    let result = removeEntryFromTree(entries, oldPath);
    result = insertEntryToTree(result, parentDir, newEntry);
    return result;
}

// ─── Validation ──────────────────────────────────────────────────────────────

/** Light sanity check: verify the tree structure looks valid after an incremental update.
 *  This is used as a safety gate — if the check fails, we fall back to loadTree(). */
export function isValidTreeStructure(entries: FileTreeEntry[]): boolean {
    // Check: no duplicate paths at the same level
    const paths = new Set<string>();
    for (const e of entries) {
        if (paths.has(e.path)) return false; // duplicate at same level
        paths.add(e.path);
        if (e.is_dir && e.children) {
            if (!isValidTreeStructure(e.children)) return false;
        }
    }
    return true;
}