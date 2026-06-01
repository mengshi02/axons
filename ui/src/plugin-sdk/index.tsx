/**
 * axons-plugin-ui — 插件 UI 共享组件库
 *
 * 导出：CSS 变量 + 基础 React 组件（Button/Card/Input/Spinner/ProgressBar/Badge/Tabs/Modal/ConfirmDialog）
 * 插件开发者通过 import 使用，保持与 axons 主界面风格一致。
 *
 * 用法：
 *   import { Button, Card, Spinner, Modal } from 'axons-plugin-ui';
 *   import 'axons-plugin-ui/theme.css';
 */

import React from 'react';

/* ═══ Button ═══ */
export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost';
  size?: 'default' | 'sm';
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = 'primary', size = 'default', className = '', children, ...rest }, ref) => {
    const sizeClass = size === 'sm' ? 'axons-btn-sm' : '';
    return (
      <button ref={ref} className={`axons-btn axons-btn-${variant} ${sizeClass} ${className}`} {...rest}>
        {children}
      </button>
    );
  },
);
Button.displayName = 'Button';

/* ═══ Card ═══ */
export interface CardProps {
  children: React.ReactNode;
  className?: string;
}

export function Card({ children, className = '' }: CardProps) {
  return <div className={`axons-card ${className}`}>{children}</div>;
}

export function CardHeader({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`axons-card-header ${className}`}>{children}</div>;
}

export function CardBody({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`axons-card-body ${className}`}>{children}</div>;
}

/* ═══ Input ═══ */
export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {}

export function Input({ className = '', ...rest }: InputProps) {
  return <input className={`axons-input ${className}`} {...rest} />;
}

/* ═══ Select ═══ */
export interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {}

export function Select({ className = '', children, ...rest }: SelectProps) {
  return <select className={`axons-select ${className}`} {...rest}>{children}</select>;
}

/* ═══ Textarea ═══ */
export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> { }

export function Textarea({ className = '', ...rest }: TextareaProps) {
  return <textarea className={`axons-textarea ${className}`} {...rest} />;
}

/* ═══ Badge ═══ */
export interface BadgeProps {
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info';
  children: React.ReactNode;
  className?: string;
}

export function Badge({ variant = 'default', children, className = '' }: BadgeProps) {
  return <span className={`axons-badge axons-badge-${variant} ${className}`}>{children}</span>;
}

/* ═══ Divider ═══ */
export interface DividerProps {
  spacing?: 'default' | 'lg';
  className?: string;
}

export function Divider({ spacing = 'default', className = '' }: DividerProps) {
  const spacingClass = spacing === 'lg' ? 'axons-divider-lg' : '';
  return <hr className={`axons-divider ${spacingClass} ${className}`} />;
}

/* ═══ EmptyState ═══ */
export interface EmptyStateProps {
  icon?: React.ReactNode;
  title?: string;
  description?: string;
  children?: React.ReactNode;
  className?: string;
}

export function EmptyState({ icon, title, description, children, className = '' }: EmptyStateProps) {
  return (
    <div className={`axons-empty-state ${className}`}>
      {icon && <div className="axons-empty-state-icon">{icon}</div>}
      {title && <p className="axons-empty-state-title">{title}</p>}
      {description && <p className="axons-empty-state-description">{description}</p>}
      {children}
    </div>
  );
}

/* ═══ Spinner ═══ */
export interface SpinnerProps {
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}

export function Spinner({ size = 'md', className = '' }: SpinnerProps) {
  return <div className={`axons-spinner axons-spinner-${size} ${className}`} />;
}

/* ═══ ProgressBar ═══ */
export interface ProgressBarProps {
  value: number; // 0-1
  variant?: 'default' | 'success' | 'warning' | 'error';
  className?: string;
}

export function ProgressBar({ value, variant = 'default', className = '' }: ProgressBarProps) {
  const pct = Math.max(0, Math.min(1, value)) * 100;
  const variantClass = variant === 'default' ? '' : `axons-progress-bar-${variant}`;
  return (
    <div className={`axons-progress ${className}`}>
      <div className={`axons-progress-bar ${variantClass}`} style={{ width: `${pct}%` }} />
    </div>
  );
}

/* ═══ List ═══ */
export interface ListProps {
  children: React.ReactNode;
  className?: string;
}

export function List({ children, className = '' }: ListProps) {
  return <ul className={`axons-list ${className}`}>{children}</ul>;
}

export interface ListItemProps {
  icon?: React.ReactNode;
  active?: boolean;
  children: React.ReactNode;
  className?: string;
  onClick?: () => void;
}

export function ListItem({ icon, active = false, children, className = '', onClick }: ListItemProps) {
  return (
    <li className={`axons-list-item ${active ? 'axons-list-item-active' : ''} ${className}`} onClick={onClick}>
      {icon && <span className="axons-list-item-icon">{icon}</span>}
      <span className="axons-list-item-content">{children}</span>
    </li>
  );
}

