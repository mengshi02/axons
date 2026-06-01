import { useState } from 'react';
import { X, FolderOpen, Github, Globe, Loader2, ChevronDown, ChevronUp, GitBranch, FolderCog, PlusCircle, Code } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { cloneRepo, newProject, type CloneRequest } from '../services/api';
import { Select, type SelectOption } from './Select';
import { Modal } from './Modal';

interface ProjectInfo {
  id: string;
  name: string;
  root_path: string;
}

interface UnifiedImportDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onImport: (path: string, watchEnabled?: boolean) => void;
  onProjectCreated?: (project: ProjectInfo, watchEnabled?: boolean) => void;
  isImporting?: boolean;
}

type ImportMode = 'local' | 'remote';
type LocalAction = 'import' | 'create';

// Supported languages for new projects
const SUPPORTED_LANGUAGES: SelectOption[] = [
  { value: 'go', label: 'Go', icon: <i className="devicon-go-original-wordmark colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'javascript', label: 'JavaScript', icon: <i className="devicon-javascript-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'typescript', label: 'TypeScript', icon: <i className="devicon-typescript-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'tsx', label: 'TSX (React)', icon: <i className="devicon-react-original colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'python', label: 'Python', icon: <i className="devicon-python-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'rust', label: 'Rust', icon: <i className="devicon-rust-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'java', label: 'Java', icon: <i className="devicon-java-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'csharp', label: 'C#', icon: <i className="devicon-csharp-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'c', label: 'C', icon: <i className="devicon-c-plain colored" style={{ fontSize: '1.2em' }} /> },
  { value: 'cpp', label: 'C++', icon: <i className="devicon-cplusplus-plain colored" style={{ fontSize: '1.2em' }} /> },
];

