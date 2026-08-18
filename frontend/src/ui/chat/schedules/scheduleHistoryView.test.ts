import assert from "node:assert/strict";
import test from "node:test";
import type { ScheduledTask, ScheduleRunRecord } from "../../../models/schedule.ts";
import {
  chainLabel,
  describeChain,
  describeCondition,
  formatRunCost,
  formatRunDuration,
  parseDiffStat,
  runStatusLabel,
} from "./scheduleHistoryView.ts";

function record(overrides: Partial<ScheduleRunRecord> = {}): ScheduleRunRecord {
  return {
    runId: "run-1",
    taskId: "task-1",
    startedAt: 1000,
    finishedAt: 4000,
    durationMs: 3000,
    status: "ok",
    ...overrides,
  };
}

function task(overrides: Partial<ScheduledTask> = {}): ScheduledTask {
  return {
    id: "task-1",
    name: "Nightly backup",
    ownerEmail: "owner@example.com",
    projectId: "p1",
    chatId: "c1",
    prompt: "back up",
    kind: "cron",
    timezone: "UTC",
    enabled: true,
    status: "active",
    runCount: 0,
    createdAt: 0,
    updatedAt: 0,
    ...overrides,
  };
}

test("parseDiffStat reads git stat rows and the total line", () => {
  const summary = parseDiffStat(
    [
      " src/a.ts        | 12 ++++++++----",
      " src/b/long name.ts |  3 +++",
      " 2 files changed, 11 insertions(+), 4 deletions(-)",
    ].join("\n")
  );

  assert.equal(summary.rows.length, 2);
  assert.deepEqual(summary.rows[0], {
    path: "src/a.ts",
    changes: 12,
    additions: 8,
    deletions: 4,
  });
  assert.equal(summary.rows[1].path, "src/b/long name.ts");
  assert.equal(summary.rows[1].additions, 3);
  assert.equal(summary.total, "2 files changed, 11 insertions(+), 4 deletions(-)");
});

test("parseDiffStat tolerates empty and shapeless input", () => {
  assert.deepEqual(parseDiffStat(""), { rows: [], total: "" });
  assert.deepEqual(parseDiffStat("nothing here\n\n"), { rows: [], total: "" });
});

test("parseDiffStat keeps binary rows without a numeric churn", () => {
  const summary = parseDiffStat(" logo.png | Bin 0 -> 1024 bytes");
  assert.equal(summary.rows.length, 1);
  assert.equal(summary.rows[0].path, "logo.png");
  assert.equal(summary.rows[0].changes, 0);
});

test("formatRunDuration scales across units", () => {
  assert.equal(formatRunDuration(0), "—");
  assert.equal(formatRunDuration(450), "450 ms");
  assert.equal(formatRunDuration(4200), "4.2s");
  assert.equal(formatRunDuration(125_000), "2m 5s");
});

test("runStatusLabel separates a gate skip from every other skip", () => {
  assert.equal(runStatusLabel(record()), "ok");
  assert.equal(runStatusLabel(record({ status: "skipped" })), "skipped");
  assert.equal(
    runStatusLabel(record({ status: "skipped", skippedByGate: true })),
    "skipped (gate)"
  );
});

test("chainLabel reads chain 2/3 and never exceeds its own depth", () => {
  assert.equal(chainLabel(undefined), "");
  assert.equal(chainLabel({ depth: 2, total: 3 }), "chain 2/3");
  assert.equal(chainLabel({ depth: 4, total: 2 }), "chain 4/4");
});

test("formatRunCost shows only what the ledger knew", () => {
  assert.equal(formatRunCost(record()), "");
  assert.equal(formatRunCost(record({ tokens: 1200 })), "1,200 tok");
  assert.equal(
    formatRunCost(record({ tokens: 1200, costUsd: 0.1234 })),
    "1,200 tok · $0.1234"
  );
});

test("describeCondition explains every gate kind", () => {
  assert.equal(describeCondition(undefined), "");
  assert.equal(
    describeCondition({ kind: "outputContains", pattern: "DRIFT", inLastRunOf: "self" }),
    "only if its own last run matches /DRIFT/"
  );
  assert.equal(
    describeCondition({ kind: "outputContains", pattern: "OK", inLastRunOf: "abc" }),
    "only if another task's last run matches /OK/"
  );
  assert.equal(
    describeCondition({ kind: "httpStatus", url: "https://a.test" }),
    "only if https://a.test answers 200"
  );
  assert.equal(
    describeCondition({ kind: "commandExitCode", command: "test -f x", expect: 0 }),
    "only if `test -f x` exits 0"
  );
  assert.equal(
    describeCondition({ kind: "weekdays", weekdays: [1, 5] }),
    "only on Mon, Fri"
  );
  assert.equal(
    describeCondition({ kind: "notIfRanWithin", minutes: 90 }),
    "not if it ran in the last 90 min"
  );
});

test("describeChain names targets and their conditions", () => {
  const tasks = [task(), task({ id: "task-2", name: "Verify" })];
  assert.equal(describeChain(undefined, tasks), "");
  assert.equal(describeChain([], tasks), "");
  assert.equal(
    describeChain([{ taskId: "task-2", when: "success", delayMin: 5 }], tasks),
    "→ Verify (success +5m)"
  );
  assert.equal(
    describeChain([{ taskId: "deleted-9999", when: "always" }], tasks),
    "→ deleted- (always)"
  );
});
