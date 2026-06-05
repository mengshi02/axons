import React, { useState, useEffect } from 'react';
import { X, Settings, Loader2, Check, AlertCircle, Key, Database, Cpu, RefreshCw, Sun, Moon, Plus, Trash2, ChevronDown, ChevronUp, Globe } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import i18n, { switchLocale } from '../i18n';
import { useTheme } from '../hooks/useTheme';
import { Select, type SelectOption } from './Select';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { Modal } from './Modal';

interface EmbeddingSettings {
    embedding_enabled: string;
    embedding_provider: string;
    embedding_api_key: string;
    embedding_model: string;
    embedding_base_url: string;
    embedding_max_context_tokens: string;
}

interface LLMModel {
    id: string;
    name: string;
    provider: string;
    api_key: string;
    model: string;
    base_url: string;
    multimodal: boolean;
}

interface RerankSettings {
    rerank_enabled: string;
    rerank_provider: string;
    rerank_api_key: string;
    rerank_model: string;
    rerank_base_url: string;
}

interface RAGSettings {
    rag_chunk_size: string;
    rag_chunk_overlap: string;
    rag_top_k: string;
    rag_rerank_enabled: string;
}

interface EmbeddingStatus {
    configured: boolean;
    status: string;
    embedding_count: number;
    model: string;
    needs_reembedding: boolean;
}

const EMBEDDING_PROVIDERS: SelectOption[] = [
    { value: 'openai', label: 'OpenAI' },
    { value: 'jina', label: 'Jina AI' },
    { value: 'custom', label: 'Custom' },
];

const RERANK_PROVIDERS: SelectOption[] = [
    { value: 'cohere', label: 'Cohere' },
    { value: 'jina', label: 'Jina AI' },
    { value: 'custom', label: 'Custom' },
];

const LLM_PROVIDERS: SelectOption[] = [
    { value: 'openai', label: 'OpenAI' },
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'custom', label: 'Custom' },
];

const EMBEDDING_MODELS: Record<string, string[]> = {
    openai: ['text-embedding-3-small', 'text-embedding-3-large', 'text-embedding-ada-002'],
    jina: ['jina-embeddings-v2-base-en', 'jina-embeddings-v2-base-code'],
};

