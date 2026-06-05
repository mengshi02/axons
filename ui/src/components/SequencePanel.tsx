import React, { useState } from 'react';
import { ArrowLeftRight, Loader2, AlertCircle, Copy, Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { useAppState } from '../hooks/useAppState';

interface SequenceEntry {
    id: number;
    name: string;
    kind: string;
    file: string;
    line: number;
}

interface SequenceMessage {
    from: string;
    to: string;
    label: string;
    kind: string;
    line?: number;
    file?: string;
    async?: boolean;
}

interface SequenceResponse {
    entry: SequenceEntry | null;
    mermaid: string;
    messages: SequenceMessage[];
    participants: string[];
    depth: number;
    total_messages: number;
}

export const SequencePanel = React.memo(function SequencePanel({ onClose: _onClose }: PanelComponentProps) {
    const { t } = useTranslation('panels');
    const { currentProject } = useAppState();
    const [name, setName] = useState('');
    const [depth, setDepth] = useState(3);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [result, setResult] = useState<SequenceResponse | null>(null);
    const [copied, setCopied] = useState(false);

    const handleGenerate = async () => {
        if (!name.trim()) return;
        setLoading(true);
        setError(null);
        setResult(null);

        try {
            const response = await fetch('/v1/sequence', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: name.trim(), depth, project_id: currentProject?.id }),
            });

            if (!response.ok) {
                const errData = await response.json().catch(() => ({ message: response.statusText }));
                throw new Error(errData.message || `HTTP ${response.status}`);
            }

            const data: SequenceResponse = await response.json();
            setResult(data);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Unknown error');
        } finally {
            setLoading(false);
        }
    };

    const handleCopy = async () => {
        if (!result?.mermaid) return;
        await navigator.clipboard.writeText(result.mermaid);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
    };

    return (
        <div className="flex flex-col h-full overflow-hidden">
            {/* Header */}
            <div className="flex items-center gap-2 px-4 py-3 border-b border-border-subtle">
                <ArrowLeftRight className="w-4 h-4 text-accent" />
                <span className="text-sm font-medium text-text-primary">Sequence Diagram</span>
            </div>

            {/* Controls */}
            <div className="p-4 border-b border-border-subtle space-y-3">
                <div>
                    <label className="block text-xs text-text-muted mb-1">Function / Entry Point</label>
                    <input
                        type="text"
                        value={name}
                        onChange={e => setName(e.target.value)}
                        onKeyDown={e => e.key === 'Enter' && handleGenerate()}
                        placeholder="e.g. handleBuild, main, processRequest"
                        className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all"
                    />
                </div>
                <div className="flex items-center gap-3">
                    <div className="flex-1">
                        <label className="block text-xs text-text-muted mb-1">Call Depth: {depth}</label>
                        <input
                            type="range"
                            min={1}
                            max={8}
                            value={depth}
                            onChange={e => setDepth(Number(e.target.value))}
                            className="w-full accent-accent"
                        />
                    </div>
                    <button
                        onClick={handleGenerate}
                        disabled={!name.trim() || loading}
                        className="px-4 py-2 bg-accent text-white text-sm rounded-lg disabled:opacity-50 disabled:cursor-not-allowed hover:bg-accent/90 transition-colors whitespace-nowrap"
                    >
                        {loading ? (
                            <span className="flex items-center gap-2">
                                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                Generating...
                            </span>
                        ) : t('sequence.generate')}
                    </button>
                </div>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-y-auto p-4 space-y-4 scrollbar-thin">
                {error && (
                    <div className="flex items-start gap-2 p-3 bg-red-500/10 border border-red-500/30 rounded-lg">
                        <AlertCircle className="w-4 h-4 text-red-400 mt-0.5 flex-shrink-0" />
                        <p className="text-sm text-red-400">{error}</p>
                    </div>
                )}

                {result && (
                    <>
                        {/* Stats */}
                        <div className="flex flex-wrap gap-2 text-xs">
                            {result.entry && (
                                <span className="px-2 py-1 bg-accent/10 text-accent rounded">
                                    Entry: {result.entry.name}
                                </span>
                            )}
                            <span className="px-2 py-1 bg-elevated text-text-muted rounded">
                                {result.total_messages} messages
                            </span>
                            <span className="px-2 py-1 bg-elevated text-text-muted rounded">
                                {result.participants?.length ?? 0} actors
                            </span>
                            <span className="px-2 py-1 bg-elevated text-text-muted rounded">
                                depth {result.depth}
                            </span>
                        </div>

                        {/* Mermaid Code */}
                        {result.mermaid && (
                            <div className="relative">
                                <div className="flex items-center justify-between mb-2">
                                    <span className="text-xs font-medium text-text-muted">Mermaid Diagram</span>
                                    <button
                                        onClick={handleCopy}
                                        className="flex items-center gap-1 px-2 py-1 text-xs text-text-muted hover:text-text-primary hover:bg-elevated rounded transition-colors"
                                    >
                                        {copied ? (
                                            <><Check className="w-3 h-3 text-green-400" /> Copied!</>
                                        ) : (
                                            <><Copy className="w-3 h-3" /> Copy</>
                                        )}
                                    </button>
                                </div>
                                <pre className="p-3 bg-elevated rounded-lg text-xs text-text-secondary font-mono overflow-x-auto scrollbar-thin border border-border-subtle whitespace-pre-wrap break-all">
                                    {result.mermaid}
                                </pre>
                            </div>
                        )}

                        {/* Message list */}
                        {result.messages && result.messages.length > 0 && (
                            <div>
                                <span className="text-xs font-medium text-text-muted mb-2 block">Call Messages</span>
                                <div className="space-y-1">
                                    {result.messages.map((msg, i) => (
                                        <div
                                            key={i}
                                            className="flex items-start gap-2 px-3 py-2 bg-elevated rounded text-xs hover:bg-hover transition-colors"
                                        >
                                            <span className="text-accent font-mono truncate max-w-[80px]">{msg.from}</span>
                                            <span className="text-text-muted">→</span>
                                            <span className="text-text-secondary font-mono truncate max-w-[80px]">{msg.to}</span>
                                            <span className="text-text-muted truncate flex-1">{msg.label}</span>
                                            {msg.async && (
                                                <span className="px-1 bg-yellow-500/20 text-yellow-400 rounded text-[10px]">async</span>
                                            )}
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}
                    </>
                )}

                {!result && !error && !loading && (
                    <div className="flex flex-col items-center justify-center h-32 text-center text-text-muted">
                        <ArrowLeftRight className="w-8 h-8 mb-2 opacity-40" />
                        <p className="text-sm">Enter a function name to generate a sequence diagram</p>
                        <p className="text-xs mt-1 opacity-60">Copy the Mermaid code and paste it into mermaid.live</p>
                    </div>
                )}
            </div>
        </div>
    );
});