import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ContainerApp, ProjectMeta, ProjectShare } from "../../../models/project";
import {
  PREVIEW_REFRESH_INTERVAL_MS,
  PREVIEW_SHARE_TTL_HOURS,
  previewPortRows,
  previewUnavailableReason,
  type PreviewPortRow,
  type PreviewUnavailableReason,
} from "../../projects/projectPreviewLinksState.ts";

export interface ProjectPreviewLinks {
  rows: PreviewPortRow[];
  loading: boolean;
  error: string | null;
  /** Set when the container cannot be scanned at all (stopped, provisioning…). */
  unavailable: PreviewUnavailableReason;
  /** True once a scan has completed, so "nothing yet" is distinguishable. */
  loaded: boolean;
  refresh: () => Promise<void>;
  /** Creates a 24h public link and returns its one-time URL. */
  createShare: (port: number) => Promise<string>;
}

/**
 * Listening ports plus the project's live public links, for the preview
 * popover and chip.
 *
 * The workspace WebSocket carries chat and project rows only — it has no
 * listener events to piggyback on — so this polls, and only while a popover is
 * open. Discovery runs `ss` inside the container, which fails outright while
 * the container is down, so a non-running project short-circuits to a state
 * instead of a request.
 */
export function useProjectPreviewLinks({
  project,
  enabled,
  polling,
}: {
  project: ProjectMeta | null;
  enabled: boolean;
  polling: boolean;
}): ProjectPreviewLinks {
  const [apps, setApps] = useState<ContainerApp[]>([]);
  const [shares, setShares] = useState<ProjectShare[]>([]);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [scannedAt, setScannedAt] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const requestId = useRef(0);

  const projectId = project?.id ?? null;
  const unavailable = project ? previewUnavailableReason(project.status) : null;
  const scannable = projectId !== null && unavailable === null;

  const refresh = useCallback(async () => {
    if (!projectId || !scannable) {
      setApps([]);
      setShares([]);
      return;
    }
    const id = ++requestId.current;
    setLoading(true);
    try {
      // A share list must survive a failed port scan: links stay valid even
      // when nothing is listening on their port right now.
      const [nextApps, nextShares] = await Promise.all([
        projectApi.listApps(projectId),
        projectApi.listShares(projectId).catch(() => [] as ProjectShare[]),
      ]);
      if (id !== requestId.current) return;
      setApps(nextApps);
      setShares(nextShares);
      setScannedAt(Date.now());
      setError(null);
    } catch (caught) {
      if (id !== requestId.current) return;
      setApps([]);
      setError((caught as Error).message);
    } finally {
      if (id === requestId.current) {
        setLoading(false);
        setLoaded(true);
      }
    }
  }, [projectId, scannable]);

  // Drop another project's ports the moment the target changes.
  useEffect(() => {
    requestId.current += 1;
    setApps([]);
    setShares([]);
    setScannedAt(0);
    setError(null);
    setLoaded(false);
    setLoading(false);
  }, [projectId]);

  useEffect(() => {
    if (!enabled || !scannable) return;
    void refresh();
  }, [enabled, scannable, refresh]);

  useEffect(() => {
    if (!polling || !enabled || !scannable) return;
    const timer = setInterval(() => void refresh(), PREVIEW_REFRESH_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [polling, enabled, scannable, refresh]);

  const createShare = useCallback(
    async (port: number): Promise<string> => {
      if (!projectId) throw new Error("No project selected.");
      const created = await projectApi.createShare(projectId, {
        port,
        ttlHours: PREVIEW_SHARE_TTL_HOURS,
      });
      // The URL is the one-time secret; only its metadata joins the list.
      setShares((current) => [
        { ...created, url: undefined },
        ...current.filter((existing) => existing.id !== created.id),
      ]);
      return created.url ?? "";
    },
    [projectId],
  );

  // Expiry is judged as of the last scan; a refresh is what moves it forward.
  const rows = useMemo(
    () => previewPortRows(apps, shares, scannedAt || Date.now()),
    [apps, shares, scannedAt],
  );

  return { rows, loading, error, unavailable, loaded, refresh, createShare };
}
