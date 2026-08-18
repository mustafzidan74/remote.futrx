/** Lifecycle of one snapshot record. Packing a large workspace takes minutes,
 *  so a record is listed before its archive exists. */
export type SnapshotStatus = "pending" | "running" | "ready" | "failed";

/** "manual" is a snapshot someone asked for; "trash" is the automatic one the
 *  platform takes when a project is deleted. */
export type SnapshotKind = "manual" | "trash";

export type SnapshotJobKind = "capture" | "restore";

export interface Snapshot {
  id: string;
  label?: string;
  kind: SnapshotKind;
  status: SnapshotStatus;
  error?: string;
  createdBy?: string;
  createdAt: number;
  finishedAt?: number;
  archive?: string;
  format?: string;
  sizeBytes?: number;
  hasDatabase: boolean;
  databaseEngine?: string;
  includesSecrets: boolean;
  slug?: string;
  template?: string;
}

/** A background capture or restore. Jobs live in memory on the server, so the
 *  list is empty again after a backend restart. */
export interface SnapshotJob {
  id: string;
  projectId: string;
  kind: SnapshotJobKind;
  snapshotId?: string;
  status: SnapshotStatus;
  error?: string;
  startedAt: number;
  finishedAt?: number;
}

export interface SnapshotsPayload {
  snapshots: Snapshot[];
  jobs: SnapshotJob[];
}
