import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { sitewatchApi } from "../../../api/sitewatchApi";
import type {
  SiteCandidate,
  SiteCheckReport,
  SiteImportInput,
  SiteImportResult,
  WatchedSiteInput,
  WatchedSiteView,
} from "../../../models/sitewatch";
import {
  SITE_INTERVAL_BOUNDS,
  SITE_LIMITS,
  type IntervalBounds,
} from "../../settings/clientSitesState";

/**
 * The Client sites panel's state.
 *
 * The table refreshes itself once a minute while the tab is visible: the
 * watcher's own cadence is minutes, so anything faster would only poll a
 * value that has not moved. Every write re-reads the collection rather than
 * patching the local array, because a create can change another row (a
 * duplicate refusal) and the server owns the ordering.
 */
const REFRESH_INTERVAL_MS = 60_000;

export interface ClientSites {
  sites: WatchedSiteView[];
  bounds: IntervalBounds;
  maxSites: number;
  maxExtraUrls: number;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  /** Wall clock captured with the last render, for countdowns and ages. */
  now: number;
  /** The site id whose check is running, for the row's spinner. */
  checkingId: string | null;
  /** The newest "Check now" result, keyed by site id. */
  report: SiteCheckReport | null;
  saving: boolean;
  refresh: () => Promise<void>;
  create: (input: WatchedSiteInput) => Promise<boolean>;
  update: (id: string, input: WatchedSiteInput) => Promise<boolean>;
  remove: (id: string) => Promise<boolean>;
  checkNow: (id: string) => Promise<void>;
  dismissReport: () => void;
  importSites: (input: SiteImportInput) => Promise<SiteImportResult | null>;
  loadCandidates: () => Promise<SiteCandidate[]>;
}

export function useClientSites(enabled: boolean): ClientSites {
  const [sites, setSites] = useState<WatchedSiteView[]>([]);
  const [bounds, setBounds] = useState<IntervalBounds>(SITE_INTERVAL_BOUNDS);
  const [maxSites, setMaxSites] = useState<number>(SITE_LIMITS.maxSites);
  const [maxExtraUrls, setMaxExtraUrls] = useState<number>(SITE_LIMITS.maxExtraUrls);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [checkingId, setCheckingId] = useState<string | null>(null);
  const [report, setReport] = useState<SiteCheckReport | null>(null);
  const requestInFlight = useRef(false);
  const hasPayload = useRef(false);

  const refresh = useCallback(async () => {
    if (requestInFlight.current) return;
    requestInFlight.current = true;
    if (!hasPayload.current) setLoading(true);
    else setRefreshing(true);
    try {
      const payload = await sitewatchApi.list();
      hasPayload.current = true;
      setSites(payload.sites ?? []);
      setBounds({
        min: payload.minIntervalMinutes || SITE_INTERVAL_BOUNDS.min,
        max: payload.maxIntervalMinutes || SITE_INTERVAL_BOUNDS.max,
      });
      setMaxSites(payload.maxSites || SITE_LIMITS.maxSites);
      setMaxExtraUrls(payload.maxExtraUrls || SITE_LIMITS.maxExtraUrls);
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

  // Countdowns tick without a network round trip: "in 4 min" must not sit
  // still for a whole minute.
  useEffect(() => {
    if (!enabled) return;
    const tick = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(tick);
  }, [enabled]);

  const write = useCallback(
    async (action: () => Promise<unknown>): Promise<boolean> => {
      setSaving(true);
      setError(null);
      try {
        await action();
        await refresh();
        return true;
      } catch (cause) {
        setError((cause as Error).message);
        return false;
      } finally {
        setSaving(false);
      }
    },
    [refresh],
  );

  const create = useCallback(
    (input: WatchedSiteInput) => write(() => sitewatchApi.create(input)),
    [write],
  );

  const update = useCallback(
    (id: string, input: WatchedSiteInput) => write(() => sitewatchApi.update(id, input)),
    [write],
  );

  const remove = useCallback((id: string) => write(() => sitewatchApi.remove(id)), [write]);

  /**
   * Runs one site's checks synchronously and shows the raw per-URL results.
   * The row itself is refreshed afterwards because the check counts: it is
   * recorded in the history and can move the state machine.
   */
  const checkNow = useCallback(
    async (id: string) => {
      setCheckingId(id);
      setError(null);
      setReport(null);
      try {
        setReport(await sitewatchApi.check(id));
        await refresh();
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setCheckingId(null);
      }
    },
    [refresh],
  );

  const importSites = useCallback(
    async (input: SiteImportInput): Promise<SiteImportResult | null> => {
      setSaving(true);
      setError(null);
      try {
        const result = await sitewatchApi.import(input);
        await refresh();
        return result;
      } catch (cause) {
        setError((cause as Error).message);
        return null;
      } finally {
        setSaving(false);
      }
    },
    [refresh],
  );

  const loadCandidates = useCallback(async (): Promise<SiteCandidate[]> => {
    try {
      return (await sitewatchApi.candidates()).candidates ?? [];
    } catch (cause) {
      setError((cause as Error).message);
      return [];
    }
  }, []);

  return {
    sites,
    bounds,
    maxSites,
    maxExtraUrls,
    loading,
    refreshing,
    error,
    now,
    checkingId,
    report,
    saving,
    refresh,
    create,
    update,
    remove,
    checkNow,
    dismissReport: () => setReport(null),
    importSites,
    loadCandidates,
  };
}
