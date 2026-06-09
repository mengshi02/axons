/**
 * useProjectActions — 共享的项目操作 hook
 *
 * 提供 build / watch / embed / delete 四个操作，以及对应的状态。
 * 被 Footer / MenuBar / ProjectSelector 三处共用，避免逻辑重复。
 */

import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppState } from './useAppState';
import { useAppStateSelector } from './useAppStateSelector';
import {
    deleteProject, fetchProjects, fetchGraph,
    startBuild, fetchTaskStatus, startProjectWatch, stopProjectWatch,
    fetchProjectWatchStatus, fetchEmbedStatus, triggerEmbed,
    type Project,
} from '../services/api';
import { useEventStream, type EmbedProgressEvent, type EmbedCompleteEvent, type EmbedErrorEvent } from './useEventStream';

// ─── Types ───────────────────────────────────────────────────

export interface EmbedStatusEntry {
    configured: boolean;
    embedding_count: number;
    code_embedding_count: number;
    needs_reembedding: boolean;
}

export type EmbedActionStatus = 'idle' | 'running' | 'success' | 'error';

export interface EmbedActionState {
    status: EmbedActionStatus;
    progress?: { current: number; total: number };
    message?: string;
    totalNodes?: number;
    newEmbeddings?: number;
    updatedEmbeddings?: number;
    error?: string;
}

export interface ProjectActionsResult {
    /** 当前正在构建的项目 ID */
    buildingProjectId: string | null;
    /** 各项目的 watch 状态 */
    watchStatus: Record<string, { is_running: boolean; watch_enabled: boolean }>;
    /** 各项目的 embed 状态 */
    embedStatus: Record<string, EmbedStatusEntry>;
    /** 各项目的 embed 操作状态 */
    embedActionStates: Record<string, EmbedActionState>;
    /** 触发项目构建 */
    startBuild: (project: Project) => Promise<void>;
    /** 切换项目 watch */
    toggleWatch: (project: Project) => Promise<void>;
    /** 触发项目 embed */
    triggerProjectEmbed: (project: Project) => Promise<void>;
    /** 删除项目 */
    deleteProjectAction: (project: Project) => Promise<void>;
}

// ─── Hook ────────────────────────────────────────────────────

