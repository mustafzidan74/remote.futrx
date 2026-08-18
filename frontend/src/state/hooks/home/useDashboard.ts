import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { dashboardApi } from "../../../api/dashboardApi";
import type { DashboardSnapshot } from "../../../models/dashboard";

/**
 * The home screen refreshes itself once a minute — often enough that a run
 * finishing or a project degrading shows up without a reload, rare enough
 * that a tab left open overnight does not fan out across nine subsystems
 * nine hundred times.
 */
const REFRESH_INTERVAL_MS = 60_000;

export interface Dashboard {
  snapshot: DashboardSnapshot | null;
  /** True only before the first payload arrives, so refreshes never flash
   *  the skeleton over a screen that already has content. */
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  /** Wall clock captured with the last render, for countdowns and ages. */
  now: number;
  refresh: () => Promise<void>;
}

export function useDashboard(enabled: boolean): Dashboard {
  const [snapshot, setSnapshot] = useState<DashboardSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const requestInFlight = useRef(false);
  const hasSnapshot = useRef(false);

  const refresh = useCallback(async () => {
    if (requestInFlight.current) return;
    requestInFlight.current = true;
    if (!hasSnapshot.current) setLoading(true);
    else setRefreshing(true);
    try {
      const next = await dashboardApi.snapshot();
      hasSnapshot.current = true;
      setSnapshot(next);
      setNow(Date.now());
      setError(null);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      requestInFlight.current = false;
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void refresh();

    // The timer only runs while the tab is visible. A backgrounded dashboard
    // is nobody's live view, and the fan-out is the most expensive read the
    // API has; the visibility handler refreshes once on return so the screen
    // is never stale by the time somebody looks at it.
    let interval = 0;
    const start = () => {
      if (interval) return;
      interval = window.setInterval(() => void refresh(), REFRESH_INTERVAL_MS);
    };
    const stop = () => {
      if (!interval) return;
      window.clearInterval(interval);
      interval = 0;
    };
    const onVisibility = () => {
      if (document.hidden) {
        stop();
        return;
      }
      void refresh();
      start();
    };

    if (!document.hidden) start();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [enabled, refresh]);

  // Countdowns tick without a network round trip: the payload is a minute
  // stale at worst, but "in 4 min" must not sit still for that whole minute.
  useEffect(() => {
    if (!enabled) return;
    const tick = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(tick);
  }, [enabled]);

  return { snapshot, loading, refreshing, error, now, refresh };
}
