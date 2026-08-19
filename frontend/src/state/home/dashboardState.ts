import type {
  AlertSeverity,
  DashboardAlert,
  DashboardProject,
  DashboardSnapshot,
} from "../../models/dashboard";

/**
 * Everything the home dashboard decides about the payload it was given.
 *
 * The server answers with facts; this module turns them into the words,
 * tones and orderings the screen renders. It is all pure functions over plain
 * records so the rules that matter — which alerts are actionable, whether
 * this week is up or down on last — can be tested without a DOM.
 */

/** Status tones. Kept separate from the single blue accent on purpose: a
 *  colour that means "critical" must never also mean "this is a bar". */
export type StatusTone = "green" | "amber" | "red" | "grey";

const SEVERITY_TONE: Record<AlertSeverity, StatusTone> = {
  crit: "red",
  warn: "amber",
  // Informational rows stay neutral. They are worth reading, not worth a
  // colour that competes with the two that mean something is wrong.
  info: "grey",
};

const SEVERITY_LABEL: Record<AlertSeverity, string> = {
  crit: "Critical",
  warn: "Needs attention",
  info: "For information",
};

/** Actions this client knows how to perform. An alert offering anything else
 *  renders as a plain row rather than a button that would do nothing. */
const SUPPORTED_ACTIONS = new Set([
  "open-project",
  "open-chat",
  "snapshot-now",
  "restore-trash",
  "enable-notifications",
  "open-monitoring",
  "open-resources",
  "open-client-sites",
]);

export interface AlertView {
  alert: DashboardAlert;
  tone: StatusTone;
  severityLabel: string;
  /** True when the alert names a fix this build can actually carry out. */
  actionable: boolean;
  actionLabel: string;
}

/**
 * Renders one alert. The server has already ordered the list most-severe
 * first, so this never re-sorts: the row a user looked at a minute ago must
 * still be in the same place after a refresh.
 */
export function describeAlert(alert: DashboardAlert): AlertView {
  const actionable = !!alert.action && SUPPORTED_ACTIONS.has(alert.action);
  return {
    alert,
    tone: SEVERITY_TONE[alert.severity] ?? "grey",
    severityLabel: SEVERITY_LABEL[alert.severity] ?? "For information",
    actionable,
    actionLabel: actionable ? alert.actionLabel || "Fix" : "",
  };
}

export interface AlertSummary {
  total: number;
  crit: number;
  warn: number;
  info: number;
  /** The worst severity present, or null when nothing is wrong. */
  worst: AlertSeverity | null;
  /** One line for the KPI tile: "2 critical · 1 warning" or "All clear". */
  headline: string;
}

/** Counts an alert list by severity and phrases the tile's subtitle. */
export function summarizeAlerts(alerts: DashboardAlert[]): AlertSummary {
  const summary: AlertSummary = {
    total: alerts.length,
    crit: 0,
    warn: 0,
    info: 0,
    worst: null,
    headline: "All clear",
  };
  for (const alert of alerts) {
    if (alert.severity === "crit") summary.crit++;
    else if (alert.severity === "warn") summary.warn++;
    else summary.info++;
  }
  if (summary.crit > 0) summary.worst = "crit";
  else if (summary.warn > 0) summary.worst = "warn";
  else if (summary.info > 0) summary.worst = "info";

  const parts: string[] = [];
  if (summary.crit > 0) parts.push(`${summary.crit} critical`);
  if (summary.warn > 0) parts.push(`${summary.warn} warning${summary.warn === 1 ? "" : "s"}`);
  if (summary.info > 0) parts.push(`${summary.info} to review`);
  if (parts.length > 0) summary.headline = parts.join(" · ");
  return summary;
}

export type TrendDirection = "up" | "down" | "flat";

export interface TrendDelta {
  direction: TrendDirection;
  /**
   * Percent change against the previous window, or null when there is no
   * baseline to divide by. A week that starts from zero has no percentage,
   * only a fact.
   */
  percent: number | null;
  /** What the tile prints under the number: "+24%", "-8%", "new", "no change". */
  label: string;
  /** Screen-reader text; the arrow glyph alone is not a description. */
  title: string;
}

