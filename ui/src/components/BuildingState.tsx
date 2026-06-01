import { useState, useEffect, useRef } from 'react';
import { Loader2, Network, CheckCircle2, Circle } from 'lucide-react';
import { useTranslation } from 'react-i18next';

interface BuildingStateProps {
  projectName?: string;
  progress?: number;
  phase?: string;
  message?: string;
}

// Map phase names to display steps
const PHASE_STEPS = [
  { key: 'collect', labelKey: 'building.parsing' },
  { key: 'detect', labelKey: 'building.parsing' },
  { key: 'parse', labelKey: 'building.parsing' },
  { key: 'insert', labelKey: 'building.extracting' },
  { key: 'edges', labelKey: 'building.building' },
  { key: 'analyses', labelKey: 'building.building' },
  { key: 'finalize', labelKey: 'building.building' },
];

export function BuildingState({ projectName, progress = 0, phase = '', message }: BuildingStateProps) {
  const { t } = useTranslation('dropzone');
  const [elapsed, setElapsed] = useState(0);
  const startTimeRef = useRef(Date.now());

  // Track elapsed time
  useEffect(() => {
    const timer = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startTimeRef.current) / 1000));
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  // Determine which steps are completed based on current phase
  const currentPhaseIndex = PHASE_STEPS.findIndex(s => s.key === phase);
  const steps = PHASE_STEPS.map((step, i) => ({
    ...step,
    completed: i < currentPhaseIndex,
    active: i === currentPhaseIndex,
  }));

  // Format elapsed time
  const formatTime = (seconds: number) => {
    if (seconds < 60) return `${seconds}s`;
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m}m ${s}s`;
  };

  // Find the three display steps (collapse sub-phases)
  const displaySteps = [
    { label: t('building.parsing'), completed: steps.filter(s => ['collect', 'detect', 'parse'].includes(s.key)).every(s => s.completed), active: steps.some(s => ['collect', 'detect', 'parse'].includes(s.key) && s.active) },
    { label: t('building.extracting'), completed: steps.filter(s => ['insert'].includes(s.key)).every(s => s.completed), active: steps.some(s => ['insert'].includes(s.key) && s.active) },
    { label: t('building.building'), completed: steps.filter(s => ['edges', 'analyses', 'finalize'].includes(s.key)).every(s => s.completed), active: steps.some(s => ['edges', 'analyses', 'finalize'].includes(s.key) && s.active) },
  ];

  const hasProgress = progress > 0 || phase !== '';

  return (
    <div className="flex items-center justify-center h-full bg-deep">
      <div className="flex flex-col items-center gap-6 max-w-md text-center">
        {/* Animated icon */}
        <div className="relative">
          <div className="w-20 h-20 border-4 border-accent/30 rounded-full" />
          <div className="absolute inset-0 w-20 h-20 border-4 border-accent border-t-transparent rounded-full animate-spin" />
          <Network className="absolute inset-0 m-auto w-8 h-8 text-accent" />
        </div>

        {/* Title */}
        <div className="space-y-2">
          <h2 className="text-xl font-semibold text-text-primary">
            {t('building.title')}
          </h2>
          {projectName && (
            <p className="text-text-secondary">
              {t('building.analyzing', { name: projectName })}
            </p>
          )}
        </div>

        {/* Progress bar (only shown when progress data available) */}
        {hasProgress && (
          <div className="w-full space-y-2">
            <div className="w-full bg-bg-secondary rounded-full h-2 overflow-hidden">
              <div
                className="bg-accent h-full rounded-full transition-all duration-300 ease-out"
                style={{ width: `${Math.min(progress, 100)}%` }}
              />
            </div>
            <div className="flex justify-between text-xs text-text-muted">
              <span>{message || phase}</span>
              <span>{progress}%</span>
            </div>
          </div>
        )}

        {/* Progress steps */}
        <div className="w-full space-y-3 text-sm">
          {displaySteps.map((step, i) => (
            <div key={i} className="flex items-center gap-3">
              {step.completed ? (
                <CheckCircle2 className="w-4 h-4 text-green-400 shrink-0" />
              ) : step.active ? (
                <Loader2 className="w-4 h-4 animate-spin text-accent shrink-0" />
              ) : (
                <Circle className="w-4 h-4 text-text-muted/30 shrink-0" />
              )}
              <span className={step.completed ? 'text-text-secondary' : step.active ? 'text-text-primary' : 'text-text-muted/50'}>
                {step.label}
              </span>
            </div>
          ))}
        </div>

        {/* Elapsed time */}
        <p className="text-xs text-text-muted">
          {hasProgress ? `${formatTime(elapsed)} elapsed` : t('building.hint')}
        </p>
      </div>
    </div>
  );
}