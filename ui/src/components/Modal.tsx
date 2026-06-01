import { useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';

export interface ModalProps {
  /** Whether the modal is visible */
  isOpen: boolean;
  /** Called when the modal should close (overlay click, Escape key, etc.) */
  onClose: () => void;
  /** Modal content — any React nodes */
  children: React.ReactNode;
  /** Click overlay to close? @default true */
  closeOnOverlayClick?: boolean;
  /** Press Escape to close? @default true */
  closeOnEscape?: boolean;
  /** Predefined max-width for the content area */
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full';
  /** Overlay darkness level */
  overlayOpacity?: 'none' | 'light' | 'medium' | 'dark' | 'darker';
  /** Apply backdrop-blur to overlay? @default true */
  backdropBlur?: boolean;
  /** Additional className for the content (card) container */
  className?: string;
}

const sizeClasses: Record<string, string> = {
  sm: 'max-w-sm',
  md: 'max-w-lg',
  lg: 'max-w-2xl',
  xl: 'max-w-5xl',
  full: 'max-w-[95vw]',
};

const opacityClasses: Record<string, string> = {
  none: '',
  light: 'bg-black/40',
  medium: 'bg-black/50',
  dark: 'bg-black/60',
  darker: 'bg-black/80',
};

export function Modal({
  isOpen,
  onClose,
  children,
  closeOnOverlayClick = true,
  closeOnEscape = true,
  size = 'md',
  overlayOpacity = 'medium',
  backdropBlur = true,
  className = '',
}: ModalProps) {
  const contentRef = useRef<HTMLDivElement>(null);
  const overlayMouseDownRef = useRef(false);

  // ── Escape key ──────────────────────────────────────────────────────────
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' && closeOnEscape) {
        e.preventDefault();
        onClose();
      }
    },
    [onClose, closeOnEscape],
  );

  useEffect(() => {
    if (!isOpen) return;
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, handleKeyDown]);

  // ── Body scroll lock ────────────────────────────────────────────────────
  useEffect(() => {
    if (!isOpen) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  }, [isOpen]);

  // ── Focus management: focus first focusable element on open ─────────────
  useEffect(() => {
    if (!isOpen) return;
    const el = contentRef.current;
    if (!el) return;
    const focusable = el.querySelector<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    if (focusable) {
      setTimeout(() => focusable.focus(), 0);
    }
  }, [isOpen]);

  // ── Overlay click handling ────────────────────────────────────────────────
  // Track mousedown origin to prevent "click-through" — a click that starts
  // on the trigger button (before the modal opens) and ends on the overlay
  // (after the modal renders) should NOT close the modal.
  const handleOverlayMouseDown = useCallback(() => {
    overlayMouseDownRef.current = true;
  }, []);

  const handleOverlayClick = useCallback(() => {
    if (closeOnOverlayClick && overlayMouseDownRef.current) {
      onClose();
    }
    overlayMouseDownRef.current = false;
  }, [closeOnOverlayClick, onClose]);

  if (!isOpen) return null;

  const overlayClass = [
    'fixed inset-0 z-50 flex items-center justify-center',
    opacityClasses[overlayOpacity],
    backdropBlur ? 'backdrop-blur-sm' : '',
  ]
    .filter(Boolean)
    .join(' ');

  const contentClass = [
    'bg-surface border border-border-subtle rounded-xl shadow-2xl w-full overflow-hidden',
    sizeClasses[size] ?? '',
    'm-4',
    className,
  ]
    .filter(Boolean)
    .join(' ');

  return createPortal(
    <div className={overlayClass} onMouseDown={handleOverlayMouseDown} onClick={handleOverlayClick}>
      <div
        ref={contentRef}
        className={contentClass}
        onMouseDown={e => e.stopPropagation()}
        onClick={e => e.stopPropagation()}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}