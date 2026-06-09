import React from 'react';
import {
    Activity, BarChart3, Radar, Route, ArrowLeftRight,
    Shield, Workflow, Terminal, CircleDot, Waypoints, Bell,
    Eye, EyeOff, Layers, Play, Trash2, Loader2,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppStateSelector } from '../hooks/useAppStateSelector';
import { useNotifications } from '../hooks/useNotifications';
import { useProjectActions, type EmbedActionState } from '../hooks/useProjectActions';
import type { PanelDef } from '../lib/panelRegistry';

// Icon name → component mapping for footer buttons
const ICON_MAP: Record<string, React.ComponentType<{ className?: string }>> = {
    Activity, BarChart3, Radar, Route, ArrowLeftRight,
    Shield, Workflow, Terminal,
};

/** 渲染单个 Footer 面板按钮 */
function FooterButton({ panel, isActive, onToggle, iconClass, btnBase, activeClass, inactiveClass }: {
    panel: PanelDef;
    isActive: boolean;
    onToggle: () => void;
    iconClass: string;
    btnBase: string;
    activeClass: string;
    inactiveClass: string;
}) {
    const { t } = useTranslation();
    const IconComponent = ICON_MAP[panel.icon];
    // panel.title stores i18n key (e.g. "panels:codeHealth.title"), t() resolves it
    const displayTitle = panel.title.includes(':') ? t(panel.title) : panel.title;
    return (
        <button
            onClick={onToggle}
            className={`${btnBase} ${isActive ? 'border-accent ' + activeClass : 'border-transparent ' + inactiveClass}`}
            title={displayTitle}
        >
            {IconComponent && <IconComponent className={iconClass} />}
        </button>
    );
}