export function UnifiedImportDialog({ isOpen, onClose, onImport, onProjectCreated, isImporting = false }: UnifiedImportDialogProps) {
  const { t } = useTranslation('dropzone');
  const [mode, setMode] = useState<ImportMode>('local');
  const [localAction, setLocalAction] = useState<LocalAction>('import');
  const [localPath, setLocalPath] = useState('');
  const [remoteURL, setRemoteURL] = useState('');
  const [branch, setBranch] = useState('main');
  const [cloneMode, setCloneMode] = useState<'managed' | 'custom'>('managed');
  const [workspace, setWorkspace] = useState('');
  const [watchEnabled, setWatchEnabled] = useState(true);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isCloning, setIsCloning] = useState(false);
  const [isCreating, setIsCreating] = useState(false);

  // New project fields
  const [projectName, setProjectName] = useState('');
  const [projectLocation, setProjectLocation] = useState('');
  const [projectLanguage, setProjectLanguage] = useState('go');
  const [initGit, setInitGit] = useState(true);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (mode === 'local') {
      if (localAction === 'import') {
      // Import existing project
        if (!localPath.trim()) {
          setError(t('importDialog.errorLocalPath'));
          return;
        }
        if (!localPath.trim().startsWith('/') && !localPath.trim().match(/^[A-Za-z]:\\/)) {
          setError(t('importDialog.errorAbsolutePath'));
          return;
        }
        onImport(localPath.trim(), watchEnabled);
      } else {
        // Create new project
        if (!projectName.trim()) {
          setError(t('importDialog.errorProjectName'));
          return;
        }
        if (!projectLocation.trim()) {
          setError(t('importDialog.errorLocation'));
          return;
        }
        if (!projectLocation.trim().startsWith('/') && !projectLocation.trim().match(/^[A-Za-z]:\\/)) {
          setError(t('importDialog.errorAbsoluteLocation'));
          return;
        }

        setIsCreating(true);
        try {
          const result = await newProject({
            name: projectName.trim(),
            location: projectLocation.trim(),
            language: projectLanguage,
            init_git: initGit,
          });

          // After successful creation, use onProjectCreated callback
          // The project is already created in backend, no need to call createProject again
          if (onProjectCreated) {
            onProjectCreated(result, watchEnabled);
          } else {
            // Fallback: use onImport for backward compatibility
            onImport(result.root_path, watchEnabled);
          }

          // Reset form
          setProjectName('');
          setProjectLocation('');
          setProjectLanguage('go');
          setInitGit(true);
          setWatchEnabled(false);
        } catch (err) {
          setError(err instanceof Error ? err.message : t('importDialog.errorCreateFailed'));
        } finally {
          setIsCreating(false);
        }
      }
    } else {
      // Remote mode
      if (!remoteURL.trim()) {
        setError(t('importDialog.errorRemoteUrl'));
        return;
      }

      setIsCloning(true);
      try {
        const request: CloneRequest = {
          remote_url: remoteURL.trim(),
          branch: branch || 'main',
          clone_mode: cloneMode,
          workspace: cloneMode === 'custom' ? workspace : undefined,
          watch_enabled: watchEnabled,
        };

        const result = await cloneRepo(request);
        
        // After successful clone, trigger import with the local path
        onImport(result.local_path, watchEnabled);
        
        // Reset form
        setRemoteURL('');
        setBranch('main');
        setCloneMode('managed');
        setWorkspace('');
        setWatchEnabled(false);
        setShowAdvanced(false);
      } catch (err) {
        setError(err instanceof Error ? err.message : t('importDialog.errorCloneFailed'));
      } finally {
        setIsCloning(false);
      }
    }
  };

  const handleClose = () => {
    setError(null);
    setLocalPath('');
    setRemoteURL('');
    setBranch('main');
    setCloneMode('managed');
    setWorkspace('');
    setWatchEnabled(false);
    setShowAdvanced(false);
    setLocalAction('import');
    setProjectName('');
    setProjectLocation('');
    setProjectLanguage('go');
    setInitGit(true);
    onClose();
  };

  // Reset local action when switching modes
  const handleModeChange = (newMode: ImportMode) => {
    setMode(newMode);
    setError(null);
  };

  const isLoading = isImporting || isCloning || isCreating;

  // Validation for submit button
  const canSubmit = mode === 'local'
    ? (localAction === 'import' ? localPath.trim() : projectName.trim() && projectLocation.trim())
    : remoteURL.trim();

  return (
    <Modal isOpen={isOpen} onClose={handleClose} size="md" overlayOpacity="none" backdropBlur={false}>
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border-subtle">
          <h2 className="text-lg font-semibold text-text-primary">{t('importDialog.title')}</h2>
          <button
            onClick={handleClose}
            disabled={isLoading}
            className="p-1 hover:bg-hover rounded-lg transition-colors disabled:opacity-50"
          >
            <X className="w-5 h-5 text-text-muted" />
          </button>
        </div>

        {/* Mode selector */}
        <div className="px-6 pt-4">
          <div className="flex gap-2 p-1 bg-elevated rounded-lg">
            <button
              type="button"
              onClick={() => handleModeChange('local')}
              disabled={isLoading}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-all ${
                mode === 'local'
                  ? 'bg-accent text-white'
                  : 'text-text-secondary hover:text-text-primary hover:bg-surface'
              }`}
            >
              <FolderOpen className="w-4 h-4" />
              {t('importDialog.local')}
            </button>
            <button
              type="button"
              onClick={() => handleModeChange('remote')}
              disabled={isLoading}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-all ${
                mode === 'remote'
                  ? 'bg-accent text-white'
                  : 'text-text-secondary hover:text-text-primary hover:bg-surface'
              }`}
            >
              <Globe className="w-4 h-4" />
              {t('importDialog.remoteUrl')}
            </button>
          </div>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          {mode === 'local' ? (
            <div className="space-y-4">
              {/* Local action selector */}
              <div className="flex gap-4">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="localAction"
                    checked={localAction === 'import'}
                    onChange={() => setLocalAction('import')}
                    disabled={isLoading}
                    className="w-4 h-4 text-accent bg-deep border-border-subtle focus:ring-accent"
                  />
                  <span className="text-sm text-text-primary">{t('importDialog.importExisting')}</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="localAction"
                    checked={localAction === 'create'}
                    onChange={() => setLocalAction('create')}
                    disabled={isLoading}
                    className="w-4 h-4 text-accent bg-deep border-border-subtle focus:ring-accent"
                  />
                  <span className="text-sm text-text-primary">{t('importDialog.createNew')}</span>
                </label>
              </div>

              {localAction === 'import' ? (
                /* Import existing project */
                <div>
                  <label className="block text-sm font-medium text-text-secondary mb-2">
                    {t('importDialog.projectPath')}
                  </label>
                  <div className="relative">
                    <FolderOpen className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
                    <input
                      type="text"
                      value={localPath}
                      onChange={(e) => setLocalPath(e.target.value)}
                      placeholder="/Users/you/myproject"
                      disabled={isLoading}
                      className="w-full pl-10 pr-4 py-2.5 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                    />
                  </div>
                  <p className="text-xs text-text-muted mt-1">
                    {t('importDialog.projectPathHint')}
                  </p>
                </div>
              ) : (
                /* Create new project form */
                <div className="space-y-3">
                  {/* Project Name */}
                  <div>
                    <label className="block text-sm font-medium text-text-secondary mb-2">
                        {t('importDialog.projectName')}
                    </label>
                    <input
                      type="text"
                      value={projectName}
                      onChange={(e) => setProjectName(e.target.value)}
                      placeholder="my-new-project"
                      disabled={isLoading}
                      className="w-full px-3 py-2.5 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                    />
                  </div>

                  {/* Location */}
                  <div>
                    <label className="block text-sm font-medium text-text-secondary mb-2">
                        {t('importDialog.location')}
                    </label>
                    <div className="relative">
                      <FolderOpen className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
                      <input
                        type="text"
                        value={projectLocation}
                        onChange={(e) => setProjectLocation(e.target.value)}
                        placeholder="/Users/you/workspace"
                        disabled={isLoading}
                        className="w-full pl-10 pr-4 py-2.5 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                      />
                    </div>
                    <p className="text-xs text-text-muted mt-1">
                        {t('importDialog.locationHint', { location: projectLocation || '/path', name: projectName || 'project' })}
                    </p>
                  </div>

                  {/* Language */}
                  <div>
                    <label className="block text-sm font-medium text-text-secondary mb-2">
                      <Code className="w-3 h-3 inline mr-1" />
                        {t('importDialog.language')}
                    </label>
                    <Select
                      value={projectLanguage}
                      onChange={setProjectLanguage}
                      options={SUPPORTED_LANGUAGES}
                      disabled={isLoading}
                    />
                  </div>

                  {/* Git init */}
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={initGit}
                      onChange={(e) => setInitGit(e.target.checked)}
                      disabled={isLoading}
                      className="w-4 h-4 text-accent bg-deep border-border-subtle rounded focus:ring-accent"
                    />
                    <span className="text-sm text-text-secondary">
                        {t('importDialog.initGit')}
                    </span>
                  </label>
                </div>
              )}
            </div>
          ) : (
            /* Remote URL input */
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-text-secondary mb-2">
                    {t('importDialog.repoUrl')}
                </label>
                <div className="relative">
                  <Github className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
                  <input
                    type="text"
                    value={remoteURL}
                    onChange={(e) => setRemoteURL(e.target.value)}
                    placeholder="https://github.com/owner/repo"
                    disabled={isLoading}
                    className="w-full pl-10 pr-4 py-2.5 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                  />
                </div>
                <p className="text-xs text-text-muted mt-1">
                    {t('importDialog.repoUrlHint')}
                </p>
              </div>

                {/* {t('importDialog.advancedOptions')} toggle */}
              <button
                type="button"
                onClick={() => setShowAdvanced(!showAdvanced)}
                disabled={isLoading}
                className="flex items-center gap-2 text-sm text-text-secondary hover:text-text-primary transition-colors disabled:opacity-50"
              >
                {showAdvanced ? (
                  <ChevronUp className="w-4 h-4" />
                ) : (
                  <ChevronDown className="w-4 h-4" />
                )}
                  {t('importDialog.advancedOptions')}
              </button>

                {/* {t('importDialog.advancedOptions')} */}
              {showAdvanced && (
                <div className="space-y-4 p-4 bg-elevated rounded-lg border border-border-subtle">
                  {/* Branch */}
                  <div>
                    <label className="block text-sm font-medium text-text-secondary mb-2">
                      <GitBranch className="w-3 h-3 inline mr-1" />
                        {t('importDialog.branch')}
                    </label>
                    <input
                      type="text"
                      value={branch}
                      onChange={(e) => setBranch(e.target.value)}
                      placeholder="main"
                      disabled={isLoading}
                      className="w-full px-3 py-2 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                    />
                  </div>

                  {/* Clone mode */}
                  <div>
                    <label className="block text-sm font-medium text-text-secondary mb-2">
                      <FolderCog className="w-3 h-3 inline mr-1" />
                        {t('importDialog.cloneLocation')}
                    </label>
                    <div className="flex gap-3">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          name="cloneMode"
                          value="managed"
                          checked={cloneMode === 'managed'}
                          onChange={() => setCloneMode('managed')}
                          disabled={isLoading}
                          className="w-4 h-4 text-accent bg-deep border-border-subtle focus:ring-accent"
                        />
                          <span className="text-sm text-text-primary">{t('importDialog.managed')}</span>
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          name="cloneMode"
                          value="custom"
                          checked={cloneMode === 'custom'}
                          onChange={() => setCloneMode('custom')}
                          disabled={isLoading}
                          className="w-4 h-4 text-accent bg-deep border-border-subtle focus:ring-accent"
                        />
                          <span className="text-sm text-text-primary">{t('importDialog.custom')}</span>
                      </label>
                    </div>
                    <p className="text-xs text-text-muted mt-1">
                      {cloneMode === 'managed'
                          ? t('importDialog.managedHint')
                          : t('importDialog.customHint')}
                    </p>
                  </div>

                  {/* Custom workspace */}
                  {cloneMode === 'custom' && (
                    <div>
                      <label className="block text-sm font-medium text-text-secondary mb-2">
                          {t('importDialog.workspacePath')}
                      </label>
                      <input
                        type="text"
                        value={workspace}
                        onChange={(e) => setWorkspace(e.target.value)}
                        placeholder="/Users/you/workspace"
                        disabled={isLoading}
                        className="w-full px-3 py-2 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50"
                      />
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Watch option (common) */}
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={watchEnabled}
              onChange={(e) => setWatchEnabled(e.target.checked)}
              disabled={isLoading}
              className="w-4 h-4 text-accent bg-deep border-border-subtle rounded focus:ring-accent"
            />
            <span className="text-sm text-text-secondary">
              {t('importDialog.autoWatch')}
            </span>
          </label>

          {/* Error message */}
          {error && (
            <div className="px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-lg">
              <p className="text-sm text-red-400">{error}</p>
            </div>
          )}

          {/* Submit button */}
          <div className="flex gap-3 pt-2">
            <button
              type="button"
              onClick={handleClose}
              disabled={isLoading}
              className="flex-1 px-4 py-2.5 bg-elevated text-text-secondary rounded-lg font-medium text-sm hover:bg-hover transition-colors disabled:opacity-50"
            >
              {t('common:action.cancel')}
            </button>
            <button
              type="submit"
              disabled={isLoading || !canSubmit}
              className="flex-1 px-4 py-2.5 bg-accent text-white rounded-lg font-medium text-sm disabled:opacity-50 disabled:cursor-not-allowed hover:bg-accent/90 transition-colors flex items-center justify-center gap-2"
            >
              {isLoading ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  {isCreating ? t('importDialog.creating') : isCloning ? t('importDialog.cloning') : t('importDialog.importing')}
                </>
              ) : (
                <>
                  {mode === 'remote' ? (
                    <>
                      <Github className="w-4 h-4" />
                        {t('importDialog.cloneAndImport')}
                    </>
                    ) : localAction === 'create' ? (
                      <>
                        <PlusCircle className="w-4 h-4" />
                          {t('importDialog.create')}
                      </>
                  ) : (
                    <>
                      <FolderOpen className="w-4 h-4" />
                            {t('importDialog.import')}
                    </>
                  )}
                </>
              )}
            </button>
          </div>
        </form>
    </Modal>
  );
}