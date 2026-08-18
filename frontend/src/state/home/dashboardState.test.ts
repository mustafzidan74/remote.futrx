import assert from "node:assert/strict";
import test from "node:test";
import type { DashboardAlert, DashboardProject } from "../../models/dashboard.ts";
import {
  describeAlert,
  formatCountdown,
  formatRelative,
  projectDot,
  summarizeAlerts,
  weekOverWeek,
} from "./dashboardState.ts";

function alert(overrides: Partial<DashboardAlert> = {}): DashboardAlert {
  return {
    id: "a1",
    severity: "warn",
    kind: "health",
    title: "Something is wrong",
    ...overrides,
  };
}

function project(overrides: Partial<DashboardProject> = {}): DashboardProject {
  return {
    id: "p1",
    name: "Shop",
    slug: "shop",
    status: "running",
    health: "unknown",
    ...overrides,
  };
}

test("an alert with a known action offers its button", () => {
  const view = describeAlert(
    alert({ action: "snapshot-now", actionLabel: "Snapshot now", severity: "warn" })
  );
  assert.equal(view.actionable, true);
  assert.equal(view.actionLabel, "Snapshot now");
  assert.equal(view.tone, "amber");
});

test("an alert this build does not understand renders without a dead button", () => {
  const view = describeAlert(
    // A newer server offering an action this client never learned.
    alert({ action: "reticulate-splines" as DashboardAlert["action"], actionLabel: "Reticulate" })
  );
  assert.equal(view.actionable, false);
  assert.equal(view.actionLabel, "");
});

test("an alert with no action at all is a plain row", () => {
  const view = describeAlert(alert({ kind: "backup", severity: "warn" }));
  assert.equal(view.actionable, false);
});

test("severity picks the status tone, and info stays neutral", () => {
  assert.equal(describeAlert(alert({ severity: "crit" })).tone, "red");
  assert.equal(describeAlert(alert({ severity: "warn" })).tone, "amber");
  // Informational rows must not borrow a colour that means "wrong".
  assert.equal(describeAlert(alert({ severity: "info" })).tone, "grey");
});

test("an empty alert list reads as all clear", () => {
  const summary = summarizeAlerts([]);
  assert.equal(summary.total, 0);
  assert.equal(summary.worst, null);
  assert.equal(summary.headline, "All clear");
});

test("alerts are counted by severity and the worst one leads the headline", () => {
  const summary = summarizeAlerts([
    alert({ id: "1", severity: "crit" }),
    alert({ id: "2", severity: "warn" }),
    alert({ id: "3", severity: "warn" }),
    alert({ id: "4", severity: "info" }),
  ]);
  assert.equal(summary.total, 4);
  assert.deepEqual([summary.crit, summary.warn, summary.info], [1, 2, 1]);
  assert.equal(summary.worst, "crit");
  assert.equal(summary.headline, "1 critical · 2 warnings · 1 to review");
});

test("the worst severity is the highest present, not the first seen", () => {
  assert.equal(summarizeAlerts([alert({ severity: "info" })]).worst, "info");
  assert.equal(
    summarizeAlerts([alert({ id: "1", severity: "info" }), alert({ id: "2", severity: "warn" })])
      .worst,
    "warn"
  );
});

test("week over week reports a rise, a fall, and a flat week", () => {
  const up = weekOverWeek(120, 100, "runs");
  assert.equal(up.direction, "up");
  assert.equal(up.percent, 20);
  assert.equal(up.label, "+20%");

  const down = weekOverWeek(75, 100, "runs");
  assert.equal(down.direction, "down");
  assert.equal(down.percent, -25);
  assert.equal(down.label, "-25%");

  const flat = weekOverWeek(100, 100, "runs");
  assert.equal(flat.direction, "flat");
  assert.equal(flat.label, "no change");
});

test("a rounding-to-zero change is flat rather than a misleading +0%", () => {
  const delta = weekOverWeek(1002, 1000, "runs");
  assert.equal(delta.direction, "flat");
  assert.equal(delta.percent, 0);
  assert.equal(delta.label, "no change");
});

test("a week with no baseline has no percentage, only the fact that it is new", () => {
  const delta = weekOverWeek(40, 0, "runs");
  assert.equal(delta.direction, "up");
  assert.equal(delta.percent, null, "dividing by an empty week would be infinity");
  assert.equal(delta.label, "new");
});

test("two empty weeks are no change, not new", () => {
  const delta = weekOverWeek(0, 0, "runs");
  assert.equal(delta.direction, "flat");
  assert.equal(delta.label, "no change");
});

test("non-finite inputs never reach the tile as NaN", () => {
  const delta = weekOverWeek(Number.NaN, 100, "cost");
  assert.equal(delta.direction, "flat");
  assert.equal(delta.label, "no change");
});

test("the project dot follows health when the monitor has a reading", () => {
  assert.equal(projectDot(project({ health: "ok" })).tone, "green");
  assert.equal(projectDot(project({ health: "warn" })).tone, "amber");
  assert.equal(projectDot(project({ health: "crit" })).tone, "red");
});

test("the dot falls back to lifecycle when the project is not monitored", () => {
  assert.equal(projectDot(project({ health: "unknown", status: "running" })).tone, "green");
  assert.equal(projectDot(project({ health: "unknown", status: "stopped" })).tone, "grey");
  assert.equal(projectDot(project({ health: "unknown", status: "error" })).tone, "red");
  assert.equal(projectDot(project({ health: "unknown", status: "" })).label, "Unknown");
});

test("the dot's tooltip carries every reason the monitor gave", () => {
  const dot = projectDot(
    project({ health: "crit", healthReasons: ["memory 97%", "preview refused"] })
  );
  assert.equal(dot.title, "Critical\nmemory 97%\npreview refused");
});

test("a countdown counts down, and a missed deadline reads as due", () => {
  const now = 1_000_000;
  assert.equal(formatCountdown(now + 5 * 60_000, now), "in 5 min");
  assert.equal(formatCountdown(now + 3 * 3_600_000, now), "in 3 h");
  assert.equal(formatCountdown(now - 60_000, now), "due now");
  assert.equal(formatCountdown(0, now), "");
});

test("relative times round to the largest useful unit", () => {
  const now = 1_000_000_000;
  assert.equal(formatRelative(now - 10_000, now), "just now");
  assert.equal(formatRelative(now - 5 * 60_000, now), "5 min ago");
  assert.equal(formatRelative(now - 2 * 3_600_000, now), "2 h ago");
  assert.equal(formatRelative(now - 3 * 86_400_000, now), "3 d ago");
  assert.equal(formatRelative(undefined, now), "");
});
