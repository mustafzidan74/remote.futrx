import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { projectVisualApi } from "../../../api/project/projectVisualApi";
import type { VisualOverview } from "../../../models/visualDiff";

export interface ProjectVisualState {
  overview: VisualOverview | null;
  loading: boolean;
  busy: boolean;
  error: string | null;
  setBaseline: (input: { port: number; paths: string[]; fullPage: boolean }) => Promise<void>;
  compare: (label: string) => Promise<void>;
  remove: (comparisonId: string) => Promise<void>;
  refresh: () => Promise<void>;
  clearError: () => void;
}

/**
 * How often the panel asks again while a run is in flight.
 *
 * Runs write their record after every page, so this is the rate at which the
 * list fills in — slow enough not to hammer the API for a job that takes
 * minutes, fast enough that a twelve-page run visibly progresses instead of
 * looking hung.
 */
const POLL_MS = 3000;

/**
 * Before/after comparison for one project.
 *
 * Both actions answer 202 with a record that is still running, so this hook's
 * real job is the polling: it starts when a run is in flight and stops the
 * moment it is not, because a panel that keeps polling a finished run is a
 * request every three seconds for as long as the tab stays open.
 */
export function useProjectVisual(projectId: string): ProjectVisualState {
  const [overview, setOverview] = useState<VisualOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    try {
      const next = await projectVisualApi.overview(projectId);
      if (alive.current) setOverview(next);
    } catch (cause) {
      // A failed poll is not worth an error banner: the previous answer is
      // still on screen and the next tick will either succeed or the user will
      // press a button and get a real message.
      if (alive.current && !overview) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [projectId, overview]);

  useEffect(() => {
    setLoading(true);
    void load();
    // load is intentionally not a dependency: it changes identity whenever the
    // overview does, and re-running this effect on every poll would restart
    // the whole panel.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  // Poll only while something is actually running.
  const running = overview?.running ?? false;
  useEffect(() => {
    if (!running) return;
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [running, load]);

  const act = useCallback(
    async (action: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      try {
        await action();
        await load();
      } catch (cause) {
        if (alive.current) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [load],
  );

  return {
    overview,
    loading,
    busy,
    error,
    setBaseline: (input) =>
      act(() =>
        projectVisualApi.setBaseline(projectId, {
          port: input.port,
          paths: input.paths,
          fullPage: input.fullPage,
        }),
      ),
    compare: (label) => act(() => projectVisualApi.compare(projectId, { label })),
    remove: (comparisonId) => act(() => projectVisualApi.deleteComparison(projectId, comparisonId)),
    refresh: load,
    clearError: () => setError(null),
  };
}