/** 渲染工程操作按钮（watch/embed/build/delete） */
function ProjectActionButton({ icon, label, active, spinning, disabled, onClick, btnBase, iconClass, activeClass, inactiveClass }: {
    icon: React.ReactNode;
    label: string;
    active?: boolean;
    spinning?: boolean;
    disabled?: boolean;
    onClick: () => void;
    btnBase: string;
    iconClass: string;
    activeClass: string;
    inactiveClass: string;
}) {
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            className={`${btnBase} ${active ? 'border-accent ' + activeClass : 'border-transparent ' + inactiveClass} ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
            title={label}
        >
            {spinning ? <Loader2 className={`${iconClass} animate-spin`} /> : icon}
        </button>
    );
}

export const Footer = React.memo(function Footer() {
    const { t } = useTranslation();
    const {
        graph,
        currentProject,
        openPanels,
        togglePanel,
        getPanelsByActivator,
    } = useAppStateSelector(s => ({
        graph: s.graph,
        currentProject: s.currentProject,
        openPanels: s.openPanels,
        togglePanel: s.togglePanel,
        getPanelsByActivator: s.getPanelsByActivator,
    }));

    const {
        buildingProjectId,
        watchStatus,
        embedActionStates,
        startBuild,
        toggleWatch,
        triggerProjectEmbed,
        deleteProjectAction,
    } = useProjectActions();

    // Notification summary: show latest unread notification in footer center
    const { notifications, unreadCount } = useNotifications();
    const latestUnread = notifications.find(n => !n.read);

    const iconClass = 'w-3.5 h-3.5';
    const activeClass = 'text-accent bg-accent/10';
    const inactiveClass = 'text-text-muted hover:text-text-primary hover:bg-hover';

    const btnBase = 'px-2 h-full flex items-center gap-1 transition-colors text-[11px] font-medium border-t-2';

    const nodeCount = graph?.nodes.length ?? 0;
    const edgeCount = graph?.relationships.length ?? 0;

    // 获取所有 activator === 'footer' 的面板
    const footerPanels = getPanelsByActivator('footer');

    // 按 footerSlot 分为三段：left(默认) / center / right
    const leftPanels = footerPanels.filter(p => (p.footerSlot ?? 'left') === 'left');
    const centerPanels = footerPanels.filter(p => p.footerSlot === 'center');
    const rightPanels = footerPanels.filter(p => p.footerSlot === 'right');

    // 工程操作状态
    const projectId = currentProject?.id;
    const isBuilding = buildingProjectId === projectId;
    const isWatching = projectId ? watchStatus[projectId]?.is_running ?? false : false;
    const embedState: EmbedActionState | undefined = projectId ? embedActionStates[projectId] : undefined;
    const isEmbedding = embedState?.status === 'running';

    return (
        <footer className="h-6 flex items-center justify-between bg-surface border-t border-border-subtle select-none shrink-0">
            {/* Left section: footerSlot='left' (默认) — 分析工具类 */}
            <div className="flex items-center h-full">
                {leftPanels.map(panel => (
                    <FooterButton
                        key={panel.id}
                        panel={panel}
                        isActive={openPanels.has(panel.id)}
                        onToggle={() => togglePanel(panel.id)}
                        iconClass={iconClass}
                        btnBase={btnBase}
                        activeClass={activeClass}
                        inactiveClass={inactiveClass}
                    />
                ))}
            </div>

            {/* Center section: footerSlot='center' + Graph stats + Notification summary */}
            <div className="flex items-center gap-3 text-[11px] text-text-muted">
                {centerPanels.map(panel => (
                    <FooterButton
                        key={panel.id}
                        panel={panel}
                        isActive={openPanels.has(panel.id)}
                        onToggle={() => togglePanel(panel.id)}
                        iconClass={iconClass}
                        btnBase={btnBase}
                        activeClass={activeClass}
                        inactiveClass={inactiveClass}
                    />
                ))}
                {latestUnread && (
                    <span className="flex items-center gap-1 text-text-secondary max-w-[200px]">
                        <Bell className="w-3 h-3 shrink-0" />
                        <span className="truncate">{latestUnread.title}</span>
                        {unreadCount > 1 && (
                            <span className="text-text-muted">({unreadCount})</span>
                        )}
                    </span>
                )}
                {graph && (
                    <>
                        <span className="flex items-center gap-1"><CircleDot className="w-3 h-3" />{t('common:unit.nodes', { count: nodeCount })}</span>
                        <span className="flex items-center gap-1"><Waypoints className="w-3 h-3" />{t('common:unit.edges', { count: edgeCount })}</span>
                    </>
                )}
            </div>

            {/* Right section: 工程操作按钮 (Watch / Embed / Build / Delete) */}
            <div className="flex items-center h-full">
                {currentProject && (
                    <>
                        <ProjectActionButton
                            icon={isWatching ? <EyeOff className={iconClass} /> : <Eye className={iconClass} />}
                            label={isWatching ? t('activitybar:projects.stopWatch') : t('activitybar:projects.watch')}
                            active={isWatching}
                            onClick={() => toggleWatch(currentProject)}
                            btnBase={btnBase}
                            iconClass={iconClass}
                            activeClass={activeClass}
                            inactiveClass={inactiveClass}
                        />
                        <ProjectActionButton
                            icon={<Layers className={iconClass} />}
                            label={isEmbedding ? (embedState?.message || t('activitybar:projects.embedding')) : t('activitybar:projects.embed')}
                            active={isEmbedding}
                            spinning={isEmbedding}
                            disabled={isEmbedding}
                            onClick={() => triggerProjectEmbed(currentProject)}
                            btnBase={btnBase}
                            iconClass={iconClass}
                            activeClass={activeClass}
                            inactiveClass={inactiveClass}
                        />
                        <ProjectActionButton
                            icon={<Play className={iconClass} />}
                            label={isBuilding ? t('activitybar:projects.building') : t('activitybar:projects.build')}
                            active={isBuilding}
                            spinning={isBuilding}
                            disabled={isBuilding}
                            onClick={() => startBuild(currentProject)}
                            btnBase={btnBase}
                            iconClass={iconClass}
                            activeClass={activeClass}
                            inactiveClass={inactiveClass}
                        />
                        <ProjectActionButton
                            icon={<Trash2 className={iconClass} />}
                            label={t('activitybar:projects.delete')}
                            onClick={() => deleteProjectAction(currentProject)}
                            btnBase={btnBase}
                            iconClass={iconClass}
                            activeClass={activeClass}
                            inactiveClass={inactiveClass}
                        />
                    </>
                )}
                {rightPanels.map(panel => (
                    <FooterButton
                        key={panel.id}
                        panel={panel}
                        isActive={openPanels.has(panel.id)}
                        onToggle={() => togglePanel(panel.id)}
                        iconClass={iconClass}
                        btnBase={btnBase}
                        activeClass={activeClass}
                        inactiveClass={inactiveClass}
                    />
                ))}
            </div>
        </footer>
    );
});