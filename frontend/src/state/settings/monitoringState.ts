import type { MonitoringSettings } from "../../models/monitoring";

/**
 * Fallback bounds for the heartbeat cadence. The server sends its own with
 * every GET; these keep the form usable before the first response lands, and
 * they mirror `MinIntervalMinutes`/`MaxIntervalMinutes` in
 * `backend/internal/service/monitoring/model.go`.
 */
export const HEARTBEAT_INTERVAL_BOUNDS = { min: 1, max: 60 } as const;

export interface IntervalBounds {
  min: number;
  max: number;
}

/** The editable half of the Monitoring panel. */
export interface HeartbeatForm {
  enabled: boolean;
  /** Blank means "keep the URL the server already stores". */
  heartbeatUrl: string;
  intervalMinutes: number;
  /** Whether the server currently holds a URL at all. */
  configured: boolean;
}

/**
 * Validates the form the same way the backend does, so the panel refuses what
 * the API would refuse and the operator learns it before a round trip.
 * Returning the first problem keeps the single error line honest about what
 * to fix next.
 */
export function validateHeartbeatForm(
  form: HeartbeatForm,
  bounds: IntervalBounds = HEARTBEAT_INTERVAL_BOUNDS,
): string | undefined {
  const url = form.heartbeatUrl.trim();
  if (url && !isAbsoluteHttpUrl(url)) {
    return "The heartbeat URL must be an absolute http(s) URL.";
  }
  if (form.enabled && !url && !form.configured) {
    return "Paste a heartbeat URL before turning the heartbeat on.";
  }
  if (
    !Number.isInteger(form.intervalMinutes) ||
    form.intervalMinutes < bounds.min ||
    form.intervalMinutes > bounds.max
  ) {
    return `The heartbeat interval must be between ${bounds.min} and ${bounds.max} minutes.`;
  }
  return undefined;
}

/** Mirrors the backend's URL check: absolute, http or https, with a host. */
export function isAbsoluteHttpUrl(value: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(value.trim());
  } catch {
    return false;
  }
  return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname !== "";
}

export type HeartbeatTone = "ok" | "failed" | "idle";

/**
 * The last-push line. The timestamp travels as a number so the component
 * formats it in the reader's locale and this stays testable.
 */
export interface HeartbeatStatus {
  tone: HeartbeatTone;
  label: string;
  at?: number;
  detail?: string;
}

export function describeLastPing(settings: MonitoringSettings | null): HeartbeatStatus {
  if (!settings?.lastPingAt || !settings.lastPingStatus) {
    return { tone: "idle", label: "No heartbeat sent yet" };
  }
  if (settings.lastPingStatus === "ok") {
    return { tone: "ok", label: "Delivered", at: settings.lastPingAt };
  }
  return {
    tone: "failed",
    label: "Failed",
    at: settings.lastPingAt,
    detail: settings.lastPingError,
  };
}

/**
 * Whole minutes until the ticker is next due, or undefined when nothing is
 * scheduled. Zero means the next tick will push. Attempts count whether or
 * not they succeeded, so a failing URL reads as "retrying in N minutes"
 * rather than as a silent stall.
 */
export function minutesUntilNextPing(
  settings: MonitoringSettings | null,
  now: number,
): number | undefined {
  if (!settings?.enabled || !settings.configured) return undefined;
  if (!settings.lastPingAt) return 0;
  const dueAt = settings.lastPingAt + settings.intervalMinutes * 60_000;
  return Math.max(0, Math.ceil((dueAt - now) / 60_000));
}

/**
 * The absolute health-check URL an operator pastes into an external monitor.
 * It is built from the browser's own origin, which is the same host the
 * monitor will poll.
 */
export function healthCheckUrl(origin: string, healthPath: string): string {
  const base = origin.replace(/\/+$/, "");
  const path = healthPath.startsWith("/") ? healthPath : `/${healthPath}`;
  return `${base}${path}`;
}
