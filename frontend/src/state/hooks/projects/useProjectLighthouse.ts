import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { projectLighthouseApi } from "../../../api/project/projectLighthouseApi";
import type { LighthouseOverview } from "../../../models/lighthouse";

export interface ProjectLighthouseState {
  overview: LighthouseOverview | null;
  loading: boolean;
  busy: boolean;
  /** The install is its own state: it is slow and has its own button. */
  installing: boolean;
  error: string | null;
  run: (input: { port: number; paths: string[]; formFactor: string; label: string }) => Promise<void>;
  install: () => Promise<void>;
  remove: (runId: string) => Promise<void>;
  refresh: () => Promise<void>;
}

/**
 * How often the panel asks again while a run is in flight.
 *
 * Runs write their record after every page and a page is tens of seconds, so
 * polling faster than this only produces identical answers.
 */
const POLL_MS = 4000;

/**
 * Local Lighthouse audits for one project.
 *
 * The run answers 202 with a record that is still working, so this hook's real
 * job is the polling: it starts when something is in flight and stops the
 * moment it is not, because a panel that keeps polling a finished run is a
 * request every four seconds for as long as the tab stays open.
 */
export function useProjectLighthouse(projectId: string, enabled: boolean): ProjectLighthouseState {
  const [overview, setOverview] = useState<LighthouseOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);
  const loaded = useRef(false);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    if (!projectId) return;
    try {
      const next = await projectLighthouseApi.overview(projectId);
      if (!alive.current) return;
      setOverview(next);
      loaded.current = true;
    } catch (cause) {
      // A failed poll is not worth an error banner: the previous answer is
      // still on screen, and a button press will produce a real message.
      if (alive.current && !loaded.current) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    if (!enabled) return;
    setLoading(true);
    void load();
  }, [enabled, load]);

  const running = overview?.running ?? false;
  useEffect(() => {
    if (!enabled || !running) return;
    const timer = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [enabled, running, load]);

  const act = useCallback(
    async (action: () => Promise<unknown>, mark: (value: boolean) => void) => {
      mark(true);
      setError(null);
      try {
        await action();
        await load();
      } catch (cause) {
        if (alive.current) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (alive.current) mark(false);
      }
    },
    [load],
  );

  return {
    overview,
    loading,
    busy,
    installing,
    error,
    run: (input) =>
      act(
        () =>
          projectLighthouseApi.run(projectId, {
            port: input.port,
            paths: input.paths,
            formFactor: input.formFactor,
            label: input.label,
          }),
        setBusy,
      ),
    install: () => act(() => projectLighthouseApi.install(projectId), setInstalling),
    remove: (runId) => act(() => projectLighthouseApi.deleteRun(projectId, runId), setBusy),
    refresh: load,
  };
}
