import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { usageApi } from "../../../api/usageApi";
import type {
  UsageGroupBy,
  UsageRecord,
  UsageSummary,
} from "../../../models/usage";
import {
  usageRangeForPreset,
  usageRangeFromDates,
  type UsageRange,
  type UsageRangePreset,
} from "../../usage/usageRangeState";

const DRILL_DOWN_LIMIT = 100;

export interface UsageDrillDown {
  projectId: string;
  label: string;
  records: UsageRecord[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
}

export interface UsageDashboard {
  range: UsageRange;
  groupBy: UsageGroupBy;
  summary: UsageSummary | null;
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  drillDown: UsageDrillDown | null;
  setPreset: (preset: UsageRangePreset) => void;
  setCustomRange: (from: string, to: string) => void;
  setGroupBy: (groupBy: UsageGroupBy) => void;
  refresh: () => Promise<void>;
  openDrillDown: (projectId: string, label: string) => Promise<void>;
  loadMoreDrillDown: () => Promise<void>;
  closeDrillDown: () => void;
}

/**
 * Owns the Usage page's query state: the selected window, the grouping
 * dimension, and the per-project drill-down. Every state change re-queries the
 * backend rather than aggregating client-side, so a member's view is filtered
 * where the membership rules live.
 */
export function useUsageDashboard(enabled: boolean): UsageDashboard {
  const [range, setRange] = useState<UsageRange>(() => usageRangeForPreset("30d", Date.now()));
  const [groupBy, setGroupBy] = useState<UsageGroupBy>("project");
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [drillDown, setDrillDown] = useState<UsageDrillDown | null>(null);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const hasSummary = useRef(false);

  const refresh = useCallback(async () => {
    if (hasSummary.current) setRefreshing(true);
    else setLoading(true);
    try {
      const next = await usageApi.summary({ from: range.from, to: range.to, groupBy });
      hasSummary.current = true;
      setSummary(next);
      setError(null);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [range.from, range.to, groupBy]);

  useEffect(() => {
    if (!enabled) return;
    void refresh();
  }, [enabled, refresh]);

  // A new window invalidates whatever rows the drill-down was showing.
  useEffect(() => {
    setDrillDown(null);
    setCursor(undefined);
  }, [range.from, range.to]);

  const fetchRecords = useCallback(
    async (projectId: string, label: string, nextCursor?: string) => {
      setDrillDown((current) => ({
        projectId,
        label,
        records: nextCursor && current ? current.records : [],
        loading: true,
        error: null,
        hasMore: false,
      }));
      try {
        const page = await usageApi.records({
          from: range.from,
          to: range.to,
          projectId: projectId || undefined,
          limit: DRILL_DOWN_LIMIT,
          cursor: nextCursor,
        });
        setCursor(page.nextCursor);
        setDrillDown((current) => ({
          projectId,
          label,
          records: nextCursor && current ? [...current.records, ...page.records] : page.records,
          loading: false,
          error: null,
          hasMore: Boolean(page.nextCursor),
        }));
      } catch (cause) {
        setDrillDown((current) => ({
          projectId,
          label,
          records: current?.records ?? [],
          loading: false,
          error: (cause as Error).message,
          hasMore: false,
        }));
      }
    },
    [range.from, range.to]
  );

  const openDrillDown = useCallback(
    async (projectId: string, label: string) => {
      setCursor(undefined);
      await fetchRecords(projectId, label);
    },
    [fetchRecords]
  );

  const loadMoreDrillDown = useCallback(async () => {
    if (!drillDown || !cursor) return;
    await fetchRecords(drillDown.projectId, drillDown.label, cursor);
  }, [drillDown, cursor, fetchRecords]);

  const setPreset = useCallback((preset: UsageRangePreset) => {
    setRange((current) =>
      preset === "custom" ? { ...current, preset } : usageRangeForPreset(preset, Date.now())
    );
  }, []);

  const setCustomRange = useCallback((from: string, to: string) => {
    setRange((current) => usageRangeFromDates(current, from, to));
  }, []);

  return useMemo(
    () => ({
      range,
      groupBy,
      summary,
      loading,
      refreshing,
      error,
      drillDown,
      setPreset,
      setCustomRange,
      setGroupBy,
      refresh,
      openDrillDown,
      loadMoreDrillDown,
      closeDrillDown: () => setDrillDown(null),
    }),
    [
      range,
      groupBy,
      summary,
      loading,
      refreshing,
      error,
      drillDown,
      setPreset,
      setCustomRange,
      refresh,
      openDrillDown,
      loadMoreDrillDown,
    ]
  );
}
