/**
 * Project health as the backend's monitor reports it.
 *
 * Rows arrive two ways and mean the same thing either way: in the workspace
 * socket's opening snapshot, and one at a time as the monitor finishes each
 * sweep. A project with no row is simply not being monitored — stopped,
 * never started, or the monitor is switched off — and its dot falls back to
 * the plain lifecycle status.
 */

export type ProjectHealthStatus = "ok" | "warn" | "crit" | "unknown";

export interface ProjectHealth {
  projectId: string;
  status: ProjectHealthStatus;
  memoryUsedBytes?: number;
  memoryLimitBytes?: number;
  memoryPct?: number;
  cpuPct?: number;
  diskUsedPct?: number;
  /** Every reachable in-container port, platform plumbing included. */
  listeners?: number[];
  /** The HTTP probe of the first application port; absent when there was none. */
  previewOk?: boolean;
  lastCheckedAt?: number;
  /** Why the status is what it is, most severe first. */
  reasons?: string[];
}

/** The GET /api/projects/health body. */
export interface ProjectHealthReport {
  enabled: boolean;
  intervalMs: number;
  projects: ProjectHealth[];
}
