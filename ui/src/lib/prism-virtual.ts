/**
 * Prism.tokenize + virtual list utilities
 *
 * Pre-processes source code with Prism.js tokenize API, splits tokens by line,
 * and provides helpers for rendering tokenized lines in a virtual list.
 *
 * NOTE: prismjs uses a global-variable pattern (window.Prism) that can break
 * when Rolldown/Vite splits it across chunks or when WebKit's module evaluation
 * order differs from V8's.  We work around this by deferring ALL prismjs
 * imports to the first call of `ensurePrismReady()`, which runs at render-time
 * when the module graph is fully resolved.
 */

import type Prism from 'prismjs';

/** Type for react-syntax-highlighter theme styles (e.g. oneDark, oneLight) */
export type SyntaxTheme = Record<string, React.CSSProperties>;

/** A single styled token within a line */
export interface StyledToken {
  /** The text content of this token */
  children: string;
  /** Prism token type (e.g. "keyword", "string") - used to look up color from theme */
  type?: string;
}

/** A line of tokenized code: an array of styled tokens */
export type TokenLine = StyledToken[];

// Lazy-initialized Prism reference — only populated after ensurePrismReady()
let _Prism: typeof Prism | null = null;
let _initPromise: Promise<void> | null = null;

/**
 * Ensure Prism core + all language grammars are loaded.
 * Idempotent — safe to call multiple times; returns the same promise.
 */
export function ensurePrismReady(): Promise<void> {
  if (_Prism) return Promise.resolve();
  if (_initPromise) return _initPromise;

  _initPromise = import('prismjs').then(async (mod) => {
    // prismjs CJS/UMD export: default is the Prism object
    const P = (mod as any).default ?? mod;
    _Prism = P;

    // Explicitly set on global scope so that grammar side-effects
    // that reference the bare `Prism` identifier can resolve it.
    if (typeof globalThis !== 'undefined' && !(globalThis as any).Prism) {
      (globalThis as any).Prism = P;
    }

    // Load language grammars (Prism only bundles markup/css/clike/javascript by default)
    await import('prismjs/components/prism-typescript');
    await import('prismjs/components/prism-jsx');
    await import('prismjs/components/prism-tsx');
    await import('prismjs/components/prism-go');
    await import('prismjs/components/prism-python');
    await import('prismjs/components/prism-rust');
    await import('prismjs/components/prism-java');
    await import('prismjs/components/prism-c');
    await import('prismjs/components/prism-cpp');
    await import('prismjs/components/prism-csharp');
    await import('prismjs/components/prism-json');
    await import('prismjs/components/prism-yaml');
    await import('prismjs/components/prism-toml');
    await import('prismjs/components/prism-bash');
    await import('prismjs/components/prism-css');
    await import('prismjs/components/prism-scss');
    await import('prismjs/components/prism-less');
    await import('prismjs/components/prism-markup');
    await import('prismjs/components/prism-sql');
    await import('prismjs/components/prism-graphql');
    await import('prismjs/components/prism-docker');
    await import('prismjs/components/prism-makefile');
    await import('prismjs/components/prism-groovy');
    await import('prismjs/components/prism-ruby');
    await import('prismjs/components/prism-ini');
    await import('prismjs/components/prism-protobuf');
    await import('prismjs/components/prism-csv');
    await import('prismjs/components/prism-rest');
    await import('prismjs/components/prism-asciidoc');
    await import('prismjs/components/prism-handlebars');
    await import('prismjs/components/prism-ejs');
    await import('prismjs/components/prism-sass');
    await import('prismjs/components/prism-markdown');
  }).catch((err) => {
    console.error('[prism-virtual] Failed to initialize Prism:', err);
    _initPromise = null; // allow retry
  });

  return _initPromise;
}

/**
 * Build a lookup map from Prism theme style keys to their CSS properties.
 * Theme keys like "keyword", "string", "class-name" etc. map to { color, fontStyle }.
 * Also extracts the default text color from CSS selector keys like 'code[class*="language-"]'.
 */
export function buildStyleMap(
  theme: SyntaxTheme
): Record<string, { color?: string; fontStyle?: number }> {
  const map: Record<string, { color?: string; fontStyle?: number }> = {};
  for (const [key, value] of Object.entries(theme)) {
    if (key.includes('[') || key.includes(':')) {
      // Extract default text color from 'code[class*="language-"]' or 'pre[class*="language-"]'
      if (key.startsWith('code[') || key.startsWith('pre[')) {
        const color = (value as Record<string, unknown>).color as string | undefined;
        if (color && !map['default']) {
          map['default'] = { color };
        }
      }
      continue;
    }
    map[key] = {
      color: (value as Record<string, unknown>).color as string | undefined,
      fontStyle: (value as Record<string, unknown>).fontStyle as number | undefined,
    };
  }
  return map;
}

/** Internal flat token before line splitting */
interface FlatToken {
  type: string;
  text: string;
}

/**
 * Recursively flatten a Prism Token tree into { type, text } pairs.
 * Prism.tokenize returns a mixed array of strings and Token objects.
 */
function flattenTokens(
  tokens: (string | Prism.Token)[],
  parentType?: string
): FlatToken[] {
  const result: FlatToken[] = [];
  for (const token of tokens) {
    if (typeof token === 'string') {
      result.push({ type: parentType || 'plain', text: token });
    } else {
      const tokenType = token.type;
      const content = token.content;
      if (typeof content === 'string') {
        result.push({ type: tokenType, text: content });
      } else if (Array.isArray(content)) {
        result.push(...flattenTokens(content, tokenType));
      } else if (content instanceof _Prism!.Token) {
        result.push(...flattenTokens([content], tokenType));
      }
    }
  }
  return result;
}

/**
 * Tokenize source code with Prism and split results into per-line token arrays.
 * Pure computation - no DOM involved.
 * Automatically ensures Prism is ready before tokenizing.
 */
export async function tokenizeToLines(code: string, language: string): Promise<TokenLine[]> {
  await ensurePrismReady();

  const P = _Prism!;
  const grammar = P.languages[language];
  if (!grammar) {
    return code.split('\n').map((line) => [{ children: line }]);
  }

  const prismTokens = P.tokenize(code, grammar);
  const flatTokens = flattenTokens(prismTokens);

  const lines: TokenLine[] = [];
  let currentLine: TokenLine = [];

  for (const token of flatTokens) {
    if (token.text.includes('\n')) {
      const parts = token.text.split('\n');
      for (let i = 0; i < parts.length; i++) {
        if (i > 0) {
          lines.push(currentLine);
          currentLine = [];
        }
        if (parts[i].length > 0) {
          currentLine.push({
            children: parts[i],
            type: token.type === 'plain' ? undefined : token.type,
          });
        }
      }
    } else {
      currentLine.push({
        children: token.text,
        type: token.type === 'plain' ? undefined : token.type,
      });
    }
  }

  if (currentLine.length > 0) {
    lines.push(currentLine);
  }

  return lines;
}

/**
 * Resolve the CSS style for a token type using the style map.
 */
export function getTokenStyle(
  tokenType: string | undefined,
  styleMap: Record<string, { color?: string; fontStyle?: number }>
): React.CSSProperties {
  if (!tokenType) return {};
  const style = styleMap[tokenType];
  if (!style) return {};
  const result: React.CSSProperties = {};
  if (style.color) result.color = style.color;
  if (style.fontStyle === 1) result.fontStyle = 'italic';
  else if (style.fontStyle === 2) result.fontStyle = 'bold';
  else if (style.fontStyle === 3) {
    result.fontStyle = 'italic';
    result.fontStyle = 'bold';
  }
  return result;
}