import { memo } from 'react';
import type { ReactElement } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

/**
 * LRU cache for rendered ReactMarkdown trees.
 *
 * Why: ReactMarkdown re-parses + walks remark/rehype plugin chain on every render.
 * For long chats where messages are immutable once finished, we can memo the rendered
 * React element by (id, content) and reuse it across re-renders.
 *
 * Notes:
 * - Streaming messages must NOT be cached (content keeps changing). Caller decides.
 * - Cache stores React elements; React handles reconciliation safely on re-mount.
 * - LRU keeps memory bounded for very long sessions.
 */

const MAX_ENTRIES = 300;

interface Entry {
  contentKey: string; // content + extra signature
  element: ReactElement;
}

// id -> entry. Map preserves insertion order, used for LRU eviction.
const cache = new Map<string, Entry>();

function touch(id: string, entry: Entry) {
  cache.delete(id);
  cache.set(id, entry);
  if (cache.size > MAX_ENTRIES) {
    // Evict oldest
    const oldestKey = cache.keys().next().value;
    if (oldestKey !== undefined) cache.delete(oldestKey);
  }
}

/**
 * Plain ReactMarkdown wrapper, memoized at component level so identical props
 * skip the parse step. This complements the explicit cache below.
 */
const MarkdownInner = memo(function MarkdownInner({ content }: { content: string }) {
  return <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>;
});

/**
 * Get a memoized rendered Markdown element for a frozen (non-streaming) message.
 *
 * @param id      Stable message id.
 * @param content Final message content.
 * @param extra   Optional extra signature (e.g. theme/locale) to bust cache when
 *                rendering context changes. Pass empty string if not relevant.
 */
export function getCachedMarkdown(
  id: string,
  content: string,
  extra: string = '',
): ReactElement {
  const contentKey = `${content.length}:${extra}:${content}`;
  const cached = cache.get(id);
  if (cached && cached.contentKey === contentKey) {
    touch(id, cached);
    return cached.element;
  }
  const element = <MarkdownInner content={content} />;
  touch(id, { contentKey, element });
  return element;
}

/**
 * Render Markdown without caching (use for streaming messages whose content keeps changing).
 */
export function renderStreamingMarkdown(content: string): ReactElement {
  return <MarkdownInner content={content} />;
}

/**
 * Drop a single entry. Useful if a message is edited or deleted.
 */
export function invalidateMarkdown(id: string): void {
  cache.delete(id);
}

/**
 * Clear the entire cache. Call when global rendering context changes
 * (e.g. user switches markdown theme, plugin set changes).
 */
export function clearMarkdownCache(): void {
  cache.clear();
}