import assert from "node:assert/strict";
import test from "node:test";
import type { ChatEvent } from "../../models/chat.ts";
import type { AssistantMessagePart } from "../../models/chatMessage.ts";
import {
  ACTIVITY_STUCK_MS,
  AgentActivityPreferenceStore,
  activityView,
  buildTurnTimeline,
  describeTool,
  emptyActivity,
  formatElapsed,
  formatGap,
  formatStepDuration,
  formatTokens,
  reduceActivity,
  timelineSummary,
  type AgentActivity,
} from "./agentActivity.ts";

type ToolPart = Extract<AssistantMessagePart, { kind: "tool" }>;

function fold(events: ChatEvent[]): AgentActivity {
  return events.reduce(reduceActivity, emptyActivity());
}

test("a chat with no events is idle", () => {
  const view = activityView(emptyActivity(), 1_000);

  assert.equal(view.phase, "idle");
  assert.equal(view.label, "");
  assert.equal(view.elapsed, "0:00");
  assert.equal(view.stuck, false);
});

test("a prompt with nothing back yet is starting", () => {
  const activity = fold([{ type: "user", text: "ship it", t: 1_000 }]);
  const view = activityView(activity, 1_000);

  assert.equal(activity.phase, "starting");
  assert.equal(view.icon, "⏳");
  assert.equal(view.label, "Starting…");
});

test("reasoning deltas read as thinking, answer deltas as writing", () => {
  const thinking = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "thinking", text: "weighing options", t: 1_200 },
  ]);
  assert.equal(thinking.phase, "thinking");
  assert.equal(activityView(thinking, 1_200).label, "Thinking…");

  const writing = reduceActivity(thinking, { type: "assistant_text", text: "Done", t: 1_400 });
  assert.equal(writing.phase, "writing");
  assert.equal(activityView(writing, 1_400).label, "Writing the answer…");
});

// The whole point of the priority order: a tool in flight is the most specific
// true thing to say, so it must survive text that arrived before it.
test("a running tool outranks reasoning and answer text", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "assistant_text", text: "Let me look", t: 1_100 },
    { type: "thinking", text: "hmm", t: 1_150 },
    {
      type: "tool_use_start",
      id: "t1",
      name: "Read",
      input: { file_path: "/root/site/wp-config.php" },
      t: 1_200,
    },
  ]);
  const view = activityView(activity, 1_200);

  assert.equal(activity.phase, "tool");
  assert.equal(view.icon, "📄");
  assert.equal(view.label, "Reading");
  assert.equal(view.target, "wp-config.php");
  assert.equal(view.title, "~/site/wp-config.php");
});

test("a tool that finishes hands the run back to thinking", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "tool_use_start", id: "t1", name: "Bash", input: { command: "ls" }, t: 1_100 },
    { type: "tool_use_end", id: "t1", output: "ok", t: 1_300 },
  ]);

  assert.equal(activity.phase, "thinking");
  assert.equal(activity.toolName, "");
});

test("complete and error end the run", () => {
  const done = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "assistant_text", text: "Done", t: 1_100 },
    { type: "complete", usage: { input_tokens: 4_000, output_tokens: 400 }, t: 1_200 },
  ]);
  assert.equal(done.phase, "idle");
  assert.equal(done.tokens, 4_400);
  assert.equal(activityView(done, 1_200).tokenLabel, "4.4k");

  const failed = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "error", message: "boom", t: 1_100 },
  ]);
  assert.equal(failed.phase, "idle");
});

test("the token counter stays hidden until a provider reports numbers", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "assistant_text", text: "working", t: 1_100 },
  ]);

  assert.equal(activityView(activity, 1_100).tokenLabel, "");
});

test("a new prompt clears the previous run's reasoning, tokens and clock", () => {
  const activity = fold([
    { type: "user", text: "first", t: 1_000 },
    { type: "thinking", text: "long thought", t: 1_100 },
    { type: "complete", usage: { input_tokens: 10 }, t: 1_200 },
    { type: "user", text: "second", t: 5_000 },
  ]);

  assert.equal(activity.reasoning, "");
  assert.equal(activity.tokens, 0);
  assert.equal(activity.startedAt, 5_000);
  // Sticky: the provider proved it reasons, so the toggle stays on offer.
  assert.equal(activity.sawReasoning, true);
});

