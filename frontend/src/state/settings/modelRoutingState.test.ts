import assert from "node:assert/strict";
import test from "node:test";
import type {
  RoutingPolicy,
  RoutingProviderModels,
  RoutingUsageSummary,
} from "../../models/modelRouting.ts";
import {
  addRule,
  changeConditionKind,
  cheapShare,
  defaultConditionValue,
  isRefAvailable,
  modelsForProvider,
  moveRule,
  nextRuleId,
  refLabel,
  removeRule,
  routingPolicyProblem,
  ruleTitle,
  savingsNote,
  setRefProvider,
  updateRule,
} from "./modelRoutingState.ts";

const catalog: RoutingProviderModels[] = [
  {
    provider: "claude",
    label: "Claude",
    models: [
      { value: "", label: "Auto" },
      { value: "opus", label: "Opus" },
      { value: "haiku", label: "Haiku" },
    ],
  },
  {
    provider: "codex",
    label: "Codex",
    models: [
      { value: "", label: "Auto" },
      { value: "gpt-5.5", label: "GPT-5.5" },
    ],
  },
];

function policy(): RoutingPolicy {
  return {
    version: 1,
    enabled: true,
    autoHeuristics: true,
    default: { provider: "claude", model: "opus" },
    cheapModel: { provider: "claude", model: "haiku" },
    expensiveModel: { provider: "claude", model: "opus" },
    rules: [
      { id: "a", when: { kind: "modeIs", value: "chat" }, use: { provider: "claude", model: "haiku" }, note: "Chat", enabled: true },
      { id: "b", when: { kind: "promptLongerThan", value: "2000" }, use: { provider: "claude", model: "opus" }, note: "Long", enabled: true },
      { id: "c", when: { kind: "regex", value: "(?i)refactor" }, use: { provider: "claude", model: "opus" }, note: "", enabled: false },
    ],
  };
}

test("moveRule reorders without mutating, and clamps at both ends", () => {
  const before = policy();
  const moved = moveRule(before, "c", -1);
  assert.deepEqual(moved.rules.map((rule) => rule.id), ["a", "c", "b"]);
  assert.deepEqual(before.rules.map((rule) => rule.id), ["a", "b", "c"], "input was mutated");

  assert.deepEqual(moveRule(before, "a", -1).rules.map((r) => r.id), ["a", "b", "c"]);
  assert.deepEqual(moveRule(before, "c", 1).rules.map((r) => r.id), ["a", "b", "c"]);
  assert.deepEqual(moveRule(before, "a", 5).rules.map((r) => r.id), ["b", "c", "a"]);
  assert.deepEqual(moveRule(before, "missing", -1).rules.map((r) => r.id), ["a", "b", "c"]);
  assert.equal(moveRule(before, "a", 0), before, "a zero move returns the same document");
});

test("addRule appends a disabled rule with a fresh id", () => {
  const added = addRule(policy());
  assert.equal(added.rules.length, 4);
  const rule = added.rules[3];
  assert.equal(rule.enabled, false, "a new rule must not route anything until it is armed");
  assert.equal(rule.id, "rule-4");
  assert.deepEqual(rule.use, { provider: "claude", model: "haiku" }, "seeded from the cheap pole");
});

test("nextRuleId never reuses an id already in the list", () => {
  const collided = { ...policy(), rules: [{ ...policy().rules[0], id: "rule-4" }] };
  assert.equal(nextRuleId(collided), "rule-2");
  const dense = {
    ...policy(),
    rules: ["rule-1", "rule-2", "rule-3", "rule-4"].map((id) => ({ ...policy().rules[0], id })),
  };
  assert.equal(nextRuleId(dense), "rule-5");
});

test("updateRule and removeRule touch only the named rule", () => {
  const updated = updateRule(policy(), "b", { enabled: false, note: "Renamed" });
  assert.equal(updated.rules[1].enabled, false);
  assert.equal(updated.rules[1].note, "Renamed");
  assert.equal(updated.rules[0].note, "Chat");

  const removed = removeRule(policy(), "b");
  assert.deepEqual(removed.rules.map((rule) => rule.id), ["a", "c"]);
  assert.deepEqual(removeRule(policy(), "nope").rules.map((r) => r.id), ["a", "b", "c"]);
});

test("changing a condition kind swaps in a value that kind can use", () => {
  assert.equal(defaultConditionValue("synthetic"), "any");
  assert.equal(defaultConditionValue("promptShorterThan"), "200");
  assert.equal(defaultConditionValue("regex"), "");

  const changed = changeConditionKind(policy(), "a", "promptShorterThan");
  assert.deepEqual(changed.rules[0].when, { kind: "promptShorterThan", value: "200" });
});