/**
 * Week-over-week change.
 *
 * Deliberately unopinionated about whether a rise is good: cost going up and
 * runs going up are the same arithmetic and opposite news, so the direction
 * travels without a colour and the view renders it in neutral ink.
 */
export function weekOverWeek(current: number, previous: number, noun = "activity"): TrendDelta {
  const flat = (label: string, title: string): TrendDelta => ({
    direction: "flat",
    percent: previous === 0 ? null : 0,
    label,
    title,
  });

  if (!Number.isFinite(current) || !Number.isFinite(previous)) {
    return flat("no change", `No comparable ${noun} last week`);
  }
  if (previous === 0) {
    if (current === 0) return flat("no change", `No ${noun} in either week`);
    return {
      direction: "up",
      percent: null,
      label: "new",
      title: `First ${noun} recorded this week`,
    };
  }

  const change = ((current - previous) / previous) * 100;
  const rounded = Math.round(change);
  if (rounded === 0) {
    return {
      direction: "flat",
      percent: 0,
      label: "no change",
      title: `About the same ${noun} as last week`,
    };
  }
  return {
    direction: rounded > 0 ? "up" : "down",
    percent: rounded,
    label: `${rounded > 0 ? "+" : ""}${rounded}%`,
    title: `${Math.abs(rounded)}% ${rounded > 0 ? "more" : "less"} ${noun} than last week`,
  };
}

/** Lifecycle fallback for a project the monitor has no opinion about. */
const LIFECYCLE_TONE: Record<string, StatusTone> = {
  running: "green",
  provisioning: "amber",
  stopped: "grey",
  error: "red",
  missing: "red",
};

const LIFECYCLE_LABEL: Record<string, string> = {
  running: "Running",
  provisioning: "Provisioning",
  stopped: "Stopped",
  error: "Error",
  missing: "Missing — needs reprovision",
};

const HEALTH_TONE: Record<string, StatusTone> = {
  ok: "green",
  warn: "amber",
  crit: "red",
};

const HEALTH_LABEL: Record<string, string> = {
  ok: "Healthy",
  warn: "Degraded",
  crit: "Critical",
};

export interface ProjectDot {
  tone: StatusTone;
  label: string;
  title: string;
}

/**
 * The dot on a project card. Health wins when the monitor has a reading;
 * otherwise the container's own lifecycle does, so the dot is never blank
 * while a project is starting or stopped.
 */
export function projectDot(project: DashboardProject): ProjectDot {
  const tone = HEALTH_TONE[project.health];
  if (!tone) {
    const label = LIFECYCLE_LABEL[project.status] ?? "Unknown";
    return { tone: LIFECYCLE_TONE[project.status] ?? "grey", label, title: label };
  }
  const label = HEALTH_LABEL[project.health];
  const reasons = project.healthReasons ?? [];
  return { tone, label, title: [label, ...reasons].join("\n") };
}

/**
 * "in 4 min" / "overdue" for a scheduled task's countdown. Deadlines in the
 * past are the timer not having fired yet, so they read as overdue rather
 * than as a negative number.
 */
export function formatCountdown(nextRunAt: number, now: number): string {
  if (!nextRunAt) return "";
  const remaining = nextRunAt - now;
  if (remaining <= 0) return "due now";
  return `in ${formatDuration(remaining)}`;
}

/** "5 min ago" for a past instant; empty for an absent one. */
export function formatRelative(at: number | undefined, now: number): string {
  if (!at) return "";
  const elapsed = now - at;
  if (elapsed < 0) return "just now";
  if (elapsed < 45_000) return "just now";
  return `${formatDuration(elapsed)} ago`;
}

/** Largest useful unit, no false precision. */
export function formatDuration(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h`;
  const days = Math.round(hours / 24);
  return `${days} d`;
}

/** Compact byte sizes for the capacity line. */
export function formatBytes(bytes: number | undefined): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

/**
 * True when the payload has nothing on it at all — a fresh box whose home
 * screen should say so rather than render six empty cards.
 */
export function isEmptyDashboard(snapshot: DashboardSnapshot): boolean {
  return (
    snapshot.projects.length === 0 &&
    snapshot.alerts.length === 0 &&
    snapshot.recent.length === 0 &&
    snapshot.upcoming.length === 0
  );
}
