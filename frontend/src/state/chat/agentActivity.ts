import type { ChatEvent } from "../../models/chat.ts";
import type { AssistantMessagePart } from "../../models/chatMessage.ts";
import { shortenPath } from "../../shared/format.ts";

/**
 * What a run is doing right now, and what each step of a finished turn was.
 *
 * Everything here is a pure function of the chat events the browser already
 * holds: no new backend data, no polling, no timers. The one moving part a
 * caller must supply is `now`, which is why elapsed time and the stuck
 * threshold are arguments rather than reads of the clock — a reducer that
 * calls `Date.now()` is a reducer you cannot test.
 */

type ToolPart = Extract<AssistantMessagePart, { kind: "tool" }>;

/**
 * The four things a run can be doing, in the priority the strip resolves them:
 * a tool in flight beats reasoning, reasoning beats answer text, and a run
 * that has produced none of the three is still starting.
 */
export type ActivityPhase = "idle" | "starting" | "thinking" | "tool" | "writing";

/** How long a run may say nothing before the strip admits it has heard nothing. */
export const ACTIVITY_STUCK_MS = 90_000;

/** Abbreviations the sidebar row and the header pill share. */
export const PHASE_ABBREVIATION: Record<ActivityPhase, string> = {
  idle: "",
  starting: "starting",
  thinking: "thinking",
  tool: "tool",
  writing: "writing",
};

export interface AgentActivity {
  phase: ActivityPhase;
  /** The tool in flight, or "" when none is. */
  toolName: string;
  toolInput: Record<string, unknown> | null;
  /** First event of the current run, in ms; 0 when no run is in flight. */
  startedAt: number;
  /** Newest event of the current run, in ms. */
  lastEventAt: number;
  /** Reasoning streamed during the current run. */
  reasoning: string;
  /**
   * True once any run in this conversation streamed reasoning. Sticky on
   * purpose: a provider that thinks out loud does so for the whole chat, and a
   * "Show thinking" toggle that appears and disappears between turns is worse
   * than one that is simply absent for providers which never reason.
   */
  sawReasoning: boolean;
  /** Tokens the provider reported for the current run; 0 means "not said". */
  tokens: number;
}

export function emptyActivity(): AgentActivity {
  return {
    phase: "idle",
    toolName: "",
    toolInput: null,
    startedAt: 0,
    lastEventAt: 0,
    reasoning: "",
    sawReasoning: false,
    tokens: 0,
  };
}

/**
 * Folds one chat event into the live activity.
 *
 * A `user` event starts a run and clears everything run-scoped; `complete` and
 * `error` end one. Anything in between advances the phase, so replaying a
 * whole conversation from the start lands on the same activity the live stream
 * would have produced.
 */
export function reduceActivity(state: AgentActivity, event: ChatEvent): AgentActivity {
  const t = event.t || state.lastEventAt;
  switch (event.type) {
    case "user":
      return {
        ...emptyActivity(),
        phase: "starting",
        sawReasoning: state.sawReasoning,
        startedAt: t,
        lastEventAt: t,
      };
    case "thinking":
      return {
        ...state,
        phase: state.toolName ? "tool" : "thinking",
        // A run resumed from history has no `user` event in the loaded page.
        startedAt: state.startedAt || t,
        lastEventAt: t,
        reasoning: state.reasoning + event.text,
        sawReasoning: true,
      };
    case "assistant_text":
      return {
        ...state,
        phase: state.toolName ? "tool" : "writing",
        startedAt: state.startedAt || t,
        lastEventAt: t,
      };
    case "tool_use_start":
      return {
        ...state,
        phase: "tool",
        toolName: event.name,
        toolInput: event.input ?? {},
        startedAt: state.startedAt || t,
        lastEventAt: t,
      };
    case "tool_use_end":
      return {
        ...state,
        // The tool is done, so the model is deciding what to do next. That is
        // reasoning even when the provider does not stream any.
        phase: state.phase === "idle" ? "idle" : "thinking",
        toolName: "",
        toolInput: null,
        lastEventAt: t,
      };
    case "complete":
      return {
        ...state,
        phase: "idle",
        toolName: "",
        toolInput: null,
        lastEventAt: t,
        tokens: state.tokens + totalTokens(event.usage),
      };
    case "error":
      return { ...state, phase: "idle", toolName: "", toolInput: null, lastEventAt: t };
    default:
      // `system`, `session`, and permission prompts prove the run is alive
      // without changing what it is doing — which is exactly what the stuck
      // hint needs to know.
      return state.phase === "idle" ? state : { ...state, lastEventAt: t };
  }
}

