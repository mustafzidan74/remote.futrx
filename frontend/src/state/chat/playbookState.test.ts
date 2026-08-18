import assert from "node:assert/strict";
import test from "node:test";
import type { SelectedSkill } from "../../models/chat.ts";
import type { Playbook } from "../../models/playbook.ts";
import {
  firstUnresolvedRange,
  playbookChatPatch,
  playbookLabel,
  resolvePlaybookPrompt,
  sortPlaybooks,
  unresolvedSummary,
} from "./playbookState.ts";

const context = {
  projectName: "Acme Shop",
  slug: "acme-shop",
  previewUrl: "https://acme-shop--3000.dev.mz-ss.tech",
};

test("resolves every known placeholder against the chat's project", () => {
  const resolved = resolvePlaybookPrompt(
    "Deploy {{project}} ({{slug}}) and test {{previewUrl}}",
    context,
  );

  assert.equal(
    resolved.text,
    "Deploy Acme Shop (acme-shop) and test https://acme-shop--3000.dev.mz-ss.tech",
  );
  assert.equal(resolved.ready, true);
  assert.deepEqual(resolved.unresolved, []);
});

test("keeps a placeholder nobody can fill and reports where it is", () => {
  const resolved = resolvePlaybookPrompt("Audit {{askUrl}} for {{project}}", context);

  assert.equal(resolved.text, "Audit {{askUrl}} for Acme Shop");
  assert.equal(resolved.ready, false);
  assert.equal(resolved.unresolved.length, 1);
  assert.equal(resolved.unresolved[0].name, "askUrl");
  assert.equal(
    resolved.text.slice(resolved.unresolved[0].start, resolved.unresolved[0].end),
    "{{askUrl}}",
  );
});

test("positions later placeholders against the resolved text, not the template", () => {
  const resolved = resolvePlaybookPrompt("{{project}} then {{askUrl}}", context);
  const range = firstUnresolvedRange(resolved);

  assert.ok(range);
  assert.equal(resolved.text.slice(range.start, range.end), "{{askUrl}}");
});

test("treats a missing project as an unresolved placeholder", () => {
  const resolved = resolvePlaybookPrompt("Open {{previewUrl}} in {{project}}", {});

  assert.equal(resolved.text, "Open {{previewUrl}} in {{project}}");
  assert.equal(resolved.ready, false);
  assert.equal(unresolvedSummary(resolved), "{{previewUrl}}, {{project}}");
});

test("treats a blank value as unresolved rather than substituting nothing", () => {
  const resolved = resolvePlaybookPrompt("Deploy {{project}}", { projectName: "   " });

  assert.equal(resolved.text, "Deploy {{project}}");
  assert.equal(resolved.ready, false);
});

test("leaves unknown placeholder names alone", () => {
  const resolved = resolvePlaybookPrompt("Ping {{whoKnows}}", context);

  assert.equal(resolved.text, "Ping {{whoKnows}}");
  assert.equal(resolved.unresolved[0].name, "whoKnows");
});

test("tolerates whitespace inside the braces and repeated placeholders", () => {
  const resolved = resolvePlaybookPrompt("{{ project }} and {{project}}", context);

  assert.equal(resolved.text, "Acme Shop and Acme Shop");
  assert.equal(resolved.ready, true);
});

test("a prompt with no placeholders is returned untouched", () => {
  const resolved = resolvePlaybookPrompt("Run the tests", context);

  assert.equal(resolved.text, "Run the tests");
  assert.equal(resolved.ready, true);
  assert.equal(firstUnresolvedRange(resolved), null);
});

function playbook(overrides: Partial<Playbook> = {}): Playbook {
  return {
    id: "security-review",
    title: "🔒 Security review",
    prompt: "Review it",
    order: 0,
    ...overrides,
  };
}

test("merges the playbook's skills into the current selection", () => {
  const current: SelectedSkill[] = [
    { name: "wp-guard", command: "wp-guard", provider: "claude", source: "global" },
  ];

  const applied = playbookChatPatch(
    playbook({
      skills: [
        { name: "wp-guard", command: "wp-guard", source: "global" },
        { name: "test-guard", command: "test-guard", source: "global" },
      ],
    }),
    { provider: "claude", selectedSkills: current, mode: "code" },
  );

  assert.equal(applied.changed, true);
  assert.deepEqual(
    applied.patch.selectedSkills?.map((skill) => skill.command),
    ["wp-guard", "test-guard"],
  );
  assert.equal(applied.patch.provider, undefined);
});

test("stamps the target provider onto skills that do not name one", () => {
  const applied = playbookChatPatch(
    playbook({ skills: [{ command: "test-guard", source: "global" }] }),
    { provider: "codex", selectedSkills: [] },
  );

  assert.equal(applied.patch.selectedSkills?.[0].provider, "codex");
  assert.equal(applied.patch.selectedSkills?.[0].name, "test-guard");
});

test("a provider switch drops the previous provider's skills and resets the model", () => {
  const applied = playbookChatPatch(
    playbook({
      provider: "claude",
      skills: [{ command: "wp-guard", source: "global" }],
    }),
    {
      provider: "codex",
      selectedSkills: [
        { name: "codex-only", command: "codex-only", provider: "codex", source: "project" },
      ],
    },
  );

  assert.equal(applied.patch.provider, "claude");
  assert.equal(applied.patch.model, "");
  assert.equal(applied.patch.reasoningEffort, "");
  assert.equal(applied.patch.serviceTier, "");
  assert.deepEqual(
    applied.patch.selectedSkills?.map((skill) => skill.command),
    ["wp-guard"],
  );
  assert.equal(applied.provider, "claude");
});

test("sets the mode only when the playbook asks for a different one", () => {
  const same = playbookChatPatch(playbook({ mode: "review" }), {
    provider: "claude",
    selectedSkills: [],
    mode: "review",
  });
  assert.equal(same.patch.mode, undefined);
  assert.equal(same.changed, false);

  const different = playbookChatPatch(playbook({ mode: "review" }), {
    provider: "claude",
    selectedSkills: [],
    mode: "code",
  });
  assert.equal(different.patch.mode, "review");
  assert.equal(different.changed, true);
});

test("a playbook whose skills are already selected produces no patch", () => {
  const current: SelectedSkill[] = [
    { name: "wp-guard", command: "wp-guard", provider: "claude", source: "global" },
  ];

  const applied = playbookChatPatch(
    playbook({ skills: [{ command: "wp-guard", source: "global" }] }),
    { provider: "claude", selectedSkills: current },
  );

  assert.equal(applied.changed, false);
  assert.deepEqual(applied.patch, {});
});

test("skips skill refs that name nothing", () => {
  const applied = playbookChatPatch(playbook({ skills: [{ source: "global" }, { command: "  " }] }), {
    provider: "claude",
    selectedSkills: [],
  });

  assert.equal(applied.changed, false);
});

test("sorts playbooks by their stored order", () => {
  const sorted = sortPlaybooks([
    playbook({ id: "c", order: 2 }),
    playbook({ id: "a", order: 0 }),
    playbook({ id: "b", order: 1 }),
  ]);

  assert.deepEqual(sorted.map((entry) => entry.id), ["a", "b", "c"]);
});

test("does not print the emoji twice when the title already carries it", () => {
  assert.equal(playbookLabel(playbook({ title: "🔒 Security review", icon: "🔒" })), "🔒 Security review");
  assert.equal(playbookLabel(playbook({ title: "Security review", icon: "🔒" })), "🔒 Security review");
  assert.equal(playbookLabel(playbook({ title: "Security review", icon: "" })), "Security review");
});
