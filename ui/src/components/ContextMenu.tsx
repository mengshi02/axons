import { useEffect, useRef } from 'react';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';

export interface ContextMenuItem {
  label: string;
  icon?: React.ReactNode;
  shortcut?: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  separator?: false;
}

export interface ContextMenuSeparator {
  separator: true;
}

export type ContextMenuEntry = ContextMenuItem | ContextMenuSeparator;

interface ContextMenuProps {
  x: number;
  y: number;
  items: ContextMenuEntry[];
  onClose: () => void;
}

export function ContextMenu({ x, y, items, onClose }: ContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null);

  // ContextMenu is mounted = open, always disable iframe pointer-events
  useIframePointerEvents(true);

  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [onClose]);

  // Adjust position to avoid going off screen
  const menuStyle: React.CSSProperties = {
    position: 'fixed',
    left: x,
    top: y,
    zIndex: 9999,
  };

  return (
    <div
      ref={menuRef}
      style={menuStyle}
      className="desktop-menu overflow-hidden bg-elevated/80 backdrop-blur-xl border border-border-default/60 rounded-lg shadow-desktop-menu py-1.5 min-w-[200px] text-[13px] animate-menu-in"
    >
      {items.map((item, i) => {
        if ('separator' in item && item.separator) {
          return <div key={i} className="mx-2 my-1.5 h-px bg-border-default/70" />;
        }
        const menuItem = item as ContextMenuItem;
        return (
          <button
            key={i}
            disabled={menuItem.disabled}
            className={`w-full text-left flex items-center justify-between gap-4 px-3 py-[6px] rounded-md mx-1 transition-all duration-150
              ${menuItem.disabled
                ? 'opacity-40 cursor-not-allowed'
                : menuItem.danger
                  ? 'text-red-400 hover:bg-red-500/10 cursor-pointer'
                : 'text-text-primary cursor-pointer hover:bg-accent/8'
              }`}
            onClick={() => {
              if (!menuItem.disabled) {
                menuItem.onClick();
                onClose();
              }
            }}
          >
            <span className="flex items-center gap-2.5">
              {menuItem.icon && <span className="w-4 h-4 flex items-center justify-center text-text-secondary">{menuItem.icon}</span>}
              <span className={menuItem.disabled ? '' : 'text-text-primary'}>{menuItem.label}</span>
            </span>
            {menuItem.shortcut && (
              <span className="text-[11px] text-text-muted/70 tracking-wide ml-auto">{menuItem.shortcut}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}