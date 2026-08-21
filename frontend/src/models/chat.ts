import type {
  ChatMode,
  ChatProvider,
  ReasoningEffort,
  ServiceTier,
} from "../config/chatCatalog";

export type {
  ChatMode,
  ChatProvider,
  ReasoningEffort,
  ServiceTier,
} from "../config/chatCatalog";

export interface ChatMeta {
  id: string;
  title: string;
  /**
   * A one-line description of what this chat is about, written by the
   * optional auxiliary model after a run settles. Shown as a subtitle in the
   * sidebar and on the dashboard; absent on a server that has no such model,
   * which is why nothing may depend on it.
   */
  summary?: string;
  provider?: ChatProvider;
  claudeSessionId?: string;
  codexSessionId?: string;
  kimiSessionId?: string;
  antigravitySessionId?: string;
  tmuxSession?: string;
  cwd?: string;
  createdAt: number;
  lastMessageAt: number;
  lastReadAt?: number;
  running?: boolean;
  model?: string;
  mode?: ChatMode;
  reasoningEffort?: ReasoningEffort;
  serviceTier?: ServiceTier;
  /**
   * Who picks the model for the next turn. "pinned" (the default, and what
   * every chat did before automatic routing existed) uses the provider and
   * model above; "auto" hands the choice to the platform routing policy.
   */
  modelPolicy?: ChatModelPolicy;
  /**
   * Points this chat's agent at one of the platform's third-party agent
   * endpoints. Absent — the default — means the vendor's own endpoint, which
   * is what every chat did before the register existed. An endpoint pins the
   * chat: it decides which CLI runs and which models are on offer, so a chat
   * carrying one is not routed.
   */
  endpointId?: string;
  projectId?: string;
  selectedSkills?: SelectedSkill[];
  /** Post-run policies: what the platform does on its own once a turn settles. */
  autopilot?: AutopilotPolicy;
  autoTest?: AutoTestPolicy;
  /** Team mode: implementer → reviewer → tester, all inside this project. */
  team?: TeamPolicy;
  /**
   * Set on a reviewer's or tester's own thread, naming the chat it answers to.
   * Companions are hidden from the sidebar and opened from the parent's Team
   * panel, so a team session adds one row to the chat list rather than three.
   */
  companionOf?: string;
  companionRole?: TeamRoleName;
}

export type ChatModelPolicy = "pinned" | "auto";

/**
 * One automatic routing decision, as the transcript shows it: which model
 * actually answered the turn and why. Absent on every turn a chat answered
 * with its own pinned model, and on every turn recorded before routing
 * existed.
 */
export interface ChatEventRouting {
  provider: string;
  model?: string;
  /** The policy rule that won; absent for the default model or a heuristic. */
  ruleId?: string;
  /** That rule's human name, which is what the badge shows. */
  rule?: string;
  /** The one-sentence explanation, including any fallback. */
  reason?: string;
}

export type TeamRoleName = "implementer" | "reviewer" | "tester";

/** Where a team session currently is. "" is armed but between loops. */
export type TeamPhase = "" | "reviewing" | "testing" | "fixing" | "done" | "error";

/** Ship/fix come from the reviewer, pass/fail from the tester. */
export type TeamVerdict = "" | "ship" | "fix" | "pass" | "fail" | "unknown";

/**
 * Team mode configuration plus the live state of the current loop. The server
 * owns everything below `autoFix`: the browser reads the counters, it never
 * spends one.
 */
export interface TeamPolicy {
  enabled: boolean;
  roles: TeamRoles;
  maxLoops?: number;
  autoFix?: boolean;
  phase?: TeamPhase;
  loopsUsed?: number;
  verdict?: TeamVerdict;
  hops?: TeamHop[];
  enabledBy?: string;
  updatedAt?: number;
}

export interface TeamRoles {
  implementer: TeamRole;
  reviewer: TeamRole;
  tester: TeamRole;
}

/**
 * One seat. An empty `provider` means "let the platform pick", which is how a
 * chat armed before a second provider was connected still gets a fresh-eyes
 * reviewer once one is.
 */
export interface TeamRole {
  provider?: ChatProvider | "";
  model?: string;
  enabled: boolean;
  /** The companion chat this seat runs in; empty for the implementer. */
  chatId?: string;
}

/** One recorded step of the loop, as the Team panel renders it. */
export interface TeamHop {
  loop: number;
  role: TeamRoleName;
  kind?: SyntheticKind;
  chatId?: string;
  verdict?: TeamVerdict;
  findings?: string;
  at?: number;
}

