import assert from "node:assert/strict";
import test from "node:test";
import {
  previousReport,
  reportMeasured,
  runHeadline,
  scoreBand,
  scoreDelta,
  type LighthouseReport,
  type LighthouseRun,
} from "../../models/lighthouse.ts";

function report(over: Partial<LighthouseReport> = {}): LighthouseReport {
  return { path: "/", performance: 80, ...over };
}

function run(over: Partial<LighthouseRun> = {}): LighthouseRun {
  return {
    id: "r1",
    status: "ready",
    port: 3000,
    paths: ["/"],
    formFactor: "mobile",
    reports: [report()],
    createdAt: 0,
    ...over,
  };
}

test("the bands are Lighthouse's own, not ours", () => {
  // An operator comparing this against Chrome's report or PageSpeed must not
  // find the two disagreeing about a colour.
  assert.equal(scoreBand(90), "good");
  assert.equal(scoreBand(89), "average");
  assert.equal(scoreBand(50), "average");
  assert.equal(scoreBand(49), "poor");
});

test("a category that was never measured is not a zero", () => {
  assert.equal(scoreBand(undefined), "unknown");
  assert.equal(scoreBand(0), "poor");
});

test("a page that failed to load is not a page that scored nothing", () => {
  const broken = report({ error: "the page did not load", performance: undefined });
  assert.equal(reportMeasured(broken), false);
  assert.equal(reportMeasured(report()), true);
});

test("the headline is the worst page, because that is the one to fix", () => {
  const mixed = run({
    paths: ["/", "/pricing"],
    reports: [report({ performance: 92 }), report({ path: "/pricing", performance: 41 })],
  });
  assert.equal(runHeadline(mixed), "2 pages · worst performance 41");
});

test("a running run says how far it has got", () => {
  const working = run({ status: "running", paths: ["/", "/a", "/b"], reports: [report()] });
  assert.equal(runHeadline(working), "auditing 1 of 3…");
});

test("a failed run says why rather than reporting nothing", () => {
  assert.equal(runHeadline(run({ status: "failed", error: "the container stopped" })), "the container stopped");
});

test("a run where every page failed does not claim a score", () => {
  const allBad = run({ reports: [report({ error: "timed out", performance: undefined })] });
  assert.equal(runHeadline(allBad), "no page could be measured");
});

test("the trend only compares like with like", () => {
  // A mobile score next to a desktop one is two different measurements, and
  // showing one as a change in the other would be a lie an operator acts on.
  const runs: LighthouseRun[] = [
    run({ id: "new", formFactor: "mobile", createdAt: 3, reports: [report({ performance: 70 })] }),
    run({ id: "desktop", formFactor: "desktop", createdAt: 2, reports: [report({ performance: 99 })] }),
    run({ id: "old", formFactor: "mobile", createdAt: 1, reports: [report({ performance: 55 })] }),
  ];
  const previous = previousReport(runs, runs[0], "/");
  assert.equal(previous?.performance, 55);
  assert.equal(scoreDelta(runs[0].reports[0], previous, "performance"), 15);
});

test("with no earlier run there is no delta to invent", () => {
  const runs = [run({ id: "only" })];
  assert.equal(previousReport(runs, runs[0], "/"), null);
  assert.equal(scoreDelta(runs[0].reports[0], null, "performance"), null);
});

test("a page missing from the earlier run has no delta either", () => {
  const runs: LighthouseRun[] = [
    run({ id: "new", createdAt: 2, reports: [report({ path: "/new" })] }),
    run({ id: "old", createdAt: 1, reports: [report({ path: "/" })] }),
  ];
  assert.equal(previousReport(runs, runs[0], "/new"), null);
});

test("an earlier run that failed is skipped, not read as zero", () => {
  const runs: LighthouseRun[] = [
    run({ id: "new", createdAt: 3, reports: [report({ performance: 60 })] }),
    run({
      id: "broken",
      createdAt: 2,
      reports: [report({ error: "timed out", performance: undefined })],
    }),
    run({ id: "old", createdAt: 1, reports: [report({ performance: 50 })] }),
  ];
  const previous = previousReport(runs, runs[0], "/");
  assert.equal(previous?.performance, 50);
});