function totalTokens(usage: Extract<ChatEvent, { type: "complete" }>["usage"]): number {
  if (!usage) return 0;
  return (
    (usage.input_tokens ?? 0) +
    (usage.output_tokens ?? 0) +
    (usage.cache_read_input_tokens ?? 0) +
    (usage.cache_creation_input_tokens ?? 0)
  );
}

/* ------------------------------------------------------------------ */
/* Tool vocabulary                                                     */
/* ------------------------------------------------------------------ */

export interface ToolDescription {
  icon: string;
  /** A verb phrase — "Reading", "Running", "Searching the workspace". */
  label: string;
  /** The short thing being acted on, safe to render on one line. */
  target: string;
  /** The unabbreviated target, for a `title` attribute. */
  title: string;
  /** "+12 −3" when the payload carries an edit, "" otherwise. */
  detail: string;
}

type ToolInput = Record<string, unknown> | null | undefined;

interface ToolEntry {
  icon: string;
  label: string;
  describe?: (input: Record<string, unknown>) => Partial<ToolDescription>;
}

/**
 * Friendly names for the tools the connected CLIs actually call.
 *
 * The table is deliberately flat and provider-neutral: Claude spells its shell
 * tool `Bash`, Codex spells the same idea `command_execution`, and both should
 * read as "Running" to the person watching. Anything missing falls back to the
 * raw tool name rather than to a vague "working" — a name you can search for
 * beats a label that hides which tool stalled.
 */
export const TOOL_LABELS: Record<string, ToolEntry> = {
  Read: { icon: "📄", label: "Reading", describe: (input) => filePath(input) },
  NotebookRead: { icon: "📄", label: "Reading", describe: (input) => filePath(input, "notebook_path") },
  Write: { icon: "💾", label: "Saving", describe: (input) => filePath(input) },
  Edit: { icon: "✏️", label: "Editing", describe: editTarget },
  MultiEdit: { icon: "✏️", label: "Editing", describe: editTarget },
  NotebookEdit: { icon: "✏️", label: "Editing", describe: (input) => filePath(input, "notebook_path") },
  file_change: { icon: "✏️", label: "Editing", describe: (input) => filePath(input) },
  apply_patch: { icon: "✏️", label: "Editing", describe: (input) => filePath(input) },
  Bash: { icon: "⚡", label: "Running", describe: commandTarget },
  BashOutput: { icon: "⚡", label: "Reading command output" },
  KillShell: { icon: "⚡", label: "Stopping a command" },
  PowerShell: { icon: "⚡", label: "Running", describe: commandTarget },
  shell: { icon: "⚡", label: "Running", describe: commandTarget },
  command_execution: { icon: "⚡", label: "Running", describe: commandTarget },
  Glob: { icon: "🔎", label: "Searching the workspace", describe: searchTarget },
  Grep: { icon: "🔎", label: "Searching the workspace", describe: searchTarget },
  Search: { icon: "🔎", label: "Searching the workspace", describe: searchTarget },
  WebFetch: { icon: "🌐", label: "Fetching", describe: urlTarget },
  fetch: { icon: "🌐", label: "Fetching", describe: urlTarget },
  WebSearch: { icon: "🌐", label: "Searching the web", describe: searchTarget },
  web_search: { icon: "🌐", label: "Searching the web", describe: searchTarget },
  Task: { icon: "🤖", label: "Delegating to a subagent", describe: taskTarget },
  Agent: { icon: "🤖", label: "Delegating to a subagent", describe: taskTarget },
  TodoWrite: { icon: "☑️", label: "Updating the plan" },
  ExitPlanMode: { icon: "📋", label: "Presenting the plan" },
  AskUserQuestion: { icon: "❓", label: "Asking you a question" },
  Skill: { icon: "🧩", label: "Using a skill", describe: (input) => plain(text(input.skill) || text(input.name)) },
  ToolSearch: { icon: "🔎", label: "Looking up a tool", describe: searchTarget },
};

