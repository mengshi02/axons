import { useEffect, useLayoutEffect, useRef, useState } from 'react';
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

  // Clamp menu position so it never overflows the viewport.
  // Start hidden (opacity-0) at the raw click position, measure the rendered
  // size via useLayoutEffect, then flip/shift as needed before making it
  // visible.  This avoids a visible jump — the menu is sized and placed in
  // the same paint cycle as it becomes opaque.
  const [menuStyle, setMenuStyle] = useState<React.CSSProperties>({
    position: 'fixed',
    left: x,
    top: y,
    zIndex: 9999,
    opacity: 0,        // hidden until position is clamped
    pointerEvents: 'none',
  });

  useLayoutEffect(() => {
    const el = menuRef.current;
    if (!el) return;

    const { width, height } = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const margin = 8; // minimum gap from viewport edge

    let left = x;
    let top = y;

    // Horizontal: flip left if menu overflows right edge
    if (left + width + margin > vw) {
      left = Math.max(margin, vw - width - margin);
    }
    if (left < margin) left = margin;

    // Vertical: flip upward if menu overflows bottom edge
    if (top + height + margin > vh) {
      top = Math.max(margin, vh - height - margin);
    }
    if (top < margin) top = margin;

    setMenuStyle({
      position: 'fixed',
      left,
      top,
      zIndex: 9999,
      opacity: 1,
      pointerEvents: 'auto',
    });
  }, [x, y, items]);

  return (
    <div
      ref={menuRef}
      style={menuStyle}
      className="bg-menu-bg border border-menu-border text-menu-fg rounded shadow-desktop-menu py-1 min-w-[220px] text-[13px] animate-menu-in"
    >
      {items.map((item, i) => {
        if ('separator' in item && item.separator) {
          return <div key={i} className="my-1 h-px bg-menu-separator" />;
        }
        const menuItem = item as ContextMenuItem;
        return (
          <button
            key={i}
            disabled={menuItem.disabled}
            className={`w-full text-left flex items-center justify-between gap-6 px-3 py-[5px] transition-colors duration-75
              ${menuItem.disabled
                ? 'opacity-40 cursor-not-allowed'
                : menuItem.danger
                  ? 'cursor-pointer hover:bg-menu-danger-hover-bg hover:text-menu-hover-fg'
                  : 'cursor-pointer hover:bg-menu-hover-bg hover:text-menu-hover-fg'
              }`}
            onClick={() => {
              if (!menuItem.disabled) {
                menuItem.onClick();
                onClose();
              }
            }}
          >
            <span className="flex items-center gap-2.5 min-w-0">
              {menuItem.icon
                ? <span className="w-4 h-4 flex-shrink-0 flex items-center justify-center opacity-70">{menuItem.icon}</span>
                : <span className="w-4 h-4 flex-shrink-0" />}
              <span className="truncate">{menuItem.label}</span>
            </span>
            {menuItem.shortcut && (
              <span className="text-[11px] opacity-50 tracking-wide flex-shrink-0">{menuItem.shortcut}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}