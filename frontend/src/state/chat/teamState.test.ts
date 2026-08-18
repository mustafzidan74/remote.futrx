import assert from "node:assert/strict";
import test from "node:test";
import type { ChatMeta, ChatProvider } from "../../models/chat.ts";
import {
  TEAM_DEFAULT_LOOPS,
  TEAM_MAX_LOOPS,
  armTeamPatch,
  boundedLoops,
  isCompanionChat,
  reviewerFallback,
  teamPillLabel,
  teamProviderOptions,
  teamRunning,
  teamView,
  verdictLabel,
} from "./teamState.ts";

function chat(overrides: Partial<ChatMeta> = {}): ChatMeta {
  return {
    id: "abcd1234",
    title: "Ship the checkout flow",
    provider: "claude",
    createdAt: 0,
    lastMessageAt: 0,
    ...overrides,
  };
}

const ALL: ChatProvider[] = ["claude", "codex", "kimi"];

test("a chat with no team policy reads as off with the documented defaults", () => {
  const view = teamView(chat(), ALL);

  assert.equal(view.enabled, false);
  assert.equal(view.maxLoops, TEAM_DEFAULT_LOOPS);
  assert.equal(view.loopsUsed, 0);
  assert.equal(view.autoFix, true);
  assert.equal(view.status, "Off");
  // The implementer seat is the chat itself and can never be switched off.
  assert.equal(view.implementer.enabled, true);
  assert.equal(view.implementer.provider, "claude");
});

test("the reviewer falls back to a different connected provider", () => {
  // Fresh eyes: a Claude chat must not be reviewed by Claude when another
  // provider is available.
  assert.equal(reviewerFallback("claude", ALL), "codex");
  assert.equal(reviewerFallback("codex", ["codex", "kimi"]), "kimi");
  assert.equal(reviewerFallback("codex", ["codex", "claude"]), "claude");
  // With one provider connected the reviewer runs on it, in a companion chat.
  assert.equal(reviewerFallback("claude", ["claude"]), "claude");
  assert.equal(reviewerFallback("kimi", []), "kimi");
});

test("a resolved seat is marked as chosen by the platform", () => {
  const resolved = teamView(chat(), ALL);
  assert.equal(resolved.reviewer.provider, "codex");
  assert.equal(resolved.reviewer.resolved, true);

  const explicit = teamView(
    chat({
      team: {
        enabled: true,
        roles: {
          implementer: { enabled: true },
          reviewer: { provider: "kimi", enabled: true },
          tester: { enabled: true },
        },
      },
    }),
    ALL,
  );
  assert.equal(explicit.reviewer.provider, "kimi");
  assert.equal(explicit.reviewer.resolved, false);
  // The tester follows the reviewer when it has no opinion of its own.
  assert.equal(explicit.tester.provider, "kimi");
});

test("a single connected provider is reported so the panel can say so", () => {
  assert.equal(teamView(chat(), ["claude"]).singleProvider, true);
  assert.equal(teamView(chat(), ALL).singleProvider, false);
});

test("the pill names the hop rather than the switch", () => {
  assert.equal(teamPillLabel("reviewing", 0, 2, ""), "Team: reviewing…");
  assert.equal(teamPillLabel("testing", 1, 2, ""), "Team: testing…");
  assert.equal(teamPillLabel("fixing", 1, 2, "fix"), "Team: fix 1/2");
  assert.equal(teamPillLabel("done", 2, 2, "pass"), "Team: PASS");
  assert.equal(teamPillLabel("error", 1, 2, ""), "Team: stopped");
  assert.equal(teamPillLabel("", 0, 2, ""), "Team: ready");
});

test("only an in-flight hop counts as running", () => {
  for (const phase of ["reviewing", "testing", "fixing"] as const) {
    assert.equal(teamRunning(phase), true, phase);
  }
  for (const phase of ["", "done", "error"] as const) {
    assert.equal(teamRunning(phase), false, phase || "idle");
  }
});

test("loop counts are clamped to the bounds the server enforces", () => {
  assert.equal(boundedLoops(undefined), TEAM_DEFAULT_LOOPS);
  assert.equal(boundedLoops(0), TEAM_DEFAULT_LOOPS);
  assert.equal(boundedLoops(1), 1);
  assert.equal(boundedLoops(99), TEAM_MAX_LOOPS);
  assert.equal(boundedLoops(-4), 1);
});

test("arming sends a complete cast so the server records what the user saw", () => {
  const patch = armTeamPatch(chat(), ALL);

  assert.equal(patch.enabled, true);
  assert.equal(patch.maxLoops, TEAM_DEFAULT_LOOPS);
  assert.equal(patch.autoFix, true);
  assert.equal(patch.roles?.implementer?.provider, "claude");
  assert.equal(patch.roles?.reviewer?.provider, "codex");
  assert.equal(patch.roles?.reviewer?.enabled, true);
  assert.equal(patch.roles?.tester?.enabled, true);
});

test("only connected providers are offered for a seat", () => {
  const options = teamProviderOptions(["codex", "kimi"]);

  assert.deepEqual(
    options.map((option) => option.value),
    ["codex", "kimi"],
  );
  assert.equal(options[0].label, "Codex");
  assert.deepEqual(teamProviderOptions([]), []);
});

test("the timeline carries the verdict, the loop, and the chat to open", () => {
  const view = teamView(
    chat({
      team: {
        enabled: true,
        maxLoops: 2,
        loopsUsed: 1,
        phase: "fixing",
        verdict: "fix",
        roles: {
          implementer: { enabled: true },
          reviewer: { provider: "codex", enabled: true, chatId: "rev00001" },
          tester: { provider: "codex", enabled: true },
        },
        hops: [
          { loop: 0, role: "reviewer", kind: "team-review", chatId: "rev00001", at: 1 },
          {
            loop: 1,
            role: "implementer",
            kind: "team-fix",
            chatId: "abcd1234",
            verdict: "fix",
            findings: "1. handle the empty cart",
            at: 2,
          },
        ],
      },
    }),
    ALL,
  );

  assert.equal(view.hops.length, 2);
  assert.equal(view.hops[0].label, "Reviewer");
  assert.equal(view.hops[0].chatId, "rev00001");
  assert.equal(view.hops[1].verdictLabel, "FIX");
  assert.equal(view.hops[1].findings, "1. handle the empty cart");
  assert.match(view.hops[1].detail, /^Implementer · loop 1 · /);
  assert.equal(view.pillLabel, "Team: fix 1/2");
  assert.equal(view.reviewer.chatId, "rev00001");
});

test("every verdict has a word, and an absent one has none", () => {
  assert.equal(verdictLabel("ship"), "SHIP");
  assert.equal(verdictLabel("fix"), "FIX");
  assert.equal(verdictLabel("pass"), "PASS");
  assert.equal(verdictLabel("fail"), "FAIL");
  assert.equal(verdictLabel("unknown"), "no verdict");
  assert.equal(verdictLabel(""), "");
});

test("companion chats are recognised by the parent they answer to", () => {
  assert.equal(isCompanionChat(chat()), false);
  assert.equal(
    isCompanionChat(chat({ companionOf: "abcd1234", companionRole: "reviewer" })),
    true,
  );
});