/** Icon and phrase for a tool call, whether or not the table knows the tool. */
export function describeTool(name: string, input: ToolInput): ToolDescription {
  const payload = input ?? {};
  const entry = TOOL_LABELS[name];
  if (entry) {
    return {
      icon: entry.icon,
      label: entry.label,
      target: "",
      title: "",
      detail: "",
      ...(entry.describe ? entry.describe(payload) : {}),
    };
  }
  // MCP tools arrive as `mcp__server__tool`. Splitting them reads far better
  // than the raw identifier while keeping the identifier in the tooltip.
  if (name.startsWith("mcp__")) {
    const segments = name.slice(5).split("__").filter(Boolean);
    return {
      icon: "🔌",
      label: "Using",
      target: segments.join(" · ") || name,
      title: name,
      detail: "",
    };
  }
  return { icon: "🛠️", label: "Running", target: name, title: name, detail: "" };
}

function filePath(input: Record<string, unknown>, key = "file_path"): Partial<ToolDescription> {
  const path = text(input[key]) || text(input.path) || text(input.filename);
  if (!path) return {};
  return { target: baseName(path), title: shortenPath(path) };
}

function editTarget(input: Record<string, unknown>): Partial<ToolDescription> {
  return { ...filePath(input), detail: editDetail(input) };
}

/**
 * "+12 −3" when the payload lets us count it. Claude's `Edit` carries the two
 * strings, `MultiEdit` an array of them; anything else gets no counter rather
 * than a guessed one.
 */
function editDetail(input: Record<string, unknown>): string {
  const edits = Array.isArray(input.edits)
    ? (input.edits as Array<Record<string, unknown>>)
    : [input];
  let added = 0;
  let removed = 0;
  let sawStrings = false;
  for (const edit of edits) {
    if (!edit || typeof edit !== "object") continue;
    const before = text(edit.old_string);
    const after = text(edit.new_string);
    if (!before && !after) continue;
    sawStrings = true;
    removed += lineCount(before);
    added += lineCount(after);
  }
  if (!sawStrings) return "";
  return `+${added} −${removed}`;
}

function lineCount(value: string): number {
  if (!value) return 0;
  return value.split("\n").length;
}

function commandTarget(input: Record<string, unknown>): Partial<ToolDescription> {
  const command = text(input.command) || text(input.cmd) || joinArgv(input.argv);
  if (!command) return {};
  const oneLine = command.replace(/\s+/g, " ").trim();
  return { target: clip(oneLine, 72), title: command };
}

function joinArgv(value: unknown): string {
  return Array.isArray(value) ? value.map((part) => String(part)).join(" ") : "";
}

function searchTarget(input: Record<string, unknown>): Partial<ToolDescription> {
  const needle = text(input.pattern) || text(input.query) || text(input.q);
  const where = text(input.path);
  if (!needle) return where ? plain(shortenPath(where)) : {};
  const full = where ? `${needle} in ${shortenPath(where)}` : needle;
  return { target: clip(full, 64), title: full };
}

function urlTarget(input: Record<string, unknown>): Partial<ToolDescription> {
  const url = text(input.url) || text(input.uri);
  if (!url) return {};
  return { target: clip(url, 56), title: url };
}

function taskTarget(input: Record<string, unknown>): Partial<ToolDescription> {
  const label = text(input.description) || text(input.subagent_type) || text(input.name);
  return label ? plain(clip(label, 56)) : {};
}

function plain(value: string): Partial<ToolDescription> {
  return value ? { target: value, title: value } : {};
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function baseName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const cut = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return cut >= 0 ? trimmed.slice(cut + 1) || trimmed : trimmed;
}

function clip(value: string, max: number): string {
  return value.length <= max ? value : `${value.slice(0, max - 1)}…`;
}

/* ------------------------------------------------------------------ */
/* The strip's view                                                    */
/* ------------------------------------------------------------------ */

