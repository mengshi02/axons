import {
    Home, FolderTree, Sparkles, Settings, Puzzle,
    Activity, BarChart3, Radar, Route, ArrowLeftRight,
    Shield, Workflow, Terminal, Code2,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppState } from '../hooks/useAppState';
import React, { useState, useRef, useEffect, type ComponentType } from 'react';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';
import { ProjectSelector } from './ProjectSelector';
import type { PanelDef } from '../lib/panelRegistry';

// Icon name → component mapping for activityBar buttons
// Covers all built-in panel icons; plugin icons use URL or Puzzle fallback
const ICON_MAP: Record<string, ComponentType<{ className?: string }>> = {
    Home, FolderTree, Sparkles, Settings, Puzzle,
    Activity, BarChart3, Radar, Route, ArrowLeftRight,
    Shield, Workflow, Terminal, Code2,
};

interface ActivityBarProps { }

export const ActivityBar = React.memo(function ActivityBar(_props: ActivityBarProps) {
    const {
        openPanel,
        togglePanel,
        openPanels,
        getPanelsByActivator,
    } = useAppState();
    const { t } = useTranslation();

    const [isHomeOpen, setIsHomeOpen] = useState(false);
    const [isGearMenuOpen, setIsGearMenuOpen] = useState(false);
    const homeRef = useRef<HTMLDivElement>(null);
    const gearRef = useRef<HTMLDivElement>(null);

    // When any popup is open, disable iframe pointer-events so clicks penetrate
    // to the host document and trigger click-outside closing logic
    useIframePointerEvents(isHomeOpen || isGearMenuOpen);

    // Click outside to close Home panel and GearMenu
    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (homeRef.current && !homeRef.current.contains(e.target as Node)) {
                setIsHomeOpen(false);
            }
            if (gearRef.current && !gearRef.current.contains(e.target as Node)) {
                setIsGearMenuOpen(false);
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    const iconBtnClass = (active: boolean) =>
        `relative w-full h-12 flex items-center justify-center transition-colors ${active
            ? 'text-accent bg-accent/10 before:absolute before:left-0 before:top-1 before:bottom-1 before:w-0.5 before:bg-accent before:rounded-r'
            : 'text-text-muted hover:text-text-primary hover:bg-hover'
        }`;

    /** Resolve panel active state — handles both registry-driven and legacy state */
    const isPanelActive = (panel: PanelDef): boolean => {
        // Home popup: use local state
        if (panel.id === 'home') return isHomeOpen;
        // All others: check openPanels set (unified via panelRegistry)
        return openPanels.has(panel.id);
    };

    /** Handle panel button click — activityBar panels are mutually exclusive */
    const handlePanelClick = (panel: PanelDef) => {
        if (panel.id === 'home') {
            setIsHomeOpen(prev => !prev);
        } else {
            togglePanel(panel.id);
        }
    };

    /** Render panel icon: lucide name → ICON_MAP, URL → img, fallback → Puzzle */
    const renderPanelIcon = (panel: PanelDef) => {
        // URL-based icon (plugin icon from manifest)
        if (panel.icon.startsWith('/') || panel.icon.startsWith('http')) {
            return <img src={panel.icon} alt={panel.title} className="w-5 h-5" />;
        }
        // Lucide icon name lookup
        const IconComponent = ICON_MAP[panel.icon];
        if (IconComponent) {
            return <IconComponent className="w-5 h-5" />;
        }
        // Fallback for unknown icon names
        return <Puzzle className="w-5 h-5" />;
    };

    // Get all activityBar panels (already sorted by order via panelRegistry)
    const activityBarPanels = getPanelsByActivator('activityBar');

    return (
        <div className="w-11 h-full bg-void flex flex-col items-center shrink-0 border-r border-border-subtle">
            {/* Top section: all activityBar panels (built-in + plugins), sorted by order */}
            <div className="flex flex-col items-center w-full">
                {activityBarPanels.map(panel => {
                    const isActive = isPanelActive(panel);
                    const displayTitle = panel.title.includes(':') ? t(panel.title) : panel.title;

                    // Home panel: wrap with ref for click-outside popup
                    if (panel.id === 'home') {
                        return (
                            <div className="relative w-full" ref={homeRef} key={panel.id}>
                                <button
                                    onClick={() => handlePanelClick(panel)}
                                    className={iconBtnClass(isActive)}
                                    title={displayTitle}
                                >
                                    {renderPanelIcon(panel)}
                                </button>
                                {isHomeOpen && (
                                    <div className="absolute left-11 top-0 z-50">
                                        <ProjectSelector onProjectSelect={() => setIsHomeOpen(false)} />
                                    </div>
                                )}
                            </div>
                        );
                    }

                    // All other panels: simple toggle button
                    return (
                        <button
                            key={panel.id}
                            onClick={() => handlePanelClick(panel)}
                            className={iconBtnClass(isActive)}
                            title={displayTitle}
                        >
                            {renderPanelIcon(panel)}
                        </button>
                    );
                })}
            </div>

            {/* Bottom section: Gear Menu only (activator='gearMenu') */}
            <div className="mt-auto w-full flex flex-col items-center pb-1">
                <div className="relative w-full" ref={gearRef}>
                    <button
                        onClick={() => setIsGearMenuOpen(prev => !prev)}
                        className={iconBtnClass(isGearMenuOpen)}
                        title="Menu"
                    >
                        <Settings className="w-5 h-5" />
                    </button>

                    {/* Gear dropdown menu — IDE style, neutral-gray independent of UI theme surface */}
                    {isGearMenuOpen && (
                        <div className="absolute left-11 bottom-0 z-50 bg-menu-bg border border-menu-border text-menu-fg rounded shadow-desktop-menu py-1 min-w-[200px] text-[13px] animate-menu-in">
                            <button
                                onClick={() => { openPanel('extensions'); setIsGearMenuOpen(false); }}
                                className="w-full text-left px-3 py-[5px] transition-colors duration-75 hover:bg-menu-hover-bg hover:text-menu-hover-fg cursor-pointer"
                            >
                                Extensions
                            </button>
                            <div className="my-1 h-px bg-menu-separator" />
                            <button
                                onClick={() => { openPanel('settings'); setIsGearMenuOpen(false); }}
                                className="w-full text-left px-3 py-[5px] transition-colors duration-75 hover:bg-menu-hover-bg hover:text-menu-hover-fg cursor-pointer"
                            >
                                Settings
                            </button>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
});