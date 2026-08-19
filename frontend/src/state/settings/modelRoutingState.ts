import type {
  RoutingCatalogItem,
  RoutingConditionKind,
  RoutingModelRef,
  RoutingPolicy,
  RoutingProviderModels,
  RoutingRule,
  RoutingUsageSummary,
} from "../../models/modelRouting";
import { SYNTHETIC_ANY } from "../../models/modelRouting.ts";
import type { ChatProvider } from "../../models/chat";

/**
 * Editing the routing policy, as pure functions over the document.
 *
 * The panel holds one draft policy and replaces it wholesale on every change,
 * so every operation here returns a new document and none of them mutate. The
 * ordering functions matter most: the server evaluates rules top-down and
 * stops at the first match, so "move up" is a behavioural change, not a
 * cosmetic one.
 */

/** A blank rule, ready for the operator to fill in. */
export function newRoutingRule(policy: RoutingPolicy): RoutingRule {
  return {
    id: nextRuleId(policy),
    when: { kind: "modeIs", value: "" },
    use: policy.cheapModel.provider
      ? { ...policy.cheapModel }
      : { ...policy.default },
    note: "",
    enabled: false,
  };
}

/**
 * A fresh id that collides with nothing already in the list. Ids are the
 * ledger's attribution key, so reusing one would silently merge two rules'
 * hit counts in the savings report.
 */
export function nextRuleId(policy: RoutingPolicy): string {
  const taken = new Set(policy.rules.map((rule) => rule.id));
  for (let index = policy.rules.length + 1; ; index += 1) {
    const candidate = `rule-${index}`;
    if (!taken.has(candidate)) return candidate;
  }
}

export function addRule(policy: RoutingPolicy): RoutingPolicy {
  return { ...policy, rules: [...policy.rules, newRoutingRule(policy)] };
}

export function updateRule(
  policy: RoutingPolicy,
  id: string,
  patch: Partial<RoutingRule>,
): RoutingPolicy {
  return {
    ...policy,
    rules: policy.rules.map((rule) => (rule.id === id ? { ...rule, ...patch } : rule)),
  };
}

export function removeRule(policy: RoutingPolicy, id: string): RoutingPolicy {
  return { ...policy, rules: policy.rules.filter((rule) => rule.id !== id) };
}

/** Moves one rule by `offset` places, clamped to the ends of the list. */
export function moveRule(policy: RoutingPolicy, id: string, offset: number): RoutingPolicy {
  const from = policy.rules.findIndex((rule) => rule.id === id);
  if (from < 0 || offset === 0) return policy;
  const to = clamp(from + offset, 0, policy.rules.length - 1);
  if (to === from) return policy;
  const rules = [...policy.rules];
  const [moved] = rules.splice(from, 1);
  rules.splice(to, 0, moved);
  return { ...policy, rules };
}

/**
 * The value a condition needs, defaulted for the kind it just became. Changing
 * kind without this leaves a character count in a mode box, which the server
 * would reject.
 */
export function defaultConditionValue(kind: RoutingConditionKind): string {
  switch (kind) {
    case "synthetic":
      return SYNTHETIC_ANY;
    case "promptShorterThan":
      return "200";
    case "promptLongerThan":
      return "2000";
    case "modeIs":
      return "chat";
    default:
      return "";
  }
}

export function changeConditionKind(
  policy: RoutingPolicy,
  id: string,
  kind: RoutingConditionKind,
): RoutingPolicy {
  return updateRule(policy, id, { when: { kind, value: defaultConditionValue(kind) } });
}

/**
 * Why the policy cannot be saved, or null when it can. The server validates
 * again — this only spares the operator a round trip and names the row.
 */
