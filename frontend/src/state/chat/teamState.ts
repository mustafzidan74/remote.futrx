import type {
  ChatMeta,
  ChatProvider,
  TeamHop,
  TeamPatch,
  TeamPolicy,
  TeamRole,
  TeamRoleName,
  TeamVerdict,
} from "../../models/chat.ts";
import { providerDisplayLabel } from "../../config/chatCatalog.ts";

/**
 * Team-mode state for the composer, the header pill, and the Team panel.
 *
 * The server owns the loop: it spends the loop counter, it decides the phase,
 * and it assigns the companion chats. Everything here is derivation and input
 * validation. The bounds and the provider-fallback order mirror
 * backend/internal/service/chat/team_policy.go and
 * backend/internal/service/team/policy.go, so a cast this module accepts is
 * one the API will accept and one the orchestrator will actually run.
 */

export const TEAM_DEFAULT_LOOPS = 2;
export const TEAM_MIN_LOOPS = 1;
export const TEAM_MAX_LOOPS = 5;

/** The badge the Run options button prints when team mode is on. */
export const TEAM_BADGE = "👥";

export const TEAM_ROLE_LABELS: Record<TeamRoleName, string> = {
  implementer: "Implementer",
  reviewer: "Reviewer",
  tester: "Tester",
};

/**
 * The order a second opinion is picked in, mirroring reviewerPreference in the
 * team service. Antigravity is absent on purpose: its print mode returns plain
 * text without structured events, which is a poor fit for a verdict pass.
 */
const REVIEWER_PREFERENCE: ChatProvider[] = ["codex", "kimi", "claude"];

export interface TeamRoleView {
  role: TeamRoleName;
  label: string;
  provider: ChatProvider;
  /** True when the provider was chosen by the platform rather than the user. */
  resolved: boolean;
  model: string;
  enabled: boolean;
  chatId: string;
}

export interface TeamView {
  enabled: boolean;
  maxLoops: number;
  loopsUsed: number;
  autoFix: boolean;
  phase: TeamPolicy["phase"];
  verdict: TeamVerdict;
  implementer: TeamRoleView;
  reviewer: TeamRoleView;
  tester: TeamRoleView;
  hops: TeamHopView[];
  /** "👥 Team: reviewing…" — the header pill. */
  pillLabel: string;
  /** The longer sentence the pill's tooltip and the panel header show. */
  status: string;
  /** True while a hop is in flight, which is when the pill animates. */
  running: boolean;
  /**
   * True when no second provider is connected, so the reviewer runs on the
   * implementer's provider. The panel says so rather than implying a second
   * opinion it cannot give.
   */
  singleProvider: boolean;
}

export interface TeamHopView {
  key: string;
  role: TeamRoleName;
  label: string;
  loop: number;
  chatId: string;
  verdict: TeamVerdict;
  verdictLabel: string;
  findings: string;
  at: number;
  /** "Reviewer · loop 1 · 12:41" — the timeline row's subtitle. */
  detail: string;
}

const EMPTY_ROLE: TeamRole = { enabled: false };

/**
 * Reads a chat's stored team policy, filling in the same fallbacks the server
 * would. `connected` is the list of providers with a usable host-side login,
 * which the auth context already tracks.
 */
export function teamView(chat: ChatMeta, connected: ChatProvider[]): TeamView {
  const policy: TeamPolicy = chat.team ?? { enabled: false, roles: emptyRoles() };
  const roles = policy.roles ?? emptyRoles();
  const chatProvider = (chat.provider ?? "codex") as ChatProvider;

  const implementerProvider = (roles.implementer?.provider || chatProvider) as ChatProvider;
  const reviewerProvider = (roles.reviewer?.provider ||
    reviewerFallback(implementerProvider, connected)) as ChatProvider;
  const testerProvider = (roles.tester?.provider || reviewerProvider) as ChatProvider;

  const maxLoops = boundedLoops(policy.maxLoops);
  const loopsUsed = Math.max(0, Math.trunc(policy.loopsUsed ?? 0));
  const phase = policy.phase ?? "";
  const verdict = policy.verdict ?? "";

  return {
    enabled: !!policy.enabled,
    maxLoops,
    loopsUsed,
    autoFix: policy.autoFix !== false,
    phase,
    verdict,
    implementer: roleView("implementer", roles.implementer, implementerProvider, true),
    reviewer: roleView("reviewer", roles.reviewer, reviewerProvider, false),
    tester: roleView("tester", roles.tester, testerProvider, false),
    hops: (policy.hops ?? []).map(hopView),
    pillLabel: teamPillLabel(phase, loopsUsed, maxLoops, verdict),
    status: teamStatus(!!policy.enabled, phase, loopsUsed, maxLoops, verdict),
    running: teamRunning(phase),
    singleProvider: reviewerProvider === implementerProvider,
  };
}

/** True while the loop has a hop in flight. */
export function teamRunning(phase: TeamPolicy["phase"]): boolean {
  return phase === "reviewing" || phase === "testing" || phase === "fixing";
}

/**
 * The header pill. It names the hop rather than the fact that team mode is on,
 * because "on" is already visible from the pill existing at all.
 */
export function teamPillLabel(
  phase: TeamPolicy["phase"],
  loopsUsed: number,
  maxLoops: number,
  verdict: TeamVerdict,
): string {
  switch (phase) {
    case "reviewing":
      return "Team: reviewing…";
    case "testing":
      return "Team: testing…";
    case "fixing":
      return `Team: fix ${Math.max(1, loopsUsed)}/${maxLoops}`;
    case "done":
      return `Team: ${verdictLabel(verdict) || "done"}`;
    case "error":
      return "Team: stopped";
    default:
      return "Team: ready";
  }
}

