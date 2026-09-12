import assert from "node:assert/strict";
import test from "node:test";
import { chatPreferenceState } from "./chatPreferenceState.ts";

test("preserves normalized skill identity and chat defaults", () => {
  const selected = [{ name: "Review", command: " /REVIEW ", provider: "codex" as const }];
  const duplicate = { name: "review", command: "/review", provider: "codex" as const };

  assert.equal(chatPreferenceState.includesSkill(selected, duplicate, "claude"), true);
  assert.deepEqual(chatPreferenceState.withoutSkill(selected, duplicate, "claude"), []);
  assert.deepEqual(
    chatPreferenceState.resolveMeta(
      { id: "chat", title: "Chat", createdAt: 1, lastMessageAt: 1 },
      null,
      {
        provider: "codex",
        model: "gpt-5",
        mode: "default",
        reasoningEffort: "high",
        serviceTier: "priority",
        approvalPolicy: "on-request",
        sandboxPolicy: "workspaceWrite",
      }
    ),
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "codex",
      model: "gpt-5",
      mode: "default",
      reasoningEffort: "high",
      serviceTier: "priority",
      approvalPolicy: "on-request",
      sandboxPolicy: "workspaceWrite",
      selectedSkills: [],
      // A chat stored before automatic routing existed reads back as pinned,
      // which is the behaviour it already had.
      modelPolicy: "pinned",
      // Likewise for a chat stored before third-party agent endpoints: no
      // endpoint means the vendor's own, which is what it always ran on.
      endpointId: "",
    }
  );
});

test("a chat switched to Auto resolves to the auto model policy", () => {
  const resolved = chatPreferenceState.resolveMeta(
    { id: "chat", title: "Chat", createdAt: 1, lastMessageAt: 1, modelPolicy: "auto" },
    null,
    {
      provider: "codex",
      model: "gpt-5",
      mode: "default",
      reasoningEffort: "",
      serviceTier: "",
      approvalPolicy: "on-request",
      sandboxPolicy: "workspaceWrite",
    }
  );
  assert.equal(resolved.modelPolicy, "auto");
  assert.equal(resolved.model, "gpt-5", "Auto must not erase the model the chat falls back to");
});

test("keeps the scheduled-tasks skill identity stable after workspace provisioning", () => {
  const selected = [{
    name: "Scheduled Tasks",
    command: "scheduled-tasks",
    provider: "codex" as const,
    source: "remote",
  }];
  const provisioned = {
    name: "Scheduled Tasks",
    command: "scheduled-tasks",
    provider: "codex" as const,
    source: "project",
  };

  assert.equal(chatPreferenceState.includesSkill(selected, provisioned, "claude"), true);
  assert.deepEqual(chatPreferenceState.withoutSkill(selected, provisioned, "claude"), []);
});

test("prefers live workspace selections from another client", () => {
  const resolved = chatPreferenceState.resolveMeta(
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "claude",
      model: "claude-opus-current",
      mode: "plan",
      reasoningEffort: "high",
      serviceTier: "fast",
    },
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "codex",
      model: "gpt-previous",
      mode: "default",
      reasoningEffort: "low",
      serviceTier: "default",
    },
    {
      provider: "codex",
      model: "",
      mode: "default",
      reasoningEffort: "",
      serviceTier: "",
      approvalPolicy: "on-request",
      sandboxPolicy: "workspaceWrite",
    },
  );

  assert.equal(resolved.provider, "claude");
  assert.equal(resolved.model, "claude-opus-current");
  assert.equal(resolved.mode, "plan");
  assert.equal(resolved.reasoningEffort, "high");
  assert.equal(resolved.serviceTier, "fast");
});

test("does not restore stale detail skills after a live workspace removal", () => {
  const resolved = chatPreferenceState.resolveMeta(
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "codex",
    },
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "codex",
      selectedSkills: [{
        name: "Code Refactorer",
        command: "code-refactorer",
        provider: "codex",
        source: "project",
      }],
    },
    {
      provider: "codex",
      model: "",
      mode: "default",
      reasoningEffort: "",
      serviceTier: "",
      approvalPolicy: "on-request",
      sandboxPolicy: "workspaceWrite",
    },
  );

  assert.deepEqual(resolved.selectedSkills, []);
});

test("preserves an explicit per-chat Auto selection", () => {
  const resolved = chatPreferenceState.resolveMeta(
    {
      id: "chat",
      title: "Chat",
      createdAt: 1,
      lastMessageAt: 1,
      provider: "claude",
      model: "",
      mode: "default",
      reasoningEffort: "",
      serviceTier: "",
    },
    null,
    {
      provider: "codex",
      model: "gpt-global-default",
      mode: "plan",
      reasoningEffort: "high",
      serviceTier: "fast",
      approvalPolicy: "on-request",
      sandboxPolicy: "workspaceWrite",
    },
  );

  assert.equal(resolved.provider, "claude");
  assert.equal(resolved.model, "");
  assert.equal(resolved.mode, "default");
  assert.equal(resolved.reasoningEffort, "");
  assert.equal(resolved.serviceTier, "");
});
