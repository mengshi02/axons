import { useState, useEffect } from 'react';
import {
  X, Plus, Trash2, Edit2, Save, AlertTriangle, Bot,
  Sparkles, Layers, ShieldCheck, GitBranch, Code2, Loader2, Check,
  Database, Terminal, TestTube2, Cloud, Cpu, Globe, FlaskConical,
  Microscope, Zap, Package, Puzzle, BookOpen, Network, Wand2,
  Fingerprint, Lock, Workflow, BarChart3,
} from 'lucide-react';
import {
  fetchAgents, fetchAgentTools, createAgent, updateAgent, deleteAgent,
  type AgentProfile, type AgentTool,
} from '../services/api';
import { useAppState } from '../hooks/useAppState';
import { useTranslation } from 'react-i18next';

const ICON_OPTIONS = [
  { value: 'sparkles', label: 'Sparkles', Icon: Sparkles },
  { value: 'layers', label: 'Layers', Icon: Layers },
  { value: 'shield-check', label: 'Shield', Icon: ShieldCheck },
  { value: 'git-branch', label: 'Git', Icon: GitBranch },
  { value: 'code-2', label: 'Code', Icon: Code2 },
  { value: 'bot', label: 'Bot', Icon: Bot },
  { value: 'database', label: 'Database', Icon: Database },
  { value: 'terminal', label: 'Terminal', Icon: Terminal },
  { value: 'test-tube-2', label: 'Test', Icon: TestTube2 },
  { value: 'cloud', label: 'Cloud', Icon: Cloud },
  { value: 'cpu', label: 'CPU', Icon: Cpu },
  { value: 'globe', label: 'Globe', Icon: Globe },
  { value: 'flask', label: 'Flask', Icon: FlaskConical },
  { value: 'microscope', label: 'Microscope', Icon: Microscope },
  { value: 'zap', label: 'Zap', Icon: Zap },
  { value: 'package', label: 'Package', Icon: Package },
  { value: 'puzzle', label: 'Puzzle', Icon: Puzzle },
  { value: 'book-open', label: 'Book', Icon: BookOpen },
  { value: 'network', label: 'Network', Icon: Network },
  { value: 'wand-2', label: 'Wand', Icon: Wand2 },
  { value: 'fingerprint', label: 'Identity', Icon: Fingerprint },
  { value: 'lock', label: 'Lock', Icon: Lock },
  { value: 'workflow', label: 'Workflow', Icon: Workflow },
  { value: 'bar-chart-3', label: 'Analytics', Icon: BarChart3 },
];

interface FormState {
  name: string;
  description: string;
  icon: string;
  system_prompt: string;
  tools: string[];
  allow_write: boolean;
}

const DEFAULT_FORM: FormState = {
  name: '',
  description: '',
  icon: 'bot',
  system_prompt: '',
  tools: [],
  allow_write: false,
};

interface Props {
  onClose: () => void;
}

