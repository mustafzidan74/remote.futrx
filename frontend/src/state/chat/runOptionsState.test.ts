import assert from "node:assert/strict";
import test from "node:test";
import {
  runOptionsAriaLabel,
  runOptionsSummary,
  type RunOptionsState,
} from "./runOptionsState.ts";

const claudeDefaults: RunOptionsState = {
  provider: "claude",
  mode: "code",
  reasoningEffort: "",
  serviceTier: "",
  autopilot: false,
  autoTest: false,
  team: false,
};

test("prints only the settings that differ from the provider default", () => {
  assert.equal(runOptionsSummary(claudeDefaults), "Code");
  assert.equal(
    runOptionsSummary({ ...claudeDefaults, mode: "full-auto", reasoningEffort: "xhigh" }),
    "Full auto · XHigh",
  );
});

test("badges the policies that keep working on their own", () => {
  assert.equal(
    runOptionsSummary({
      ...claudeDefaults,
      mode: "full-auto",
      reasoningEffort: "xhigh",
      autopilot: true,
    }),
    "Full auto · XHigh · 🛫",
  );
  assert.equal(
    runOptionsSummary({ ...claudeDefaults, autopilot: true, autoTest: true }),
    "Code · 🛫 · 🧪",
  );
  assert.equal(runOptionsSummary({ ...claudeDefaults, team: true }), "Code · 👥");
});

test("names the speed tier only where the provider has one", () => {
  // Codex is the only CLI with a service tier; Claude ignores the stored value.
  assert.equal(
    runOptionsSummary({ ...claudeDefaults, provider: "codex", serviceTier: "priority" }),
    "Code · Priority",
  );
  assert.equal(runOptionsSummary({ ...claudeDefaults, serviceTier: "priority" }), "Code");
});

test("spells the badges out for screen readers", () => {
  assert.equal(
    runOptionsAriaLabel({
      ...claudeDefaults,
      mode: "review",
      reasoningEffort: "high",
      autopilot: true,
      autoTest: true,
    }),
    "Run options: Review mode, High thinking, autopilot on, auto-test on",
  );
});
