import { Search, FileText, Loader2 } from 'lucide-react';
import { useAppStateSelector } from '../hooks/useAppStateSelector';
import React, { useState, useMemo, useRef, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';
import { getRuntimeMode } from '../lib/config';
import { MenuBar } from './MenuBar';
import type { GraphNode } from '../types/graph';
import type { SearchResult } from '../types/graph';
import { searchCode } from '../services/api';
import { NotificationBell } from './NotificationBell';
import { NotificationPanel } from './NotificationPanel';
import type { Notification } from '../services/api';

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

interface TopSearchBarProps {
    onFocusNode?: (nodeId: string) => void;
    notifications: Notification[];
    unreadCount: number;
    isPanelOpen: boolean;
    onTogglePanel: () => void;
    onMarkRead: (id: string) => void;
    onMarkAllRead: () => void;
    onDeleteNotification: (id: string) => void;
    onClosePanel: () => void;
    onOpenPanel?: (panelId: string) => void;
}

type SearchMode = 'node' | 'fulltext';

export const TopSearchBar = React.memo(function TopSearchBar({ onFocusNode, notifications, unreadCount, isPanelOpen, onTogglePanel, onMarkRead, onMarkAllRead, onDeleteNotification, onClosePanel, onOpenPanel }: TopSearchBarProps) {
    const { t } = useTranslation('common');
    const {
        graph,
        openCodePanel,
        currentProject,
    } = useAppStateSelector(s => ({
        graph: s.graph,
        openCodePanel: s.openCodePanel,
        currentProject: s.currentProject,
    }));

    const [searchMode, setSearchMode] = useState<SearchMode>('node');
    const [searchQuery, setSearchQuery] = useState('');
    const [isSearchOpen, setIsSearchOpen] = useState(false);

    // When search dropdown is open, disable iframe pointer-events so clicks
    // penetrate to the host document and trigger click-outside closing logic
    useIframePointerEvents(isSearchOpen);
    const [selectedIndex, setSelectedIndex] = useState(0);
    // Fulltext search state
    const [fulltextResults, setFulltextResults] = useState<SearchResult[]>([]);
    const [isFulltextSearching, setIsFulltextSearching] = useState(false);
    const searchRef = useRef<HTMLDivElement>(null);
    const inputRef = useRef<HTMLInputElement>(null);

    // Handle panel:// protocol from notification actions
    const handleActionClick = useCallback((url: string) => {
        if (url.startsWith('panel://') && onOpenPanel) {
            const panelId = url.slice('panel://'.length);
            onOpenPanel(panelId);
            onClosePanel();
        }
    }, [onOpenPanel, onClosePanel]);

    // Node search results (local filter)
    const nodeResults = useMemo(() => {
        if (searchMode !== 'node' || !graph || !searchQuery.trim()) return [];
        const query = searchQuery.toLowerCase();
        return graph.nodes
            .filter(node => node.properties.name.toLowerCase().includes(query))
            .slice(0, 10);
    }, [graph, searchQuery, searchMode]);

    // Fulltext search trigger
    const triggerFulltextSearch = useCallback(async (query: string) => {
        if (!query.trim()) return;
        setIsFulltextSearching(true);
        try {
            const data = await searchCode(query.trim(), 20, undefined, { projectId: currentProject?.id });
            setFulltextResults(data.results);
        } catch {
            setFulltextResults([]);
        } finally {
            setIsFulltextSearching(false);
        }
    }, [currentProject?.id]);

    // Reset results on mode switch or query clear
    useEffect(() => {
        if (!searchQuery.trim()) {
            setFulltextResults([]);
            setSelectedIndex(0);
        }
    }, [searchQuery, searchMode]);

    // Click outside handlers
    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (searchRef.current && !searchRef.current.contains(e.target as Node)) {
                setIsSearchOpen(false);
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    // Keyboard shortcuts
    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
                e.preventDefault();
                inputRef.current?.focus();
                setIsSearchOpen(true);
            }
            if (e.key === 'Escape') {
                setIsSearchOpen(false);
                inputRef.current?.blur();
            }
        };
        document.addEventListener('keydown', handleKeyDown);
        return () => document.removeEventListener('keydown', handleKeyDown);
    }, []);

    const totalResults = searchMode === 'node' ? nodeResults.length : fulltextResults.length;

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (searchMode === 'node') {
            if (!isSearchOpen || nodeResults.length === 0) return;
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSelectedIndex(i => Math.min(i + 1, nodeResults.length - 1));
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSelectedIndex(i => Math.max(i - 1, 0));
            } else if (e.key === 'Enter') {
                e.preventDefault();
                const selected = nodeResults[selectedIndex];
                if (selected) handleSelectNode(selected);
            }
        } else {
            // fulltext mode
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                setSelectedIndex(i => Math.min(i + 1, fulltextResults.length - 1));
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setSelectedIndex(i => Math.max(i - 1, 0));
            } else if (e.key === 'Enter') {
                e.preventDefault();
                if (fulltextResults.length > 0 && isSearchOpen) {
                    const selected = fulltextResults[selectedIndex];
                    if (selected) handleSelectFulltextResult(selected);
                } else {
                    triggerFulltextSearch(searchQuery);
                    setIsSearchOpen(true);
                    setSelectedIndex(0);
                }
            }
        }
    };

    const handleSelectNode = useCallback((node: GraphNode) => {
        onFocusNode?.(node.id);
        openCodePanel();
        setSearchQuery('');
        setIsSearchOpen(false);
        setSelectedIndex(0);
    }, [onFocusNode, openCodePanel]);

    const handleSelectFulltextResult = useCallback((result: SearchResult) => {
        window.dispatchEvent(new CustomEvent('navigate-to-file', {
            detail: { path: result.filePath, line: result.startLine }
        }));
        openCodePanel();
        setSearchQuery('');
        setIsSearchOpen(false);
        setFulltextResults([]);
        setSelectedIndex(0);
    }, [openCodePanel]);

    const handleSwitchMode = (mode: SearchMode) => {
        setSearchMode(mode);
        setSearchQuery('');
        setFulltextResults([]);
        setSelectedIndex(0);
        setIsSearchOpen(false);
        inputRef.current?.focus();
    };

    const showDropdown = isSearchOpen && searchQuery.trim();

    return (
        <div
            className="flex items-center px-3 py-0.5 bg-deep border-b border-border-subtle"
            style={{ '--desktop-draggable': 'drag' } as React.CSSProperties}
        >
            {/* Left side: MenuBar in web mode, spacer in desktop mode */}
            {getRuntimeMode() === 'web' ? (
                <MenuBar />
            ) : (
                <div className="flex-1" />
            )}

            {/* Search - centered */}
            <div className="flex-1 max-w-lg relative" ref={searchRef} style={{ '--desktop-draggable': 'no-drag' } as React.CSSProperties}>
                <div className="flex items-center gap-1.5 px-2 py-0.5 bg-surface border border-border-subtle rounded-md transition-all focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/20">
                    {isFulltextSearching
                        ? <Loader2 className="w-4 h-4 text-text-muted flex-shrink-0 animate-spin" />
                        : <Search className="w-4 h-4 text-text-muted flex-shrink-0" />
                    }
                    <input
                        ref={inputRef}
                        type="text"
                        placeholder={searchMode === 'node' ? t('search.nodesPlaceholder') : t('search.fullTextPlaceholder')}
                        value={searchQuery}
                        onChange={(e) => {
                            setSearchQuery(e.target.value);
                            setIsSearchOpen(true);
                            setSelectedIndex(0);
                        }}
                        onFocus={() => setIsSearchOpen(true)}
                        onKeyDown={handleKeyDown}
                        className="flex-1 bg-transparent border-none outline-none text-xs text-text-primary placeholder:text-text-muted min-w-0"
                    />

                    {/* Mode toggle pill */}
                    <div className="flex items-center flex-shrink-0 bg-elevated rounded-md p-0.5 gap-0.5">
                        <button
                            onClick={() => handleSwitchMode('node')}
                            className={`px-2 py-0.5 rounded text-[11px] font-medium transition-colors ${searchMode === 'node'
                                ? 'bg-accent text-white'
                                : 'text-text-muted hover:text-text-primary'
                                }`}
                        >
                            Node
                        </button>
                        <button
                            onClick={() => handleSwitchMode('fulltext')}
                            className={`px-2 py-0.5 rounded text-[11px] font-medium transition-colors ${searchMode === 'fulltext'
                                ? 'bg-accent text-white'
                                : 'text-text-muted hover:text-text-primary'
                                }`}
                        >
                            Full Text
                        </button>
                    </div>

                    <kbd className="px-1.5 py-0.5 bg-elevated border border-border-subtle rounded text-[10px] text-text-muted font-mono flex-shrink-0">
                        ⌘K
                    </kbd>
                </div>

                {/* Search Results Dropdown */}
                {showDropdown && (
                    <div className="absolute top-full left-0 right-0 mt-1 bg-surface border border-border-subtle rounded-lg shadow-xl overflow-hidden z-50">
                        {/* Node mode results */}
                        {searchMode === 'node' && (
                            nodeResults.length === 0 ? (
                                <div className="px-4 py-3 text-sm text-text-muted">
                                    No nodes found for "{searchQuery}"
                                </div>
                            ) : (
                                <div className="max-h-80 overflow-y-auto">
                                    {nodeResults.map((node, index) => (
                                        <button
                                            key={node.id}
                                            onClick={() => handleSelectNode(node)}
                                            className={`w-full px-4 py-2.5 flex items-center gap-3 text-left transition-colors ${index === selectedIndex ? 'bg-accent/20 text-text-primary' : 'hover:bg-hover text-text-secondary'
                                                }`}
                                        >
                                            <span
                                                className="w-2.5 h-2.5 rounded-full flex-shrink-0"
                                                style={{ backgroundColor: NODE_TYPE_COLORS[node.label] || '#6b7280' }}
                                            />
                                            <span className="flex-1 truncate text-sm font-medium">
                                                {node.properties.name}
                                            </span>
                                            <span className="text-xs text-text-muted px-2 py-0.5 bg-elevated rounded">
                                                {node.label}
                                            </span>
                                        </button>
                                    ))}
                                </div>
                            )
                        )}

                        {/* Fulltext mode results */}
                        {searchMode === 'fulltext' && (
                            isFulltextSearching ? (
                                <div className="px-4 py-4 flex items-center gap-2 text-sm text-text-muted">
                                    <Loader2 className="w-4 h-4 animate-spin" />
                                    Searching...
                                </div>
                            ) : fulltextResults.length === 0 ? (
                                <div className="px-4 py-3 text-sm text-text-muted">
                                    Press Enter to search "{searchQuery}"
                                </div>
                            ) : (
                                <div className="max-h-96 overflow-y-auto">
                                    <div className="px-3 py-1.5 text-[11px] text-text-muted border-b border-border-subtle bg-elevated">
                                        {totalResults} results
                                    </div>
                                    {fulltextResults.map((result, index) => (
                                        <button
                                            key={result.id}
                                            onClick={() => handleSelectFulltextResult(result)}
                                            className={`w-full px-4 py-2.5 flex flex-col gap-1 text-left transition-colors ${index === selectedIndex ? 'bg-accent/20' : 'hover:bg-hover'
                                                }`}
                                        >
                                            <div className="flex items-center gap-2">
                                                <FileText className="w-3.5 h-3.5 text-accent flex-shrink-0" />
                                                <span className="text-sm font-medium text-text-primary truncate flex-1">
                                                    {result.name}
                                                </span>
                                                <span className="text-[11px] px-1.5 py-0.5 bg-accent/10 text-accent rounded flex-shrink-0">
                                                    {result.type}
                                                </span>
                                            </div>
                                            <div className="text-xs text-text-muted pl-5 truncate">
                                                {result.filePath}:{result.startLine}
                                                {result.endLine > result.startLine && `-${result.endLine}`}
                                            </div>
                                            {result.content && (
                                                <div className="text-xs text-text-secondary font-mono bg-elevated px-2 py-1 rounded ml-5 truncate">
                                                    {result.content.slice(0, 120)}
                                                </div>
                                            )}
                                        </button>
                                    ))}
                                </div>
                            )
                        )}
                    </div>
                )}
            </div>

            {/* Right spacer for centering */}
            <div className="flex-1 relative">
                <div style={{ '--desktop-draggable': 'no-drag' } as React.CSSProperties}>
                    <NotificationBell
                        unreadCount={unreadCount}
                        onClick={onTogglePanel}
                    />
                </div>
                {isPanelOpen && (
                    <NotificationPanel
                        notifications={notifications}
                        onMarkRead={onMarkRead}
                        onMarkAllRead={onMarkAllRead}
                        onDelete={onDeleteNotification}
                        onClose={onClosePanel}
                        onActionClick={handleActionClick}
                    />
                )}
            </div>
        </div>
    );
});