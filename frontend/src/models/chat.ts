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
  projectId?: string;
  selectedSkills?: SelectedSkill[];
  /** Post-run policies: what the platform does on its own once a turn settles. */
  autopilot?: AutopilotPolicy;
  autoTest?: AutoTestPolicy;
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
  | { type: "user"; text: string; synthetic?: SyntheticKind }
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
export type SyntheticKind = "autopilot" | "autotest" | "github-review";

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
  selectedSkills?: SelectedSkill[];
  autopilot?: AutopilotPatch;
  autoTest?: AutoTestPatch;
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
