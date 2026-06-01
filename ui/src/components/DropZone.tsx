import { useState, useCallback } from 'react';
import { Upload, Loader2, Plus, Clock, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppState } from '../hooks/useAppState';
import { useRecentPaths } from '../hooks/useRecentPaths';
import { UnifiedImportDialog } from './UnifiedImportDialog';

interface DropZoneProps {
  onImport: (path: string, watchEnabled?: boolean) => void;
}

export function DropZone({ onImport }: DropZoneProps) {
  const { t } = useTranslation('dropzone');
  const { loading, isLoading } = useAppState();
  const { recentPaths, removeRecentPath } = useRecentPaths();
  const [isDragging, setIsDragging] = useState(false);
  const [isImportOpen, setIsImportOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
  }, []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);

    const files = e.dataTransfer.files;
    if (files.length > 0) {
      const file = files[0];
      // Use webkitRelativePath for directory drops
      const path = (file as any).webkitRelativePath?.split('/')[0] || file.name;
      setError(null);
      onImport(path);
    }
  }, [onImport]);

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files && files.length > 0) {
      const path = (files[0] as any).webkitRelativePath?.split('/')[0] || files[0].name;
      setError(null);
      onImport(path);
    }
  };

  const isCurrentlyLoading = loading || isLoading;

  return (
    <div className="h-screen w-full flex items-center justify-center bg-deep">
      <div className="max-w-xl w-full mx-auto p-8">
        {/* Logo and title */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-16 h-16 rounded-2xl bg-gradient-to-br from-accent to-node-interface shadow-glow mb-4">
            <img src="/favicon.svg" alt="Axons" className="w-10 h-10" />
          </div>
          <h1 className="text-3xl font-bold text-text-primary mb-2">{t('title')}</h1>
          <p className="text-text-secondary">
            {t('subtitle')}
          </p>
        </div>

        {/* Recent import paths */}
        {recentPaths.length > 0 && (
          <div className="mb-6">
            <h2 className="text-sm font-medium text-text-muted mb-3">{t('recentImports')}</h2>
            <div className="grid gap-2">
              {recentPaths.slice(0, 5).map((recent) => (
                <div
                  key={recent.path}
                  className="flex items-center gap-3 px-4 py-3 bg-surface border border-border-subtle rounded-lg hover:bg-hover hover:border-accent/50 transition-all group"
                >
                  <button
                    onClick={() => {
                      setError(null);
                      onImport(recent.path);
                    }}
                    className="flex items-center gap-3 flex-1 min-w-0 text-left"
                  >
                    <Clock className="w-5 h-5 text-accent shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium text-text-primary truncate">
                        {recent.projectName}
                      </div>
                      <div className="text-xs text-text-muted truncate">
                        {recent.path}
                      </div>
                    </div>
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      removeRecentPath(recent.path);
                    }}
                    className="opacity-0 group-hover:opacity-100 p-1 hover:bg-error/20 rounded transition-all"
                    title={t('removeFromHistory')}
                  >
                    <X className="w-4 h-4 text-text-muted hover:text-error" />
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Drop zone */}
        <div
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          className={`relative border-2 border-dashed rounded-xl p-8 text-center transition-all ${isDragging
            ? 'border-accent bg-accent/10'
            : 'border-border-subtle hover:border-accent/50'
            }`}
        >
          <input
            type="file"
            // @ts-ignore - webkitdirectory is non-standard but widely supported
            webkitdirectory=""
            onChange={handleFileSelect}
            className="absolute inset-0 w-full h-full opacity-0 cursor-pointer"
          />

          <div className="flex flex-col items-center gap-4">
            <div className={`w-12 h-12 rounded-full flex items-center justify-center ${isDragging ? 'bg-accent/20' : 'bg-elevated'
              }`}>
              <Upload className={`w-6 h-6 ${isDragging ? 'text-accent' : 'text-text-muted'}`} />
            </div>

            <div>
              <p className="text-sm font-medium text-text-primary mb-1">
                {t('dropFolder')}
              </p>
              <p className="text-xs text-text-muted">
                {t('supportedLanguages')}
              </p>
            </div>
          </div>
        </div>

        {/* Or divider */}
        <div className="flex items-center gap-4 my-6">
          <div className="flex-1 h-px bg-border-subtle" />
          <span className="text-xs text-text-muted">{t('or')}</span>
          <div className="flex-1 h-px bg-border-subtle" />
        </div>

        {/* Import button */}
        <div className="flex flex-col gap-3">
          <button
            onClick={() => setIsImportOpen(true)}
            disabled={isCurrentlyLoading}
            className="flex items-center justify-center gap-2 px-4 py-3 bg-surface border border-border-subtle rounded-lg font-medium text-sm text-text-primary hover:bg-hover hover:border-accent/50 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isCurrentlyLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                {t('importing')}
              </>
            ) : (
              <>
                <Plus className="w-4 h-4" />
                  {t('importFrom')}
              </>
            )}
          </button>

          <p className="text-xs text-text-muted text-center">
            {t('importHint')}
          </p>

          {error && (
            <p className="text-sm text-red-400 text-center">{error}</p>
          )}
        </div>

        {/* Features */}
        <div className="mt-8 grid grid-cols-3 gap-4">
          <div className="text-center p-4 rounded-lg bg-surface/50">
            <div className="text-2xl mb-2">📊</div>
            <h3 className="text-sm font-medium text-text-primary">{t('features.visualize.title')}</h3>
            <p className="text-xs text-text-muted mt-1">{t('features.visualize.desc')}</p>
          </div>
          <div className="text-center p-4 rounded-lg bg-surface/50">
            <div className="text-2xl mb-2">🔍</div>
            <h3 className="text-sm font-medium text-text-primary">{t('features.explore.title')}</h3>
            <p className="text-xs text-text-muted mt-1">{t('features.explore.desc')}</p>
          </div>
          <div className="text-center p-4 rounded-lg bg-surface/50">
            <div className="text-2xl mb-2">🤖</div>
            <h3 className="text-sm font-medium text-text-primary">{t('features.ai.title')}</h3>
            <p className="text-xs text-text-muted mt-1">{t('features.ai.desc')}</p>
          </div>
        </div>
      </div>

      {/* Unified import dialog */}
      <UnifiedImportDialog
        isOpen={isImportOpen}
        onClose={() => setIsImportOpen(false)}
        onImport={(path, watchEnabled) => {
          onImport(path, watchEnabled);
          setIsImportOpen(false);
        }}
        isImporting={isCurrentlyLoading}
      />
    </div>
  );
}