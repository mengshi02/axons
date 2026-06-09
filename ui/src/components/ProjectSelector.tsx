import React, { useState } from 'react';
import { FolderPlus, Trash2, Play, Loader2, Eye, EyeOff, Layers, CheckCircle, AlertCircle, History, RotateCcw, X } from 'lucide-react';
import { useAppStateSelector } from '../hooks/useAppStateSelector';
import { useAppState } from '../hooks/useAppState';
import { useProjectActions, type EmbedActionState } from '../hooks/useProjectActions';
import { createProject, startProjectWatch, type Project } from '../services/api';
import { UnifiedImportDialog } from './UnifiedImportDialog';
import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from './ConfirmDialog';
import { useRecentPaths, type RecentImportPath } from '../hooks/useRecentPaths';

// Language color mapping for badges (maps from backend names)
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

// Language badge component with abbreviation support
function LanguageBadge({ language }: { language: string }) {
    const colorClass = LANGUAGE_COLORS[language] || DEFAULT_COLOR;

    // Abbreviate long language names for display
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

// Extract project name from path (last directory name)
function extractProjectName(path: string): string {
    const normalized = path.replace(/\\/g, '/');
    const parts = normalized.split('/').filter(p => p.length > 0);
    return parts[parts.length - 1] || 'project';
}

interface ProjectSelectorProps {
    onProjectSelect?: () => void;
}

export function ProjectSelector({ onProjectSelect }: ProjectSelectorProps) {
    const { t } = useTranslation('activitybar');
    const { projects, currentProject } = useAppStateSelector(s => ({
        projects: s.projects,
        currentProject: s.currentProject,
    }));
    const { loadProjects, setCurrentProject, setGraph, markProjectBuilding } = useAppState();
    const {
        buildingProjectId,
        watchStatus,
        embedStatus,
        embedActionStates,
        startBuild,
        toggleWatch,
        triggerProjectEmbed,
        deleteProjectAction,
    } = useProjectActions();

    const [isImportOpen, setIsImportOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [deleteProjectInfo, setDeleteProjectInfo] = useState<Project | null>(null);
    const { recentPaths, addRecentPath, removeRecentPath } = useRecentPaths();

    // ─── Import handlers ─────────────────────────────────────
    const handleImportProject = async (path: string, watchEnabled?: boolean) => {
        if (!path.trim()) return;
        const name = extractProjectName(path.trim());
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

    // Handle newly created project (project already exists in backend, no need to create again)
    const handleProjectCreated = async (project: { id: string; name: string; root_path: string }, watchEnabled?: boolean) => {
        setLoading(true);
        try {
            addRecentPath(project.root_path, project.name);
            markProjectBuilding(project.id, true);
            setGraph(null);
            setCurrentProject(project as Project);
            await loadProjects();
            setIsImportOpen(false);
            await startBuild(project as Project);
            if (watchEnabled) {
                try {
                    await startProjectWatch(project.id);
                } catch (err) { console.error('Failed to start watch:', err); }
            }
        } catch (err) {
            console.error('Failed to handle created project:', err);
            alert(err instanceof Error ? err.message : t('projects.handleFailed'));
        } finally { setLoading(false); }
    };

    const handleReimport = async (recentPath: RecentImportPath) => {
        await handleImportProject(recentPath.path, false);
    };

    const handleRemoveRecent = (e: React.MouseEvent, path: string) => {
        e.stopPropagation();
        removeRecentPath(path);
    };

    // Filter out paths that are already in project list
    const availableRecentPaths = recentPaths.filter(rp =>
        !projects.some(p => p.root_path === rp.path)
    );

    const handleDeleteClick = (e: React.MouseEvent, project: Project) => {
        e.stopPropagation();
        setDeleteProjectInfo(project);
    };

    const confirmDeleteProject = async () => {
        if (!deleteProjectInfo) return;
        const project = deleteProjectInfo;
        setDeleteProjectInfo(null);
        await deleteProjectAction(project);
    };

    // ─── Render ──────────────────────────────────────────────
    return (
        <>
            <div className="w-80 bg-surface border border-border-subtle rounded-lg shadow-xl overflow-hidden">
                {/* Project list */}
                <div className="max-h-64 overflow-y-auto">
                    {projects.length === 0 ? (
                        <div className="px-4 py-3 text-sm text-text-muted text-center">
                            No projects yet. Create one to get started.
                        </div>
                    ) : (
                        projects.map((project) => {
                            const isCurrent = currentProject?.id === project.id;
                            const embedState: EmbedActionState | undefined = embedActionStates[project.id];
                            return (
                                <button
                                    key={project.id}
                                    onClick={() => {
                                        setCurrentProject(project);
                                        onProjectSelect?.();
                                    }}
                                    className={`w-full px-4 py-2.5 flex items-center gap-2 text-left transition-colors group ${isCurrent ? 'bg-accent/10 border-l-2 border-accent' : 'hover:bg-hover border-l-2 border-transparent'}`}
                                >
                                    <span className={`w-2 h-2 rounded-full flex-shrink-0 ${isCurrent ? 'bg-accent animate-pulse' : 'bg-text-muted'}`} />
                                    <span className={`text-sm font-medium truncate max-w-[120px] ${isCurrent ? 'text-accent' : 'text-text-primary'}`}>
                                        {project.name}
                                    </span>
                                    {/* Language stack badges */}
                                    {project.language_stack && project.language_stack.length > 0 && (
                                        <div className="flex gap-1 flex-shrink-0">
                                            {project.language_stack.slice(0, 2).map(lang => (
                                                <LanguageBadge key={lang} language={lang} />
                                            ))}
                                            {project.language_stack.length > 2 && (
                                                <span className="text-[10px] text-text-muted cursor-help flex-shrink-0"
                                                    title={project.language_stack.slice(2).join(', ')}>
                                                    +{project.language_stack.length - 2}
                                                </span>
                                            )}
                                        </div>
                                    )}
                                    <div className="flex-1" />
                                    {/* Watch status indicator */}
                                    {watchStatus[project.id]?.is_running && (
                                        <span title={t('projects.watchingChanges')}>
                                            <Eye className="w-3.5 h-3.5 text-green-500" />
                                        </span>
                                    )}
                                    <button
                                        onClick={(e) => { e.stopPropagation(); toggleWatch(project); }}
                                        className="opacity-0 group-hover:opacity-100 p-1 hover:bg-blue-500/20 rounded transition-all"
                                        title={watchStatus[project.id]?.is_running ? t('projects.stopWatching') : t('projects.startWatching')}
                                    >
                                        {watchStatus[project.id]?.is_running ? (
                                            <EyeOff className="w-3.5 h-3.5 text-blue-400" />
                                        ) : (
                                            <Eye className="w-3.5 h-3.5 text-blue-400" />
                                        )}
                                    </button>
                                    <button
                                        onClick={(e) => { e.stopPropagation(); triggerProjectEmbed(project); }}
                                        disabled={embedState?.status === 'running'}
                                        className={`p-1 rounded transition-all ${embedState?.status === 'running'
                                            ? 'opacity-100'
                                            : embedState?.status === 'success'
                                                ? 'opacity-100 animate-embed-success bg-green-500/20'
                                                : embedState?.status === 'error'
                                                    ? 'opacity-100 animate-embed-error bg-red-500/20'
                                                    : 'opacity-0 group-hover:opacity-100 hover:bg-accent/20'
                                            }`}
                                        title={
                                            embedState?.status === 'running'
                                                ? embedState.message || t('projects.startingEmbedding')
                                                : embedState?.status === 'success'
                                                    ? t('projects.embedSuccess', { new: embedState.newEmbeddings, updated: embedState.updatedEmbeddings })
                                                    : embedState?.status === 'error'
                                                        ? `Error: ${embedState.error}`
                                                        : embedStatus[project.id]?.needs_reembedding
                                                            ? t('projects.reembed', { desc: embedStatus[project.id]?.embedding_count ?? 0, code: embedStatus[project.id]?.code_embedding_count ?? 0 })
                                                            : t('projects.embedProject', { desc: embedStatus[project.id]?.embedding_count ?? 0, code: embedStatus[project.id]?.code_embedding_count ?? 0 })
                                        }
                                    >
                                        {embedState?.status === 'running' ? (
                                            <Layers className="w-3.5 h-3.5 text-accent animate-embed-pulse" />
                                        ) : embedState?.status === 'success' ? (
                                            <CheckCircle className="w-3.5 h-3.5 text-green-500" />
                                            ) : embedState?.status === 'error' ? (
                                            <AlertCircle className="w-3.5 h-3.5 text-red-500" />
                                        ) : (
                                            <Layers className={`w-3.5 h-3.5 ${embedStatus[project.id]?.needs_reembedding ? 'text-yellow-400' : 'text-accent'}`} />
                                        )}
                                    </button>
                                    {/* Build button */}
                                    <button
                                        onClick={(e) => { e.stopPropagation(); startBuild(project); }}
                                        disabled={buildingProjectId === project.id}
                                        className={`p-1 rounded transition-all ${buildingProjectId === project.id
                                            ? 'opacity-100'
                                            : 'opacity-0 group-hover:opacity-100 hover:bg-accent/20'
                                            }`}
                                        title={t('projects.buildProject', { name: project.name })}
                                    >
                                        {buildingProjectId === project.id ? (
                                            <Loader2 className="w-3.5 h-3.5 text-accent animate-spin" />
                                        ) : (
                                            <Play className="w-3.5 h-3.5 text-accent" />
                                        )}
                                    </button>
                                    <button
                                        onClick={(e) => handleDeleteClick(e, project)}
                                        className="opacity-0 group-hover:opacity-100 p-1 hover:bg-red-500/20 rounded transition-all"
                                        title={t('projects.deleteProject')}
                                    >
                                        <Trash2 className="w-3.5 h-3.5 text-red-400" />
                                    </button>
                                </button>
                            );
                        })
                    )}
                </div>

                {/* Create new project button */}
                <div className="border-t border-border-subtle">
                    <button
                        onClick={() => setIsImportOpen(true)}
                        className="w-full px-4 py-2.5 flex items-center gap-2 text-sm text-text-secondary hover:bg-hover transition-colors"
                    >
                        <FolderPlus className="w-4 h-4" />
                        <span>{t('projects.importProject')}</span>
                    </button>
                </div>

                {/* Recent import paths */}
                {availableRecentPaths.length > 0 && (
                    <div className="border-t border-border-subtle">
                        <div className="px-4 py-2 flex items-center gap-2 text-xs text-text-muted uppercase tracking-wider">
                            <History className="w-3 h-3" />
                            <span>{t('projects.recentImports')}</span>
                        </div>
                        <div className="max-h-32 overflow-y-auto">
                            {availableRecentPaths.map((rp) => (
                                <button
                                    key={rp.path}
                                    onClick={() => handleReimport(rp)}
                                    disabled={loading}
                                    className="w-full px-4 py-2 flex items-center gap-2 text-left hover:bg-hover transition-colors group disabled:opacity-50"
                                >
                                    <RotateCcw className="w-3.5 h-3.5 text-text-muted flex-shrink-0" />
                                    <span className="text-sm text-text-secondary truncate w-[130px] text-left" title={rp.path}>
                                        {rp.projectName}
                                    </span>
                                    <span className="text-xs text-text-muted truncate flex-1 text-right" title={rp.path}>
                                        {rp.path}
                                    </span>
                                    <button
                                        onClick={(e) => handleRemoveRecent(e, rp.path)}
                                        className="opacity-0 group-hover:opacity-100 p-0.5 hover:bg-red-500/20 rounded transition-all flex-shrink-0"
                                        title={t('projects.removeFromHistory')}
                                    >
                                        <X className="w-3 h-3 text-text-muted" />
                                    </button>
                                </button>
                            ))}
                        </div>
                    </div>
                )}
            </div>

            {/* Unified import dialog */}
            <UnifiedImportDialog
                isOpen={isImportOpen}
                onClose={() => setIsImportOpen(false)}
                onImport={handleImportProject}
                onProjectCreated={handleProjectCreated}
                isImporting={loading}
            />

            <ConfirmDialog
                isOpen={deleteProjectInfo !== null}
                title={t('projects.deleteProject')}
                message={t('projects.confirmDelete', { name: deleteProjectInfo?.name })}
                confirmLabel={t('common:action.delete')}
                variant="danger"
                onConfirm={confirmDeleteProject}
                onCancel={() => setDeleteProjectInfo(null)}
            />
        </>
    );
}