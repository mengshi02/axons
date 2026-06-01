/**
 * PrismCodeEditor - Editable code editor using CodeMirror 6
 *
 * Used ONLY in edit mode. Read-only browsing stays with VirtualCodeView
 * to preserve all existing features (highlightRange, search, scrollToLine, etc).
 *
 * Features:
 * - Auto-close brackets/quotes
 * - Bracket matching + rainbow brackets
 * - Undo/redo, search/replace (with regex support)
 * - Auto-indent, comment toggle, Tab indent/dedent
 * - Language-aware autocomplete
 * - Vim mode (controlled externally via vimMode prop)
 */
import { useEffect, useRef, useMemo, useCallback } from 'react';
import CodeMirror, { type ReactCodeMirrorRef } from '@uiw/react-codemirror';
import { oneDark } from '@codemirror/theme-one-dark';
import { go } from '@codemirror/lang-go';
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { rust } from '@codemirror/lang-rust';
import { java } from '@codemirror/lang-java';
import { cpp } from '@codemirror/lang-cpp';
import { css } from '@codemirror/lang-css';
import { json } from '@codemirror/lang-json';
import { xml } from '@codemirror/lang-xml';
import { sql } from '@codemirror/lang-sql';
import { markdown } from '@codemirror/lang-markdown';
import { yaml } from '@codemirror/lang-yaml';
import { EditorView } from '@codemirror/view';
import { vim } from '@replit/codemirror-vim';

interface PrismCodeEditorProps {
  /** Source code content */
  code: string;
  /** Prism language identifier (e.g. "typescript", "go") */
  language: string;
  /** Theme name: 'moon' for dark, 'sun' for light */
  themeName: 'moon' | 'sun';
  /** Called when code is modified */
  onUpdate?: (value: string) => void;
  /** Additional CSS class */
  className?: string;
  /** 1-based line number to scroll to on mount */
  scrollToLine?: number | null;
  /** Enable Vim keybindings */
  vimMode?: boolean;
}

/** Map prism language ids to CodeMirror language extensions */
function getLanguageExtension(language: string) {
  switch (language) {
    case 'go':
      return go();
    case 'typescript':
    case 'tsx':
      return javascript({ typescript: true, jsx: language === 'tsx' });
    case 'javascript':
    case 'jsx':
      return javascript({ jsx: language === 'jsx' });
    case 'python':
      return python();
    case 'rust':
      return rust();
    case 'java':
      return java();
    case 'c':
    case 'cpp':
    case 'csharp':
      return cpp();
    case 'css':
    case 'scss':
    case 'less':
    case 'sass':
      return css();
    case 'json':
      return json();
    case 'markup':
    case 'xml':
    case 'html':
      return xml();
    case 'sql':
      return sql();
    case 'markdown':
      return markdown();
    case 'yaml':
      return yaml();
    default:
      return null;
  }
}

/** Light theme overrides for CodeMirror */
const lightTheme = EditorView.theme({
  '&': {
    backgroundColor: '#ffffff',
    color: '#24292f',
  },
  '.cm-gutters': {
    backgroundColor: '#f6f8fa',
    color: '#8c959f',
    borderRight: '1px solid #d0d7de',
  },
  '.cm-activeLineGutter': {
    backgroundColor: '#eaeef2',
  },
  '.cm-activeLine': {
    backgroundColor: '#f0f4f8',
  },
  '.cm-selectionBackground, ::selection': {
    backgroundColor: '#add6ff !important',
  },
  '.cm-cursor': {
    borderLeftColor: '#0969da',
  },
  '.cm-matchingBracket': {
    backgroundColor: '#add6ff66',
    outline: '1px solid #0969da',
  },
}, { dark: false });

/** Editable code editor component (edit mode only) */
export function PrismCodeEditor({
  code,
  language,
  themeName,
  onUpdate,
  className,
  scrollToLine,
  vimMode = false,
}: PrismCodeEditorProps) {
  const editorRef = useRef<ReactCodeMirrorRef>(null);
  const scrollToLineRef = useRef(scrollToLine);
  scrollToLineRef.current = scrollToLine;

  const langExtension = useMemo(() => getLanguageExtension(language), [language]);

  const extensions = useMemo(() => {
    const exts = [];
    if (vimMode) exts.push(vim());
    if (langExtension) exts.push(langExtension);
    if (themeName === 'sun') exts.push(lightTheme);
    return exts;
  }, [langExtension, themeName, vimMode]);

  /** 滚动到指定行（1-based） */
  const scrollViewToLine = useCallback((view: EditorView, line: number) => {
    if (line <= 0) return;
    const targetLine = view.state.doc.line(Math.min(line, view.state.doc.lines));
    view.dispatch({
      effects: EditorView.scrollIntoView(targetLine.from, { y: 'center' }),
    });
  }, []);

  // editor 就绪时立即滚动（处理初次挂载时 view 还未创建的时序问题）
  const handleCreateEditor = useCallback((view: EditorView) => {
    const line = scrollToLineRef.current;
    if (line && line > 0) {
      // 双 rAF 确保 DOM layout 完成后再滚动
      requestAnimationFrame(() => requestAnimationFrame(() => scrollViewToLine(view, line)));
    }
  }, [scrollViewToLine]);

  // scrollToLine prop 变化时（切换节点）也滚动
  useEffect(() => {
    if (!scrollToLine || scrollToLine <= 0) return;
    const view = editorRef.current?.view;
    if (!view) return;
    scrollViewToLine(view, scrollToLine);
  }, [scrollToLine, scrollViewToLine]);

  return (
    <div
      className={`prism-editor-wrapper ${className || ''}`}
      style={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
    >
      <CodeMirror
        ref={editorRef}
        value={code}
        theme={themeName === 'moon' ? oneDark : 'light'}
        extensions={extensions}
        onChange={onUpdate}
        onCreateEditor={handleCreateEditor}
        basicSetup={{
          lineNumbers: true,
          foldGutter: true,
          autocompletion: true,
          bracketMatching: true,
          closeBrackets: true,
          indentOnInput: true,
          highlightActiveLine: true,
          highlightActiveLineGutter: true,
          searchKeymap: true,
          tabSize: 2,
        }}
        style={{
          height: '100%',
          fontSize: '0.8125rem',
          fontFamily: 'monospace',
          overflow: 'auto',
        }}
      />
    </div>
  );
}