/** The sentence the tooltip and the panel header show. */
export function teamStatus(
  enabled: boolean,
  phase: TeamPolicy["phase"],
  loopsUsed: number,
  maxLoops: number,
  verdict: TeamVerdict,
): string {
  if (!enabled) return "Off";
  const spent = `loop ${loopsUsed}/${maxLoops}`;
  switch (phase) {
    case "reviewing":
      return `The reviewer is reading the diff · ${spent}`;
    case "testing":
      return `The tester is running Playwright · ${spent}`;
    case "fixing":
      return `The implementer is addressing findings · ${spent}`;
    case "done":
      return `Finished — ${verdictSentence(verdict)} · ${spent}`;
    case "error":
      return `Stopped early — open the companion chat to see why · ${spent}`;
    default:
      return `Armed — the next turn starts a review · ${spent}`;
  }
}

/** "SHIP", "FIX", "PASS", "FAIL" — the word a timeline row shows. */
export function verdictLabel(verdict: TeamVerdict): string {
  switch (verdict) {
    case "ship":
      return "SHIP";
    case "fix":
      return "FIX";
    case "pass":
      return "PASS";
    case "fail":
      return "FAIL";
    case "unknown":
      return "no verdict";
    default:
      return "";
  }
}

function verdictSentence(verdict: TeamVerdict): string {
  switch (verdict) {
    case "pass":
      return "reviewed and tested";
    case "ship":
      return "reviewed, no test pass configured";
    case "fail":
      return "tests failed, a human is needed";
    case "fix":
      return "the reviewer still says fix";
    default:
      return "nothing left to do";
  }
}

/**
 * The reviewer the platform would pick: a *different* connected provider where
 * one exists, because a model reviewing its own output is the failure team mode
 * exists to avoid. With one provider connected it falls back to that provider,
 * and the fresh eyes come from the companion chat's empty context instead.
 */
export function reviewerFallback(
  implementer: ChatProvider,
  connected: ChatProvider[],
): ChatProvider {
  const available = new Set(connected);
  for (const candidate of REVIEWER_PREFERENCE) {
    if (candidate !== implementer && available.has(candidate)) return candidate;
  }
  return implementer;
}

/**
 * The cast the switch arms on its own: both companion seats on, each with the
 * provider the platform would pick. Sent as one patch so the server records
 * the choice the user could see rather than re-deriving it later.
 */
export function armTeamPatch(chat: ChatMeta, connected: ChatProvider[]): TeamPatch {
  const view = teamView(chat, connected);
  return {
    enabled: true,
    maxLoops: view.maxLoops,
    autoFix: view.autoFix,
    roles: {
      implementer: { provider: view.implementer.provider, enabled: true },
      reviewer: { provider: view.reviewer.provider, enabled: true },
      tester: { provider: view.tester.provider, enabled: true },
    },
  };
}

/** Clamps a loop count the same way the server does. */
export function boundedLoops(value: number | undefined): number {
  if (!value) return TEAM_DEFAULT_LOOPS;
  return Math.min(TEAM_MAX_LOOPS, Math.max(TEAM_MIN_LOOPS, Math.trunc(value)));
}

/**
 * Only providers with a host-side login may fill a seat: offering a provider
 * nobody logged in to produces a run that fails on its first hop.
 */
export function teamProviderOptions(
  connected: ChatProvider[],
): { value: ChatProvider; label: string }[] {
  return connected.map((provider) => ({
    value: provider,
    label: providerDisplayLabel(provider),
  }));
}

/**
 * Companion chats are opened from the Team panel, not the sidebar, so one team
 * session adds one row to the chat list rather than three.
 */
export function isCompanionChat(chat: ChatMeta): boolean {
  return !!chat.companionOf;
}

function roleView(
  role: TeamRoleName,
  seat: TeamRole | undefined,
  provider: ChatProvider,
  alwaysEnabled: boolean,
): TeamRoleView {
  const stored = seat ?? EMPTY_ROLE;
  return {
    role,
    label: TEAM_ROLE_LABELS[role],
    provider,
    resolved: !stored.provider,
    model: stored.model ?? "",
    enabled: alwaysEnabled || !!stored.enabled,
    chatId: stored.chatId ?? "",
  };
}

function hopView(hop: TeamHop, index: number): TeamHopView {
  const role = (hop.role ?? "implementer") as TeamRoleName;
  const label = TEAM_ROLE_LABELS[role] ?? "Team";
  const parts = [label, `loop ${hop.loop ?? 0}`];
  if (hop.at) parts.push(clockTime(hop.at));
  return {
    key: `${hop.at ?? 0}-${index}`,
    role,
    label,
    loop: hop.loop ?? 0,
    chatId: hop.chatId ?? "",
    verdict: hop.verdict ?? "",
    verdictLabel: verdictLabel(hop.verdict ?? ""),
    findings: hop.findings ?? "",
    at: hop.at ?? 0,
    detail: parts.join(" · "),
  };
}

function emptyRoles() {
  return {
    implementer: { ...EMPTY_ROLE, enabled: true },
    reviewer: { ...EMPTY_ROLE },
    tester: { ...EMPTY_ROLE },
  };
}

function clockTime(epochMilli: number): string {
  const date = new Date(epochMilli);
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  return `${hours}:${minutes}`;
}
