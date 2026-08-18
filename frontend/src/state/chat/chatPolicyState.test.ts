import assert from "node:assert/strict";
import test from "node:test";
import type { ChatMeta } from "../../models/chat.ts";
import {
  AUTOPILOT_DEFAULT_DURATION_MIN,
  AUTOPILOT_DEFAULT_ROUNDS,
  AUTOPILOT_MAX_ROUNDS,
  armAutopilotPatch,
  autoTestEnabled,
  autopilotDraftFrom,
  autopilotMinutesLeft,
  autopilotView,
  validateAutopilotDraft,
} from "./chatPolicyState.ts";

function chat(overrides: Partial<ChatMeta> = {}): ChatMeta {
  return {
    id: "abcd1234",
    title: "Ship the checkout flow",
    createdAt: 0,
    lastMessageAt: 0,
    ...overrides,
  };
}

// A chat saved before post-run policies existed has no autopilot key at all.
// It must read as "off, with the documented budget" rather than as a loop with
// zero rounds available.
test("autopilotView defaults a chat that has never been armed", () => {
  const view = autopilotView(chat(), false);

  assert.equal(view.enabled, false);
  assert.equal(view.roundsUsed, 0);
  assert.equal(view.maxRounds, AUTOPILOT_DEFAULT_ROUNDS);
  assert.equal(view.maxDurationMin, AUTOPILOT_DEFAULT_DURATION_MIN);
  assert.equal(view.status, "Off");
});

test("autopilotView reports the round the loop is on", () => {
  const startedAt = new Date(2026, 7, 18, 12, 40).getTime();
  const view = autopilotView(
    chat({ autopilot: { enabled: true, maxRounds: 8, roundsUsed: 3, startedAt } }),
    true,
  );

  assert.equal(view.status, "round 3/8 · started 12:40 · running");
  assert.equal(view.pillLabel, "Autopilot on · round 3/8");
});

test("autopilotView says waiting between rounds", () => {
  const startedAt = new Date(2026, 7, 18, 9, 5).getTime();
  const view = autopilotView(
    chat({ autopilot: { enabled: true, maxRounds: 4, roundsUsed: 1, startedAt } }),
    false,
  );

  assert.equal(view.status, "round 1/4 · started 09:05 · waiting");
});

// A stale round count next to an off switch reads as if the loop were running.
test("autopilotView hides the counters once the loop is off", () => {
  const view = autopilotView(
    chat({ autopilot: { enabled: false, maxRounds: 8, roundsUsed: 5, startedAt: 1 } }),
    false,
  );

  assert.equal(view.status, "Off");
  assert.equal(view.roundsUsed, 5, "the count is still readable by the popover");
});

test("autopilotView clamps a stored limit that is out of range", () => {
  const view = autopilotView(chat({ autopilot: { enabled: true, maxRounds: 9000 } }), false);

  assert.equal(view.maxRounds, AUTOPILOT_MAX_ROUNDS);
});

test("autoTestEnabled reads the stored switch", () => {
  assert.equal(autoTestEnabled(chat()), false);
  assert.equal(autoTestEnabled(chat({ autoTest: { enabled: false } })), false);
  assert.equal(autoTestEnabled(chat({ autoTest: { enabled: true } })), true);
});

test("autopilotMinutesLeft counts down the wall-clock budget", () => {
  const startedAt = new Date(2026, 7, 18, 12, 0).getTime();
  const view = { startedAt, maxDurationMin: 120 };

  assert.equal(autopilotMinutesLeft(view, new Date(2026, 7, 18, 13, 0)), 60);
  assert.equal(autopilotMinutesLeft(view, new Date(2026, 7, 18, 14, 0)), null);
  assert.equal(autopilotMinutesLeft({ startedAt: 0, maxDurationMin: 120 }), null);
});

test("autopilotDraftFrom seeds the inputs from the stored limits", () => {
  const view = autopilotView(chat({ autopilot: { enabled: true, maxRounds: 12, maxDurationMin: 45 } }), false);

  assert.deepEqual(autopilotDraftFrom(view), { maxRounds: "12", maxDurationMin: "45" });
});

test("validateAutopilotDraft accepts limits inside the bounds", () => {
  const result = validateAutopilotDraft({ maxRounds: "12", maxDurationMin: "240" });

  assert.equal(result.valid, true);
  assert.equal(result.error, null);
  assert.deepEqual(result.patch, { maxRounds: 12, maxDurationMin: 240 });
});

// Clearing a box mid-edit must not read as "set it to zero", or the popover
// would reject the user's own keystrokes.
test("validateAutopilotDraft treats a blank field as unchanged", () => {
  const result = validateAutopilotDraft({ maxRounds: "", maxDurationMin: "  " });

  assert.equal(result.valid, true);
  assert.deepEqual(result.patch, {});
});

test("validateAutopilotDraft rejects values the server would reject", () => {
  const cases: Array<[string, { maxRounds: string; maxDurationMin: string }]> = [
    ["zero rounds", { maxRounds: "0", maxDurationMin: "120" }],
    ["too many rounds", { maxRounds: "51", maxDurationMin: "120" }],
    ["too short a window", { maxRounds: "8", maxDurationMin: "4" }],
    ["beyond a day", { maxRounds: "8", maxDurationMin: "1441" }],
    ["not a number", { maxRounds: "eight", maxDurationMin: "120" }],
    ["a negative", { maxRounds: "-2", maxDurationMin: "120" }],
    ["a fraction", { maxRounds: "2.5", maxDurationMin: "120" }],
  ];

  for (const [name, draft] of cases) {
    const result = validateAutopilotDraft(draft);
    assert.equal(result.valid, false, name);
    assert.equal(result.patch, null, name);
    assert.ok(result.error, `${name} should explain itself`);
  }
});

test("armAutopilotPatch sends the limits together with the switch", () => {
  assert.deepEqual(armAutopilotPatch({ maxRounds: "6", maxDurationMin: "90" }), {
    enabled: true,
    maxRounds: 6,
    maxDurationMin: 90,
  });
});

test("armAutopilotPatch refuses to arm on an invalid draft", () => {
  assert.equal(armAutopilotPatch({ maxRounds: "0", maxDurationMin: "90" }), null);
});
