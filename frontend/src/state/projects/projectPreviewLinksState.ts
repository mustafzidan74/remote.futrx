import type { ContainerApp, ProjectShare } from "../../models/project";
import { isShareExpired, isShareablePort } from "./projectShareState.ts";

/**
 * Quick-access preview links: the state behind the sidebar's Preview popover
 * and the chat header's Preview chip. Both show the same thing — the ports a
 * project is listening on right now — so the selection and share rules live
 * here rather than in either component.
 */

/**
 * Ports move while an agent works, so the list is re-read whenever a popover
 * opens and, at most, this often while it stays open. Discovery runs `ss`
 * inside the container, so it is not free enough to poll continuously.
 */
export const PREVIEW_REFRESH_INTERVAL_MS = 15_000;

/** Lifetime of the one-click "Share 24h" link. */
export const PREVIEW_SHARE_TTL_HOURS = 24;

export interface PreviewPortRow {
  port: number;
  /** Process name reported by the listener scan, when it could read one. */
  process?: string;
  /** Platform plumbing (noVNC, IDE proxy, CDP) is listed but never shareable. */
  shareable: boolean;
  /** Live public links already pointing at this port. */
  shareCount: number;
}

/**
 * One row per listening port. Platform ports stay visible — an operator who
 * sees :6080 in the list understands why it has no Share action — but they
 * sort below the app ports so the interesting one is on top.
 */
export function previewPortRows(
  apps: ContainerApp[],
  shares: ProjectShare[],
  now: number,
): PreviewPortRow[] {
  const rows = new Map<number, PreviewPortRow>();
  for (const app of apps) {
    if (!Number.isInteger(app.port) || rows.has(app.port)) continue;
    rows.set(app.port, {
      port: app.port,
      process: app.process,
      shareable: isShareablePort(app.port),
      shareCount: 0,
    });
  }
  for (const share of shares) {
    const row = rows.get(share.port);
    if (!row || isShareExpired(share, now)) continue;
    row.shareCount += 1;
  }
  return [...rows.values()].sort((left, right) => {
    if (left.shareable !== right.shareable) return left.shareable ? -1 : 1;
    return left.port - right.port;
  });
}

/**
 * The port the chip opens: a port that already has a public link wins (someone
 * decided that one is the app), otherwise the lowest shareable port.
 */
export function preferredPreviewPort(rows: PreviewPortRow[]): number | null {
  const shareable = rows.filter((row) => row.shareable);
  if (shareable.length === 0) return null;
  const shared = shareable.filter((row) => row.shareCount > 0);
  const pool = shared.length > 0 ? shared : shareable;
  return pool.reduce((lowest, row) => (row.port < lowest ? row.port : lowest), pool[0].port);
}

export function hasPreviewPort(rows: PreviewPortRow[]): boolean {
  return preferredPreviewPort(rows) !== null;
}

export function previewChipLabel(port: number): string {
  return `Preview :${port}`;
}

/** Why a project has no ports to offer, when the reason is not "none yet". */
export type PreviewUnavailableReason = "stopped" | "provisioning" | "missing" | null;

export function previewUnavailableReason(status: string): PreviewUnavailableReason {
  if (status === "provisioning") return "provisioning";
  if (status === "stopped") return "stopped";
  if (status === "missing") return "missing";
  return null;
}

/* ------------------------------------------------------------------ *
 * Copy / share feedback
 * ------------------------------------------------------------------ */

export type PreviewLinkAction = "copy" | "share";
export type PreviewLinkStatus = "idle" | "working" | "done" | "error";

export interface PreviewLinkFeedback {
  status: PreviewLinkStatus;
  action: PreviewLinkAction | null;
  port: number | null;
  /** Set on success. For a share this is the one-time token URL. */
  url?: string;
  /** False when the clipboard write was refused and the user must copy by hand. */
  copied?: boolean;
  error?: string;
}

export type PreviewLinkEvent =
  | { type: "start"; action: PreviewLinkAction; port: number }
  | { type: "done"; action: PreviewLinkAction; port: number; url: string; copied: boolean }
  | { type: "failed"; action: PreviewLinkAction; port: number; error: string }
  | { type: "clear" };

export const previewLinkFeedbackInitial: PreviewLinkFeedback = {
  status: "idle",
  action: null,
  port: null,
};

/**
 * One in-flight copy/share at a time. Results that do not match the request
 * still in flight are dropped, so a slow share for :3000 can never overwrite
 * the feedback for a later click on :5173.
 */
export function previewLinkFeedbackReduce(
  state: PreviewLinkFeedback,
  event: PreviewLinkEvent,
): PreviewLinkFeedback {
  switch (event.type) {
    case "start":
      return { status: "working", action: event.action, port: event.port };
    case "done":
      if (!isInFlight(state, event.action, event.port)) return state;
      return {
        status: "done",
        action: event.action,
        port: event.port,
        url: event.url,
        copied: event.copied,
      };
    case "failed":
      if (!isInFlight(state, event.action, event.port)) return state;
      return { status: "error", action: event.action, port: event.port, error: event.error };
    case "clear":
      return previewLinkFeedbackInitial;
  }
}

function isInFlight(
  state: PreviewLinkFeedback,
  action: PreviewLinkAction,
  port: number,
): boolean {
  return state.status === "working" && state.action === action && state.port === port;
}

export function isPreviewLinkBusy(
  state: PreviewLinkFeedback,
  action: PreviewLinkAction,
  port: number,
): boolean {
  return isInFlight(state, action, port);
}

export function isPreviewLinkDone(
  state: PreviewLinkFeedback,
  action: PreviewLinkAction,
  port: number,
): boolean {
  return state.status === "done" && state.action === action && state.port === port;
}

export function previewLinkError(
  state: PreviewLinkFeedback,
  port: number,
): string | undefined {
  return state.status === "error" && state.port === port ? state.error : undefined;
}

/** The issued share link, while it is still the newest thing that happened. */
export function issuedShareUrl(state: PreviewLinkFeedback): string | undefined {
  return state.status === "done" && state.action === "share" ? state.url : undefined;
}
