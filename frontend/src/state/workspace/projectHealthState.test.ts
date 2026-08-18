import assert from "node:assert/strict";
import test from "node:test";
import type { ProjectHealth } from "../../models/health.ts";
import type { ProjectMeta } from "../../models/project.ts";
import { projectHealthState } from "./projectHealthState.ts";
import { projectDeepLinkState } from "./projectDeepLink.ts";

function project(id: string, status: ProjectMeta["status"] = "running"): ProjectMeta {
  return {
    id,
    name: `Project ${id}`,
    slug: `project-${id}`,
    cwd: `/${id}`,
    containerName: id,
    status,
    createdAt: 1,
    updatedAt: 1,
  };
}

function health(projectId: string, overrides: Partial<ProjectHealth> = {}): ProjectHealth {
  return { projectId, status: "ok", ...overrides };
}

test("replace builds a map keyed by project id and ignores malformed rows", () => {
  const map = projectHealthState.replace([
    health("a", { status: "warn" }),
    health("b"),
    { projectId: "", status: "ok" },
  ]);
  assert.deepEqual(Object.keys(map).sort(), ["a", "b"]);
  assert.equal(map.a.status, "warn");
  assert.deepEqual(projectHealthState.replace(undefined), {});
});

test("apply upserts a row and clears it when the monitor sends none", () => {
  const first = projectHealthState.apply({}, "a", health("a", { status: "crit" }));
  assert.equal(first.a.status, "crit");

  const updated = projectHealthState.apply(first, "a", health("a", { status: "ok" }));
  assert.equal(updated.a.status, "ok");
  assert.notEqual(updated, first, "a changed row must produce a new map");

  const cleared = projectHealthState.apply(updated, "a");
  assert.deepEqual(cleared, {});

  const noop = projectHealthState.apply(cleared, "a");
  assert.equal(noop, cleared, "clearing an absent row must not churn the map");

  assert.equal(projectHealthState.apply(cleared, ""), cleared);
});

test("the dot shows health when the monitor has a reading", () => {
  const cases: Array<{
    name: string;
    health: ProjectHealth;
    tone: string;
    label: string;
  }> = [
    { name: "ok", health: health("a"), tone: "green", label: "Healthy" },
    { name: "warn", health: health("a", { status: "warn" }), tone: "amber", label: "Degraded" },
    { name: "crit", health: health("a", { status: "crit" }), tone: "red", label: "Critical" },
  ];
  for (const testCase of cases) {
    const dot = projectHealthState.dot(project("a"), testCase.health);
    assert.equal(dot.tone, testCase.tone, testCase.name);
    assert.equal(dot.label, testCase.label, testCase.name);
    assert.equal(dot.monitored, true, testCase.name);
  }
});

test("the dot falls back to the lifecycle status without a reading", () => {
  const cases: Array<{ status: ProjectMeta["status"]; tone: string; label: string }> = [
    { status: "running", tone: "green", label: "Running" },
    { status: "stopped", tone: "grey", label: "Stopped" },
    { status: "provisioning", tone: "amber", label: "Provisioning" },
    { status: "error", tone: "red", label: "Error" },
    { status: "missing", tone: "red", label: "Missing - needs reprovision" },
    { status: "", tone: "grey", label: "Unknown" },
  ];
  for (const testCase of cases) {
    const dot = projectHealthState.dot(project("a", testCase.status));
    assert.equal(dot.tone, testCase.tone, testCase.status || "empty");
    assert.equal(dot.label, testCase.label, testCase.status || "empty");
    assert.equal(dot.monitored, false, testCase.status || "empty");
  }
});

test("an unknown reading falls back to lifecycle but says so", () => {
  const dot = projectHealthState.dot(project("a"), health("a", { status: "unknown" }));
  assert.equal(dot.tone, "green");
  assert.equal(dot.monitored, false);
  assert.equal(dot.title, "Running · health unknown");
});

test("the tooltip lists every reason under the verdict", () => {
  const dot = projectHealthState.dot(
    project("a"),
    health("a", {
      status: "crit",
      reasons: ["memory 94% (1.41/1.5 GiB)", "the app on port 3000 is not answering"],
    })
  );
  assert.equal(
    dot.title,
    "Critical\nmemory 94% (1.41/1.5 GiB)\nthe app on port 3000 is not answering"
  );
});

test("a healthy tooltip still reports the memory it measured", () => {
  const dot = projectHealthState.dot(project("a"), health("a", { memoryPct: 42 }));
  assert.equal(dot.title, "Healthy\nmemory 42%");
});

test("the memory meter needs both a reading and a limit", () => {
  assert.equal(projectHealthState.memoryPercent(undefined), undefined);
  assert.equal(projectHealthState.memoryPercent(health("a", { memoryPct: 40 })), undefined);
  assert.equal(
    projectHealthState.memoryPercent(
      health("a", { memoryUsedBytes: 400, memoryLimitBytes: 1000, memoryPct: 40 })
    ),
    40
  );
});

test("a project deep link only accepts well-formed ids", () => {
  assert.equal(projectDeepLinkState.parse("?project=ABCD1234"), "abcd1234");
  assert.equal(projectDeepLinkState.parse("?project=../etc/passwd"), null);
  assert.equal(projectDeepLinkState.parse("?project=xyz"), null);
  assert.equal(projectDeepLinkState.parse("?chat=abcd"), null);
  assert.equal(projectDeepLinkState.parse(""), null);
});

test("consuming a project deep link leaves the rest of the query intact", () => {
  assert.equal(
    projectDeepLinkState.withoutProjectParam("/", "?project=abcd&tab=info", "#top"),
    "/?tab=info#top"
  );
  assert.equal(projectDeepLinkState.withoutProjectParam("/", "?project=abcd", ""), "/");
});