/* ═══ Tabs ═══ */
export interface TabsProps {
  tabs: Array<{ id: string; label: string }>;
  activeTab: string;
  onChange: (id: string) => void;
  className?: string;
}

export function Tabs({ tabs, activeTab, onChange, className = '' }: TabsProps) {
  const handleKeyDown = (e: React.KeyboardEvent) => {
    const currentIndex = tabs.findIndex(t => t.id === activeTab);
    let nextIndex = currentIndex;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      nextIndex = (currentIndex + 1) % tabs.length;
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    } else if (e.key === 'Home') {
      e.preventDefault();
      nextIndex = 0;
    } else if (e.key === 'End') {
      e.preventDefault();
      nextIndex = tabs.length - 1;
    }
    if (nextIndex !== currentIndex) {
      onChange(tabs[nextIndex].id);
    }
  };

  return (
    <div className={`axons-tabs ${className}`} role="tablist" onKeyDown={handleKeyDown}>
      {tabs.map(tab => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={activeTab === tab.id}
          tabIndex={activeTab === tab.id ? 0 : -1}
          className={`axons-tab ${activeTab === tab.id ? 'axons-tab-active' : ''}`}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

/* ═══ ConfirmDialog ═══ */
export interface ConfirmDialogProps {
  isOpen: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: 'default' | 'danger' | 'warning';
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  isOpen,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  variant = 'danger',
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const cancelRef = React.useRef<HTMLButtonElement>(null);

  React.useEffect(() => {
    if (isOpen) {
      // Focus the cancel button by default to prevent accidental confirm
      setTimeout(() => cancelRef.current?.focus(), 0);
    }
  }, [isOpen]);

  React.useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onCancel]);

  if (!isOpen) return null;

  return (
    <Modal isOpen={isOpen} onClose={onCancel} size="sm" overlayOpacity="none" backdropBlur={false}>
      <div className="axons-confirm-body">
        <div className={`axons-confirm-icon axons-confirm-icon-${variant}`}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
        </div>
        <div className="axons-confirm-content">
          <h3 className="axons-confirm-title">{title}</h3>
          <p className="axons-confirm-message">{message}</p>
        </div>
      </div>
      <div className="axons-confirm-footer">
        <Button ref={cancelRef} variant="ghost" onClick={onCancel}>
          {cancelLabel}
        </Button>
        <Button variant={variant === 'danger' ? 'primary' : 'primary'} className={`axons-confirm-btn-${variant}`} onClick={onConfirm}>
          {confirmLabel}
        </Button>
      </div>
    </Modal>
  );
}

/* ═══ Modal ═══ */
export interface ModalProps {
  /** Whether the modal is visible */
  isOpen: boolean;
  /** Called when the modal should close */
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

const modalSizeClasses: Record<string, string> = {
  sm: 'axons-modal-sm',
  md: 'axons-modal-md',
  lg: 'axons-modal-lg',
  xl: 'axons-modal-xl',
  full: 'axons-modal-full',
};

const modalOpacityClasses: Record<string, string> = {
  none: '',
  light: 'axons-modal-overlay-light',
  medium: 'axons-modal-overlay-medium',
  dark: 'axons-modal-overlay-dark',
  darker: 'axons-modal-overlay-darker',
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
  const contentRef = React.useRef<HTMLDivElement>(null);

  const handleKeyDown = React.useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' && closeOnEscape) {
        e.preventDefault();
        onClose();
        return;
      }
      // Focus trap: Tab / Shift+Tab cycles within the modal
      if (e.key === 'Tab' && contentRef.current) {
        const focusableSelector = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';
        const focusable = contentRef.current.querySelectorAll<HTMLElement>(focusableSelector);
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    },
    [onClose, closeOnEscape],
  );

  React.useEffect(() => {
    if (!isOpen) return;
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, handleKeyDown]);

  React.useEffect(() => {
    if (!isOpen) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, [isOpen]);

  React.useEffect(() => {
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

  if (!isOpen) return null;

  const overlayClass = [
    'axons-modal-overlay',
    modalOpacityClasses[overlayOpacity],
    backdropBlur ? 'axons-modal-overlay-blur' : '',
  ].filter(Boolean).join(' ');

  const contentClass = [
    'axons-modal-content',
    modalSizeClasses[size] ?? '',
    className,
  ].filter(Boolean).join(' ');

  return (
    <div className={overlayClass} onClick={closeOnOverlayClick ? onClose : undefined}>
      <div ref={contentRef} className={contentClass} onClick={e => e.stopPropagation()} role="dialog" aria-modal="true">
        {children}
      </div>
    </div>
  );
}
