import { memo } from 'react';
import {
  Loader2,
  AlertTriangle,
  CheckCircle2,
  Wrench,
  RefreshCw,
} from 'lucide-react';
import type { TFunction } from'i18next';
import { getCachedMarkdown, renderStreamingMarkdown } from './markdownCache';

/**
 * Message shape kept in sync with RightPanel.
 * Fields here MUST be reflected in `areEqual` below — adding a new field
 * that affects rendering without updating the comparator will cause stale UI.
 */
export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  type: 'text' | 'thinking' | 'tool';
  content: string;
  timestamp: Date;
  toolName?: string;
  toolInput?: Record<string, unknown>;
  toolOutput?: string;
  toolStatus?: 'running' | 'done' | 'error';
  toolDurationMs?: number;
  images?: string[];
  errorType?: string;
  retryable?: boolean;
}

export interface MessageItemProps {
  message: ChatMessage;
  /** True when this message is currently receiving streaming tokens. */
  isStreaming: boolean;
  /** i18n translator for chat namespace. */
  t: TFunction;
  /** Open image lightbox. */
  onImageClick: (src: string) => void;
  /** Retry an errored message — parent locates last user message and refills the composer. */
  onRetry: (errorMessageId: string) => void;
}

/**
 * Filter out leading [Context: ...] markers from displayed content.
 * Same logic as the original RightPanel.
 */
function filterContextMarker(content: string): string {
  return content.replace(/^\[Context:.*?\]\s*\n*/, '');
}

function MessageItemImpl({ message, isStreaming, t, onImageClick, onRetry }: MessageItemProps) {
  const isUser = message.role === 'user';

  // Tool message
  if (message.type === 'tool') {
    return (
      <div className={`flex flex-col ${isUser ? 'items-end' : 'items-start'}`}>
        <div
          className={`max-w-[85%] text-xs p-2 rounded border ${
            message.toolStatus === 'running'
              ? 'bg-accent/5 border-accent/30'
              : message.toolStatus === 'error'
                ? 'bg-red-500/5 border-red-500/20'
                : 'bg-surface border-border-subtle'
          }`}
        >
          <div className="flex items-center gap-1.5">
            {message.toolStatus === 'running' ? (
              <Loader2 className="w-3 h-3 animate-spin text-accent" />
            ) : message.toolStatus === 'error' ? (
              <AlertTriangle className="w-3 h-3 text-red-400" />
            ) : (
              <CheckCircle2 className="w-3 h-3 text-green-400" />
            )}
            <Wrench className="w-3 h-3 text-accent" />
            <span className="font-medium text-accent">{message.toolName}</span>
            {message.toolStatus === 'running' && (
              <span className="text-text-muted">running...</span>
            )}
            {message.toolStatus !== 'running' && message.toolDurationMs !== undefined && (
              <span className="text-text-muted ml-auto">
                {message.toolDurationMs < 1000
                  ? `${message.toolDurationMs}ms`
                  : `${(message.toolDurationMs / 1000).toFixed(1)}s`}
              </span>
            )}
          </div>
          {message.toolInput && (
            <pre className="mt-1 text-text-muted overflow-hidden text-ellipsis">
              {JSON.stringify(message.toolInput).slice(0, 100)}
            </pre>
          )}
        </div>
        <span className="text-[10px] text-text-muted mt-0.5 px-1">
          {message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
        </span>
      </div>
    );
  }

  // Text message (user or assistant)
  if (message.type === 'text') {
    const filtered = filterContextMarker(message.content);
    // Streaming → re-render every token; Frozen → look up LRU cache.
    const markdownEl = message.content
      ? isStreaming
        ? renderStreamingMarkdown(filtered)
        : getCachedMarkdown(message.id, filtered)
      : null;

    return (
      <div className={`flex flex-col ${isUser ? 'items-end' : 'items-start'}`}>
        <div
          className={`max-w-[85%] overflow-hidden rounded-lg px-3 py-2 ${
            isUser
              ? 'bg-accent text-white user-msg-bubble'
              : message.errorType
                ? 'bg-red-500/10 text-red-400 border border-red-500/20'
                : 'bg-elevated text-text-primary'
          }`}
        >
          {message.errorType && (
            <div className="flex items-center gap-1.5 mb-1">
              <AlertTriangle className="w-3.5 h-3.5 text-red-400" />
              <span className="text-xs font-medium text-red-400">
                {message.errorType === 'rate_limit'
                  ? t('rateLimitError')
                  : message.errorType === 'auth_error'
                    ? t('authError')
                    : message.errorType === 'server_error'
                      ? t('serverError')
                      : t('agentError')}
              </span>
            </div>
          )}
          {message.images && message.images.length > 0 && (
            <div className="flex gap-1 mt-1.5 flex-wrap">
              {message.images.map((src, i) => (
                <img
                  key={i}
                  src={src}
                  alt={`image ${i + 1}`}
                  className="w-4 h-4 object-cover rounded cursor-zoom-in hover:opacity-80 transition-opacity"
                  onClick={() => onImageClick(src)}
                />
              ))}
            </div>
          )}
          {markdownEl && (
            <div className="text-sm prose prose-invert prose-sm max-w-none break-words">
              {markdownEl}
            </div>
          )}
          {message.retryable && (
            <button
              onClick={() => onRetry(message.id)}
              className="mt-2 flex items-center gap-1.5 px-2.5 py-1 text-xs font-medium text-red-400 hover:text-red-300 hover:bg-red-500/10 rounded transition-colors"
            >
              <RefreshCw className="w-3 h-3" />
              重试
            </button>
          )}
        </div>
        <span className="text-[10px] text-text-muted mt-0.5 px-1">
          {message.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
        </span>
      </div>
    );
  }

  return null;
}

/**
 * Strict comparator. Any rendering-affecting field MUST be listed here.
 * If you add a new field to ChatMessage that affects rendering, update this function.
 */
function areEqual(prev: MessageItemProps, next: MessageItemProps): boolean {
  if (prev.isStreaming !== next.isStreaming) return false;
  if (prev.t !== next.t) return false;
  if (prev.onImageClick !== next.onImageClick) return false;
  if (prev.onRetry !== next.onRetry) return false;

  const a = prev.message;
  const b = next.message;
  if (a === b) return true;
  if (a.id !== b.id) return false;
  if (a.role !== b.role) return false;
  if (a.type !== b.type) return false;
  if (a.content !== b.content) return false;
  if (a.toolName !== b.toolName) return false;
  if (a.toolStatus !== b.toolStatus) return false;
  if (a.toolDurationMs !== b.toolDurationMs) return false;
  if (a.toolOutput !== b.toolOutput) return false;
  if (a.errorType !== b.errorType) return false;
  if (a.retryable !== b.retryable) return false;
  // toolInput/ images are generally write-once; compare by reference (set together with status).
  if (a.toolInput !== b.toolInput) return false;
  if (a.images !== b.images) return false;
  if (a.timestamp !== b.timestamp) return false;
  return true;
}

export const MessageItem = memo(MessageItemImpl, areEqual);