export interface ActivityView {
  phase: ActivityPhase;
  icon: string;
  label: string;
  target: string;
  title: string;
  detail: string;
  /** "1:07" — how long the current run has been going. */
  elapsed: string;
  /** True once nothing has arrived for ACTIVITY_STUCK_MS. */
  stuck: boolean;
  /** "…still working (no output for 1m30s)", or "". */
  stuckNote: string;
  /** "12.4k", or "" when the provider reported nothing yet. */
  tokenLabel: string;
  reasoning: string;
  /** Whether a "Show thinking" toggle is worth offering at all. */
  canShowThinking: boolean;
}

const PHASE_LABEL: Record<Exclude<ActivityPhase, "tool">, { icon: string; label: string }> = {
  idle: { icon: "", label: "" },
  starting: { icon: "⏳", label: "Starting…" },
  thinking: { icon: "💭", label: "Thinking…" },
  writing: { icon: "✍️", label: "Writing the answer…" },
};

/** Everything the strip renders, resolved against a caller-supplied clock. */
export function activityView(
  activity: AgentActivity,
  now: number,
  stuckMs: number = ACTIVITY_STUCK_MS,
): ActivityView {
  const tool = activity.phase === "tool" ? describeTool(activity.toolName, activity.toolInput) : null;
  const phrase = tool ?? PHASE_LABEL[activity.phase as Exclude<ActivityPhase, "tool">];
  const silentMs = activity.lastEventAt > 0 ? Math.max(0, now - activity.lastEventAt) : 0;
  const stuck = activity.phase !== "idle" && silentMs >= stuckMs;
  return {
    phase: activity.phase,
    icon: phrase.icon,
    label: phrase.label,
    target: tool?.target ?? "",
    title: tool?.title ?? "",
    detail: tool?.detail ?? "",
    elapsed: formatElapsed(activity.startedAt > 0 ? Math.max(0, now - activity.startedAt) : 0),
    stuck,
    stuckNote: stuck ? `…still working (no output for ${formatGap(silentMs)})` : "",
    tokenLabel: activity.tokens > 0 ? formatTokens(activity.tokens) : "",
    reasoning: activity.reasoning,
    canShowThinking: activity.sawReasoning,
  };
}

