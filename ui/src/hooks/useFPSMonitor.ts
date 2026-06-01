import { useState, useEffect, useRef, useCallback } from 'react';

interface FPSMonitorResult {
  /** Current average FPS over the measurement window */
  fps: number;
  /** Whether a low-FPS warning is active */
  isLowFPS: boolean;
  /** Dismiss the current low-FPS warning */
  dismissWarning: () => void;
}

/**
 * useFPSMonitor tracks frames-per-second using requestAnimationFrame.
 * When average FPS drops below `threshold` for `sustainedMs` milliseconds,
 * it sets isLowFPS=true so the UI can show a degradation hint.
 */
export function useFPSMonitor(
  threshold: number = 15,
  sustainedMs: number = 3000,
  enabled: boolean = true
): FPSMonitorResult {
  const [fps, setFps] = useState(60);
  const [isLowFPS, setIsLowFPS] = useState(false);
  const dismissedRef = useRef(false);
  const frameCountRef = useRef(0);
  const lastTimeRef = useRef(performance.now());
  const lowStartRef = useRef<number | null>(null);
  const rafRef = useRef<number>(0);

  const dismissWarning = useCallback(() => {
    dismissedRef.current = true;
    setIsLowFPS(false);
  }, []);

  useEffect(() => {
    if (!enabled) return;

    const measure = () => {
      frameCountRef.current++;
      const now = performance.now();
      const elapsed = now - lastTimeRef.current;

      // Sample every 500ms
      if (elapsed >= 500) {
        const currentFps = Math.round((frameCountRef.current * 1000) / elapsed);
        setFps(currentFps);
        frameCountRef.current = 0;
        lastTimeRef.current = now;

        // Check for sustained low FPS
        if (currentFps < threshold) {
          if (lowStartRef.current === null) {
            lowStartRef.current = now;
          } else if (now - lowStartRef.current >= sustainedMs) {
            if (!dismissedRef.current) {
              setIsLowFPS(true);
            }
          }
        } else {
          lowStartRef.current = null;
          dismissedRef.current = false;
          setIsLowFPS(false);
        }
      }

      rafRef.current = requestAnimationFrame(measure);
    };

    rafRef.current = requestAnimationFrame(measure);

    return () => {
      if (rafRef.current) {
        cancelAnimationFrame(rafRef.current);
      }
    };
  }, [threshold, sustainedMs, enabled]);

  return { fps, isLowFPS, dismissWarning };
}