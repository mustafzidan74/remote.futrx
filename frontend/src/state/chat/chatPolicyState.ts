import type {
  AutoTestPolicy,
  AutopilotPatch,
  AutopilotPolicy,
  ChatMeta,
} from "../../models/chat";

/**
 * Post-run policy state for the composer.
 *
 * The server owns the counters — it is the only thing that can spend a round —
 * so everything here is derivation and input validation. The bounds mirror
 * backend/internal/service/chat/policy.go: a value this module accepts is one
 * the API will accept, so the popover can disable its own Save rather than
 * discovering a 400.
 */

export const AUTOPILOT_DEFAULT_ROUNDS = 8;
export const AUTOPILOT_DEFAULT_DURATION_MIN = 120;

export const AUTOPILOT_MIN_ROUNDS = 1;
export const AUTOPILOT_MAX_ROUNDS = 50;
export const AUTOPILOT_MIN_DURATION_MIN = 5;
export const AUTOPILOT_MAX_DURATION_MIN = 1440;

export interface AutopilotView {
  enabled: boolean;
  maxRounds: number;
  maxDurationMin: number;
  roundsUsed: number;
  startedAt: number;
  /** "round 3/8 · started 12:40 · running" — the popover's status line. */
  status: string;
  /** "Autopilot on · round 3/8" — the header pill. */
  pillLabel: string;
}

/** Reads a chat's stored autopilot policy, filling in the documented defaults. */
export function autopilotView(chat: ChatMeta, streaming: boolean): AutopilotView {
  const policy: AutopilotPolicy = chat.autopilot ?? { enabled: false };
  const maxRounds = boundedRounds(policy.maxRounds);
  const maxDurationMin = boundedDuration(policy.maxDurationMin);
  const roundsUsed = Math.max(0, Math.trunc(policy.roundsUsed ?? 0));
  const startedAt = policy.startedAt ?? 0;
  return {
    enabled: !!policy.enabled,
    maxRounds,
    maxDurationMin,
    roundsUsed,
    startedAt,
    status: autopilotStatus(!!policy.enabled, roundsUsed, maxRounds, startedAt, streaming),
    pillLabel: `Autopilot on · round ${roundsUsed}/${maxRounds}`,
  };
}

export function autoTestEnabled(chat: ChatMeta): boolean {
  const policy: AutoTestPolicy | undefined = chat.autoTest;
  return !!policy?.enabled;
}

/**
 * The popover's one-line status. A disarmed chat says so rather than showing a
 * stale round count, because "round 3/8" next to an off switch reads as if the
 * loop were still running.
 */
export function autopilotStatus(
  enabled: boolean,
  roundsUsed: number,
  maxRounds: number,
  startedAt: number,
  streaming: boolean,
): string {
  if (!enabled) return "Off";
  const parts = [`round ${roundsUsed}/${maxRounds}`];
  if (startedAt > 0) parts.push(`started ${clockTime(startedAt)}`);
  // "waiting" is the honest word between rounds: the loop is armed, but the
  // follow-up prompt has not been sent yet.
  parts.push(streaming ? "running" : "waiting");
  return parts.join(" · ");
}

/** Minutes left in the wall-clock budget, or null once it is spent. */
export function autopilotMinutesLeft(
  view: Pick<AutopilotView, "startedAt" | "maxDurationMin">,
  now = new Date(),
): number | null {
  if (!view.startedAt) return null;
  const elapsedMin = (now.getTime() - view.startedAt) / 60000;
  const left = Math.ceil(view.maxDurationMin - elapsedMin);
  return left > 0 ? left : null;
}

export interface AutopilotDraft {
  maxRounds: string;
  maxDurationMin: string;
}

export function autopilotDraftFrom(view: AutopilotView): AutopilotDraft {
  return {
    maxRounds: String(view.maxRounds),
    maxDurationMin: String(view.maxDurationMin),
  };
}

export interface AutopilotDraftValidation {
  valid: boolean;
  /** The reason the popover shows next to a rejected field. */
  error: string | null;
  /** The patch to send, present only when valid. */
  patch: AutopilotPatch | null;
}

/**
 * Validates a draft against the same bounds the server enforces. Blank fields
 * mean "leave it alone" rather than "zero", so clearing a box while typing
 * never produces a rejected request.
 */
export function validateAutopilotDraft(draft: AutopilotDraft): AutopilotDraftValidation {
  const patch: AutopilotPatch = {};

  const rounds = parseLimit(draft.maxRounds);
  if (rounds === "invalid") {
    return { valid: false, error: "Rounds must be a whole number.", patch: null };
  }
  if (rounds !== null) {
    if (rounds < AUTOPILOT_MIN_ROUNDS || rounds > AUTOPILOT_MAX_ROUNDS) {
      return {
        valid: false,
        error: `Rounds must be between ${AUTOPILOT_MIN_ROUNDS} and ${AUTOPILOT_MAX_ROUNDS}.`,
        patch: null,
      };
    }
    patch.maxRounds = rounds;
  }

  const duration = parseLimit(draft.maxDurationMin);
  if (duration === "invalid") {
    return { valid: false, error: "Minutes must be a whole number.", patch: null };
  }
  if (duration !== null) {
    if (duration < AUTOPILOT_MIN_DURATION_MIN || duration > AUTOPILOT_MAX_DURATION_MIN) {
      return {
        valid: false,
        error: `Minutes must be between ${AUTOPILOT_MIN_DURATION_MIN} and ${AUTOPILOT_MAX_DURATION_MIN}.`,
        patch: null,
      };
    }
    patch.maxDurationMin = duration;
  }

  return { valid: true, error: null, patch };
}

/**
 * The patch that arms autopilot with the drafted limits. Arming and setting
 * the limits travel together so the server resets the round counter against
 * the budget the user just chose, not the previous one.
 */
export function armAutopilotPatch(draft: AutopilotDraft): AutopilotPatch | null {
  const validation = validateAutopilotDraft(draft);
  if (!validation.valid || !validation.patch) return null;
  return { ...validation.patch, enabled: true };
}

function parseLimit(raw: string): number | null | "invalid" {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  if (!/^\d+$/.test(trimmed)) return "invalid";
  return Number.parseInt(trimmed, 10);
}

function boundedRounds(value: number | undefined): number {
  return bounded(value, AUTOPILOT_DEFAULT_ROUNDS, AUTOPILOT_MIN_ROUNDS, AUTOPILOT_MAX_ROUNDS);
}

function boundedDuration(value: number | undefined): number {
  return bounded(
    value,
    AUTOPILOT_DEFAULT_DURATION_MIN,
    AUTOPILOT_MIN_DURATION_MIN,
    AUTOPILOT_MAX_DURATION_MIN,
  );
}

function bounded(value: number | undefined, fallback: number, low: number, high: number): number {
  if (!value) return fallback;
  return Math.min(high, Math.max(low, Math.trunc(value)));
}

function clockTime(epochMilli: number): string {
  const date = new Date(epochMilli);
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${hours}:${minutes}`;
}
