import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';

/**
 * Imperative handle exposed to parent components.
 * 父组件通过 ref 与输入框交互，不再让按键触发父组件 re-render。
 */
export interface ChatComposerHandle {
  /** Read current value (synchronous, no re-render). */
  getValue: () => string;
  /** Replace current value. */
  setValue: (value: string) => void;
  /** Clear current value. */
  clear: () => void;
  /** Focus the underlying textarea. */
  focus: () => void;
  /** Insert text at cursor position. */
  insertAtCursor: (text: string) => void;
}

export interface ChatComposerProps {
  placeholder?: string;
  rows?: number;
  className?: string;
  /** Initial value (uncontrolled — parent does not hold state). */
  initialValue?: string;
  /** Submit handler — receives the trimmed text and the raw value. */
  onSubmit: (trimmed: string, raw: string) => void;
  /** Notify parent only when the "is empty" boolean flips, to update Send button disabled state. */
  onEmptyChange?: (isEmpty: boolean) => void;
  /** Whether sending is in progress (parent decides; passed via prop, not state read from parent each keystroke). */
  disabled?: boolean;
  /** Max auto-grow height (px). Default 200 to match prior behavior. */
  maxHeight?: number;
}

/**
 * Self-managed chat input. Holds its own value state so that keystrokes
 * never cause the parent (chat panel + history list) to re-render.
 *
 * Parent reads value imperatively via the ref on submit / retry.
 * Only "isEmpty" flips are propagated upward (for Send button disabled state).
 */
export const ChatComposer = forwardRef<ChatComposerHandle, ChatComposerProps>(function ChatComposer(
  {
    placeholder,
    rows = 3,
    className,
    initialValue = '',
    onSubmit,
    onEmptyChange,
    disabled,
    maxHeight = 200,
  },
  ref,
) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [value, setValue] = useState<string>(initialValue);
  const lastEmptyRef = useRef<boolean>(initialValue.trim().length === 0);

  // Auto-resize on value change
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`;
  }, [value, maxHeight]);

  // Notify parent only when emptiness flips
  useEffect(() => {
    const isEmpty = value.trim().length === 0;
    if (isEmpty !== lastEmptyRef.current) {
      lastEmptyRef.current = isEmpty;
      onEmptyChange?.(isEmpty);
    }
  }, [value, onEmptyChange]);

  // Imperative API
  useImperativeHandle(
    ref,
    () => ({
      getValue: () => textareaRef.current?.value ?? '',
      setValue: (v: string) => setValue(v),
      clear: () => setValue(''),
      focus: () => textareaRef.current?.focus(),
      insertAtCursor: (text: string) => {
        const el = textareaRef.current;
        if (!el) {
          setValue(prev => prev + text);
          return;
        }
        const start = el.selectionStart ?? el.value.length;
        const end = el.selectionEnd ?? el.value.length;
        const next = el.value.slice(0, start) + text + el.value.slice(end);
        setValue(next);
        // Restore cursor after React updates the DOM
        requestAnimationFrame(() => {
          if (textareaRef.current) {
            const pos = start + text.length;
            textareaRef.current.selectionStart = pos;
            textareaRef.current.selectionEnd = pos;
            textareaRef.current.focus();
          }
        });
      },
    }),
    [],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // 与原实现保持一致：keyCode 229 表示 IME 组合中，避免误触发发送
      if (e.key === 'Enter' && !e.shiftKey && e.keyCode !== 229) {
        e.preventDefault();
        if (disabled) return;
        const raw = textareaRef.current?.value ?? '';
        const trimmed = raw.trim();
        if (!trimmed) return;
        onSubmit(trimmed, raw);
      }
    },
    [onSubmit, disabled],
  );

  return (
    <textarea
      ref={textareaRef}
      value={value}
      onChange={e => setValue(e.target.value)}
      onKeyDown={handleKeyDown}
      placeholder={placeholder}
      rows={rows}
      className={className}
    />
  );
});