export function AgentManagerPanel({ onClose }: Props) {
  const { loadAgents } = useAppState();
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [tools, setTools] = useState<AgentTool[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  // editing state
  const [editingId, setEditingId] = useState<string | null>(null); // null = create new
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<FormState>(DEFAULT_FORM);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    load();
  }, []);

  const load = async () => {
    setLoading(true);
    try {
      const [agentsRes, toolsRes] = await Promise.all([fetchAgents(), fetchAgentTools()]);
      setAgents(agentsRes.agents || []);
      setTools(toolsRes.tools || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  const openCreate = () => {
    setEditingId(null);
    setForm(DEFAULT_FORM);
    setShowForm(true);
    setError(null);
  };

  const openEdit = (agent: AgentProfile) => {
    setEditingId(agent.id);
    setForm({
      name: agent.name,
      description: agent.description,
      icon: agent.icon,
      system_prompt: agent.system_prompt,
      tools: agent.tools || [],
      allow_write: agent.allow_write,
    });
    setShowForm(true);
    setError(null);
  };

  const closeForm = () => {
    setShowForm(false);
    setEditingId(null);
    setForm(DEFAULT_FORM);
    setError(null);
  };

  const handleSave = async () => {
    if (!form.name.trim()) { setError('Agent name is required'); return; }
    setSaving(true);
    setError(null);
    try {
      if (editingId) {
        await updateAgent(editingId, form);
        setSuccessMsg('Updated successfully');
      } else {
        await createAgent(form);
        setSuccessMsg('Created successfully');
      }
      await load();
      await loadAgents();
      closeForm();
      setTimeout(() => setSuccessMsg(null), 2000);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteAgent(id);
      setConfirmDeleteId(null);
      await load();
      await loadAgents();
      setSuccessMsg('Deleted');
      setTimeout(() => setSuccessMsg(null), 2000);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  };

  const toggleTool = (toolName: string) => {
    setForm(prev => ({
      ...prev,
      tools: prev.tools.includes(toolName)
        ? prev.tools.filter(t => t !== toolName)
        : [...prev.tools, toolName],
    }));
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-4 border-b border-border-subtle">
        <div>
          <h2 className="text-base font-semibold text-text-primary">Agent Manager</h2>
          <p className="text-xs text-text-muted mt-0.5">Create and manage custom agent roles</p>
        </div>
        <button onClick={onClose} className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors">
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Success message */}
      {successMsg && (
        <div className="flex items-center gap-2 px-4 py-2 bg-green-500/10 border-b border-green-500/20 text-xs text-green-400">
          <Check className="w-3.5 h-3.5" />
          {successMsg}
        </div>
      )}

      {/* Body */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="w-5 h-5 text-accent animate-spin" />
        </div>
      ) : showForm ? (
        <AgentForm
          form={form}
          tools={tools}
          editingId={editingId}
          error={error}
          saving={saving}
          onFormChange={setForm}
          onToggleTool={toggleTool}
          onSave={handleSave}
          onCancel={closeForm}
        />
      ) : (
        <div className="flex-1 overflow-y-auto">
          {/* Built-in agents */}
          <div className="px-4 pt-4 pb-2">
            <p className="text-xs font-medium text-text-muted uppercase tracking-wider mb-2">Built-in</p>
            <div className="space-y-1">
              {agents.filter(a => a.is_builtin).map(agent => (
                <AgentRow key={agent.id} agent={agent} readonly onEdit={() => { }} onDelete={() => { }} />
              ))}
            </div>
          </div>

          {/* Custom agents */}
          <div className="px-4 pt-2 pb-4">
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs font-medium text-text-muted uppercase tracking-wider">Custom</p>
              <button
                onClick={openCreate}
                className="flex items-center gap-1 text-xs text-accent hover:text-accent/80 transition-colors"
              >
                <Plus className="w-3 h-3" />
                New
              </button>
            </div>
            {agents.filter(a => !a.is_builtin).length === 0 ? (
              <div className="text-center py-8 text-text-muted text-sm">
                <Bot className="w-8 h-8 mx-auto mb-2 opacity-40" />
                <p>No custom agents yet.</p>
                <button onClick={openCreate} className="mt-2 text-accent text-xs hover:underline">Create one</button>
              </div>
            ) : (
              <div className="space-y-1">
                {agents.filter(a => !a.is_builtin).map(agent => (
                  <AgentRow
                    key={agent.id}
                    agent={agent}
                    readonly={false}
                    confirmDeleteId={confirmDeleteId}
                    onEdit={() => openEdit(agent)}
                    onDelete={() => setConfirmDeleteId(agent.id)}
                    onConfirmDelete={() => handleDelete(agent.id)}
                    onCancelDelete={() => setConfirmDeleteId(null)}
                  />
                ))}
              </div>
            )}
          </div>

          {error && (
            <div className="mx-4 mb-4 flex items-center gap-2 px-3 py-2 bg-red-500/10 border border-red-500/20 rounded text-xs text-red-400">
              <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
              {error}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ---- Sub-components ----

interface AgentRowProps {
  agent: AgentProfile;
  readonly: boolean;
  confirmDeleteId?: string | null;
  onEdit: () => void;
  onDelete: () => void;
  onConfirmDelete?: () => void;
  onCancelDelete?: () => void;
}

function AgentRow({ agent, readonly, confirmDeleteId, onEdit, onDelete, onConfirmDelete, onCancelDelete }: AgentRowProps) {
  const isConfirming = confirmDeleteId === agent.id;
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-elevated hover:bg-hover transition-colors group">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-sm font-medium text-text-primary truncate">{agent.name}</span>
          {agent.is_builtin && <span className="text-xs px-1 py-0.5 bg-accent/10 text-accent rounded shrink-0">built-in</span>}
        </div>
        <p className="text-xs text-text-muted truncate">{agent.description}</p>
      </div>
      {!readonly && (
        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
          {isConfirming ? (
            <>
              <button onClick={onConfirmDelete} className="px-2 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600 transition-colors">Confirm</button>
              <button onClick={onCancelDelete} className="px-2 py-1 text-xs text-text-muted hover:text-text-primary transition-colors">Cancel</button>
            </>
          ) : (
            <>
              <button onClick={onEdit} className="p-1 text-text-muted hover:text-text-primary hover:bg-surface rounded transition-colors">
                <Edit2 className="w-3.5 h-3.5" />
              </button>
              <button onClick={onDelete} className="p-1 text-text-muted hover:text-red-400 hover:bg-surface rounded transition-colors">
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
}

interface AgentFormProps {
  form: FormState;
  tools: AgentTool[];
  editingId: string | null;
  error: string | null;
  saving: boolean;
  onFormChange: (f: FormState) => void;
  onToggleTool: (name: string) => void;
  onSave: () => void;
  onCancel: () => void;
}

function AgentForm({ form, tools, editingId, error, saving, onFormChange, onToggleTool, onSave, onCancel }: AgentFormProps) {
  const { t } = useTranslation('chat');
  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* Scrollable fields */}
      <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
        <h3 className="text-sm font-semibold text-text-primary">{editingId ? 'Edit Agent' : 'New Agent'}</h3>

        {/* Name */}
        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">Name *</label>
          <input
            type="text"
            value={form.name}
            onChange={e => onFormChange({ ...form, name: e.target.value })}
            placeholder="e.g. Security Expert"
            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20"
          />
        </div>

        {/* Description */}
        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">Description</label>
          <input
            type="text"
            value={form.description}
            onChange={e => onFormChange({ ...form, description: e.target.value })}
            placeholder={t('agent.summaryPlaceholder')}
            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20"
          />
        </div>

        {/* Icon */}
        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">Icon</label>
          <div className="flex gap-2 flex-wrap">
            {ICON_OPTIONS.map(({ value, Icon }) => (
              <button
                key={value}
                onClick={() => onFormChange({ ...form, icon: value })}
                className={`p-2 rounded-lg border transition-colors ${form.icon === value
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-border-subtle bg-elevated text-text-muted hover:text-text-primary hover:border-accent/50'
                  }`}
              >
                <Icon className="w-4 h-4" />
              </button>
            ))}
          </div>
        </div>

        {/* Tools */}
        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">
            Tool Access
            <span className="text-text-muted font-normal ml-1">(empty = all tools)</span>
          </label>
          <div className="max-h-36 overflow-y-auto space-y-1 pr-1">
            {tools.length === 0 ? (
              <p className="text-xs text-text-muted">No tools available (configure MCP first)</p>
            ) : (
              tools.map(tool => (
                <label key={tool.name} className="flex items-start gap-2 cursor-pointer group">
                  <input
                    type="checkbox"
                    checked={form.tools.includes(tool.name)}
                    onChange={() => onToggleTool(tool.name)}
                    className="mt-0.5 accent-accent"
                  />
                  <div className="min-w-0">
                    <span className="text-xs font-medium text-text-primary">{tool.name}</span>
                    {tool.description && (
                      <p className="text-xs text-text-muted truncate">{tool.description}</p>
                    )}
                  </div>
                </label>
              ))
            )}
          </div>
        </div>

        {/* Allow write */}
        <div>
          <label className="flex items-center gap-2.5 cursor-pointer">
            <input
              type="checkbox"
              checked={form.allow_write}
              onChange={e => onFormChange({ ...form, allow_write: e.target.checked })}
              className="accent-accent"
            />
            <div>
              <span className="text-sm text-text-primary flex items-center gap-1.5">
                Allow file writes / command execution
                <AlertTriangle className="w-3.5 h-3.5 text-yellow-400" />
              </span>
              <p className="text-xs text-text-muted">Enables write_file, run_command and other privileged tools</p>
            </div>
          </label>
        </div>

        {/* System Prompt */}
        <div>
          <label className="block text-xs font-medium text-text-secondary mb-1">System Prompt</label>
          <textarea
            value={form.system_prompt}
            onChange={e => onFormChange({ ...form, system_prompt: e.target.value })}
            placeholder={t('agent.descriptionPlaceholder')}
            rows={5}
            className="w-full px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted resize-none focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20"
          />
        </div>

        {/* Error */}
        {error && (
          <div className="flex items-center gap-2 px-3 py-2 bg-red-500/10 border border-red-500/20 rounded text-xs text-red-400">
            <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
            {error}
          </div>
        )}
      </div>

      {/* Buttons — fixed at bottom, outside scroll area */}
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border-subtle shrink-0">
        <button
          onClick={onCancel}
          className="px-3 py-1.5 text-sm text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
        >
          Cancel
        </button>
        <button
          onClick={onSave}
          disabled={saving}
          className="flex items-center gap-1.5 px-4 py-1.5 text-sm bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
          {editingId ? 'Save' : 'Create'}
        </button>
      </div>
    </div>
  );
}