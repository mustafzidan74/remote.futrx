import { requestJson } from "../apiRequest";
import type { Snapshot, SnapshotJob, SnapshotsPayload } from "../../models/snapshot";
import { API_ROUTES } from "../../config/routes";

export const projectSnapshotsApi = {
  listSnapshots: (id: string) =>
    requestJson<SnapshotsPayload>("GET", API_ROUTES.projects.snapshots(id)),

  /** Answers 202 with a pending record; poll listSnapshots for the outcome. */
  createSnapshot: (id: string, body: { label?: string; includeSecrets?: boolean }) =>
    requestJson<Snapshot>("POST", API_ROUTES.projects.snapshots(id), body),

  /** Replaces the project's files. Members must pass confirm; admins may not. */
  restoreSnapshot: (id: string, snapshotId: string) =>
    requestJson<SnapshotJob>(
      "POST",
      API_ROUTES.projects.snapshotRestore(id, snapshotId),
      { confirm: true },
    ),

  deleteSnapshot: (id: string, snapshotId: string) =>
    requestJson<{ ok: boolean }>(
      "DELETE",
      API_ROUTES.projects.snapshot(id, snapshotId),
    ),
};
