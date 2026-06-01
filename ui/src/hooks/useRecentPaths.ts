import { useState, useCallback, useEffect } from 'react';

const STORAGE_KEY = 'axons_recent_import_paths';
const MAX_RECENT = 10;

export interface RecentImportPath {
  path: string;
  projectName: string;
  lastImportedAt: string;
}

export function useRecentPaths() {
  const [recentPaths, setRecentPaths] = useState<RecentImportPath[]>([]);

  // Load from localStorage on mount
  useEffect(() => {
    try {
      const data = localStorage.getItem(STORAGE_KEY);
      if (data) {
        setRecentPaths(JSON.parse(data));
      }
    } catch {
      // Ignore parse errors
    }
  }, []);

  const addRecentPath = useCallback((path: string, projectName: string) => {
    setRecentPaths(prev => {
      // Remove duplicate and add to top
      const filtered = prev.filter(p => p.path !== path);
      const updated = [
        { path, projectName, lastImportedAt: new Date().toISOString() },
        ...filtered
      ].slice(0, MAX_RECENT);
      
      // Persist to localStorage
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(updated));
      } catch {
        // Storage full or unavailable
      }
      
      return updated;
    });
  }, []);

  const removeRecentPath = useCallback((path: string) => {
    setRecentPaths(prev => {
      const updated = prev.filter(p => p.path !== path);
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(updated));
      } catch {
        // Storage full or unavailable
      }
      return updated;
    });
  }, []);

  const clearRecentPaths = useCallback(() => {
    setRecentPaths([]);
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // Ignore errors
    }
  }, []);

  return {
    recentPaths,
    addRecentPath,
    removeRecentPath,
    clearRecentPaths
  };
}