import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectMeta } from "../../../models/project";
import type { Snapshot, SnapshotJob } from "../../../models/snapshot";
import type { ProjectDataLoadSignal } from "../../projects/projectContainerRecords";
import { SNAPSHOT_POLL_INTERVAL_MS, snapshotState } from "../../projects/snapshotState";

export interface SnapshotsRecord {
  loading: boolean;
  data?: Snapshot[];
  jobs?: SnapshotJob[];
  error?: string;
}

/**
 * One project's snapshots. Captures and restores run on the server in the
 * background, so this hook polls while anything is in flight and stops as soon
 * as everything has settled — there is no websocket for snapshot progress.
 */
export function useProjectSnapshots(project: ProjectMeta | null, enabled: boolean) {
  const [record, setRecord] = useState<SnapshotsRecord>({ loading: false });
  // Kept in a ref so the polling effect does not restart on every payload.
  const running = snapshotState.hasRunningJob(record.data ?? [], record.jobs ?? []);
  const projectId = project?.id;
  const busyRef = useRef(false);

  const load = useCallback(
    async (signal?: ProjectDataLoadSignal) => {
      if (!projectId) {
        setRecord({ loading: false });
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const payload = await projectApi.listSnapshots(projectId);
        if (signal?.cancelled) return;
        setRecord({ loading: false, data: payload.snapshots, jobs: payload.jobs });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, error: (error as Error).message });
      }
    },
    [projectId],
  );

  useEffect(() => {
    if (!enabled || !projectId) return;
    const signal = { cancelled: false };
    void load(signal);
    return () => {
      signal.cancelled = true;
    };
  }, [enabled, projectId, load]);

  useEffect(() => {
    if (!enabled || !projectId || !running) return;
    const signal = { cancelled: false };
    const timer = setInterval(() => void load(signal), SNAPSHOT_POLL_INTERVAL_MS);
    return () => {
      signal.cancelled = true;
      clearInterval(timer);
    };
  }, [enabled, projectId, running, load]);

  const create = useCallback(
    async (label: string, includeSecrets: boolean) => {
      if (!projectId || busyRef.current) return;
      busyRef.current = true;
      try {
        const created = await projectApi.createSnapshot(projectId, {
          label: label.trim() || undefined,
          includeSecrets,
        });
        setRecord((current) => ({
          ...current,
          loading: false,
          data: snapshotState.replace(current.data ?? [], created),
        }));
      } finally {
        busyRef.current = false;
      }
    },
    [projectId],
  );

  const restore = useCallback(
    async (snapshotId: string) => {
      if (!projectId) return;
      await projectApi.restoreSnapshot(projectId, snapshotId);
      await load();
    },
    [projectId, load],
  );

  const remove = useCallback(
    async (snapshotId: string) => {
      if (!projectId) return;
      await projectApi.deleteSnapshot(projectId, snapshotId);
      setRecord((current) => ({
        ...current,
        loading: false,
        data: snapshotState.remove(current.data ?? [], snapshotId),
      }));
    },
    [projectId],
  );

  return { record, running, load, create, restore, remove };
}