test("routingPolicyProblem names the row that cannot be saved", () => {
  assert.equal(routingPolicyProblem(policy()), null);

  const noDefault = { ...policy(), default: { provider: "" as const } };
  assert.match(routingPolicyProblem(noDefault) ?? "", /default model/i);

  const noDestination = updateRule(policy(), "a", { use: { provider: "" } });
  assert.match(routingPolicyProblem(noDestination) ?? "", /"Chat".*destination/i);

  const badCount = updateRule(policy(), "b", { when: { kind: "promptLongerThan", value: "lots" } });
  assert.match(routingPolicyProblem(badCount) ?? "", /"Long".*character count/i);

  const zeroCount = updateRule(policy(), "b", { when: { kind: "promptLongerThan", value: "0" } });
  assert.match(routingPolicyProblem(zeroCount) ?? "", /character count/i);

  // An unnamed rule is reported by its id, which is what the ledger uses too.
  const badRegex = updateRule(policy(), "c", { when: { kind: "regex", value: "(" } });
  assert.match(routingPolicyProblem(badRegex) ?? "", /"c".*expression/i);

  const emptySkill = updateRule(policy(), "a", { when: { kind: "skillSelected", value: " " } });
  assert.match(routingPolicyProblem(emptySkill) ?? "", /needs a value/i);
});

test("a Go inline-flag regex is accepted", () => {
  const withFlags = updateRule(policy(), "c", { when: { kind: "regex", value: "(?i)(refactor|debug)" } });
  assert.equal(routingPolicyProblem(withFlags), null);
});

test("ruleTitle prefers the note and falls back to the id", () => {
  assert.equal(ruleTitle(policy().rules[0]), "Chat");
  assert.equal(ruleTitle(policy().rules[2]), "c");
  assert.equal(ruleTitle({ ...policy().rules[0], note: "   " }), "a");
});

test("isRefAvailable warns only about a provider the host has not connected", () => {
  const ref = { provider: "claude" as const, model: "haiku" };
  assert.equal(isRefAvailable(ref, ["claude", "codex"]), true);
  assert.equal(isRefAvailable(ref, ["codex"]), false);
  assert.equal(isRefAvailable({ provider: "" }, ["claude"]), false);
  // An empty list means the server could not be asked, not that nothing works.
  assert.equal(isRefAvailable(ref, []), true);
});

test("setRefProvider drops a model the new provider does not offer", () => {
  const ref = { provider: "claude" as const, model: "opus" };
  assert.deepEqual(setRefProvider(catalog, ref, "codex"), { provider: "codex", model: "" });
  assert.deepEqual(setRefProvider(catalog, { provider: "claude", model: "" }, "codex"), {
    provider: "codex",
    model: "",
  });
  const effort = { provider: "claude" as const, model: "opus", reasoningEffort: "high" as const };
  assert.equal(setRefProvider(catalog, effort, "codex").reasoningEffort, "high");
});

test("modelsForProvider and refLabel read from the server's catalog", () => {
  assert.equal(modelsForProvider(catalog, "codex").length, 2);
  assert.deepEqual(modelsForProvider(catalog, ""), []);
  assert.equal(refLabel(catalog, { provider: "claude", model: "haiku" }), "Claude Haiku");
  assert.equal(refLabel(catalog, { provider: "claude", model: "" }), "Claude Auto");
  assert.equal(refLabel(catalog, { provider: "" }), "Not set");
  assert.equal(refLabel(catalog, { provider: "kimi", model: "k2" }), "kimi k2");
});

function summary(overrides: Partial<RoutingUsageSummary> = {}): RoutingUsageSummary {
  return {
    enabled: true,
    routedRuns: 10,
    cheapRuns: 7,
    expensiveRuns: 2,
    otherRuns: 1,
    pricedRuns: 10,
    routedCostUsd: 4,
    baselineCostUsd: 10,
    estimatedSavedUsd: 6,
    defaultModel: "Claude Sonnet",
    cheapModel: "Claude Haiku",
    expensiveModel: "Claude Opus",
    topRules: [],
    ...overrides,
  };
}

test("cheapShare is null when nothing was routed", () => {
  assert.equal(cheapShare(summary()), 0.7);
  assert.equal(cheapShare(summary({ routedRuns: 0, cheapRuns: 0 })), null);
});

test("savingsNote always labels the figure as an estimate and never as a bill", () => {
  assert.match(savingsNote(summary()), /^Estimate only: all 10 routed runs/);
  assert.match(savingsNote(summary()), /less than the Claude Sonnet/);
  assert.match(
    savingsNote(summary({ estimatedSavedUsd: -3 })),
    /more than the Claude Sonnet/,
    "a loss must be described as a loss",
  );
  assert.match(savingsNote(summary({ pricedRuns: 4 })), /4 of 10 routed runs/);
  assert.match(savingsNote(summary({ pricedRuns: 0 })), /no saving is estimated/);
  assert.match(savingsNote(summary({ routedRuns: 0 })), /No runs were routed/);
});