test("a provider that never reasons is offered no thinking toggle", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "assistant_text", text: "Done", t: 1_100 },
  ]);

  assert.equal(activityView(activity, 1_100).canShowThinking, false);
});

test("reasoning accumulates across deltas for the expandable area", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 1_000 },
    { type: "thinking", text: "first ", t: 1_100 },
    { type: "thinking", text: "second", t: 1_150 },
  ]);

  assert.equal(activity.reasoning, "first second");
  assert.equal(activityView(activity, 1_150).canShowThinking, true);
});

test("elapsed counts from the prompt, not from the newest event", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 60_000 },
    { type: "assistant_text", text: "x", t: 100_000 },
  ]);

  assert.equal(activityView(activity, 155_000).elapsed, "1:35");
});

// A run that has said nothing for a minute and a half is the difference
// between "thinking hard" and "hung", and only the strip can tell the user.
test("silence past the threshold adds the still-working hint", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 0 },
    { type: "thinking", text: "hmm", t: 1_000 },
  ]);

  const quiet = activityView(activity, 1_000 + ACTIVITY_STUCK_MS - 1);
  assert.equal(quiet.stuck, false);
  assert.equal(quiet.stuckNote, "");

  const stuck = activityView(activity, 1_000 + ACTIVITY_STUCK_MS);
  assert.equal(stuck.stuck, true);
  assert.equal(stuck.stuckNote, "…still working (no output for 1m30s)");
});

test("an event of any kind resets the silence clock", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 0 },
    { type: "thinking", text: "hmm", t: 1_000 },
    { type: "system", subtype: "keepalive", t: 80_000 },
  ]);

  assert.equal(activityView(activity, 100_000).stuck, false);
});

test("a finished run never reports itself as stuck", () => {
  const activity = fold([
    { type: "user", text: "ship it", t: 0 },
    { type: "complete", t: 1_000 },
  ]);

  assert.equal(activityView(activity, 10_000_000).stuck, false);
});

test("tool labels cover the shapes the CLIs actually send", () => {
  assert.deepEqual(describeTool("Bash", { command: "wp plugin update --all" }), {
    icon: "⚡",
    label: "Running",
    target: "wp plugin update --all",
    title: "wp plugin update --all",
    detail: "",
  });

  const grep = describeTool("Grep", { pattern: "add_action", path: "/root/site" });
  assert.equal(grep.icon, "🔎");
  assert.equal(grep.label, "Searching the workspace");
  assert.equal(grep.target, "add_action in ~/site");

  const fetch = describeTool("WebFetch", { url: "https://mz-ss.tech/health" });
  assert.equal(fetch.icon, "🌐");
  assert.equal(fetch.label, "Fetching");
  assert.equal(fetch.target, "https://mz-ss.tech/health");
});

test("an edit shows its line counts when the payload carries them", () => {
  const single = describeTool("Edit", {
    file_path: "/root/site/functions.php",
    old_string: "a\nb\nc",
    new_string: "a\nb\nc\nd\ne",
  });
  assert.equal(single.icon, "✏️");
  assert.equal(single.label, "Editing");
  assert.equal(single.target, "functions.php");
  assert.equal(single.detail, "+5 −3");

  const multi = describeTool("MultiEdit", {
    file_path: "/root/site/functions.php",
    edits: [
      { old_string: "x", new_string: "x\ny" },
      { old_string: "p\nq", new_string: "p" },
    ],
  });
  assert.equal(multi.detail, "+3 −3");
});

test("an edit with no diff payload gets no invented counter", () => {
  assert.equal(describeTool("Edit", { file_path: "/root/a.txt" }).detail, "");
});

