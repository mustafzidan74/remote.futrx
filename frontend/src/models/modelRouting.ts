import type { ChatProvider, ReasoningEffort } from "./chat";

/**
 * Automatic model routing: the admin-owned policy that decides which model
 * answers a turn, and the decision it produces for one prompt.
 *
 * The vocabulary is the server's — `internal/service/routing` normalizes
 * anything it does not recognize away — so these types are the whole contract.
 */

/** What a rule looks at. */
export type RoutingConditionKind =
  | "synthetic"
  | "promptShorterThan"
  | "promptLongerThan"
  | "modeIs"
  | "skillSelected"
  | "projectIs"
  | "regex";

/** The `synthetic` value that matches every platform-issued run. */
export const SYNTHETIC_ANY = "any";

export interface RoutingCondition {
  kind: RoutingConditionKind;
  value?: string;
}

/** A provider plus a model inside it. An empty model is that provider's Auto. */
export interface RoutingModelRef {
  provider: ChatProvider | "";
  model?: string;
  /** Overrides the chat's own effort when this reference wins. */
  reasoningEffort?: ReasoningEffort;
}

export interface RoutingRule {
  id: string;
  when: RoutingCondition;
  use: RoutingModelRef;
  /** The human name the transcript header and the savings report show. */
  note?: string;
  enabled: boolean;
}

export interface RoutingPolicy {
  version: number;
  updatedAt?: number;
  updatedBy?: string;
  /** The master switch. Off means every chat keeps its own model. */
  enabled: boolean;
  default: RoutingModelRef;
  rules: RoutingRule[];
  cheapModel: RoutingModelRef;
  expensiveModel: RoutingModelRef;
  /** The built-in prompt-shape classifier that runs after the rule list. */
  autoHeuristics: boolean;
}

export interface RoutingCatalogItem {
  value: string;
  label: string;
}

export interface RoutingProviderModels {
  provider: ChatProvider;
  label: string;
  models: RoutingCatalogItem[];
}

/** GET/PUT /api/admin/model-routing */
export interface RoutingView {
  policy: RoutingPolicy;
  /** Providers with a live host credential. A rule pointing elsewhere falls back. */
  providers: string[];
  catalog: RoutingProviderModels[];
}

/** POST /api/admin/model-routing/test and /api/model-routing/preview */
export interface RoutingDecision {
  provider: string;
  model?: string;
  reasoningEffort?: string;
  /** False means the chat's own provider and model are in force. */
  routed: boolean;
  ruleId?: string;
  /** The winning rule's human name. */
  rule?: string;
  reason: string;
}

export interface RoutingTestInput {
  prompt: string;
  mode?: string;
  provider?: string;
  model?: string;
  synthetic?: string;
  projectId?: string;
  projectSlug?: string;
  skills?: string[];
  /** Ask what a chat pinned to its own model would do. */
  pinned?: boolean;
}

/** The "Auto routing" card of the usage dashboard. Every figure is an estimate. */
export interface RoutingUsageSummary {
  enabled: boolean;
  routedRuns: number;
  cheapRuns: number;
  expensiveRuns: number;
  otherRuns: number;
  /** Runs both halves of the comparison could be computed for. */
  pricedRuns: number;
  routedCostUsd: number;
  baselineCostUsd: number;
  /** Baseline minus actual. Negative when routing cost more than the default. */
  estimatedSavedUsd: number;
  defaultModel?: string;
  cheapModel?: string;
  expensiveModel?: string;
  topRules: RoutingRuleHits[];
}

export interface RoutingRuleHits {
  ruleId: string;
  label: string;
  runs: number;
  costUsd: number;
}

/** The condition kinds, in the order the rule editor offers them. */
export const ROUTING_CONDITION_KINDS: ReadonlyArray<{
  kind: RoutingConditionKind;
  label: string;
  /** What the value box means, or "" when the kind takes no value. */
  valueLabel: string;
  placeholder: string;
}> = [
  { kind: "synthetic", label: "Platform-issued run", valueLabel: "Label", placeholder: SYNTHETIC_ANY },
  { kind: "modeIs", label: "Chat mode is", valueLabel: "Mode", placeholder: "chat" },
  {
    kind: "promptShorterThan",
    label: "Prompt shorter than (no code block)",
    valueLabel: "Characters",
    placeholder: "200",
  },
  { kind: "promptLongerThan", label: "Prompt longer than", valueLabel: "Characters", placeholder: "2000" },
  { kind: "skillSelected", label: "Skill selected", valueLabel: "Skill", placeholder: "browser" },
  { kind: "projectIs", label: "Project is", valueLabel: "Id or slug", placeholder: "acme" },
  { kind: "regex", label: "Prompt matches regex", valueLabel: "Expression", placeholder: "(?i)refactor" },
];

/** The synthetic labels a rule may name, plus the catch-all. */
export const ROUTING_SYNTHETIC_VALUES: readonly string[] = [
  SYNTHETIC_ANY,
  "autopilot",
  "autotest",
  "team-review",
  "team-test",
  "team-fix",
  "github-review",
];
