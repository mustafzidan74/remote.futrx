import type { ProjectHealth, ProjectHealthStatus } from "../../models/health";
import type { ProjectMeta, ProjectStatus } from "../../models/project";

/** Health rows keyed by project id. */
export type ProjectHealthMap = Record<string, ProjectHealth>;

/** What the sidebar dot renders: one colour, one label, one tooltip. */
export interface ProjectHealthDot {
  tone: "green" | "amber" | "red" | "grey";
  label: string;
  title: string;
  /** True while the monitor has an opinion, so the dot means health and not lifecycle. */
  monitored: boolean;
}

const STATUS_TONE: Record<ProjectHealthStatus, ProjectHealthDot["tone"]> = {
  ok: "green",
  warn: "amber",
  crit: "red",
  unknown: "grey",
};

const STATUS_LABEL: Record<ProjectHealthStatus, string> = {
  ok: "Healthy",
  warn: "Degraded",
  crit: "Critical",
  unknown: "Health unknown",
};

/**
 * Lifecycle fallback for a project the monitor is not watching: a stopped
 * container has no health to report, but the row still needs a dot.
 */
const LIFECYCLE_TONE: Record<ProjectStatus, ProjectHealthDot["tone"]> = {
  "": "grey",
  provisioning: "amber",
  running: "green",
  stopped: "grey",
  error: "red",
  missing: "red",
};

const LIFECYCLE_LABEL: Record<ProjectStatus, string> = {
  "": "Unknown",
  provisioning: "Provisioning",
  running: "Running",
  stopped: "Stopped",
  error: "Error",
  missing: "Missing - needs reprovision",
};

class ProjectHealthState {
  /** Replaces the whole map from a workspace snapshot. */
  replace(rows: ProjectHealth[] | undefined): ProjectHealthMap {
    const next: ProjectHealthMap = {};
    for (const row of rows ?? []) {
      if (row?.projectId) next[row.projectId] = row;
    }
    return next;
  }

  /**
   * Folds one broadcast in. An absent row is the monitor saying "stop showing
   * health for this project", which happens the sweep after it stops.
   */
  apply(current: ProjectHealthMap, projectId: string, row?: ProjectHealth): ProjectHealthMap {
    if (!projectId) return current;
    if (!row) {
      if (!(projectId in current)) return current;
      const next = { ...current };
      delete next[projectId];
      return next;
    }
    if (current[projectId] === row) return current;
    return { ...current, [projectId]: { ...row, projectId } };
  }

  /**
   * Resolves the dot for one project. Health wins when the monitor has a
   * reading; otherwise the project's own lifecycle status does, so the dot
   * never goes blank while a container is starting or stopped.
   */
  dot(project: ProjectMeta, health?: ProjectHealth): ProjectHealthDot {
    if (!health || health.status === "unknown") {
      const tone = LIFECYCLE_TONE[project.status] ?? "grey";
      const label = LIFECYCLE_LABEL[project.status] ?? "Unknown";
      return {
        tone,
        label,
        title: health ? `${label} · health unknown` : label,
        monitored: false,
      };
    }
    const label = STATUS_LABEL[health.status];
    return {
      tone: STATUS_TONE[health.status],
      label,
      title: this.tooltip(label, health),
      monitored: true,
    };
  }

  /** The tooltip: the verdict, then every reason on its own line. */
  tooltip(label: string, health: ProjectHealth): string {
    const lines = [label, ...(health.reasons ?? [])];
    if ((health.reasons ?? []).length === 0 && health.memoryPct != null) {
      lines.push(`memory ${health.memoryPct}%`);
    }
    return lines.join("\n");
  }

  /** Percentage for the memory meter, or undefined when nothing is measured. */
  memoryPercent(health?: ProjectHealth): number | undefined {
    if (!health?.memoryLimitBytes || !health.memoryUsedBytes) return undefined;
    return health.memoryPct ?? undefined;
  }
}

export const projectHealthState = new ProjectHealthState();