test("an unknown tool falls back to its own name", () => {
  assert.deepEqual(describeTool("SummonKraken", { anything: 1 }), {
    icon: "🛠️",
    label: "Running",
    target: "SummonKraken",
    title: "SummonKraken",
    detail: "",
  });
});

test("an MCP tool is split into server and tool, keeping the raw id", () => {
  const described = describeTool("mcp__playwright__browser_click", {});

  assert.equal(described.icon, "🔌");
  assert.equal(described.label, "Using");
  assert.equal(described.target, "playwright · browser_click");
  assert.equal(described.title, "mcp__playwright__browser_click");
});

test("a known tool with an empty payload still names itself", () => {
  const described = describeTool("Read", {});

  assert.equal(described.label, "Reading");
  assert.equal(described.target, "");
});

function toolPart(overrides: Partial<ToolPart> = {}): ToolPart {
  return {
    kind: "tool",
    id: "t1",
    name: "Read",
    input: { file_path: "/root/site/wp-config.php" },
    status: "done",
    startedAt: 1_000,
    endedAt: 1_450,
    ...overrides,
  };
}

test("the timeline turns tool parts into log rows with durations", () => {
  const steps = buildTurnTimeline([
    toolPart(),
    toolPart({
      id: "t2",
      name: "Bash",
      input: { command: "npm test" },
      startedAt: 1_500,
      endedAt: 8_000,
      output: "42 passing\nrest of the log",
    }),
  ]);

  assert.equal(steps.length, 2);
  assert.equal(steps[0].icon, "📄");
  assert.equal(steps[0].target, "wp-config.php");
  assert.equal(steps[0].duration, "450ms");
  assert.equal(steps[1].label, "Running");
  assert.equal(steps[1].duration, "6.5s");
  assert.equal(steps[1].note, "42 passing");
  assert.equal(timelineSummary(steps), "2 steps · 7s");
});

test("a failed step carries its error text into the row", () => {
  const steps = buildTurnTimeline([
    toolPart({ name: "Bash", input: { command: "false" }, isError: true, output: "exit status 1" }),
  ]);

  assert.equal(steps[0].isError, true);
  assert.equal(steps[0].note, "exit status 1");
  assert.equal(timelineSummary(steps), "1 step · 450ms");
});

test("a running step shows no duration yet", () => {
  const steps = buildTurnTimeline([toolPart({ status: "running", endedAt: undefined })]);

  assert.equal(steps[0].status, "running");
  assert.equal(steps[0].duration, "");
  assert.equal(timelineSummary(steps), "1 step");
});

// Events written before the timeline existed have no timestamps on their tool
// parts. They must still list, just without a duration column.
test("steps from a transcript with no timings still render", () => {
  const steps = buildTurnTimeline([
    toolPart({ startedAt: undefined, endedAt: undefined }),
  ]);

  assert.equal(steps[0].durationMs, 0);
  assert.equal(steps[0].duration, "");
});

test("formatters keep the strip one line", () => {
  assert.equal(formatElapsed(0), "0:00");
  assert.equal(formatElapsed(9_000), "0:09");
  assert.equal(formatElapsed(3_725_000), "62:05");
  assert.equal(formatGap(45_000), "45s");
  assert.equal(formatGap(90_000), "1m30s");
  assert.equal(formatTokens(980), "980");
  assert.equal(formatTokens(12_400), "12.4k");
  assert.equal(formatTokens(2_000_000), "2M");
  assert.equal(formatStepDuration(340), "340ms");
  assert.equal(formatStepDuration(1_240), "1.2s");
  assert.equal(formatStepDuration(125_000), "2m 05s");
});

test("the show-thinking choice survives a reload and a dead storage", () => {
  const values = new Map<string, string>();
  const store = new AgentActivityPreferenceStore({
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => void values.set(key, value),
  });

  assert.equal(store.showThinking(), false);
  store.setShowThinking(true);
  assert.equal(store.showThinking(), true);
  store.setShowThinking(false);
  assert.equal(store.showThinking(), false);

  const denied = new AgentActivityPreferenceStore({
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
  });
  assert.equal(denied.showThinking(), false);
  denied.setShowThinking(true);
});
