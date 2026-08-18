import assert from "node:assert/strict";
import test from "node:test";
import type { TrashedProject } from "../../models/project.ts";
import type { Snapshot, SnapshotJob } from "../../models/snapshot.ts";
import { snapshotState } from "./snapshotState.ts";

const now = Date.UTC(2026, 7, 18, 12, 0, 0);
const day = 24 * 60 * 60 * 1000;

function snapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return {
    id: "snap-1",
    kind: "manual",
    status: "ready",
    createdAt: now,
    hasDatabase: false,
    includesSecrets: false,
    ...overrides,
  };
}

function job(overrides: Partial<SnapshotJob> = {}): SnapshotJob {
  return {
    id: "job-1",
    projectId: "p1",
    kind: "capture",
    status: "ready",
    startedAt: now,
    ...overrides,
  };
}

function trashed(overrides: Partial<TrashedProject> = {}): TrashedProject {
  return {
    id: "p1",
    name: "Demo",
    slug: "demo",
    cwd: "/var/lib/remote/projects/demo/workspace",
    containerName: "demo",
    status: "missing",
    createdAt: now - 30 * day,
    updatedAt: now,
    deletedAt: now,
    expiresAt: now + 7 * day,
    ...overrides,
  };
}

test("only a settled snapshot can be restored", () => {
  assert.equal(snapshotState.restorable(snapshot({ status: "ready" })), true);
  assert.equal(snapshotState.restorable(snapshot({ status: "running" })), false);
  assert.equal(snapshotState.restorable(snapshot({ status: "failed" })), false);
  assert.equal(snapshotState.isSettling(snapshot({ status: "pending" })), true);
  assert.equal(snapshotState.isSettling(snapshot({ status: "failed" })), false);
});

test("polling continues while a record or a job is still in flight", () => {
  assert.equal(snapshotState.hasRunningJob([], []), false);
  assert.equal(snapshotState.hasRunningJob([snapshot({ status: "running" })], []), true);
  assert.equal(snapshotState.hasRunningJob([], [job({ status: "running" })]), true);
  assert.equal(
    snapshotState.hasRunningJob([snapshot()], [job({ status: "failed" })]),
    false,
  );
});

test("a failed restore is surfaced even though no record carries its error", () => {
  assert.equal(snapshotState.failedRestore([job()]), null);
  assert.equal(snapshotState.failedRestore([job({ kind: "restore", status: "running" })]), null);
  const failed = job({ id: "job-9", kind: "restore", status: "failed", error: "archive truncated" });
  assert.equal(snapshotState.failedRestore([failed, job()])?.id, "job-9");
});

test("describes what an archive holds", () => {
  assert.equal(snapshotState.describeContents(snapshot()), "workspace + agent homes");
  assert.equal(
    snapshotState.describeContents(snapshot({ hasDatabase: true, databaseEngine: "mysql" })),
    "workspace + agent homes, mysql database",
  );
  assert.equal(
    snapshotState.describeContents(snapshot({ hasDatabase: true, includesSecrets: true })),
    "workspace + agent homes, database, secrets",
  );
});

test("status wording carries the failure cause", () => {
  assert.equal(snapshotState.describeStatus(snapshot()), "Ready");
  assert.equal(snapshotState.describeStatus(snapshot({ status: "pending" })), "Queued");
  assert.equal(snapshotState.describeStatus(snapshot({ status: "running" })), "Packing…");
  assert.equal(
    snapshotState.describeStatus(snapshot({ status: "failed", error: "no space left" })),
    "Failed: no space left",
  );
  assert.equal(snapshotState.describeStatus(snapshot({ status: "failed" })), "Failed");
});

test("counts whole days left in the trash, rounding up", () => {
  assert.equal(snapshotState.daysLeft(trashed(), now), 7);
  assert.equal(snapshotState.daysLeft(trashed({ expiresAt: now + day / 2 }), now), 1);
  assert.equal(snapshotState.daysLeft(trashed({ expiresAt: now - day }), now), 0);
  assert.equal(snapshotState.daysLeft(trashed({ expiresAt: 0 }), now), null);
  assert.equal(snapshotState.daysLeft(trashed({ expiresAt: undefined }), now), null);
});

test("describes the retention window in words", () => {
  assert.equal(snapshotState.describeRetention(trashed(), now), "7 days left");
  assert.equal(
    snapshotState.describeRetention(trashed({ expiresAt: now + day }), now),
    "1 day left",
  );
  assert.equal(
    snapshotState.describeRetention(trashed({ expiresAt: now - day }), now),
    "Purged within the next sweep",
  );
  assert.equal(
    snapshotState.describeRetention(trashed({ expiresAt: undefined }), now),
    "Kept until an admin purges it",
  );
});

test("replacing a record keeps the newest-first order", () => {
  const list = [snapshot({ id: "a", createdAt: 3 }), snapshot({ id: "b", createdAt: 2 })];
  const updated = snapshotState.replace(list, snapshot({ id: "b", createdAt: 2, status: "failed" }));
  assert.deepEqual(
    updated.map((entry) => entry.id),
    ["a", "b"],
  );
  assert.equal(updated[1].status, "failed");

  const inserted = snapshotState.replace(list, snapshot({ id: "c", createdAt: 4 }));
  assert.deepEqual(
    inserted.map((entry) => entry.id),
    ["c", "a", "b"],
  );
});

test("removes records and trashed projects by id", () => {
  const list = [snapshot({ id: "a" }), snapshot({ id: "b" })];
  assert.deepEqual(
    snapshotState.remove(list, "a").map((entry) => entry.id),
    ["b"],
  );
  const projects = [trashed({ id: "p1" }), trashed({ id: "p2" })];
  assert.deepEqual(
    snapshotState.removeProject(projects, "p1").map((entry) => entry.id),
    ["p2"],
  );
});
