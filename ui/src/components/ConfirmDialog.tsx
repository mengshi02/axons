import { useEffect, useRef, useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Modal } from './Modal';

export interface ConfirmDialogCheckbox {
  id: string;                          // Corresponds to arg name
  label: string;                       // From arg's description
  defaultChecked?: boolean;            // From arg's default
}

interface ConfirmDialogProps {
  isOpen: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: 'danger' | 'warning' | 'default';
  checkboxes?: ConfirmDialogCheckbox[];
  onConfirm: (checkboxValues?: Record<string, boolean>) => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  isOpen,
  title,
  message,
  confirmLabel,
  cancelLabel,
  variant = 'danger',
  checkboxes,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const { t } = useTranslation('common');
  const _confirmLabel = confirmLabel ?? t('action.delete');
  const _cancelLabel = cancelLabel ?? t('action.cancel');
  const confirmRef = useRef<HTMLButtonElement>(null);

  const [checkboxValues, setCheckboxValues] = useState<Record<string, boolean>>(() => {
    if (!checkboxes) return {};
    const init: Record<string, boolean> = {};
    for (const cb of checkboxes) {
      init[cb.id] = cb.defaultChecked ?? false;
    }
    return init;
  });

  // Reset checkbox values when dialog opens with new checkboxes
  useEffect(() => {
    if (isOpen && checkboxes) {
      const init: Record<string, boolean> = {};
      for (const cb of checkboxes) {
        init[cb.id] = cb.defaultChecked ?? false;
      }
      setCheckboxValues(init);
    }
  }, [isOpen, checkboxes]);

  useEffect(() => {
    if (isOpen) {
      // Focus the confirm button by default to prevent accidental confirm
      setTimeout(() => confirmRef.current?.focus(), 0);
    }
  }, [isOpen]);

  const variantStyles = {
    danger: 'bg-red-500 hover:bg-red-600 focus:ring-red-500/50',
    warning: 'bg-amber-500 hover:bg-amber-600 focus:ring-amber-500/50',
    default: 'bg-accent hover:bg-accent/90 focus:ring-accent/50',
  };

  const iconStyles = {
    danger: 'text-red-400 bg-red-500/10',
    warning: 'text-amber-400 bg-amber-500/10',
    default: 'text-accent bg-accent/10',
  };

  return (
    <Modal isOpen={isOpen} onClose={onCancel} size="sm" overlayOpacity="none" backdropBlur={false}>
      <div className="px-6 py-5">
        <div className="flex items-start gap-3">
          <div className={`shrink-0 p-2 rounded-lg ${iconStyles[variant]}`}>
            <AlertTriangle className="w-5 h-5" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-base font-semibold text-text-primary">{title}</h3>
            <p className="mt-1.5 text-sm text-text-secondary leading-relaxed">{message}</p>
          </div>
        </div>
        {checkboxes && checkboxes.length > 0 && (
          <div className="mt-3 space-y-2">
            {checkboxes.map(cb => (
              <label key={cb.id} className="flex items-center gap-2 text-sm text-text-secondary cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={checkboxValues[cb.id] ?? false}
                  onChange={e => setCheckboxValues(prev => ({ ...prev, [cb.id]: e.target.checked }))}
                  className="rounded border-border-subtle"
                />
                {cb.label}
              </label>
            ))}
          </div>
        )}
      </div>
      <div className="flex items-center justify-end gap-2 px-6 py-4 bg-elevated/50 border-t border-border-subtle">
        <button
          onClick={onCancel}
          className="px-3.5 py-1.5 text-sm rounded-lg text-text-secondary hover:bg-hover transition-colors"
        >
          {_cancelLabel}
        </button>
        <button
          ref={confirmRef}
          onClick={() => onConfirm(checkboxes ? checkboxValues : undefined)}
          className={`px-3.5 py-1.5 text-sm text-white rounded-lg transition-colors focus:outline-none focus:ring-2 ${variantStyles[variant]}`}
        >
          {_confirmLabel}
        </button>
      </div>
    </Modal>
  );
}