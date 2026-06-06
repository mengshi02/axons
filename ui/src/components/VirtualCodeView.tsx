/**
 * VirtualCodeView - Virtualized code rendering component
 *
 * Uses Prism.tokenize for syntax highlighting and @tanstack/react-virtual
 * for virtual scrolling. Only renders DOM for lines within the viewport,
 * providing O(1) rendering performance regardless of file size.
 */
import { useState, useEffect, useRef, useCallback } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { tokenizeToLines, buildStyleMap, getTokenStyle, ensurePrismReady, type TokenLine, type SyntaxTheme } from '../lib/prism-virtual';

interface VirtualCodeViewProps {
    /** Full source code content */
    code: string;
    /** Prism language identifier (e.g. "typescript", "go") */
    language: string;
    /** Syntax theme (oneDark or oneLight) */
    theme: SyntaxTheme;
    /** Line range to highlight (1-based) */
    highlightRange: { startLine: number; endLine: number } | null;
    /** Set of line numbers matching search (1-based) */
    searchResultSet: Set<number>;
    /** Current search match line number (1-based) */
    currentSearchLine: number | null;
    /** Called when virtualizer is ready and we need to scroll to a line */
    scrollToLine?: number | null;
    /** Estimated line height in pixels (default 22) */
    estimatedLineHeight?: number;
}

/** Line number gutter width */
const LINE_NUMBER_WIDTH = '2.5em';
/** Line number right padding */
const LINE_NUMBER_PADDING = '1em';

export function VirtualCodeView({
    code,
    language,
    theme,
    highlightRange,
    searchResultSet,
    currentSearchLine,
    scrollToLine,
    estimatedLineHeight = 22,
}: VirtualCodeViewProps) {
    const scrollRef = useRef<HTMLDivElement>(null);

    // Pre-compute style map from theme (cached by theme reference)
    const styleMap = useRef(buildStyleMap(theme));
    useEffect(() => { styleMap.current = buildStyleMap(theme); }, [theme]);

    // Tokenize code into per-line token arrays — async because Prism loads lazily
    const [tokenLines, setTokenLines] = useState<TokenLine[]>([]);

    useEffect(() => {
        let cancelled = false;
        tokenizeToLines(code, language).then(lines => {
            if (!cancelled) setTokenLines(lines);
        });
        return () => { cancelled = true; };
    }, [code, language]);

    // Kick off Prism preloading on mount (parallel with first render)
    useEffect(() => { ensurePrismReady(); }, []);

    // Line number color from theme
    const lineNumberColor = (() => {
        const commentStyle = styleMap.current['comment'];
        return commentStyle?.color || '#6b7280';
    })();

    // Default text color from theme (for tokens without specific styling)
    const defaultTextColor = (() => {
        return styleMap.current['default']?.color || '#abb2bf';
    })();

    // Create virtualizer
    const virtualizer = useVirtualizer({
        count: tokenLines.length,
        getScrollElement: () => scrollRef.current,
        estimateSize: () => estimatedLineHeight,
        overscan: 20, // Render 20 lines above/below viewport for smooth scrolling
    });

    // Scroll to a specific line when scrollToLine changes
    useEffect(() => {
        if (scrollToLine != null && scrollToLine > 0 && scrollToLine <= tokenLines.length) {
            // Use requestAnimationFrame to ensure virtualizer has calculated sizes
            requestAnimationFrame(() => {
                virtualizer.scrollToIndex(scrollToLine - 1, { align: 'center' });
            });
        }
    }, [scrollToLine, tokenLines.length, virtualizer]);

    // Render a single token span
    const renderToken = useCallback(
        (token: TokenLine[number], idx: number) => {
            const style = getTokenStyle(token.type, styleMap.current);
            if (Object.keys(style).length === 0) {
                // No special styling - render as plain text span
                return <span key={idx}>{token.children}</span>;
            }
            return (
                <span key={idx} style={style}>
                    {token.children}
                </span>
            );
        },
        [styleMap]
    );

    // Show loading state while Prism initializes
    if (tokenLines.length === 0 && code.length > 0) {
        return (
            <div
                className="overflow-auto"
                style={{ fontSize: '0.8125rem', fontFamily: 'monospace', height: '100%', color: defaultTextColor }}
            >
                <pre style={{ padding: '0.75rem', whiteSpace: 'pre' }}>{code}</pre>
            </div>
        );
    }

    return (
        <div
            ref={scrollRef}
            className="overflow-auto"
            style={{ fontSize: '0.8125rem', fontFamily: 'monospace', height: '100%', color: defaultTextColor }}
        >
            <div
                style={{
                    height: `${virtualizer.getTotalSize()}px`,
                    width: '100%',
                    position: 'relative',
                }}
            >
                {virtualizer.getVirtualItems().map((virtualRow) => {
                    const lineNumber = virtualRow.index + 1; // 1-based
                    const lineTokens = tokenLines[virtualRow.index];

                    // Determine line decoration
                    const isHighlighted =
                        highlightRange !== null &&
                        lineNumber >= highlightRange.startLine &&
                        lineNumber <= highlightRange.endLine;
                    const isSearchMatch = searchResultSet.has(lineNumber);
                    const isCurrentSearchMatch = currentSearchLine === lineNumber;

                    // Line background color
                    let lineBackground = 'transparent';
                    let borderLeft = '3px solid transparent';
                    if (isCurrentSearchMatch) {
                        lineBackground = 'rgba(234, 179, 8, 0.3)';
                        borderLeft = '3px solid #eab308';
                    } else if (isSearchMatch) {
                        lineBackground = 'rgba(234, 179, 8, 0.15)';
                    } else if (isHighlighted) {
                        lineBackground = 'rgba(124, 58, 237, 0.2)';
                        borderLeft = '3px solid #7c3aed';
                    }

                    return (
                        <div
                            key={virtualRow.key}
                            data-index={virtualRow.index}
                            data-line-number={lineNumber}
                            ref={virtualizer.measureElement}
                            style={{
                                position: 'absolute',
                                top: 0,
                                left: 0,
                                width: '100%',
                                transform: `translateY(${virtualRow.start}px)`,
                                background: lineBackground,
                                borderLeft,
                                paddingLeft: '0.75rem',
                                display: 'flex',
                                alignItems: 'baseline',
                                minHeight: `${estimatedLineHeight}px`,
                            }}
                            className={
                                isHighlighted || isSearchMatch ? 'highlighted-line' : ''
                            }
                        >
                            {/* Line number gutter */}
                            <span
                                className="linenumber"
                                style={{
                                    minWidth: LINE_NUMBER_WIDTH,
                                    paddingRight: LINE_NUMBER_PADDING,
                                    color: lineNumberColor,
                                    background: 'transparent',
                                    textAlign: 'right',
                                    userSelect: 'none',
                                    display: 'inline-block',
                                    flexShrink: 0,
                                }}
                            >
                                {lineNumber}
                            </span>
                            {/* Token content */}
                            <code style={{ background: 'transparent', whiteSpace: 'pre', color: defaultTextColor }}>
                                {lineTokens.map(renderToken)}
                            </code>
                        </div>
                    );
                })}
            </div>
        </div>
    );
}