/**
 * Keeps a chat working while the operator is away. `roundsUsed` and
 * `startedAt` are the counters the server spends; the browser only reads them.
 */
export interface AutopilotPolicy {
  enabled: boolean;
  maxRounds?: number;
  roundsUsed?: number;
  maxDurationMin?: number;
  startedAt?: number;
  enabledBy?: string;
}

/** Asks for a Playwright verification pass after every turn that changed something. */
export interface AutoTestPolicy {
  enabled: boolean;
  enabledBy?: string;
}

export interface SelectedSkill {
  name: string;
  command?: string;
  provider?: ChatProvider;
  source?: string;
}

type ChatEventBase = { seq?: number; t: number };

export type ChatEvent = ChatEventBase & (
  | { type: "user"; text: string; synthetic?: SyntheticKind; routing?: ChatEventRouting }
  | { type: "assistant_text"; text: string; messageId?: string }
  | { type: "thinking"; text: string }
  | { type: "tool_use_start"; id: string; name: string; input: Record<string, unknown> }
  | { type: "tool_use_end"; id: string; output?: string; isError?: boolean }
  | { type: "permission_request"; id: string; toolName: string; input: Record<string, unknown> }
  | { type: "system"; subtype: string; data?: Record<string, unknown> }
  | { type: "session"; provider?: ChatProvider; claudeSessionId?: string; codexSessionId?: string; kimiSessionId?: string; antigravitySessionId?: string }
  | {
      type: "complete";
      usage?: {
        input_tokens?: number;
        output_tokens?: number;
        cache_read_input_tokens?: number;
        cache_creation_input_tokens?: number;
      };
    }
  | { type: "error"; message: string }
  | { type: "sync"; running?: boolean }
);

export interface ChatEventPage {
  events: ChatEvent[];
  nextBefore?: number;
  lastSeq: number;
  hasMore: boolean;
}

/**
 * Labels a prompt the platform composed rather than one the user typed. The
 * server normalizes anything it does not recognize away, so these three
 * strings are the whole vocabulary.
 *
 * "github-review" is the odd one out: the other two are unattended rounds the
 * platform decided on, while this one was asked for by a human but is built
 * from text that arrived from outside this server.
 */
export type SyntheticKind =
  | "autopilot"
  | "autotest"
  | "team-review"
  | "team-test"
  | "team-fix"
  | "team-summary"
  | "github-review";

export type ClientToServer =
  | { type: "prompt"; text: string; clientId?: string; synthetic?: SyntheticKind }
  | { type: "cancel" }
  | { type: "permission"; id: string; approved: boolean };

export type ChatStatus = "loading" | "ready" | "streaming" | "error";

export interface QueuedPrompt {
  id: string;
  text: string;
}

// Server verdict on a prompt sent with a clientId: accepted means a run
// started from it; rejected means the run lock was held and it was discarded.
export interface PromptOutcome {
  clientId: string;
  accepted: boolean;
}

export interface CreateChatInput {
  tmuxSession?: string;
  cwd?: string;
  title?: string;
  provider?: ChatProvider;
  model?: string;
  mode?: ChatMode;
  reasoningEffort?: ReasoningEffort;
  serviceTier?: ServiceTier;
  modelPolicy?: ChatModelPolicy;
  endpointId?: string;
  projectId?: string;
  selectedSkills?: SelectedSkill[];
}

export interface UpdateChatInput {
  title?: string;
  cwd?: string;
  provider?: ChatProvider;
  model?: string;
  mode?: ChatMode;
  reasoningEffort?: ReasoningEffort;
  serviceTier?: ServiceTier;
  modelPolicy?: ChatModelPolicy;
  /** "" points the chat back at the vendor's own endpoint. */
  endpointId?: string;
  selectedSkills?: SelectedSkill[];
  autopilot?: AutopilotPatch;
  autoTest?: AutoTestPatch;
  team?: TeamPatch;
}

/** Every field is optional so a toggle need not restate the limits. */
export interface AutopilotPatch {
  enabled?: boolean;
  maxRounds?: number;
  maxDurationMin?: number;
}

export interface AutoTestPatch {
  enabled?: boolean;
}

/**
 * Patches the configuration half of team mode. The loop counters, the phase,
 * and the companion chat ids are the server's; sending them changes nothing.
 */
export interface TeamPatch {
  enabled?: boolean;
  maxLoops?: number;
  autoFix?: boolean;
  roles?: {
    implementer?: TeamRolePatch;
    reviewer?: TeamRolePatch;
    tester?: TeamRolePatch;
  };
}

export interface TeamRolePatch {
  provider?: ChatProvider | "";
  model?: string;
  enabled?: boolean;
}
