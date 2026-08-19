import assert from "node:assert/strict";
import test from "node:test";
import type { ChatEvent } from "../../models/chat.ts";
import { chatEventStateProjector } from "./chatEventStateProjector.ts";

test("projects chat events into the existing message and usage model", () => {
  const events: ChatEvent[] = [
    { type: "user", text: "hello", t: 1 },
    { type: "assistant_text", text: "hel", t: 2 },
    { type: "assistant_text", text: "lo", t: 3 },
    { type: "tool_use_start", id: "tool-1", name: "shell", input: { command: "pwd" }, t: 4 },
    { type: "tool_use_end", id: "tool-1", output: "/workspace", isError: false, t: 5 },
    { type: "complete", usage: { input_tokens: 3, output_tokens: 5 }, t: 6 },
  ];

  const state = chatEventStateProjector.fromEvents(events, {
    hasMore: false,
    lastSeq: 0,
  });

  assert.deepEqual(state.blocks, [
    { type: "user", text: "hello", t: 1 },
    {
      type: "assistant",
      parts: [
        { kind: "text", text: "hello" },
        {
          kind: "tool",
          id: "tool-1",
          name: "shell",
          input: { command: "pwd" },
          output: "/workspace",
          isError: false,
          status: "done",
          startedAt: 4,
          endedAt: 5,
        },
      ],
      t: 2,
      isComplete: true,
    },
  ]);

  assert.deepEqual(state.usageTotals, {
    inputTokens: 3,
    outputTokens: 5,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
  });
});

// The badge above a synthetic bubble is the only thing distinguishing an
// unattended autopilot round from a prompt the operator typed, so the label
// has to survive projection.
test("carries the synthetic label onto the user block", () => {
  const state = chatEventStateProjector.fromEvents(
    [
      { seq: 1, t: 1, type: "user", text: "build the cart" },
      { seq: 2, t: 2, type: "user", text: "keep going", synthetic: "autopilot" },
      { seq: 3, t: 3, type: "user", text: "verify it", synthetic: "autotest" },
    ],
    { hasMore: false, nextBefore: 0, lastSeq: 3 },
  );

  assert.deepEqual(state.blocks, [
    { type: "user", text: "build the cart", t: 1 },
    { type: "user", text: "keep going", t: 2, synthetic: "autopilot" },
    { type: "user", text: "verify it", t: 3, synthetic: "autotest" },
  ]);
});

// The turn timeline shows how long each step took. Nothing new comes from the
// backend for it: the two tool events already carry the instants.
test("stamps each tool part with the instants its two events carry", () => {
  const state = chatEventStateProjector.fromEvents(
    [
      { seq: 1, t: 100, type: "user", text: "run the tests" },
      { seq: 2, t: 200, type: "tool_use_start", id: "tool-1", name: "Bash", input: { command: "npm test" } },
      { seq: 3, t: 6_400, type: "tool_use_end", id: "tool-1", output: "ok" },
    ],
    { hasMore: false, nextBefore: 0, lastSeq: 3 },
  );

  const assistant = state.blocks[1];
  assert.equal(assistant.type, "assistant");
  const part = assistant.type === "assistant" ? assistant.parts[0] : null;
  assert.equal(part?.kind, "tool");
  assert.equal(part?.kind === "tool" ? part.startedAt : 0, 200);
  assert.equal(part?.kind === "tool" ? part.endedAt : 0, 6_400);
});

// The activity strip reads this fold rather than a second event subscription,
// so replayed history and the live socket cannot disagree about the phase.
test("folds the live activity from the same events as the blocks", () => {
  const state = chatEventStateProjector.fromEvents(
    [
      { seq: 1, t: 100, type: "user", text: "read the config" },
      { seq: 2, t: 150, type: "thinking", text: "looking" },
      { seq: 3, t: 200, type: "tool_use_start", id: "tool-1", name: "Read", input: { file_path: "/root/wp-config.php" } },
    ],
    { hasMore: false, nextBefore: 0, lastSeq: 3 },
  );

  assert.equal(state.activity.phase, "tool");
  assert.equal(state.activity.toolName, "Read");
  assert.equal(state.activity.startedAt, 100);
  assert.equal(state.activity.sawReasoning, true);
});

test("an appended completion returns the activity to idle", () => {
  const started = chatEventStateProjector.fromEvents(
    [{ seq: 1, t: 100, type: "user", text: "go" }],
    { hasMore: false, nextBefore: 0, lastSeq: 1 },
  );
  assert.equal(started.activity.phase, "starting");

  const finished = chatEventStateProjector.append(started, [
    { seq: 2, t: 900, type: "complete", usage: { input_tokens: 12, output_tokens: 8 } },
  ]);

  assert.equal(finished.activity.phase, "idle");
  assert.equal(finished.activity.tokens, 20);
});

test("an empty state starts idle", () => {
  assert.equal(chatEventStateProjector.empty().activity.phase, "idle");
});

// On Auto the model that answered is not the one the composer named when the
// prompt was typed, so the turn has to carry the decision itself.
test("carries the routing decision onto the user block", () => {
  const routing = {
    provider: "claude",
    model: "haiku",
    ruleId: "chat-mode",
    rule: "Chat mode is cheap",
    reason: 'rule "Chat mode is cheap" matched',
  };
  const state = chatEventStateProjector.fromEvents(
    [
      { seq: 1, t: 1, type: "user", text: "pinned turn" },
      { seq: 2, t: 2, type: "user", text: "routed turn", routing },
      { seq: 3, t: 3, type: "user", text: "routed check", synthetic: "autotest", routing },
    ],
    { hasMore: false, nextBefore: 0, lastSeq: 3 },
  );

  assert.deepEqual(state.blocks, [
    { type: "user", text: "pinned turn", t: 1 },
    { type: "user", text: "routed turn", t: 2, routing },
    { type: "user", text: "routed check", t: 3, synthetic: "autotest", routing },
  ]);
});
