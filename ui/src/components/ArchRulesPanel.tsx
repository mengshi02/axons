import React, { useState, useEffect, useCallback } from 'react';
import { PencilRuler, CheckCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { Select, type SelectOption } from './Select';
import { ConfirmDialog } from './ConfirmDialog';
import { Modal } from './Modal';

interface ArchRule {
  id: number;
  name: string;
  kind: 'deny' | 'allow';
  from_pattern: string;
  to_pattern: string;
  description?: string;
  enabled: boolean;
  project_id?: number;
  created_at?: string;
}

interface Violation {
  rule_id: number;
  rule_name: string;
  kind: string;
  from_pattern: string;
  to_pattern: string;
  source_file: string;
  target_file: string;
  source_name: string;
  target_name: string;
  edge_kind: string;
}

type TabType = 'rules' | 'violations';

const RULE_KIND_OPTIONS: SelectOption[] = [
  { value: 'deny', label: 'Deny' },
  { value: 'allow', label: 'Allow' },
];

export function ArchRulesPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('panels');
  const { currentProject } = useAppState();

  const [tab, setTab] = useState<TabType>('rules');
  const [rules, setRules] = useState<ArchRule[]>([]);
  const [violations, setViolations] = useState<Violation[]>([]);
  const [loading, setLoading] = useState(false);
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // New rule form state
  const [showForm, setShowForm] = useState(false);
  const [formName, setFormName] = useState('');
  const [formKind, setFormKind] = useState<'deny' | 'allow'>('deny');
  const [formFrom, setFormFrom] = useState('');
  const [formTo, setFormTo] = useState('');
  const [formDesc, setFormDesc] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [deleteRuleId, setDeleteRuleId] = useState<number | null>(null);

  const projectQuery = currentProject ? `?project_id=${currentProject.id}` : '';

  const loadRules = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/v1/arch/rules${projectQuery}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setRules(data.rules || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load rules');
    } finally {
      setLoading(false);
    }
  }, [projectQuery]);

  useEffect(() => {
    loadRules();
  }, [loadRules]);

  const handleCreateRule = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formName || !formFrom || !formTo) return;
    setSubmitting(true);
    try {
      const body: Record<string, unknown> = {
        name: formName,
        kind: formKind,
        from_pattern: formFrom,
        to_pattern: formTo,
        description: formDesc,
      };
      if (currentProject) body.project_id = currentProject.id;
      const res = await fetch('/v1/arch/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setFormName(''); setFormFrom(''); setFormTo(''); setFormDesc('');
      setShowForm(false);
      loadRules();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create rule');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeleteRule = async (id: number) => {
    try {
      await fetch(`/v1/arch/rules/${id}`, { method: 'DELETE' });
      setDeleteRuleId(null);
      loadRules();
    } catch {
      setError('Failed to delete rule');
    }
  };

  const handleValidate = async () => {
    setValidating(true);
    setViolations([]);
    setError(null);
    try {
      const res = await fetch(`/v1/arch/validate${projectQuery}`, { method: 'POST' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setViolations(data.violations || []);
      setTab('violations');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Validation failed');
    } finally {
      setValidating(false);
    }
  };

  // ArchRulesPanel is always rendered when open (App.tsx controls visibility via panelRegistry)
  return (
    <Modal isOpen={true} onClose={onClose} size="xl" overlayOpacity="none" backdropBlur={false} className="max-h-[85vh] flex flex-col">

        {/* Header */}
              <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--color-border-subtle)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <h2 style={{ margin: 0, color: 'var(--color-text-primary)', fontSize: 16, fontWeight: 600 }}>{t('rules.title')}</h2>
                      <p style={{ margin: '2px 0 0', color: 'var(--color-text-secondary)', fontSize: 12 }}>
              Define dependency rules to enforce architectural boundaries
            </p>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button onClick={handleValidate} disabled={validating || rules.length === 0} style={{
              padding: '6px 14px', borderRadius: 6, border: 'none', cursor: 'pointer',
                          background: validating ? 'var(--color-hover)' : 'var(--color-accent)', color: '#fff', fontSize: 13,
            }}>
              {validating ? 'Validating...' : 'Validate'}
            </button>
            <button onClick={() => setShowForm(f => !f)} style={{
                          padding: '6px 14px', borderRadius: 6, border: '1px solid var(--color-border-default)', cursor: 'pointer',
                          background: 'var(--color-elevated)', color: 'var(--color-text-primary)', fontSize: 13,
            }}>
              + Add Rule
            </button>
            <button onClick={onClose} style={{
              background: 'transparent', border: 'none', cursor: 'pointer',
                          color: 'var(--color-text-muted)', fontSize: 20, lineHeight: 1, padding: '2px 6px',
            }}>×</button>
          </div>
        </div>

        {/* Tabs */}
              <div style={{ display: 'flex', borderBottom: '1px solid var(--color-border-subtle)', padding: '0 20px' }}>
          {(['rules', 'violations'] as TabType[]).map(t => (
            <button key={t} onClick={() => setTab(t)} style={{
              background: 'transparent', border: 'none', cursor: 'pointer',
              padding: '10px 16px', fontSize: 13,
                  color: tab === t ? 'var(--color-accent)' : 'var(--color-text-secondary)',
                  borderBottom: tab === t ? '2px solid var(--color-accent)' : '2px solid transparent',
              marginBottom: -1,
            }}>
              {t === 'rules' ? `Rules (${rules.length})` : `Violations (${violations.length})`}
            </button>
          ))}
        </div>

        {/* Add Rule Form */}
        {showForm && (
          <form onSubmit={handleCreateRule} style={{
                      padding: '12px 20px', borderBottom: '1px solid var(--color-border-subtle)',
                      background: 'var(--color-elevated)',
            display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 12px',
          }}>
            <input placeholder={t('rules.ruleName')} value={formName} onChange={e => setFormName(e.target.value)}
              required style={inputStyle} />
            <Select
              value={formKind}
              onChange={(value) => setFormKind(value as 'deny' | 'allow')}
              options={RULE_KIND_OPTIONS}
            />
            <input placeholder="From pattern * (e.g. controller)" value={formFrom} onChange={e => setFormFrom(e.target.value)}
              required style={inputStyle} />
            <input placeholder="To pattern * (e.g. database)" value={formTo} onChange={e => setFormTo(e.target.value)}
              required style={inputStyle} />
            <input placeholder="Description (optional)" value={formDesc} onChange={e => setFormDesc(e.target.value)}
              style={{ ...inputStyle, gridColumn: '1 / -1' }} />
            <div style={{ gridColumn: '1 / -1', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                          <button type="button" onClick={() => setShowForm(false)} style={{ ...btnStyle, background: 'var(--color-hover)', color: 'var(--color-text-primary)' }}>Cancel</button>
                          <button type="submit" disabled={submitting} style={{ ...btnStyle, background: 'var(--color-accent)' }}>
                {submitting ? 'Saving...' : 'Save Rule'}
              </button>
            </div>
          </form>
        )}

        {error && (
          <div style={{ padding: '8px 20px', background: 'rgba(239,68,68,0.1)', color: '#f87171', fontSize: 13 }}>
            {error}
          </div>
        )}

        {/* Content */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '12px 20px' }}>
          {loading ? (
                      <div style={{ textAlign: 'center', color: 'var(--color-text-secondary)', paddingTop: 40 }}>Loading...</div>
          ) : tab === 'rules' ? (
            rules.length === 0 ? (
                <div style={{ textAlign: 'center', color: 'var(--color-text-muted)', paddingTop: 40 }}>
                  <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 8 }}>
                    <PencilRuler style={{ width: 32, height: 32, color: 'var(--color-text-muted)' }} />
                  </div>
                  <p>{t('rules.noRules')}</p>
              </div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                                          <tr style={{ color: 'var(--color-text-secondary)' }}>
                    {['Name', 'Kind', 'From → To', 'Description', ''].map(h => (
                        <th key={h} style={{ padding: '6px 8px', textAlign: 'left', borderBottom: '1px solid var(--color-border-subtle)', fontWeight: 500 }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rules.map(rule => (
                      <tr key={rule.id} style={{ borderBottom: '1px solid var(--color-border-subtle)' }}>
                          <td style={{ padding: '8px', color: 'var(--color-text-primary)' }}>{rule.name}</td>
                      <td style={{ padding: '8px' }}>
                        <span style={{
                          padding: '2px 8px', borderRadius: 4, fontSize: 11,
                          background: rule.kind === 'deny' ? 'rgba(239,68,68,0.15)' : 'rgba(34,197,94,0.15)',
                          color: rule.kind === 'deny' ? '#f87171' : '#86efac',
                        }}>{rule.kind}</span>
                      </td>
                          <td style={{ padding: '8px', color: 'var(--color-text-secondary)', fontFamily: 'monospace' }}>
                              <span style={{ color: 'var(--color-node-file)' }}>{rule.from_pattern}</span>
                              <span style={{ color: 'var(--color-text-muted)' }}> → </span>
                              <span style={{ color: 'var(--color-node-interface)' }}>{rule.to_pattern}</span>
                      </td>
                          <td style={{ padding: '8px', color: 'var(--color-text-muted)' }}>{rule.description || '—'}</td>
                      <td style={{ padding: '8px' }}>
                        <button onClick={() => setDeleteRuleId(rule.id)} style={{
                          background: 'transparent', border: '1px solid rgba(239,68,68,0.3)',
                          color: '#f87171', borderRadius: 4, cursor: 'pointer', padding: '2px 8px', fontSize: 12,
                        }}>Delete</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          ) : (
            violations.length === 0 ? (
                  <div style={{ textAlign: 'center', color: 'var(--color-text-muted)', paddingTop: 40 }}>
                    <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 8 }}>
                      <CheckCircle style={{ width: 32, height: 32, color: '#4ade80' }} />
                    </div>
                <p style={{ color: '#4ade80' }}>No violations found. Architecture is clean!</p>
                {rules.length === 0 && <p style={{ fontSize: 12, marginTop: 4 }}>Add deny rules first, then run Validate.</p>}
              </div>
            ) : (
              <div>
                <div style={{ marginBottom: 12, color: '#f87171', fontSize: 13 }}>
                  Found {violations.length} violation{violations.length !== 1 ? 's' : ''}
                </div>
                {violations.map((v, i) => (
                  <div key={i} style={{
                    padding: '10px 14px', marginBottom: 8, borderRadius: 8,
                    background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)',
                  }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                      <span style={{ color: '#f87171', fontSize: 13, fontWeight: 500 }}>Rule: {v.rule_name}</span>
                            <span style={{ color: 'var(--color-text-muted)', fontSize: 11 }}>{v.edge_kind}</span>
                    </div>
                        <div style={{ fontFamily: 'monospace', fontSize: 12, color: 'var(--color-text-secondary)' }}>
                            <span style={{ color: 'var(--color-node-file)' }}>{v.source_name}</span>
                            <span style={{ color: 'var(--color-text-muted)' }}> ({v.source_file.split('/').slice(-2).join('/')}) → </span>
                            <span style={{ color: 'var(--color-node-interface)' }}>{v.target_name}</span>
                            <span style={{ color: 'var(--color-text-muted)' }}> ({v.target_file.split('/').slice(-2).join('/')})</span>
                    </div>
                  </div>
                ))}
              </div>
            )
          )}
      </div>

      <ConfirmDialog
        isOpen={deleteRuleId !== null}
        title="Delete Rule"
        message="Are you sure you want to delete this architecture rule? This action cannot be undone."
        confirmLabel="Delete"
        onConfirm={() => { if (deleteRuleId !== null) handleDeleteRule(deleteRuleId); }}
        onCancel={() => setDeleteRuleId(null)}
      />
    </Modal >
  );
}

const inputStyle: React.CSSProperties = {
  padding: '7px 10px', borderRadius: 6,
    border: '1px solid var(--color-border-default)',
    background: 'var(--color-deep)', color: 'var(--color-text-primary)', fontSize: 13,
  outline: 'none', width: '100%', boxSizing: 'border-box',
};

const btnStyle: React.CSSProperties = {
  padding: '7px 16px', borderRadius: 6, border: 'none',
  cursor: 'pointer', color: '#fff', fontSize: 13,
};