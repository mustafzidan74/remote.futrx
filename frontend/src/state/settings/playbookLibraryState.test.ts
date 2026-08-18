import assert from "node:assert/strict";
import test from "node:test";
import type { Playbook } from "../../models/playbook.ts";
import type { RegisteredSkill } from "../../models/skill.ts";
import {
  hasPlaybookSkill,
  movePlaybook,
  newPlaybook,
  playbookIdFromTitle,
  playbookLibraryProblem,
  playbookLibraryRequest,
  removePlaybook,
  togglePlaybookSkill,
  unknownPlaybookSkills,
  updatePlaybook,
} from "./playbookLibraryState.ts";

function entry(overrides: Partial<Playbook> = {}): Playbook {
  return { id: "one", title: "One", prompt: "Do it", order: 0, ...overrides };
}

function skill(overrides: Partial<RegisteredSkill> = {}): RegisteredSkill {
  return { name: "wp-guard", command: "wp-guard", provider: "claude", source: "global", ...overrides };
}

test("derives a slug id from the title and keeps it unique", () => {
  assert.equal(playbookIdFromTitle("🔒 Security review", []), "security-review");
  assert.equal(playbookIdFromTitle("Security review", ["security-review"]), "security-review-2");
  assert.equal(
    playbookIdFromTitle("Security review", ["security-review", "security-review-2"]),
    "security-review-3",
  );
  assert.equal(playbookIdFromTitle("🔒🔒🔒", []), "playbook");
});

test("a new entry lands at the end of the library", () => {
  const created = newPlaybook([entry(), entry({ id: "two", order: 1 })]);
  assert.equal(created.order, 2);
  assert.equal(created.id, "new-playbook");
});

test("updates only the addressed entry", () => {
  const library = [entry(), entry({ id: "two", title: "Two", order: 1 })];
  const updated = updatePlaybook(library, "two", { title: "Renamed" });
  assert.equal(updated[0].title, "One");
  assert.equal(updated[1].title, "Renamed");
});

test("removing an entry renumbers the rest", () => {
  const library = [entry(), entry({ id: "two", order: 1 }), entry({ id: "three", order: 2 })];
  const remaining = removePlaybook(library, "two");
  assert.deepEqual(remaining.map((item) => [item.id, item.order]), [
    ["one", 0],
    ["three", 1],
  ]);
});

test("moving swaps neighbours and renumbers", () => {
  const library = [entry(), entry({ id: "two", order: 1 }), entry({ id: "three", order: 2 })];
  const moved = movePlaybook(library, "three", -1);
  assert.deepEqual(moved.map((item) => item.id), ["one", "three", "two"]);
  assert.deepEqual(moved.map((item) => item.order), [0, 1, 2]);
});

test("moving past either end is a no-op", () => {
  const library = [entry(), entry({ id: "two", order: 1 })];
  assert.equal(movePlaybook(library, "one", -1), library);
  assert.equal(movePlaybook(library, "two", 1), library);
  assert.equal(movePlaybook(library, "absent", 1), library);
});

test("toggling a skill adds it, then removes the same one", () => {
  const library = [entry()];
  const added = togglePlaybookSkill(library, "one", skill());
  assert.equal(hasPlaybookSkill(added[0], skill()), true);
  assert.deepEqual(added[0].skills, [{ name: "wp-guard", command: "wp-guard", source: "global" }]);

  const removed = togglePlaybookSkill(added, "one", skill());
  assert.deepEqual(removed[0].skills, []);
});

test("the same command from two sources are different selections", () => {
  const library = togglePlaybookSkill([entry()], "one", skill({ source: "global" }));
  const both = togglePlaybookSkill(library, "one", skill({ source: "project" }));
  assert.equal(both[0].skills?.length, 2);
});

test("flags skill refs the server does not publish", () => {
  const playbook = entry({
    skills: [
      { command: "wp-guard", source: "global" },
      { command: "not-installed", source: "global" },
    ],
  });
  assert.deepEqual(unknownPlaybookSkills(playbook, [skill()]), ["not-installed"]);
  assert.deepEqual(unknownPlaybookSkills(entry(), [skill()]), []);
});

test("the request trims fields, drops blank rows, and renumbers", () => {
  const request = playbookLibraryRequest([
    entry({ id: "one", title: "  One  ", prompt: "  Do it  ", icon: " 🔒 ", hint: " short ", order: 5 }),
    entry({ id: "blank", title: "   ", prompt: "  ", order: 6 }),
    entry({ id: "two", title: "Two", prompt: "Also", order: 7 }),
  ]);

  assert.deepEqual(request.map((item) => [item.id, item.order]), [
    ["one", 0],
    ["two", 1],
  ]);
  assert.equal(request[0].title, "One");
  assert.equal(request[0].prompt, "Do it");
  assert.equal(request[0].icon, "🔒");
  assert.equal(request[0].hint, "short");
});

test("reports the first blocking problem, or none", () => {
  assert.equal(playbookLibraryProblem([entry()]), null);
  assert.equal(playbookLibraryProblem([entry({ title: "  " })]), "Every playbook needs a title.");
  assert.equal(
    playbookLibraryProblem([entry({ title: "Ship it", prompt: "  " })]),
    '"Ship it" needs a prompt.',
  );
});