/** "mm:ss", counting past an hour rather than wrapping to zero. */
export function formatElapsed(elapsedMs: number): string {
  const total = Math.max(0, Math.floor(elapsedMs / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

/** "45s" / "1m30s" — the wording the stuck hint uses. */
export function formatGap(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  if (total < 60) return `${total}s`;
  return `${Math.floor(total / 60)}m${String(total % 60).padStart(2, "0")}s`;
}

/** "980" / "12.4k" / "1.2M" — a counter that never widens the one-line strip. */
export function formatTokens(tokens: number): string {
  if (tokens < 1000) return String(Math.max(0, Math.round(tokens)));
  if (tokens < 1_000_000) return `${trimZero(tokens / 1000)}k`;
  return `${trimZero(tokens / 1_000_000)}M`;
}

function trimZero(value: number): string {
  return value.toFixed(1).replace(/\.0$/, "");
}

/* ------------------------------------------------------------------ */
/* The per-turn timeline                                               */
/* ------------------------------------------------------------------ */

export interface TimelineStep {
  key: string;
  toolName: string;
  icon: string;
  label: string;
  target: string;
  title: string;
  detail: string;
  status: "running" | "done";
  isError: boolean;
  durationMs: number;
  /** "1.2s" / "340ms" / "2m 05s", or "" while the step is still running. */
  duration: string;
  /** One line of the tool's result, or of its error. */
  note: string;
}

/**
 * The steps of one assistant turn, as a log rather than as a count.
 *
 * "3 tools used" told the reader nothing they could act on. The same three
 * events already carry which tool, on what, for how long, and whether it
 * failed — so this reshapes them instead of asking the backend for more.
 */
export function buildTurnTimeline(parts: ToolPart[]): TimelineStep[] {
  return parts.map((part, index) => {
    const described = describeTool(part.name, part.input);
    const durationMs =
      part.startedAt && part.endedAt && part.endedAt >= part.startedAt
        ? part.endedAt - part.startedAt
        : 0;
    return {
      key: part.id || `step-${index}`,
      toolName: part.name,
      icon: described.icon,
      label: described.label,
      target: described.target,
      title: described.title,
      detail: described.detail,
      status: part.status,
      isError: !!part.isError,
      durationMs,
      duration: part.status === "running" || durationMs <= 0 ? "" : formatStepDuration(durationMs),
      note: resultNote(part.output),
    };
  });
}

/** "3 steps · 6.4s" — what the collapsed timeline says on its own. */
export function timelineSummary(steps: TimelineStep[]): string {
  const count = `${steps.length} ${steps.length === 1 ? "step" : "steps"}`;
  const total = steps.reduce((sum, step) => sum + step.durationMs, 0);
  return total > 0 ? `${count} · ${formatStepDuration(total)}` : count;
}

/** "340ms" / "1.2s" / "2m 05s" — precision that shrinks as the number grows. */
export function formatStepDuration(ms: number): string {
  if (ms <= 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${trimZero(ms / 1000)}s`;
  const seconds = Math.round(ms / 1000);
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

/** The first line of a tool result worth putting in a log row. */
function resultNote(output: string | undefined): string {
  if (!output) return "";
  const line = output.split("\n").find((candidate) => candidate.trim().length > 0);
  return line ? clip(line.trim(), 160) : "";
}

/* ------------------------------------------------------------------ */
/* Per-device preferences                                              */
/* ------------------------------------------------------------------ */

const SHOW_THINKING_STORAGE_KEY = "remote.futrx.showThinking.v1";

type StorageLike = Pick<Storage, "getItem" | "setItem">;

/**
 * Whether this browser wants reasoning streamed into the activity strip.
 *
 * It lives in localStorage rather than in account settings because it is a
 * property of the screen you are watching: the same operator wants the running
 * commentary on a desktop and the one-line strip on a phone.
 */
export class AgentActivityPreferenceStore {
  private readonly storage: StorageLike | null;

  constructor(storage: StorageLike | null = defaultStorage()) {
    this.storage = storage;
  }

  showThinking(): boolean {
    if (!this.storage) return false;
    try {
      return this.storage.getItem(SHOW_THINKING_STORAGE_KEY) === "true";
    } catch {
      return false;
    }
  }

  setShowThinking(show: boolean): void {
    if (!this.storage) return;
    try {
      this.storage.setItem(SHOW_THINKING_STORAGE_KEY, show ? "true" : "false");
    } catch {
      // Quota or privacy-mode failures degrade to a per-session choice.
    }
  }
}

function defaultStorage(): StorageLike | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

export const agentActivityPreferences = new AgentActivityPreferenceStore();

/* ------------------------------------------------------------------ */
/* Cross-view publication                                              */
/* ------------------------------------------------------------------ */

export interface LiveChatPhase {
  chatId: string;
  phase: ActivityPhase;
}

type PhaseListener = (live: LiveChatPhase | null) => void;

/**
 * The open chat's phase, published for views that are not inside the thread.
 *
 * The sidebar row and the chat thread are siblings under the app shell, and
 * the phase is derived from a socket only the thread holds. Threading it up
 * through the workspace context would make every keystroke of a streamed
 * answer a workspace-wide render; a one-slot store with explicit subscribers
 * re-renders only the row that wants it.
 */
class AgentPhaseStore {
  private live: LiveChatPhase | null = null;
  private readonly listeners = new Set<PhaseListener>();

  get current(): LiveChatPhase | null {
    return this.live;
  }

  publish(chatId: string, phase: ActivityPhase): void {
    if (this.live?.chatId === chatId && this.live.phase === phase) return;
    this.live = phase === "idle" ? null : { chatId, phase };
    this.emit();
  }

  clear(chatId: string): void {
    if (this.live?.chatId !== chatId) return;
    this.live = null;
    this.emit();
  }

  subscribe(listener: PhaseListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private emit(): void {
    for (const listener of this.listeners) listener(this.live);
  }
}

export const agentPhaseStore = new AgentPhaseStore();
