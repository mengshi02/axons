import { Bell } from 'lucide-react';

interface NotificationBellProps {
  unreadCount: number;
  onClick: () => void;
}

export function NotificationBell({ unreadCount, onClick }: NotificationBellProps) {
  return (
    <button
      onClick={onClick}
      className="relative p-1.5 rounded-md hover:bg-hover transition-colors"
      title="Notifications"
    >
      <Bell className="w-4 h-4 text-text-muted" />
      {unreadCount > 0 && (
        <span className="absolute -top-0.5 -right-0.5 min-w-[14px] h-[14px]
                         flex items-center justify-center rounded-full
                         bg-red-500 text-white text-[9px] font-bold leading-none px-0.5">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </button>
  );
}