export function useProjectActions(): ProjectActionsResult {
    const { t } = useTranslation('activitybar');
    const { currentProject } = useAppStateSelector(s => ({
        currentProject: s.currentProject,
    }));
    const { setGraph, markProjectBuilding, loadProjects, setCurrentProject } = useAppState();

    const [buildingProjectId, setBuildingProjectId] = useState<string | null>(null);
    const [watchStatus, setWatchStatus] = useState<Record<string, { is_running: boolean; watch_enabled: boolean }>>({});
    const [embedStatus, setEmbedStatus] = useState<Record<string, EmbedStatusEntry>>({});
    const [embedActionStates, setEmbedActionStates] = useState<Record<string, EmbedActionState>>({});

    // ─── Embed status loader ─────────────────────────────────
    const loadEmbedStatusForProject = useCallback(async (projectId: string) => {
        try {
            const status = await fetchEmbedStatus(projectId);
            setEmbedStatus(prev => ({
                ...prev,
                [projectId]: {
                    configured: status.is_configured,
                    embedding_count: status.embedding_count,
                    code_embedding_count: status.code_embedding_count,
                    needs_reembedding: status.needs_reembedding,
                },
            }));
        } catch { /* ignore */ }
    }, []);

    // ─── Embed SSE handlers ─────────────────────────────────
    const handleEmbedProgress = useCallback((data: EmbedProgressEvent) => {
        setEmbedActionStates(prev => ({
            ...prev,
            [data.project_id]: {
                status: 'running',
                progress: { current: data.current, total: data.total },
                message: t('projects.embeddingProgress', { current: data.current, total: data.total }),
            },
        }));
    }, [t]);

    const handleEmbedComplete = useCallback((data: EmbedCompleteEvent) => {
        setEmbedActionStates(prev => ({
            ...prev,
            [data.project_id]: {
                status: 'success',
                totalNodes: data.total_nodes,
                newEmbeddings: data.new_embeddings,
                updatedEmbeddings: data.updated_embeddings,
                message: t('projects.embeddingDone', { new: data.new_embeddings, updated: data.updated_embeddings }),
            },
        }));
        loadEmbedStatusForProject(data.project_id);
        setTimeout(() => {
            setEmbedActionStates(prev => {
                const newState = { ...prev };
                delete newState[data.project_id];
                return newState;
            });
        }, 2000);
    }, [loadEmbedStatusForProject]);

    const handleEmbedError = useCallback((data: EmbedErrorEvent) => {
        setEmbedActionStates(prev => ({
            ...prev,
            [data.project_id]: { status: 'error', error: data.error, message: data.error },
        }));
        setTimeout(() => {
            setEmbedActionStates(prev => {
                const newState = { ...prev };
                delete newState[data.project_id];
                return newState;
            });
        }, 3000);
    }, []);

    useEventStream({
        onEmbedProgress: handleEmbedProgress,
        onEmbedComplete: handleEmbedComplete,
        onEmbedError: handleEmbedError,
    });

    // ─── Load watch/embed status for current project ─────────
    useEffect(() => {
        if (currentProject) {
            fetchProjectWatchStatus(currentProject.id)
                .then(status => setWatchStatus(prev => ({
                    ...prev,
                    [currentProject.id]: { is_running: status.is_running, watch_enabled: status.watch_enabled },
                })))
                .catch(() => {});
            loadEmbedStatusForProject(currentProject.id);
        }
    }, [currentProject, loadEmbedStatusForProject]);

    // ─── Actions ─────────────────────────────────────────────
    const startBuildAction = useCallback(async (project: Project) => {
        setBuildingProjectId(project.id);
        try {
            const task = await startBuild({ root_dir: project.root_path, full_build: true, project_id: project.id });
            const pollInterval = setInterval(async () => {
                try {
                    const status = await fetchTaskStatus(task.task_id);
                    if (status.status === 'complete') {
                        clearInterval(pollInterval);
                        setBuildingProjectId(null);
                        markProjectBuilding(project.id, false);
                        try {
                            const graphData = await fetchGraph({ projectId: project.id, filterConnected: true, includeStats: true });
                            setGraph(graphData);
                        } catch (err) { console.error('[useProjectActions] Failed to fetch graph:', err); }
                    } else if (status.status === 'error') {
                        clearInterval(pollInterval);
                        setBuildingProjectId(null);
                        markProjectBuilding(project.id, false);
                    }
                } catch (err) {
                    clearInterval(pollInterval);
                    setBuildingProjectId(null);
                    markProjectBuilding(project.id, false);
                }
            }, 1000);
        } catch (err) {
            setBuildingProjectId(null);
            markProjectBuilding(project.id, false);
        }
    }, [setGraph, markProjectBuilding]);

    const toggleWatch = useCallback(async (project: Project) => {
        try {
            const status = watchStatus[project.id];
            if (status?.is_running) {
                await stopProjectWatch(project.id);
                setWatchStatus(prev => ({ ...prev, [project.id]: { is_running: false, watch_enabled: false } }));
            } else {
                await startProjectWatch(project.id);
                setWatchStatus(prev => ({ ...prev, [project.id]: { is_running: true, watch_enabled: true } }));
            }
        } catch (err) { console.error('Failed to toggle watch:', err); }
    }, [watchStatus]);

    const triggerProjectEmbed = useCallback(async (project: Project) => {
        if (embedActionStates[project.id]?.status === 'running') return;
        setEmbedActionStates(prev => ({ ...prev, [project.id]: { status: 'running', message: t('projects.startingEmbedding') } }));
        try {
            await triggerEmbed({ strategy: 'incremental', projectId: project.id });
        } catch (err) {
            setEmbedActionStates(prev => ({
                ...prev,
                [project.id]: {
                    status: 'error',
                    error: err instanceof Error ? err.message : t('projects.embeddingFailed'),
                    message: err instanceof Error ? err.message : t('projects.embeddingFailed'),
                },
            }));
            setTimeout(() => {
                setEmbedActionStates(prev => { const s = { ...prev }; delete s[project.id]; return s; });
            }, 3000);
        }
    }, [embedActionStates, t]);

    const deleteProjectAction = useCallback(async (project: Project) => {
        try {
            const isDeletingCurrent = currentProject?.id === project.id;
            await deleteProject(project.id);
            const result = await fetchProjects();
            const remaining = (result.projects || []).filter((p: Project) => p.id !== project.id);
            await loadProjects();
            if (isDeletingCurrent) {
                setGraph(null);
                setCurrentProject(remaining.length > 0 ? remaining[0] : null);
            }
        } catch (err) { console.error('Failed to delete project:', err); }
    }, [currentProject, setGraph, setCurrentProject, loadProjects]);

    return {
        buildingProjectId,
        watchStatus,
        embedStatus,
        embedActionStates,
        startBuild: startBuildAction,
        toggleWatch,
        triggerProjectEmbed,
        deleteProjectAction,
    };
}