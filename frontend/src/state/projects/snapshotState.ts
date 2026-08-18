import type { Snapshot, SnapshotJob } from "../../models/snapshot";
import type { TrashedProject } from "../../models/project";

/** How long the UI keeps polling after a capture or restore is kicked off. */
export const SNAPSHOT_POLL_INTERVAL_MS = 3000;

const MILLIS_PER_DAY = 24 * 60 * 60 * 1000;

/**
 * Presentation rules for snapshots and the Trash. Everything here is pure so
 * the wording, the ordering, and the "is something still running?" question
 * can be pinned by tests instead of being re-derived inside components.
 */
class SnapshotState {
  /** A snapshot whose archive is still being written cannot be restored. */
  isSettling(snapshot: Snapshot): boolean {
    return snapshot.status === "pending" || snapshot.status === "running";
  }

  restorable(snapshot: Snapshot): boolean {
    return snapshot.status === "ready";
  }

  /** True while any capture or restore is in flight, which is what keeps the
   *  list polling and the buttons disabled. */
  hasRunningJob(snapshots: Snapshot[], jobs: SnapshotJob[]): boolean {
    return (
      jobs.some((job) => job.status === "running") ||
      snapshots.some((snapshot) => this.isSettling(snapshot))
    );
  }

  /** The newest failed job that no longer has a record to carry its error. A
   *  failed restore leaves the snapshot itself untouched, so without this the
   *  failure would be invisible. */
  failedRestore(jobs: SnapshotJob[]): SnapshotJob | null {
    for (const job of jobs) {
      if (job.kind === "restore" && job.status === "failed") return job;
    }
    return null;
  }

  /** One-line description of what a snapshot holds. */
  describeContents(snapshot: Snapshot): string {
    const parts = ["workspace + agent homes"];
    if (snapshot.hasDatabase) {
      parts.push(snapshot.databaseEngine ? `${snapshot.databaseEngine} database` : "database");
    }
    if (snapshot.includesSecrets) parts.push("secrets");
    return parts.join(", ");
  }

  /** Status wording shown next to a record. */
  describeStatus(snapshot: Snapshot): string {
    switch (snapshot.status) {
      case "pending":
        return "Queued";
      case "running":
        return "Packing…";
      case "failed":
        return snapshot.error ? `Failed: ${snapshot.error}` : "Failed";
      default:
        return "Ready";
    }
  }

  /** Default label for the "Snapshot now" field: the automatic snapshots are
   *  already labelled, so only manual ones need a hint. */
  defaultLabel(now: number): string {
    return new Date(now).toLocaleString();
  }

  /**
   * Whole days left before the janitor purges a trashed project. Rounded up so
   * a project purged in twenty minutes never reads "0 days left", and clamped
   * at zero for one that is already past its window.
   */
  daysLeft(project: TrashedProject, now: number): number | null {
    if (!project.expiresAt) return null;
    return Math.max(0, Math.ceil((project.expiresAt - now) / MILLIS_PER_DAY));
  }

  /** The sentence under a trashed project's name. */
  describeRetention(project: TrashedProject, now: number): string {
    const days = this.daysLeft(project, now);
    if (days === null) return "Kept until an admin purges it";
    if (days === 0) return "Purged within the next sweep";
    return `${days} day${days === 1 ? "" : "s"} left`;
  }

  /** Replaces one record in place, keeping the newest-first order the server
   *  already applied. */
  replace(snapshots: Snapshot[], next: Snapshot): Snapshot[] {
    const index = snapshots.findIndex((snapshot) => snapshot.id === next.id);
    if (index < 0) return [next, ...snapshots];
    const copy = snapshots.slice();
    copy[index] = next;
    return copy;
  }

  remove(snapshots: Snapshot[], snapshotId: string): Snapshot[] {
    return snapshots.filter((snapshot) => snapshot.id !== snapshotId);
  }

  removeProject(projects: TrashedProject[], projectId: string): TrashedProject[] {
    return projects.filter((project) => project.id !== projectId);
  }
}

export const snapshotState = new SnapshotState();
