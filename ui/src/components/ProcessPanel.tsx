import React, { useState, useEffect } from 'react';
import { X, RefreshCw, GitBranch, Workflow } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { Modal } from './Modal';

interface ProcessRecord {
  id: string;
  label: string;
  process_type: string;
  step_count: number;
  entry_point_id: number;
  terminal_id: number;
  community_ids: number[];
  project_id?: number;
  created_at?: string;
}

interface ProcessStep {
  process_id: string;
  node_id: number;
  step: number;
  node_name: string;
  node_kind: string;
  node_file: string;
  node_line: number;
}

export const ProcessPanel = React.memo(function ProcessPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { currentProject } = useAppState();
  const [processes, setProcesses] = useState<ProcessRecord[]>([]);
  const [selected, setSelected] = useState<ProcessRecord | null>(null);
  const [steps, setSteps] = useState<ProcessStep[]>([]);
  const [loading, setLoading] = useState(false);
  const [detecting, setDetecting] = useState(false);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState('');

  const projectId = currentProject?.id;

  const fetchProcesses = async () => {
    if (!projectId) return;
    setLoading(true);
    setError('');
    try {
      const res = await fetch(`/v1/processes?project_id=${encodeURIComponent(projectId)}&limit=50`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setProcesses(data.processes || []);
      setTotal(data.count || 0);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const fetchProcess = async (id: string) => {
    if (!projectId) return;
    try {
      const res = await fetch(`/v1/processes/${encodeURIComponent(id)}?project_id=${encodeURIComponent(projectId)}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setSteps(data.steps || []);
    } catch (e: any) {
      setError(e.message);
    }
  };

  const triggerDetect = async () => {
    if (!projectId) return;
    setDetecting(true);
    try {
      await fetch(`/v1/processes/detect?project_id=${encodeURIComponent(projectId)}`, { method: 'POST' });
      // Poll after a short delay
      setTimeout(() => {
        fetchProcesses();
        setDetecting(false);
      }, 3000);
    } catch {
      setDetecting(false);
    }
  };

  useEffect(() => {
    fetchProcesses();
  }, []);

  const handleSelect = (proc: ProcessRecord) => {
    setSelected(proc);
    setSteps([]);
    fetchProcess(proc.id);
  };

  const typeColor = (t: string) =>
    t === 'intra_community' ? 'text-green-400' :
    t === 'cross_community' ? 'text-blue-400' : 'text-gray-400';

  return (
    <Modal isOpen={true} onClose={onClose} size="xl" overlayOpacity="none" backdropBlur={false} className="max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-border-subtle">
          <div className="flex items-center gap-2">
            <Workflow size={18} className="text-accent" />
            <h2 className="text-text-primary font-semibold text-sm">{t('flow.title')}</h2>
            <span className="text-xs text-text-muted ml-1">({total} {t('flow.processes', { count: total })})</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={triggerDetect}
              disabled={detecting}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-elevated hover:bg-hover border border-border-subtle text-accent text-xs font-medium transition-colors disabled:opacity-50"
            >
              <RefreshCw size={12} className={detecting ? 'animate-spin' : ''} />
              {detecting ? t('flow.detecting') : t('flow.redetect')}
            </button>
            <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-hover text-text-muted">
              <X size={16} />
            </button>
          </div>
        </div>

        <div className="flex flex-1 overflow-hidden">
          {/* Process list */}
          <div className="w-80 border-r border-border-subtle overflow-y-auto">
            {loading && (
              <div className="p-4 text-center text-text-muted text-sm">{t('flow.loading')}</div>
            )}
            {error && (
              <div className="p-4 text-red-400 text-xs">{error}</div>
            )}
            {!loading && processes.length === 0 && (
              <div className="p-6 text-center">
                <GitBranch size={24} className="text-text-muted mx-auto mb-2" />
                <p className="text-text-secondary text-sm">{t('flow.noProcesses')}</p>
                <p className="text-text-muted text-xs mt-1">{t('flow.buildOrRedetect')}</p>
              </div>
            )}
            {processes.map(proc => (
              <button
                key={proc.id}
                onClick={() => handleSelect(proc)}
                className={`w-full text-left px-4 py-3 border-b border-border-subtle hover:bg-hover transition-colors ${selected?.id === proc.id ? 'bg-elevated border-l-2 border-l-accent' : ''
                }`}
              >
                <div className="text-text-primary text-xs font-medium truncate">{proc.label}</div>
                <div className="flex items-center gap-2 mt-1">
                  <span className={`text-xs ${typeColor(proc.process_type)}`}>{proc.process_type}</span>
                  <span className="text-text-muted text-xs">·</span>
                  <span className="text-text-muted text-xs">{t('flow.stepCount', { count: proc.step_count })}</span>
                </div>
              </button>
            ))}
          </div>

          {/* Step detail */}
          <div className="flex-1 overflow-y-auto p-4">
            {!selected && (
              <div className="h-full flex items-center justify-center text-text-muted text-sm">
                {t('flow.selectToView')}
              </div>
            )}
            {selected && (
              <div>
                <h3 className="text-text-primary font-semibold text-sm mb-4">{selected.label}</h3>
                {steps.length === 0 && (
                  <div className="text-text-muted text-sm">{t('flow.loadingSteps')}</div>
                )}
                <div className="space-y-1">
                  {steps.map((step, idx) => (
                    <div key={step.step} className="flex items-start gap-3">
                      <div className="flex flex-col items-center">
                        <div className="w-6 h-6 rounded-full bg-accent/20 text-accent text-xs flex items-center justify-center font-mono font-bold flex-shrink-0">
                          {step.step}
                        </div>
                        {idx < steps.length - 1 && (
                          <div className="w-px h-4 bg-border-subtle mt-1" />
                        )}
                      </div>
                      <div className="pb-3 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-text-primary text-xs font-medium">{step.node_name}</span>
                          <span className="text-text-muted text-xs">{step.node_kind}</span>
                        </div>
                        <div className="text-text-muted text-xs truncate mt-0.5">
                          {step.node_file}:{step.node_line}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
    </Modal>
  );
});