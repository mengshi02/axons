import { useEffect, useRef, useState } from 'react';
import { X, CheckCircle2, AlertTriangle, AlertCircle, Info, Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';
import type { Notification } from '../services/api';

interface NotificationPanelProps {
  notifications: Notification[];
  onMarkRead: (id: string) => void;
  onMarkAllRead: () => void;
  onDelete: (id: string) => void;
  onClose: () => void;
  onActionClick?: (url: string) => void;
}

type FilterType = 'all' | 'info' | 'success' | 'warning' | 'error' | 'progress';

const TYPE_ICON: Record<string, React.ComponentType<{ className?: string }>> = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  error: AlertCircle,
  progress: Loader2,
};

const TYPE_COLOR: Record<string, string> = {
  info: 'text-blue-400',
  success: 'text-green-400',
  warning: 'text-yellow-400',
  error: 'text-red-400',
  progress: 'text-purple-400',
};

const FILTER_OPTIONS: Array<{ value: FilterType; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'error', label: 'Errors' },
  { value: 'warning', label: 'Warnings' },
  { value: 'success', label: 'Success' },
  { value: 'progress', label: 'Progress' },
  { value: 'info', label: 'Info' },
];

function formatRelativeTime(timestamp: string): string {
  const now = Date.now();
  const then = new Date(timestamp).getTime();
  const diff = now - then;
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return 'Just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function getSourceName(source: string): string {
  if (source === 'host') return 'Axons';
  // Show plugin ID as fallback (plugin name would come from manifest)
  return source;
}

export function NotificationPanel({
  notifications,
  onMarkRead,
  onMarkAllRead,
  onDelete,
  onClose,
  onActionClick,
}: NotificationPanelProps) {
  const { t } = useTranslation('notifications');
  const panelRef = useRef<HTMLDivElement>(null);
  const [filter, setFilter] = useState<FilterType>('all');

  // When panel is mounted (= open), disable iframe pointer-events so clicks
  // penetrate to the host document and trigger click-outside closing logic
  useIframePointerEvents(true);

  // Filter notifications by type
  const filteredNotifications = filter === 'all'
    ? notifications
    : notifications.filter(n => n.type === filter);

  // Click outside to close
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [onClose]);

  // Escape to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleNotificationClick = (n: Notification) => {
    if (!n.read) onMarkRead(n.id);
    // Execute first action if available
    const actions = n.actions ?? [];
    if (actions.length > 0 && actions[0].url && onActionClick) {
      onActionClick(actions[0].url);
    }
  };

  return (
    <div
      ref={panelRef}
      className="absolute top-full right-0 mt-1 w-80 bg-surface border border-border-subtle rounded-lg shadow-xl overflow-hidden z-50"
      style={{ '--wails-draggable': 'no-drag' } as React.CSSProperties}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-border-subtle bg-elevated">
        <span className="text-sm font-semibold text-text-primary">{t('title')}</span>
        {notifications.some(n => !n.read) && (
          <button
            onClick={onMarkAllRead}
            className="text-[11px] text-accent hover:text-accent/80 transition-colors"
          >
            {t('markAllRead')}
          </button>
        )}
      </div>

      {/* Filter tabs */}
      <div className="flex items-center gap-1 px-2 py-1 border-b border-border-subtle/50 bg-elevated/50">
        {FILTER_OPTIONS.map(opt => (
          <button
            key={opt.value}
            onClick={() => setFilter(opt.value)}
            className={`px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors ${filter === opt.value
              ? 'bg-accent text-white'
              : 'text-text-muted hover:text-text-primary hover:bg-hover'
              }`}
          >
            {opt.label}
          </button>
        ))}
      </div>

      {/* Notification list */}
      <div className="max-h-64 overflow-y-auto">
        {filteredNotifications.length === 0 ? (
          <div className="px-4 py-6 text-sm text-text-muted text-center">
            {t('noNotifications')}
          </div>
        ) : (
          filteredNotifications.map(n => {
            const IconComponent = TYPE_ICON[n.type] || Info;
            const iconColor = TYPE_COLOR[n.type] || 'text-text-muted';
            return (
              <div
                key={n.id}
                className={`px-3 py-2 border-b border-border-subtle/50 cursor-pointer hover:bg-hover transition-colors ${!n.read ? 'bg-accent/5' : ''}`}
                onClick={() => handleNotificationClick(n)}
              >
                <div className="flex items-start gap-2">
                  {/* Unread dot + icon */}
                  <div className="flex items-center gap-1.5 pt-0.5 flex-shrink-0">
                    {!n.read && <span className="w-1.5 h-1.5 rounded-full bg-accent" />}
                    <IconComponent className={`w-3.5 h-3.5 ${iconColor} ${n.type === 'progress' ? 'animate-spin' : ''}`} />
                  </div>

                  {/* Content */}
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium text-text-primary truncate">{n.title}</div>
                    {n.message && (
                      <div className="text-xs text-text-secondary mt-0.5 truncate">{n.message}</div>
                    )}
                    {(n.actions ?? []).length > 0 && (
                      <div className="flex gap-1 mt-1">
                        {(n.actions ?? []).map(a => (
                          <button
                            key={a.id}
                            onClick={(e) => {
                              e.stopPropagation();
                              if (onActionClick) onActionClick(a.url);
                            }}
                            className="text-[11px] px-1.5 py-0.5 rounded bg-accent/10 text-accent hover:bg-accent/20 transition-colors"
                          >
                            {a.label}
                          </button>
                        ))}
                      </div>
                    )}
                    <div className="text-[10px] text-text-muted mt-0.5">
                      {getSourceName(n.source)} · {formatRelativeTime(n.timestamp)}
                    </div>
                  </div>

                  {/* Delete button */}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onDelete(n.id);
                    }}
                    className="p-0.5 text-text-muted hover:text-text-primary transition-colors flex-shrink-0"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}