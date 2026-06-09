/**
 * MenuBar — IDE-style top menu bar for web mode
 *
 * Layout: [Logo] [File ▾] [Edit ▾] [View ▾] [Help ▾]
 *
 * - Web mode: renders full menu bar with logo + menus
 * - Desktop mode: hidden (Electron native menu handles this)
 *
 * The File menu includes the project list for switching.
 * Project actions (build/watch/embed/delete) are in the Footer.
 */

import React, { useState, useRef, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppStateSelector } from '../hooks/useAppStateSelector';
import { useAppState } from '../hooks/useAppState';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';
import { useProjectActions } from '../hooks/useProjectActions';
import { getRuntimeMode } from '../lib/config';
import {
    Eye, RotateCcw, X, History,
} from 'lucide-react';
import {
    createProject, startProjectWatch,
    type Project,
} from '../services/api';
import { UnifiedImportDialog } from './UnifiedImportDialog';
import { ConfirmDialog } from './ConfirmDialog';
import { useRecentPaths, type RecentImportPath } from '../hooks/useRecentPaths';

// ─── Types ───────────────────────────────────────────────────

interface MenuItemDef {
    label: string;
    accelerator?: string;
    onClick?: () => void;
    separator?: boolean;
    disabled?: boolean;
}

interface MenuDef {
    id: string;
    label: string;
    items: MenuItemDef[];
}

// ─── Language colors (shared with ProjectSelector) ───────────

const LANGUAGE_COLORS: Record<string, string> = {
    'Go': 'bg-cyan-500/20 text-cyan-400 border-cyan-500/30',
    'JavaScript': 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
    'TypeScript': 'bg-blue-500/20 text-blue-400 border-blue-500/30',
    'TSX': 'bg-blue-500/20 text-blue-400 border-blue-500/30',
    'Python': 'bg-green-500/20 text-green-400 border-green-500/30',
    'Rust': 'bg-orange-500/20 text-orange-400 border-orange-500/30',
    'Java': 'bg-red-500/20 text-red-400 border-red-500/30',
    'C#': 'bg-purple-500/20 text-purple-400 border-purple-500/30',
    'C': 'bg-gray-500/20 text-gray-400 border-gray-500/30',
    'C++': 'bg-pink-500/20 text-pink-400 border-pink-500/30',
};
const DEFAULT_COLOR = 'bg-slate-500/20 text-slate-400 border-slate-500/30';

function LanguageBadge({ language }: { language: string }) {
    const colorClass = LANGUAGE_COLORS[language] || DEFAULT_COLOR;
    const displayName = language === 'JavaScript' ? 'JS'
        : language === 'TypeScript' ? 'TS'
            : language === 'TSX' ? 'TSX'
                : language === 'Python' ? 'PY'
                    : language;
    return (
        <span className={`px-1.5 py-0.5 text-[10px] font-medium rounded border flex-shrink-0 ${colorClass}`}>
            {displayName}
        </span>
    );
}

// ─── Component ───────────────────────────────────────────────