export function routingPolicyProblem(policy: RoutingPolicy): string | null {
  if (!policy.default.provider) return "Choose a default model.";
  for (const rule of policy.rules) {
    if (!rule.use.provider) return `Rule "${ruleTitle(rule)}" has no destination model.`;
    const value = (rule.when.value ?? "").trim();
    switch (rule.when.kind) {
      case "promptShorterThan":
      case "promptLongerThan":
        if (!/^\d+$/.test(value) || Number(value) <= 0) {
          return `Rule "${ruleTitle(rule)}" needs a positive character count.`;
        }
        break;
      case "regex":
        if (!value) return `Rule "${ruleTitle(rule)}" needs an expression.`;
        try {
          // The server compiles Go's RE2, which rejects a superset of what
          // JavaScript rejects; catching the obvious syntax errors here is
          // still worth the round trip it saves.
          new RegExp(stripGoFlags(value));
        } catch {
          return `Rule "${ruleTitle(rule)}" has an invalid expression.`;
        }
        break;
      case "skillSelected":
      case "projectIs":
        if (!value) return `Rule "${ruleTitle(rule)}" needs a value.`;
        break;
      default:
        break;
    }
  }
  return null;
}

/** The name a rule is referred to by in messages and in the transcript. */
export function ruleTitle(rule: RoutingRule): string {
  return (rule.note ?? "").trim() || rule.id;
}

/**
 * Whether a destination can actually be used right now. An armed rule pointing
 * at a disconnected provider still saves — it just falls back at run time —
 * so this drives a warning, never a block.
 */
export function isRefAvailable(ref: RoutingModelRef, providers: string[]): boolean {
  if (!ref.provider) return false;
  if (providers.length === 0) return true;
  return providers.includes(ref.provider);
}

/** The model list a picker shows for one provider. */
export function modelsForProvider(
  catalog: RoutingProviderModels[],
  provider: ChatProvider | "",
): RoutingCatalogItem[] {
  if (!provider) return [];
  return catalog.find((entry) => entry.provider === provider)?.models ?? [];
}

/** How a destination reads in the pickers and the rule list. */
export function refLabel(catalog: RoutingProviderModels[], ref: RoutingModelRef): string {
  if (!ref.provider) return "Not set";
  const entry = catalog.find((item) => item.provider === ref.provider);
  const providerLabel = entry?.label ?? ref.provider;
  const modelLabel =
    entry?.models.find((model) => model.value === (ref.model ?? ""))?.label ?? ref.model ?? "Auto";
  return `${providerLabel} ${modelLabel}`;
}

/**
 * Sets a destination's provider, dropping a model that provider does not
 * offer. Without this, switching Claude→Codex would leave "opus" behind and
 * the server would reject the policy.
 */
export function setRefProvider(
  catalog: RoutingProviderModels[],
  ref: RoutingModelRef,
  provider: ChatProvider | "",
): RoutingModelRef {
  const models = modelsForProvider(catalog, provider);
  const keep = models.some((model) => model.value === (ref.model ?? ""));
  return { ...ref, provider, model: keep ? ref.model : "" };
}

/**
 * The share of routed runs that went to the cheap pole, 0–1. Used for the
 * card's bar; a period with no routed runs has no share at all.
 */
export function cheapShare(summary: RoutingUsageSummary): number | null {
  if (summary.routedRuns <= 0) return null;
  return summary.cheapRuns / summary.routedRuns;
}

/**
 * The savings sentence, worded so it can never be read as an invoice: it is a
 * counterfactual priced from the editable table.
 */
export function savingsNote(summary: RoutingUsageSummary): string {
  if (summary.routedRuns === 0) return "No runs were routed automatically in this period.";
  if (summary.pricedRuns === 0) {
    return "No baseline could be priced for these runs, so no saving is estimated.";
  }
  const excluded = summary.routedRuns - summary.pricedRuns;
  const scope =
    excluded > 0
      ? `${summary.pricedRuns} of ${summary.routedRuns} routed runs could be priced`
      : `all ${summary.routedRuns} routed runs`;
  const direction = summary.estimatedSavedUsd >= 0 ? "less than" : "more than";
  return `Estimate only: ${scope}, and they cost ${direction} the ${
    summary.defaultModel || "default model"
  } would have on the same tokens.`;
}

function stripGoFlags(expression: string): string {
  // `(?i)` and friends are Go's inline flag syntax; JavaScript spells them
  // differently, so they are removed before the syntax check.
  return expression.replace(/\(\?[ims]+\)/g, "");
}

function clamp(value: number, low: number, high: number): number {
  if (value < low) return low;
  if (value > high) return high;
  return value;
}
