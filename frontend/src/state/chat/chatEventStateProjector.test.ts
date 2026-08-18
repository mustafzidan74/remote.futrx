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