export const MenuBar = React.memo(function MenuBar() {
    const { t } = useTranslation('activitybar');
    const runtimeMode = getRuntimeMode();

    // Desktop mode: Electron native menu handles this — render nothing
    if (runtimeMode === 'desktop') return null;

    const {
        projects, currentProject, openPanel,
    } = useAppStateSelector(s => ({
        projects: s.projects,
        currentProject: s.currentProject,
        openPanel: s.openPanel,
    }));
    const { setCurrentProject, setGraph, markProjectBuilding, loadProjects } = useAppState();
    const { startBuild, watchStatus, deleteProjectAction } = useProjectActions();

    // ─── Menu state ──────────────────────────────────────────
    const [openMenuId, setOpenMenuId] = useState<string | null>(null);
    const menuBarRef = useRef<HTMLDivElement>(null);

    // When any dropdown is open, disable iframe pointer-events
    useIframePointerEvents(openMenuId !== null);

    // Click outside to close
    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (menuBarRef.current && !menuBarRef.current.contains(e.target as Node)) {
                setOpenMenuId(null);
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    // ─── Project action state (import & delete only) ─────────
    const [isImportOpen, setIsImportOpen] = useState(false);
    const [deleteProjectInfo, setDeleteProjectInfo] = useState<Project | null>(null);
    const [loading, setLoading] = useState(false);
    const { recentPaths, addRecentPath, removeRecentPath } = useRecentPaths();

    // ─── Import handlers ─────────────────────────────────────
    const handleImportProject = async (path: string, watchEnabled?: boolean) => {
        if (!path.trim()) return;
        const name = path.trim().replace(/\\/g, '/').split('/').filter(Boolean).pop() || 'project';
        setLoading(true);
        try {
            const project = await createProject(name, path.trim());
            addRecentPath(path.trim(), name);
            markProjectBuilding(project.id, true);
            setGraph(null);
            setCurrentProject(project);
            await loadProjects();
            setIsImportOpen(false);
            await startBuild(project);
            if (watchEnabled) {
                try {
                    await startProjectWatch(project.id);
                } catch (err) { console.error('Failed to start watch:', err); }
            }
        } catch (err) {
            console.error('Failed to create project:', err);
            alert(err instanceof Error ? err.message : t('projects.createFailed'));
        } finally { setLoading(false); }
    };

    const handleReimport = async (recentPath: RecentImportPath) => {
        await handleImportProject(recentPath.path, false);
    };

    const availableRecentPaths = recentPaths.filter(rp => !projects.some(p => p.root_path === rp.path));

    // ─── Build menus ─────────────────────────────────────────
    const menus: MenuDef[] = [
        {
            id: 'file',
            label: t('menu.file'),
            items: [
                { label: t('projects.importProject'), accelerator: '⌘N', onClick: () => setIsImportOpen(true) },
                { separator: true, label: '' },
            ],
        },
        {
            id: 'edit',
            label: t('menu.edit'),
            items: [
                { label: t('menu.undo'), accelerator: '⌘Z', onClick: () => document.execCommand('undo') },
                { label: t('menu.redo'), accelerator: '⇧⌘Z', onClick: () => document.execCommand('redo') },
                { separator: true, label: '' },
                { label: t('menu.cut'), accelerator: '⌘X', onClick: () => document.execCommand('cut') },
                { label: t('menu.copy'), accelerator: '⌘C', onClick: () => document.execCommand('copy') },
                { label: t('menu.paste'), accelerator: '⌘V', onClick: () => document.execCommand('paste') },
                { label: t('menu.selectAll'), accelerator: '⌘A', onClick: () => document.execCommand('selectAll') },
            ],
        },
        {
            id: 'view',
            label: t('menu.view'),
            items: [
                { label: t('menu.files'), onClick: () => openPanel('fileTree') },
                { label: t('menu.aiAssistant'), onClick: () => openPanel('rightPanel') },
                { separator: true, label: '' },
                { label: t('menu.terminal'), onClick: () => openPanel('terminal') },
                { separator: true, label: '' },
                { label: t('menu.extensions'), onClick: () => openPanel('extensions') },
                { label: t('menu.settings'), accelerator: '⌘,', onClick: () => openPanel('settings') },
            ],
        },
        {
            id: 'help',
            label: t('menu.help'),
            items: [
                { label: t('menu.officialWebsite'), onClick: () => window.open('https://www.axons.chat', '_blank', 'noopener') },
                { label: t('menu.reportIssue'), onClick: () => window.open('https://github.com/mengshi02/axons/issues', '_blank', 'noopener') },
                { label: t('menu.releaseNotes'), onClick: () => window.open('https://github.com/mengshi02/axons/releases', '_blank', 'noopener') },
            ],
        },
    ];

    // ─── Menu open/close ─────────────────────────────────────
    const handleMenuClick = (menuId: string) => {
        setOpenMenuId(prev => prev === menuId ? null : menuId);
    };

    const handleMenuHover = (menuId: string) => {
        if (openMenuId !== null) {
            setOpenMenuId(menuId);
        }
    };

    const closeMenu = useCallback(() => setOpenMenuId(null), []);

    // ─── Render project list (inside File dropdown) ──────────
    const renderProjectList = () => (
        <div className="py-1">
            {/* Section header */}
            <div className="px-3 py-1 text-[11px] text-menu-fg/50 uppercase tracking-wider font-medium">
                {t('projectsTitle')}
            </div>

            {projects.length === 0 ? (
                <div className="px-3 py-2 text-sm text-menu-fg/50">
                    {t('menu.noProjects')}
                </div>
            ) : (
                projects.map((project) => {
                    const isCurrent = currentProject?.id === project.id;

                    return (
                        <button
                            key={project.id}
                            onClick={() => { setCurrentProject(project); closeMenu(); }}
                            className={`w-full px-3 py-1.5 flex items-center gap-2 text-left transition-colors ${isCurrent ? 'bg-accent/10 text-accent' : 'text-menu-fg hover:bg-menu-hover-bg'
                                }`}
                        >
                            {/* Current indicator */}
                            <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${isCurrent ? 'bg-accent' : 'bg-menu-fg/30'}`} />
                            {/* Project name */}
                            <span className="text-[13px] truncate max-w-[140px]">{project.name}</span>
                            {/* Language badges */}
                            {project.language_stack?.slice(0, 2).map((lang: string) => (
                                <LanguageBadge key={lang} language={lang} />
                            ))}
                            {/* Watch indicator */}
                            {watchStatus[project.id]?.is_running && (
                                <Eye className="w-3 h-3 text-green-500 flex-shrink-0" />
                            )}
                        </button>
                    );
                })
            )}

            {/* Recent imports */}
            {availableRecentPaths.length > 0 && (
                <>
                    <div className="my-1 h-px bg-menu-separator" />
                    <div className="px-3 py-1 text-[11px] text-menu-fg/50 uppercase tracking-wider font-medium flex items-center gap-1.5">
                        <History className="w-3 h-3" />
                        {t('projects.recentImports')}
                    </div>
                    {availableRecentPaths.slice(0, 5).map((rp) => (
                        <button
                            key={rp.path}
                            onClick={() => handleReimport(rp)}
                            disabled={loading}
                            className="w-full px-3 py-1 flex items-center gap-2 text-left hover:bg-menu-hover-bg transition-colors group/recent disabled:opacity-50"
                        >
                            <RotateCcw className="w-3 h-3 text-menu-fg/40 flex-shrink-0" />
                            <span className="text-[13px] text-menu-fg truncate max-w-[100px]">{rp.projectName}</span>
                            <span className="text-[11px] text-menu-fg/40 truncate flex-1 text-right">{rp.path}</span>
                            <button
                                onClick={(e) => { e.stopPropagation(); removeRecentPath(rp.path); }}
                                className="opacity-0 group-hover/recent:opacity-100 p-0.5 hover:bg-red-500/20 rounded transition-all flex-shrink-0"
                            >
                                <X className="w-2.5 h-2.5 text-menu-fg/40" />
                            </button>
                        </button>
                    ))}
                </>
            )}
        </div>
    );

    // ─── Render dropdown for a menu ──────────────────────────
    const renderDropdown = (menu: MenuDef) => {
        const isFileMenu = menu.id === 'file';

        return (
            <div className="absolute left-0 top-full mt-0 z-50 bg-menu-bg border border-menu-border text-menu-fg rounded-b-md shadow-desktop-menu py-1 min-w-[220px] text-[13px] animate-menu-in">
                {menu.items.filter(i => !i.separator).map((item, idx) => (
                    <button
                        key={idx}
                        onClick={() => { item.onClick?.(); closeMenu(); }}
                        disabled={item.disabled}
                        className="w-full text-left px-3 py-[5px] flex items-center gap-2 transition-colors duration-75 hover:bg-menu-hover-bg hover:text-menu-hover-fg cursor-pointer disabled:opacity-50 disabled:cursor-default"
                    >
                        <span className="flex-1">{item.label}</span>
                        {item.accelerator && (
                            <span className="text-[11px] text-menu-fg/40 font-mono">{item.accelerator}</span>
                        )}
                    </button>
                ))}

                {/* File menu: project list section */}
                {isFileMenu && (
                    <>
                        <div className="my-1 h-px bg-menu-separator" />
                        {renderProjectList()}
                    </>
                )}
            </div>
        );
    };

    // ─── Main render ─────────────────────────────────────────
    return (
        <>
            <div
                ref={menuBarRef}
                className="flex items-center h-full flex-1"
                style={{ '--desktop-draggable': 'no-drag' } as React.CSSProperties}
            >
                {/* Logo */}
                <div className="flex items-center mr-2">
                    <img src="/favicon.svg" alt="Axons" className="w-5 h-5" />
                </div>

                {/* Menu buttons */}
                {menus.map((menu) => (
                    <div key={menu.id} className="relative">
                        <button
                            onClick={() => handleMenuClick(menu.id)}
                            onMouseEnter={() => handleMenuHover(menu.id)}
                            className={`px-2 py-0.5 text-[13px] rounded-sm transition-colors ${openMenuId === menu.id
                                ? 'bg-menu-hover-bg text-menu-hover-fg'
                                : 'text-text-secondary hover:text-text-primary hover:bg-hover'
                                }`}
                        >
                            {menu.label}
                        </button>
                        {openMenuId === menu.id && renderDropdown(menu)}
                    </div>
                ))}
            </div>

            {/* Import dialog */}
            <UnifiedImportDialog
                isOpen={isImportOpen}
                onClose={() => setIsImportOpen(false)}
                onImport={handleImportProject}
                isImporting={loading}
            />

            {/* Delete confirm dialog */}
            <ConfirmDialog
                isOpen={deleteProjectInfo !== null}
                title={t('projects.deleteProject')}
                message={t('projects.confirmDelete', { name: deleteProjectInfo?.name })}
                confirmLabel={t('common:action.delete')}
                variant="danger"
                onConfirm={async () => {
                    if (!deleteProjectInfo) return;
                    const project = deleteProjectInfo;
                    setDeleteProjectInfo(null);
                    await deleteProjectAction(project);
                }}
                onCancel={() => setDeleteProjectInfo(null)}
            />
        </>
    );
});