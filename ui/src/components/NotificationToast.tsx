import { useEffect, useState, useCallback, useRef } from 'react';
import { X, CheckCircle2, AlertTriangle, AlertCircle, Info, Loader2 } from 'lucide-react';
import type { Notification } from '../services/api';
import { getRuntimeMode } from '../lib/config';

// --- Notification sound (Web Audio API, desktop only) --- 

let audioCtx: AudioContext | null = null;

/** Play a short notification beep sound. Only plays on desktop runtime. */
function playNotificationSound() {
  if (getRuntimeMode() !== 'desktop') return;

  try {
    if (!audioCtx) {
      audioCtx = new AudioContext();
    }
    // Resume context if suspended (browser autoplay policy)
    if (audioCtx.state === 'suspended') {
      audioCtx.resume();
    }

    const osc = audioCtx.createOscillator();
    const gain = audioCtx.createGain();

    osc.connect(gain);
    gain.connect(audioCtx.destination);

    // Pleasant notification tone: 880Hz sine, 150ms duration, gentle fade-out
    osc.type = 'sine';
    osc.frequency.setValueAtTime(880, audioCtx.currentTime);
    gain.gain.setValueAtTime(0.3, audioCtx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audioCtx.currentTime + 0.15);

    osc.start(audioCtx.currentTime);
    osc.stop(audioCtx.currentTime + 0.15);
  } catch {
    // Silently ignore audio failures — sound is non-critical UX enhancement
  }
}

interface NotificationToastProps {
  notifications: Notification[];
  isPanelOpen: boolean;
  onMarkRead: (id: string) => void;
}

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

// Auto-dismiss duration (ms) — error/warning stay until manually dismissed
const AUTO_DISMISS_MS = 3000;
const MAX_TOASTS = 3;

interface ToastEntry {
  notification: Notification;
  id: number;
}

export function NotificationToast({ notifications, isPanelOpen, onMarkRead }: NotificationToastProps) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const counterRef = useRef(0);
  const prevNotificationIdsRef = useRef<Set<string>>(new Set());

  // Watch for new notifications → create toasts
  useEffect(() => {
    const prevIds = prevNotificationIdsRef.current;
    const currentIds = new Set(notifications.map(n => n.id));

    // Find newly created unread notifications
    const newToasts: ToastEntry[] = [];
    for (const n of notifications) {
      if (!prevIds.has(n.id) && !n.read) {
        newToasts.push({ notification: n, id: counterRef.current++ });
      }
    }

    if (newToasts.length > 0 && !isPanelOpen) {
      // Play notification sound on desktop when new toast appears
      playNotificationSound();
      setToasts(prev => {
        const updated = [...prev, ...newToasts];
        // Keep only the most recent MAX_TOASTS
        return updated.slice(-MAX_TOASTS);
      });
    }

    prevNotificationIdsRef.current = currentIds;
  }, [notifications, isPanelOpen]);

  const dismiss = useCallback((toastId: number) => {
    setToasts(prev => prev.filter(t => t.id !== toastId));
  }, []);

  return (
    <div className="fixed bottom-4 right-4 flex flex-col gap-2 z-[100] pointer-events-none">
      {toasts.map(toast => (
        <ToastItem
          key={toast.id}
          toast={toast}
          onDismiss={() => dismiss(toast.id)}
          onMarkRead={onMarkRead}
        />
      ))}
    </div>
  );
}

function ToastItem({ toast, onDismiss, onMarkRead }: {
  toast: ToastEntry;
  onDismiss: () => void;
  onMarkRead: (id: string) => void;
}) {
  const n = toast.notification;
  const IconComponent = TYPE_ICON[n.type] || Info;
  const iconColor = TYPE_COLOR[n.type] || 'text-text-muted';
  const autoDismiss = n.type !== 'error' && n.type !== 'warning';

  useEffect(() => {
    if (!autoDismiss) return;
    const timer = setTimeout(onDismiss, AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, [autoDismiss, onDismiss]);

  return (
    <div
      className="pointer-events-auto min-w-[280px] max-w-[360px] bg-surface border border-border-subtle rounded-lg shadow-xl p-3 flex items-start gap-2 animate-in slide-in-from-right"
      onClick={() => {
        onMarkRead(n.id);
        onDismiss();
      }}
    >
      <IconComponent className={`w-4 h-4 mt-0.5 flex-shrink-0 ${iconColor} ${n.type === 'progress' ? 'animate-spin' : ''}`} />
      <div className="flex-1 min-w-0">
        <div className="text-sm font-medium text-text-primary truncate">{n.title}</div>
        {n.message && <div className="text-xs text-text-secondary mt-0.5 truncate">{n.message}</div>}
      </div>
      <button
        onClick={(e) => {
          e.stopPropagation();
          onDismiss();
        }}
        className="p-0.5 text-text-muted hover:text-text-primary transition-colors flex-shrink-0"
      >
        <X className="w-3 h-3" />
      </button>
    </div>
  );
}