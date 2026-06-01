/**
 * Prism.tokenize + virtual list utilities
 *
 * Pre-processes source code with Prism.js tokenize API, splits tokens by line,
 * and provides helpers for rendering tokenized lines in a virtual list.
 */
import Prism from 'prismjs';
// Load language grammars (Prism only bundles markup/css/clike/javascript by default)
import 'prismjs/components/prism-typescript';
import 'prismjs/components/prism-jsx';
import 'prismjs/components/prism-tsx';
import 'prismjs/components/prism-go';
import 'prismjs/components/prism-python';
import 'prismjs/components/prism-rust';
import 'prismjs/components/prism-java';
import 'prismjs/components/prism-c';
import 'prismjs/components/prism-cpp';
import 'prismjs/components/prism-csharp';
import 'prismjs/components/prism-json';
import 'prismjs/components/prism-yaml';
import 'prismjs/components/prism-toml';
import 'prismjs/components/prism-bash';
import 'prismjs/components/prism-css'; // re-ensure
import 'prismjs/components/prism-scss';
import 'prismjs/components/prism-less';
import 'prismjs/components/prism-markup'; // re-ensure (html/xml)
import 'prismjs/components/prism-sql';
import 'prismjs/components/prism-graphql';
import 'prismjs/components/prism-docker';
import 'prismjs/components/prism-makefile';
import 'prismjs/components/prism-groovy';
import 'prismjs/components/prism-ruby';
import 'prismjs/components/prism-ini';
import 'prismjs/components/prism-protobuf';
import 'prismjs/components/prism-csv';
import 'prismjs/components/prism-rest';
import 'prismjs/components/prism-asciidoc';
import 'prismjs/components/prism-handlebars';
import 'prismjs/components/prism-ejs';
import 'prismjs/components/prism-sass';
import 'prismjs/components/prism-markdown';
// Note: prismjs does not have 'gitignore' or 'properties' components - falls back to plain text

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
      } else if (content instanceof Prism.Token) {
        result.push(...flattenTokens([content], tokenType));
      }
    }
  }
  return result;
}

/**
 * Tokenize source code with Prism and split results into per-line token arrays.
 * Pure computation - no DOM involved.
 */
export function tokenizeToLines(code: string, language: string): TokenLine[] {
  const grammar = Prism.languages[language];
  if (!grammar) {
    return code.split('\n').map((line) => [{ children: line }]);
  }

  const prismTokens = Prism.tokenize(code, grammar);
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
  else if (style.fontStyle === 2) result.fontWeight = 'bold';
  else if (style.fontStyle === 3) {
    result.fontStyle = 'italic';
    result.fontWeight = 'bold';
  }
  return result;
}