export const SettingsPanel = React.memo(function SettingsPanel({ onClose }: PanelComponentProps) {
    const { theme, setTheme } = useTheme();
    const { t } = useTranslation('settings');
    const [activeTab, setActiveTab] = useState<'theme' | 'embedding' | 'llm' | 'rerank' | 'rag' | 'language'>('theme');
    const [isSaving, setIsSaving] = useState(false);
    const [isLoading, setIsLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [success, setSuccess] = useState<string | null>(null);

    // Available locales from language plugins
    const [availableLocales, setAvailableLocales] = useState<Array<{ code: string; nativeName: string; englishName: string; pluginId: string; iconPath: string }>>([]);

    // Settings state
    const [embeddingSettings, setEmbeddingSettings] = useState<EmbeddingSettings>({
        embedding_enabled: 'false',
        embedding_provider: '',
        embedding_api_key: '',
        embedding_model: '',
        embedding_base_url: '',
        embedding_max_context_tokens: '0',
    });

    const [rerankSettings, setRerankSettings] = useState<RerankSettings>({
        rerank_enabled: 'false',
        rerank_provider: '',
        rerank_api_key: '',
        rerank_model: '',
        rerank_base_url: '',
    });

    const [ragSettings, setRAGSettings] = useState<RAGSettings>({
        rag_chunk_size: '1000',
        rag_chunk_overlap: '200',
        rag_top_k: '10',
        rag_rerank_enabled: 'false',
    });

    const [embeddingStatus, setEmbeddingStatus] = useState<EmbeddingStatus | null>(null);
    const [isTestingConnection, setIsTestingConnection] = useState(false);
    const [connectionResult, setConnectionResult] = useState<{ ok: boolean; message: string } | null>(null);
    const [detectedDimension, setDetectedDimension] = useState<number | null>(null);
    const [isTestingRerankConnection, setIsTestingRerankConnection] = useState(false);
    const [rerankConnectionResult, setRerankConnectionResult] = useState<{ ok: boolean; message: string } | null>(null);

    // LLM multi-model state
    const [llmEnabled, setLLMEnabled] = useState(false);
    const [llmModels, setLLMModels] = useState<LLMModel[]>([]);
    const [editingModel, setEditingModel] = useState<LLMModel | null>(null);
    const [isEditingNew, setIsEditingNew] = useState(false);
    const [expandedModelId, setExpandedModelId] = useState<string | null>(null);
    const [isSavingModel, setIsSavingModel] = useState(false);
    const [deleteModelId, setDeleteModelId] = useState<string | null>(null);
    const [testingModelId, setTestingModelId] = useState<string | null>(null);
    const [modelTestResults, setModelTestResults] = useState<Record<string, { ok: boolean; message: string }>>({});

    const saveLocaleToSettings = async (locale: string) => {
        try {
            await fetch('/v1/settings', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ category: 'locale', settings: { locale } }),
            });
        } catch {
            // Silently fail — i18n.changeLanguage already took effect locally
        }
    };

    // Load settings on mount
    useEffect(() => {
        loadSettings();
    }, []);

    // Fetch available locales from language plugins
    useEffect(() => {
        const fetchLocales = () => {
            fetch('/v1/plugins/locales')
                .then(r => r.ok ? r.json() : { locales: {} })
                .then(data => {
                    const locales = Object.entries(data.locales || {}).map(([code, info]: [string, any]) => ({
                        code,
                        nativeName: info.nativeName as string,
                        englishName: info.englishName as string,
                        pluginId: info.pluginId as string,
                        iconPath: (info.iconPath as string) || '',
                    }));
                    setAvailableLocales(locales);

                    // Sync locale→pluginId mapping so i18next loadPath can resolve
                    // plugin IDs. This is especially important when fetchLocales()
                    // is called from the locale-available event handler — the SSE
                    // onLocaleAvailable callback updates __localePluginMap, but
                    // there can be a timing gap. Ensuring the mapping is always
                    // up-to-date after a /v1/plugins/locales response prevents
                    // switchLocale from hitting a stale/empty mapping.
                    const map = (window as any).__localePluginMap as Record<string, string> || {};
                    for (const loc of locales) {
                        map[loc.code] = loc.pluginId;
                    }
                    (window as any).__localePluginMap = map;
                })
                .catch(() => { /* ignore */ });
        };
        fetchLocales();

        // Refresh locales when a locale plugin is installed/uninstalled
        const handleLocaleAvailable = () => fetchLocales();
        const handleLocaleUnavailable = () => {
            fetchLocales();
            // If current language was uninstalled, fallback is handled by i18next
        };
        window.addEventListener('locale-available', handleLocaleAvailable);
        window.addEventListener('locale-unavailable', handleLocaleUnavailable);
        return () => {
            window.removeEventListener('locale-available', handleLocaleAvailable);
            window.removeEventListener('locale-unavailable', handleLocaleUnavailable);
        };
    }, []);

    const loadSettings = async () => {
        setIsLoading(true);
        setError(null);
        try {
            // Load all settings
            const response = await fetch('/v1/settings');
            if (!response.ok) throw new Error('Failed to load settings');
            const data = await response.json();

            // Parse settings by category
            if (data.settings?.embedding) {
                const emb = data.settings.embedding;
                setEmbeddingSettings({
                    embedding_enabled: emb.embedding_enabled?.value || 'false',
                    embedding_provider: emb.embedding_provider?.value || '',
                    embedding_api_key: emb.embedding_api_key?.value || '',
                    embedding_model: emb.embedding_model?.value || '',
                    embedding_base_url: emb.embedding_base_url?.value || '',
                    embedding_max_context_tokens: emb.embedding_max_context_tokens?.value || '0',
                });
            }

            // Load LLM enabled flag
            if (data.settings?.llm) {
                setLLMEnabled(data.settings.llm.llm_enabled?.value === 'true');
            }

            if (data.settings?.rerank) {
                const rerank = data.settings.rerank;
                setRerankSettings({
                    rerank_enabled: rerank.rerank_enabled?.value || 'false',
                    rerank_provider: rerank.rerank_provider?.value || '',
                    rerank_api_key: rerank.rerank_api_key?.value || '',
                    rerank_model: rerank.rerank_model?.value || '',
                    rerank_base_url: rerank.rerank_base_url?.value || '',
                });
            }

            if (data.settings?.rag) {
                const rag = data.settings.rag;
                setRAGSettings({
                    rag_chunk_size: rag.rag_chunk_size?.value || '1000',
                    rag_chunk_overlap: rag.rag_chunk_overlap?.value || '200',
                    rag_top_k: rag.rag_top_k?.value || '10',
                    rag_rerank_enabled: rag.rag_rerank_enabled?.value || 'false',
                });
            }

            // Load embedding status
            await loadEmbeddingStatus();
            // Load LLM models
            await loadLLMModels();
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to load settings');
        } finally {
            setIsLoading(false);
        }
    };

    const loadEmbeddingStatus = async () => {
        try {
            const response = await fetch('/v1/settings/check');
            if (response.ok) {
                const data = await response.json();
                setEmbeddingStatus(data);
            }
        } catch {
            // Ignore status errors
        }
    };

    // ---- LLM multi-model management ----
    const loadLLMModels = async () => {
        try {
            const res = await fetch('/api/llm-models');
            if (res.ok) {
                const data = await res.json();
                setLLMModels(data.models || []);
            }
        } catch { /* ignore */ }
    };

    const handleAddModel = () => {
        setEditingModel({ id: '', name: '', provider: 'custom', api_key: '', model: '', base_url: '', multimodal: false });
        setIsEditingNew(true);
    };

    const handleEditModel = (m: LLMModel) => {
        setEditingModel({ ...m });
        setIsEditingNew(false);
        setExpandedModelId(m.id);
    };

    const handleCancelEdit = () => {
        setEditingModel(null);
        setIsEditingNew(false);
    };

    const handleSaveModel = async () => {
        if (!editingModel) return;
        setIsSavingModel(true);
        try {
            if (isEditingNew) {
                const res = await fetch('/api/llm-models', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(editingModel),
                });
                if (!res.ok) throw new Error('Failed to create model');
            } else {
                const res = await fetch(`/api/llm-models/${editingModel.id}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(editingModel),
                });
                if (!res.ok) throw new Error('Failed to update model');
            }
            await loadLLMModels();
            setEditingModel(null);
            setIsEditingNew(false);
            setSuccess('LLM model saved');
            setTimeout(() => setSuccess(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save model');
        } finally {
            setIsSavingModel(false);
        }
    };

    const handleDeleteModel = async (id: string) => {
        setDeleteModelId(null);
        try {
            await fetch(`/api/llm-models/${id}`, { method: 'DELETE' });
            await loadLLMModels();
        } catch { /* ignore */ }
    };

    const handleTestModelConnection = async (m: LLMModel) => {
        setTestingModelId(m.id);
        setModelTestResults(prev => { const r = { ...prev }; delete r[m.id]; return r; });
        try {
            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
            const res = await fetch('/v1/settings/test-connection', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                signal: controller.signal,
                body: JSON.stringify({ type: 'llm', base_url: m.base_url, api_key: m.api_key, model: m.model }),
            });
            clearTimeout(timer);
            const data = await res.json();
            setModelTestResults(prev => ({ ...prev, [m.id]: { ok: data.ok, message: data.message } }));
        } catch (err) {
            setModelTestResults(prev => ({ ...prev, [m.id]: { ok: false, message: err instanceof Error ? err.message : 'Failed' } }));
        } finally {
            setTestingModelId(null);
        }
    };

    const handleSaveEmbedding = async () => {
        setIsSaving(true);
        setError(null);
        setSuccess(null);

        try {
            // First save the embedding settings
            const response = await fetch('/v1/settings', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    category: 'embedding',
                    settings: embeddingSettings,
                }),
            });

            if (!response.ok) throw new Error('Failed to save settings');

            // If we have a detected dimension, save it too
            if (detectedDimension !== null) {
                await fetch('/v1/settings', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        category: 'embedding',
                        settings: {
                            embedding_dimension: String(detectedDimension),
                        },
                    }),
                });
            }

            // Reload status
            await loadEmbeddingStatus();
            setSuccess('Embedding settings saved successfully');
            setTimeout(() => setSuccess(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save settings');
        } finally {
            setIsSaving(false);
        }
    };

    const handleSaveRerank = async () => {
        setIsSaving(true);
        setError(null);
        setSuccess(null);

        try {
            const response = await fetch('/v1/settings', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    category: 'rerank',
                    settings: rerankSettings,
                }),
            });

            if (!response.ok) throw new Error('Failed to save settings');
            setSuccess('Rerank settings saved successfully');
            setTimeout(() => setSuccess(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save settings');
        } finally {
            setIsSaving(false);
        }
    };

    const handleSaveRAG = async () => {
        setIsSaving(true);
        setError(null);
        setSuccess(null);

        try {
            const response = await fetch('/v1/settings', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    category: 'rag',
                    settings: ragSettings,
                }),
            });

            if (!response.ok) throw new Error('Failed to save settings');
            setSuccess('RAG settings saved successfully');
            setTimeout(() => setSuccess(null), 3000);
        } catch (err) {
            setError(err instanceof Error ? err.message : 'Failed to save settings');
        } finally {
            setIsSaving(false);
        }
    };

    const handleTestConnection = async () => {
        setIsTestingConnection(true);
        setConnectionResult(null);
        try {
            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
            const response = await fetch('/v1/embed/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                signal: controller.signal,
                body: JSON.stringify({
                    provider: embeddingSettings.embedding_provider,
                    base_url: embeddingSettings.embedding_base_url,
                    api_key: embeddingSettings.embedding_api_key,
                    model: embeddingSettings.embedding_model,
                }),
            });
            clearTimeout(timer);
            const data = await response.json();
            if (data.success) {
                setConnectionResult({
                    ok: true,
                    message: `Connected! Model: ${data.model}, Dimension: ${data.dimension}`
                });
                // Store dimension for later use when saving
                setDetectedDimension(data.dimension);
            } else {
                setConnectionResult({ ok: false, message: data.error || 'Connection failed' });
            }
        } catch (err) {
            setConnectionResult({ ok: false, message: err instanceof Error ? err.message : 'Connection failed' });
        } finally {
            setIsTestingConnection(false);
        }
    };

    const handleTestRerankConnection = async () => {
        setIsTestingRerankConnection(true);
        setRerankConnectionResult(null);
        try {
            const controller = new AbortController();
            const timer = setTimeout(() => controller.abort(), 5 * 60 * 1000);
            const response = await fetch('/v1/settings/test-connection', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                signal: controller.signal,
                body: JSON.stringify({
                    type: 'rerank',
                    base_url: rerankSettings.rerank_base_url,
                    api_key: rerankSettings.rerank_api_key,
                    model: rerankSettings.rerank_model,
                }),
            });
            clearTimeout(timer);
            const data = await response.json();
            setRerankConnectionResult({ ok: data.ok, message: data.message });
        } catch (err) {
            setRerankConnectionResult({ ok: false, message: err instanceof Error ? err.message : 'Connection failed' });
        } finally {
            setIsTestingRerankConnection(false);
        }
    };

    // SettingsPanel is always rendered when open (App.tsx controls visibility via panelRegistry)
    return (
        <Modal isOpen={true} onClose={onClose} size="lg" overlayOpacity="none" backdropBlur={false} className="max-h-[90vh] flex flex-col">
                {/* Header */}
                <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
                    <div className="flex items-center gap-2">
                        <Settings className="w-4 h-4 text-accent" />
                        <span className="text-sm font-medium text-text-primary">{t('title')}</span>
                    </div>
                    <button
                        onClick={onClose}
                        className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
                    >
                        <X className="w-4 h-4" />
                    </button>
                </div>

                {/* Tabs */}
                <div className="flex border-b border-border-subtle">
                    <button
                        onClick={() => setActiveTab('theme')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'theme'
                            ? 'text-accent border-b-2 border-accent'
                            : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        {theme === 'sun' ? <Sun className="w-3.5 h-3.5" /> : <Moon className="w-3.5 h-3.5" />}
                        {t('tab.theme')}
                    </button>
                    <button
                        onClick={() => setActiveTab('embedding')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'embedding'
                                ? 'text-accent border-b-2 border-accent'
                                : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        <Database className="w-3.5 h-3.5" />
                        {t('tab.embedding')}
                    </button>
                    <button
                        onClick={() => setActiveTab('llm')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'llm'
                                ? 'text-accent border-b-2 border-accent'
                                : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        <Cpu className="w-3.5 h-3.5" />
                        {t('tab.llm')}
                    </button>
                    <button
                        onClick={() => setActiveTab('rerank')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'rerank'
                                ? 'text-accent border-b-2 border-accent'
                                : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        <RefreshCw className="w-3.5 h-3.5" />
                        {t('tab.rerank')}
                    </button>
                    <button
                        onClick={() => setActiveTab('rag')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'rag'
                                ? 'text-accent border-b-2 border-accent'
                                : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        <Key className="w-3.5 h-3.5" />
                        {t('tab.rag')}
                    </button>
                    <button
                        onClick={() => setActiveTab('language')}
                        className={`flex items-center gap-1.5 px-4 py-2.5 text-sm transition-colors ${activeTab === 'language'
                            ? 'text-accent border-b-2 border-accent'
                            : 'text-text-muted hover:text-text-primary'
                            }`}
                    >
                        <Globe className="w-3.5 h-3.5" />
                        {t('tab.language')}
                    </button>
                </div>

                {/* Content */}
                <div className="flex-1 overflow-y-auto p-4 space-y-4">
                    {isLoading ? (
                        <div className="flex items-center justify-center py-8">
                            <Loader2 className="w-6 h-6 animate-spin text-accent" />
                        </div>
                    ) : (
                        <>
                            {/* Error */}
                            {error && (
                                <div className="flex items-center gap-2 p-3 bg-red-500/10 border border-red-500/20 rounded text-sm text-red-400">
                                    <AlertCircle className="w-4 h-4 flex-shrink-0" />
                                    <span>{error}</span>
                                </div>
                            )}

                            {/* Success */}
                            {success && (
                                <div className="flex items-center gap-2 p-3 bg-green-500/10 border border-green-500/20 rounded text-sm text-green-400">
                                    <Check className="w-4 h-4 flex-shrink-0" />
                                    <span>{success}</span>
                                </div>
                            )}

                                {/* Theme Tab */}
                                {activeTab === 'theme' && (
                                    <div className="space-y-4">
                                        <p className="text-sm text-text-secondary">
                                            {t('theme.description')}
                                        </p>

                                        {/* Theme Options */}
                                        <div className="grid grid-cols-2 gap-3">
                                            {/* Moon Theme */}
                                            <button
                                                onClick={() => setTheme('moon')}
                                                className={`p-4 rounded-lg border-2 transition-all ${theme === 'moon'
                                                    ? 'border-accent bg-accent/10'
                                                    : 'border-border-subtle hover:border-border-default hover:bg-hover'
                                                    }`}
                                            >
                                                <div className="flex flex-col items-center gap-2">
                                                    <Moon className={`w-8 h-8 ${theme === 'moon' ? 'text-accent' : 'text-text-muted'}`} />
                                                    <span className={`text-sm font-medium ${theme === 'moon' ? 'text-accent' : 'text-text-primary'}`}>
                                                        {t('theme.moon.name')}
                                                    </span>
                                                    <span className="text-xs text-text-muted">
                                                        {t('theme.moon.desc')}
                                                    </span>
                                                </div>
                                            </button>

                                            {/* Sun Theme */}
                                            <button
                                                onClick={() => setTheme('sun')}
                                                className={`p-4 rounded-lg border-2 transition-all ${theme === 'sun'
                                                    ? 'border-accent bg-accent/10'
                                                    : 'border-border-subtle hover:border-border-default hover:bg-hover'
                                                    }`}
                                            >
                                                <div className="flex flex-col items-center gap-2">
                                                    <Sun className={`w-8 h-8 ${theme === 'sun' ? 'text-accent' : 'text-text-muted'}`} />
                                                    <span className={`text-sm font-medium ${theme === 'sun' ? 'text-accent' : 'text-text-primary'}`}>
                                                        {t('theme.sun.name')}
                                                    </span>
                                                    <span className="text-xs text-text-muted">
                                                        {t('theme.sun.desc')}
                                                    </span>
                                                </div>
                                            </button>
                                        </div>
                                    </div>
                                )}

                            {/* Embedding Tab */}
                            {activeTab === 'embedding' && (
                                <>
                                    {/* Status */}
                                    {embeddingStatus && (
                                        <div className="p-3 bg-elevated rounded-lg border border-border-subtle">
                                            <div className="flex items-center justify-between">
                                                <div className="flex items-center gap-2">
                                                    <div className={`w-2 h-2 rounded-full ${embeddingStatus.configured ? 'bg-green-500' : 'bg-yellow-500'}`} />
                                                    <span className="text-sm text-text-primary">
                                                            {embeddingStatus.configured ? t('embedding.connected', { model: embeddingStatus.model, dimension: String(embeddingStatus.embedding_count) }) : t('common:status.notConfigured')}
                                                    </span>
                                                </div>
                                                <div className="text-xs text-text-muted">
                                                        {t('common:unit.embeddings', { count: embeddingStatus.embedding_count })}
                                                </div>
                                            </div>
                                            {embeddingStatus.model && (
                                                <div className="text-xs text-text-muted mt-1">Model: {embeddingStatus.model}</div>
                                            )}
                                            {embeddingStatus.needs_reembedding && (
                                                <div className="text-xs text-yellow-400 mt-1">
                                                    Model changed - re-embedding recommended
                                                </div>
                                            )}
                                        </div>
                                    )}

                                    {/* Enable */}
                                    <div className="flex items-center gap-2">
                                        <input
                                            type="checkbox"
                                            id="embedding_enabled"
                                            checked={embeddingSettings.embedding_enabled === 'true'}
                                            onChange={e => setEmbeddingSettings(prev => ({
                                                ...prev,
                                                embedding_enabled: e.target.checked ? 'true' : 'false',
                                            }))}
                                            className="w-4 h-4 accent-accent"
                                        />
                                        <label htmlFor="embedding_enabled" className="text-sm text-text-primary">
                                                {t('embedding.enable')}
                                        </label>
                                    </div>

                                    {/* Provider */}
                                    <div>
                                        <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                {t('embedding.provider')}
                                        </label>
                                            <Select
                                            value={embeddingSettings.embedding_provider}
                                                onChange={(provider) => {
                                                const defaultModel = EMBEDDING_MODELS[provider]?.[0] || '';
                                                setEmbeddingSettings(prev => ({
                                                    ...prev,
                                                    embedding_provider: provider,
                                                    embedding_model: defaultModel,
                                                }));
                                            }}
                                                options={EMBEDDING_PROVIDERS}
                                                placeholder={t('embedding.selectProvider')}
                                            />
                                    </div>

                                    {/* API Key */}
                                        {embeddingSettings.embedding_provider && (
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                    {t('embedding.apiKey')}
                                            </label>
                                            <input
                                                type="password"
                                                value={embeddingSettings.embedding_api_key}
                                                onChange={e => setEmbeddingSettings(prev => ({
                                                    ...prev,
                                                    embedding_api_key: e.target.value,
                                                }))}
                                                    placeholder={t('embedding.enterApiKey')}
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                            />
                                        </div>
                                    )}

                                    {/* Model */}
                                    {embeddingSettings.embedding_provider && (
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                    {t('embedding.model')}
                                            </label>
                                            <input
                                                type="text"
                                                value={embeddingSettings.embedding_model}
                                                onChange={e => setEmbeddingSettings(prev => ({
                                                    ...prev,
                                                    embedding_model: e.target.value,
                                                }))}
                                                    placeholder={t('embedding.modelName')}
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                                list="embedding-model-list"
                                            />
                                            <datalist id="embedding-model-list">
                                                {EMBEDDING_MODELS[embeddingSettings.embedding_provider]?.map(m => (
                                                    <option key={m} value={m} />
                                                ))}
                                            </datalist>
                                        </div>
                                    )}

                                        {/* Base URL + Test Connection */}
                                        {embeddingSettings.embedding_provider === 'custom' && (
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                    {t('embedding.baseUrl')}
                                            </label>
                                            <input
                                                type="text"
                                                value={embeddingSettings.embedding_base_url}
                                                    onChange={e => {
                                                        setConnectionResult(null);
                                                        setEmbeddingSettings(prev => ({
                                                            ...prev,
                                                            embedding_base_url: e.target.value,
                                                        }));
                                                    }}
                                                    placeholder="https://your-custom-endpoint/v1"
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                            />
                                                <div className="flex items-center gap-2 mt-2">
                                                    <button
                                                        onClick={handleTestConnection}
                                                        disabled={isTestingConnection || !embeddingSettings.embedding_base_url}
                                                        className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-text-secondary border border-border-subtle rounded-lg hover:bg-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                                                    >
                                                        {isTestingConnection ? (
                                                            <>
                                                                <Loader2 className="w-3 h-3 animate-spin" />
                                                                {t('common:action.testing')}
                                                            </>
                                                        ) : (
                                                            <>
                                                                <RefreshCw className="w-3 h-3" />
                                                                    {t('embedding.testConnection')}
                                                            </>
                                                        )}
                                                    </button>
                                                    {connectionResult && (
                                                        <span className={`text-xs flex items-center gap-1 ${connectionResult.ok ? 'text-green-400' : 'text-red-400'}`}>
                                                            {connectionResult.ok
                                                                ? <Check className="w-3 h-3" />
                                                                : <AlertCircle className="w-3 h-3" />
                                                            }
                                                            {connectionResult.message}
                                                        </span>
                                                    )}
                                                </div>
                                        </div>
                                    )}

                                        {/* Max Context Tokens */}
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                {t('embedding.maxContextTokens')}
                                            </label>
                                            <input
                                                type="number"
                                                value={embeddingSettings.embedding_max_context_tokens}
                                                onChange={e => setEmbeddingSettings(prev => ({
                                                    ...prev,
                                                    embedding_max_context_tokens: e.target.value,
                                                }))}
                                                placeholder="0 = use default (512)"
                                                min="0"
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                            />
                                            <p className="mt-1 text-xs text-text-muted">
                                                {t('embedding.maxContextTokensHint')}
                                            </p>
                                        </div>

                                    {/* Save Button */}
                                        <div className="flex items-center justify-end pt-4">
                                        <button
                                            onClick={handleSaveEmbedding}
                                            disabled={isSaving}
                                            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                                        >
                                            {isSaving ? (
                                                <>
                                                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                                        {t('common:action.saving')}
                                                </>
                                            ) : (
                                                <>
                                                    <Check className="w-3.5 h-3.5" />
                                                            {t('common:action.save')}
                                                        </>
                                            )}
                                        </button>
                                    </div>
                                </>
                            )}

                                {/* LLM Tab - Multi-model list */}
                            {activeTab === 'llm' && (
                                <>
                                        {/* Enable LLM toggle */}
                                        <div className="flex items-center justify-between p-3 bg-elevated rounded-lg border border-border-subtle">
                                            <div>
                                                <p className="text-sm text-text-primary">{t('llm.enable')}</p>
                                                <p className="text-xs text-text-muted">{t('llm.enableDesc')}</p>
                                            </div>
                                            <input
                                                type="checkbox"
                                                checked={llmEnabled}
                                                onChange={async e => {
                                                    const val = e.target.checked;
                                                    setLLMEnabled(val);
                                                    await fetch('/v1/settings', {
                                                        method: 'PUT',
                                                        headers: { 'Content-Type': 'application/json' },
                                                        body: JSON.stringify({ category: 'llm', settings: { llm_enabled: val ? 'true' : 'false' } })
                                                    });
                                                }}
                                                className="w-4 h-4 accent-accent cursor-pointer"
                                            />
                                        </div>

                                        {/* Model list */}
                                        <div className="space-y-2">
                                            {llmModels.length === 0 && !isEditingNew && (
                                                <p className="text-xs text-text-muted py-4 text-center">{t('llm.noModels')}</p>
                                            )}
                                            {llmModels.map(m => (
                                                <div key={m.id} className="border border-border-subtle rounded-lg overflow-hidden">
                                                    {/* Row header */}
                                                    <div
                                                        className="flex items-center justify-between px-3 py-2 bg-elevated cursor-pointer hover:bg-hover transition-colors"
                                                        onClick={() => setExpandedModelId(expandedModelId === m.id ? null : m.id)}
                                                    >
                                                        <div className="flex items-center gap-2 min-w-0">
                                                            {expandedModelId === m.id ? <ChevronUp className="w-3.5 h-3.5 text-text-muted flex-shrink-0" /> : <ChevronDown className="w-3.5 h-3.5 text-text-muted flex-shrink-0" />}
                                                            <span className="text-sm text-text-primary truncate">{m.name || m.model || t('llm.unnamed')}</span>
                                                            <span className={`text-xs px-1.5 py-0.5 rounded flex-shrink-0 ${m.provider === 'anthropic' ? 'bg-orange-500/15 text-orange-400' : 'bg-blue-500/15 text-blue-400'}`}>
                                                                {m.provider === 'anthropic' ? 'Anthropic' : m.provider === 'openai' ? 'OpenAI' : 'Custom'}
                                                            </span>
                                                            {m.multimodal && <span className="text-xs px-1.5 py-0.5 bg-accent/15 text-accent rounded flex-shrink-0">{t('llm.multimodal')}</span>}
                                                        </div>
                                                        <div className="flex items-center gap-1 min-w-[88px] justify-end shrink-0 min-h-[28px]">
                                                            {modelTestResults[m.id] && (
                                                                <span className={`text-xs ${modelTestResults[m.id].ok ? 'text-green-400' : 'text-red-400'}`}>
                                                                    {modelTestResults[m.id].ok ? <Check className="w-3 h-3 inline" /> : <AlertCircle className="w-3 h-3 inline" />}
                                                                </span>
                                                            )}
                                                            {deleteModelId === m.id ? (
                                                                <>
                                                                    <button onClick={e => { e.stopPropagation(); handleDeleteModel(m.id); }} className="px-2 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600 transition-colors">{t('common:action.confirm')}</button>
                                                                    <button onClick={e => { e.stopPropagation(); setDeleteModelId(null); }} className="px-2 py-1 text-xs text-text-muted hover:text-text-primary transition-colors">{t('common:action.cancel')}</button>
                                                                </>
                                                            ) : (
                                                                <button onClick={e => { e.stopPropagation(); setDeleteModelId(m.id); }} className="p-1 text-text-muted hover:text-red-400 transition-colors">
                                                                    <Trash2 className="w-3.5 h-3.5" />
                                                                </button>
                                                            )}
                                                        </div>
                                                    </div>
                                                    {/* Expanded edit form */}
                                                    {expandedModelId === m.id && (
                                                        <div className="p-3 space-y-3 border-t border-border-subtle">
                                                            {editingModel?.id === m.id ? (
                                                                <LLMModelForm
                                                                    model={editingModel}
                                                                    onChange={setEditingModel}
                                                                    onSave={handleSaveModel}
                                                                    onCancel={handleCancelEdit}
                                                                    isSaving={isSavingModel}
                                                                    onTest={() => handleTestModelConnection(editingModel)}
                                                                    isTesting={testingModelId === m.id}
                                                                    testResult={modelTestResults[m.id]}
                                                                />
                                                            ) : (
                                                                <div className="flex justify-between items-center">
                                                                        <span className="text-xs text-text-muted">{m.model} · {m.base_url || t('llm.defaultEndpoint')}</span>
                                                                        <button onClick={() => handleEditModel(m)} className="text-xs text-accent hover:underline">{t('common:action.edit')}</button>
                                                                </div>
                                                            )}
                                                        </div>
                                                    )}
                                                </div>
                                            ))}
                                        </div>

                                        {/* Add new model form */}
                                        {isEditingNew && editingModel && (
                                            <div className="border border-accent/40 rounded-lg p-3 space-y-3">
                                                <p className="text-xs font-medium text-text-secondary">{t('llm.newModel')}</p>
                                                <LLMModelForm
                                                    model={editingModel}
                                                    onChange={setEditingModel}
                                                    onSave={handleSaveModel}
                                                    onCancel={handleCancelEdit}
                                                    isSaving={isSavingModel}
                                                    onTest={() => handleTestModelConnection(editingModel)}
                                                    isTesting={testingModelId === ''}
                                                    testResult={modelTestResults['']}
                                                />
                                        </div>
                                    )}

                                        {/* Add button */}
                                        {!isEditingNew && (
                                        <button
                                                onClick={handleAddModel}
                                                className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-accent border border-accent/40 rounded-lg hover:bg-accent/10 transition-colors"
                                        >
                                                <Plus className="w-3.5 h-3.5" />
                                                {t('llm.addModel')}
                                        </button>
                                        )}
                                </>
                            )}

                            {/* Rerank Tab */}
                            {activeTab === 'rerank' && (
                                <>
                                    {/* Enable */}
                                    <div className="flex items-center gap-2">
                                        <input
                                            type="checkbox"
                                            id="rerank_enabled"
                                            checked={rerankSettings.rerank_enabled === 'true'}
                                            onChange={e => setRerankSettings(prev => ({
                                                ...prev,
                                                rerank_enabled: e.target.checked ? 'true' : 'false',
                                            }))}
                                            className="w-4 h-4 accent-accent"
                                        />
                                        <label htmlFor="rerank_enabled" className="text-sm text-text-primary">
                                            Enable reranking for search results
                                        </label>
                                    </div>

                                    {/* Provider */}
                                    <div>
                                        <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                {t('embedding.provider')}
                                        </label>
                                            <Select
                                            value={rerankSettings.rerank_provider}
                                                onChange={(provider) => setRerankSettings(prev => ({
                                                ...prev,
                                                rerank_provider: provider,
                                            }))}
                                                options={RERANK_PROVIDERS}
                                                placeholder={t('embedding.selectProvider')}
                                            />
                                    </div>

                                    {/* API Key */}
                                    {rerankSettings.rerank_provider && (
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                    {t('embedding.apiKey')}
                                            </label>
                                            <input
                                                type="password"
                                                value={rerankSettings.rerank_api_key}
                                                onChange={e => setRerankSettings(prev => ({
                                                    ...prev,
                                                    rerank_api_key: e.target.value,
                                                }))}
                                                    placeholder={t('embedding.enterApiKey')}
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                            />
                                        </div>
                                    )}

                                    {/* Model */}
                                    {rerankSettings.rerank_provider && (
                                        <div>
                                            <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                Model (optional)
                                            </label>
                                            <input
                                                type="text"
                                                value={rerankSettings.rerank_model}
                                                onChange={e => setRerankSettings(prev => ({
                                                    ...prev,
                                                    rerank_model: e.target.value,
                                                }))}
                                                placeholder="Default model will be used if empty"
                                                className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                            />
                                        </div>
                                    )}

                                        {/* Base URL + Test Connection */}
                                        {rerankSettings.rerank_provider === 'custom' && (
                                            <div>
                                                <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                                    Base URL
                                                </label>
                                                <input
                                                    type="text"
                                                    value={rerankSettings.rerank_base_url}
                                                    onChange={e => {
                                                        setRerankConnectionResult(null);
                                                        setRerankSettings(prev => ({
                                                            ...prev,
                                                            rerank_base_url: e.target.value,
                                                        }));
                                                    }}
                                                    placeholder="https://your-custom-endpoint/v1"
                                                    className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent"
                                                />
                                                <div className="flex items-center gap-2 mt-2">
                                                    <button
                                                        onClick={handleTestRerankConnection}
                                                        disabled={isTestingRerankConnection || !rerankSettings.rerank_base_url}
                                                        className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-text-secondary border border-border-subtle rounded-lg hover:bg-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                                                    >
                                                        {isTestingRerankConnection ? (
                                                            <>
                                                                <Loader2 className="w-3 h-3 animate-spin" />
                                                                {t('common:action.testing')}
                                                            </>
                                                        ) : (
                                                            <>
                                                                <RefreshCw className="w-3 h-3" />
                                                                    {t('embedding.testConnection')}
                                                            </>
                                                        )}
                                                    </button>
                                                    {rerankConnectionResult && (
                                                        <span className={`text-xs flex items-center gap-1 ${rerankConnectionResult.ok ? 'text-green-400' : 'text-red-400'}`}>
                                                            {rerankConnectionResult.ok
                                                                ? <Check className="w-3 h-3" />
                                                                : <AlertCircle className="w-3 h-3" />
                                                            }
                                                            {rerankConnectionResult.message}
                                                        </span>
                                                    )}
                                                </div>
                                            </div>
                                        )}

                                    {/* Save Button */}
                                    <div className="flex justify-end pt-4">
                                        <button
                                            onClick={handleSaveRerank}
                                            disabled={isSaving}
                                            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                                        >
                                            {isSaving ? (
                                                <>
                                                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                                        {t('common:action.saving')}
                                                </>
                                            ) : (
                                                <>
                                                    <Check className="w-3.5 h-3.5" />
                                                            {t('common:action.save')}
                                                </>
                                            )}
                                        </button>
                                    </div>
                                </>
                            )}

                            {/* RAG Tab */}
                            {activeTab === 'rag' && (
                                <>
                                    {/* Chunk Size */}
                                    <div>
                                        <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                            Chunk Size
                                        </label>
                                        <input
                                            type="number"
                                            value={ragSettings.rag_chunk_size}
                                            onChange={e => setRAGSettings(prev => ({
                                                ...prev,
                                                rag_chunk_size: e.target.value,
                                            }))}
                                            min={100}
                                            max={8000}
                                            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary focus:outline-none focus:border-accent"
                                        />
                                        <p className="text-xs text-text-muted mt-1">Size of text chunks for RAG (default: 1000)</p>
                                    </div>

                                    {/* Chunk Overlap */}
                                    <div>
                                        <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                            Chunk Overlap
                                        </label>
                                        <input
                                            type="number"
                                            value={ragSettings.rag_chunk_overlap}
                                            onChange={e => setRAGSettings(prev => ({
                                                ...prev,
                                                rag_chunk_overlap: e.target.value,
                                            }))}
                                            min={0}
                                            max={2000}
                                            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary focus:outline-none focus:border-accent"
                                        />
                                        <p className="text-xs text-text-muted mt-1">Overlap between chunks (default: 200)</p>
                                    </div>

                                    {/* Top K */}
                                    <div>
                                        <label className="block text-xs font-medium text-text-secondary mb-1.5">
                                            Top K Results
                                        </label>
                                        <input
                                            type="number"
                                            value={ragSettings.rag_top_k}
                                            onChange={e => setRAGSettings(prev => ({
                                                ...prev,
                                                rag_top_k: e.target.value,
                                            }))}
                                            min={1}
                                            max={100}
                                            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary focus:outline-none focus:border-accent"
                                        />
                                        <p className="text-xs text-text-muted mt-1">Number of results to retrieve (default: 10)</p>
                                    </div>

                                    {/* Rerank Enabled */}
                                    <div className="flex items-center gap-2">
                                        <input
                                            type="checkbox"
                                            id="rag_rerank_enabled"
                                            checked={ragSettings.rag_rerank_enabled === 'true'}
                                            onChange={e => setRAGSettings(prev => ({
                                                ...prev,
                                                rag_rerank_enabled: e.target.checked ? 'true' : 'false',
                                            }))}
                                            className="w-4 h-4 accent-accent"
                                        />
                                        <label htmlFor="rag_rerank_enabled" className="text-sm text-text-primary">
                                            Enable reranking in RAG pipeline
                                        </label>
                                    </div>

                                    {/* Save Button */}
                                    <div className="flex justify-end pt-4">
                                        <button
                                            onClick={handleSaveRAG}
                                            disabled={isSaving}
                                            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-white text-sm rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                                        >
                                            {isSaving ? (
                                                <>
                                                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                                        {t('common:action.saving')}
                                                </>
                                            ) : (
                                                <>
                                                    <Check className="w-3.5 h-3.5" />
                                                            {t('common:action.save')}
                                                </>
                                            )}
                                        </button>
                                    </div>
                                </>
                            )}
                                {/* Language Tab */}
                                {activeTab === 'language' && (
                                    <div className="space-y-4">
                                        <p className="text-sm text-text-secondary">
                                            {t('language.description')}
                                        </p>
                                        <div className="grid grid-cols-2 gap-3">
                                            {/* English (default, always available) */}
                                            <button
                                                onClick={() => { switchLocale('en'); saveLocaleToSettings('en'); }}
                                                className={`p-4 rounded-lg border-2 transition-all ${i18n.language === 'en' || i18n.language?.startsWith('en')
                                                    ? 'border-accent bg-accent/10'
                                                    : 'border-border-subtle hover:border-border-default hover:bg-hover'
                                                    }`}
                                            >
                                                <div className="flex flex-col items-center gap-2">
                                                    <Globe className={`w-8 h-8 ${i18n.language === 'en' || i18n.language?.startsWith('en') ? 'text-accent' : 'text-text-muted'}`} />
                                                    <span className={`text-sm font-medium ${i18n.language === 'en' || i18n.language?.startsWith('en') ? 'text-accent' : 'text-text-primary'}`}>
                                                        English
                                                    </span>
                                                    <span className="text-xs text-text-muted">
                                                        English
                                                    </span>
                                                </div>
                                            </button>
                                            {/* Dynamic locale plugins */}
                                            {availableLocales.map(locale => (
                                                <button
                                                    key={locale.code}
                                                    onClick={() => { switchLocale(locale.code); saveLocaleToSettings(locale.code); }}
                                                    className={`p-4 rounded-lg border-2 transition-all ${i18n.language === locale.code
                                                        ? 'border-accent bg-accent/10'
                                                        : 'border-border-subtle hover:border-border-default hover:bg-hover'
                                                        }`}
                                                >
                                                    <div className="flex flex-col items-center gap-2">
                                                        {locale.iconPath ? (
                                                            <img
                                                                src={`/plugins/${locale.pluginId}/${locale.iconPath}`}
                                                                alt={locale.nativeName}
                                                                className={`w-8 h-8 ${i18n.language === locale.code ? 'opacity-100' : 'opacity-60'}`}
                                                            />
                                                        ) : (
                                                                <Globe className={`w-8 h-8 ${i18n.language === locale.code ? 'text-accent' : 'text-text-muted'}`} />
                                                        )}
                                                        <span className={`text-sm font-medium ${i18n.language === locale.code ? 'text-accent' : 'text-text-primary'}`}>
                                                            {locale.nativeName}
                                                        </span>
                                                        <span className="text-xs text-text-muted">
                                                            {locale.englishName}
                                                        </span>
                                                    </div>
                                                </button>
                                            ))}
                                        </div>
                                        {availableLocales.length === 0 && (!i18n.language || i18n.language === 'en' || i18n.language?.startsWith('en')) && (
                                            <p className="text-xs text-text-muted">
                                                {t('language.onlyDefault')}
                                            </p>
                                        )}
                                    </div>
                                )}
                        </>
                )}
            </div>

        </Modal>
    );
});

// ---- LLMModelForm sub-component ----
interface LLMModelFormProps {
    model: LLMModel;
    onChange: (m: LLMModel | null) => void;
    onSave: () => void;
    onCancel: () => void;
    isSaving: boolean;
    onTest: () => void;
    isTesting: boolean;
    testResult?: { ok: boolean; message: string };
}

function LLMModelForm({ model, onChange, onSave, onCancel, isSaving, onTest, isTesting, testResult }: LLMModelFormProps) {
    const { t } = useTranslation('settings');
    const set = (key: keyof LLMModel, value: string | boolean) =>
        onChange({ ...model, [key]: value });
    return (
        <div className="space-y-3">
            {/* Name */}
            <div>
                <label className="block text-xs font-medium text-text-secondary mb-1">Name</label>
                <input type="text" value={model.name} onChange={e => set('name', e.target.value)}
                    placeholder="e.g. My GPT-4o"
                    className="w-full px-3 py-1.5 bg-surface border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent" />
            </div>
            {/* Provider */}
            <div>
                <label className="block text-xs font-medium text-text-secondary mb-1">Provider</label>
                <Select
                    value={model.provider}
                    onChange={(v) => set('provider', v)}
                    options={LLM_PROVIDERS}
                    placeholder={t('embedding.selectProvider')}
                />
                {/* Protocol hint */}
                {model.provider === 'anthropic' ? (
                    <div className="mt-1.5 flex items-center gap-1.5">
                        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-xs rounded bg-orange-500/15 text-orange-400 border border-orange-500/20">
                            Anthropic Native API
                        </span>
                        <span className="text-xs text-text-muted">x-api-key · claude-* models</span>
                    </div>
                ) : (
                    <div className="mt-1.5 flex items-center gap-1.5">
                        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-xs rounded bg-blue-500/15 text-blue-400 border border-blue-500/20">
                            OpenAI Compatible
                        </span>
                        <span className="text-xs text-text-muted">
                            {model.provider === 'custom'
                                ? 'Custom base_url, OpenAI-compatible format'
                                : 'OpenAI / DeepSeek / Qwen and compatible APIs'}
                        </span>
                    </div>
                )}
            </div>
            {/* API Key */}
            <div>
                <label className="block text-xs font-medium text-text-secondary mb-1">API Key</label>
                <input type="password" value={model.api_key} onChange={e => set('api_key', e.target.value)}
                    placeholder="Enter API key"
                    className="w-full px-3 py-1.5 bg-surface border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent" />
            </div>
            {/* Model name */}
            <div>
                <label className="block text-xs font-medium text-text-secondary mb-1">Model</label>
                <input type="text" value={model.model} onChange={e => set('model', e.target.value)}
                    placeholder="e.g. gpt-4o, claude-3-opus"
                    className="w-full px-3 py-1.5 bg-surface border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent" />
            </div>
            {/* Base URL */}
            <div>
                <label className="block text-xs font-medium text-text-secondary mb-1">Base URL</label>
                <input type="text" value={model.base_url} onChange={e => set('base_url', e.target.value)}
                    placeholder={model.provider === 'anthropic'
                        ? 'https://api.anthropic.com  (leave empty for default)'
                        : 'https://api.openai.com/v1  (leave empty for default)'}
                    className="w-full px-3 py-1.5 bg-surface border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent" />
            </div>
            {/* Multimodal */}
            <div className="flex items-center gap-2">
                <input type="checkbox" id={`mm-${model.id}`} checked={model.multimodal}
                    onChange={e => set('multimodal', e.target.checked)}
                    className="w-4 h-4 accent-accent" />
                <label htmlFor={`mm-${model.id}`} className="text-sm text-text-primary">Multimodal (supports image input)</label>
            </div>
            {/* Test + actions */}
            <div className="flex items-center gap-2 pt-1">
                <button onClick={onTest} disabled={isTesting}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-text-secondary border border-border-subtle rounded-lg hover:bg-hover disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
                    {isTesting ? <><Loader2 className="w-3 h-3 animate-spin" />Testing...</> : <><RefreshCw className="w-3 h-3" />Test</>}
                </button>
                {testResult && (
                    <span className={`text-xs flex items-center gap-1 ${testResult.ok ? 'text-green-400' : 'text-red-400'}`}>
                        {testResult.ok ? <Check className="w-3 h-3" /> : <AlertCircle className="w-3 h-3" />}
                        {testResult.message}
                    </span>
                )}
                <div className="flex-1" />
                <button onClick={onCancel} className="px-2.5 py-1 text-xs text-text-muted border border-border-subtle rounded-lg hover:bg-hover transition-colors">{t('common:action.cancel')}</button>
                <button onClick={onSave} disabled={isSaving}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-xs bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50 transition-colors">
                    {isSaving ? <><Loader2 className="w-3 h-3 animate-spin" />{t('common:action.saving')}</> : <><Check className="w-3 h-3" />{t('common:action.save')}</>}
                </button>
            </div>
        </div>
    );
}