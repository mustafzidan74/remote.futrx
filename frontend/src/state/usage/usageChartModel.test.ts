import test from "node:test";
import assert from "node:assert/strict";
import { EMPTY_USAGE_TOTALS, type UsageDayPoint } from "../../models/usage.ts";
import {
  buildUsageChart,
  formatCostWithConfidence,
  formatTokens,
  formatUsd,
  usageConfidenceNote,
} from "./usageChartModel.ts";

const daily: UsageDayPoint[] = [
  { day: "2026-08-01", totalTokens: 1000, costUsd: 0.5, runs: 2 },
  { day: "2026-08-02", totalTokens: 0, costUsd: 0, runs: 0 },
  { day: "2026-08-03", totalTokens: 4000, costUsd: 1.5, runs: 6 },
];

test("scales bars against the tallest day", () => {
  const chart = buildUsageChart(daily, "tokens");
  assert.equal(chart.peak, 4000);
  assert.deepEqual(
    chart.bars.map((bar) => bar.ratio),
    [0.25, 0, 1]
  );
  assert.equal(chart.isEmpty, false);
  assert.equal(chart.peakLabel, "4.0K");
});

test("scales the cost metric independently of tokens", () => {
  const chart = buildUsageChart(daily, "cost");
  assert.equal(chart.peak, 1.5);
  assert.deepEqual(
    chart.bars.map((bar) => bar.ratio),
    [1 / 3, 0, 1]
  );
  assert.equal(chart.peakLabel, "$1.50");
});

test("an all-empty window draws flat instead of dividing by zero", () => {
  const chart = buildUsageChart(
    [
      { day: "2026-08-01", totalTokens: 0, costUsd: 0, runs: 0 },
      { day: "2026-08-02", totalTokens: 0, costUsd: 0, runs: 0 },
    ],
    "tokens"
  );
  assert.equal(chart.isEmpty, true);
  assert.ok(chart.bars.every((bar) => bar.ratio === 0));
});

test("labels each bar with its day, value and run count", () => {
  const chart = buildUsageChart(daily, "tokens");
  assert.equal(chart.bars[0].label, "2026-08-01: 1.0K tokens · 2 runs");
  assert.equal(chart.bars[1].label, "2026-08-02: 0 tokens · 0 runs");
  assert.equal(
    buildUsageChart([daily[0]], "cost").bars[0].label,
    "2026-08-01: $0.50 · 2 runs"
  );
});

test("formats tokens compactly", () => {
  assert.equal(formatTokens(0), "0");
  assert.equal(formatTokens(812), "812");
  assert.equal(formatTokens(1234), "1.2K");
  assert.equal(formatTokens(34_500), "35K");
  assert.equal(formatTokens(1_250_000), "1.3M");
  assert.equal(formatTokens(12_500_000), "13M");
});

test("keeps sub-cent costs visible", () => {
  assert.equal(formatUsd(0), "$0.00");
  assert.equal(formatUsd(0.0007), "$0.0007");
  assert.equal(formatUsd(1.239), "$1.24");
});

test("marks estimated and unpriced totals", () => {
  assert.equal(
    formatCostWithConfidence({ ...EMPTY_USAGE_TOTALS, costUsd: 2, runs: 2 }),
    "$2.00"
  );
  assert.equal(
    formatCostWithConfidence({ ...EMPTY_USAGE_TOTALS, costUsd: 2, estimatedCostUsd: 2 }),
    "~$2.00"
  );
  assert.equal(
    formatCostWithConfidence({ ...EMPTY_USAGE_TOTALS, costUsd: 2, estimatedCostUsd: 0.5 }),
    "$2.00*"
  );
});

test("explains where a total is uncertain", () => {
  assert.equal(usageConfidenceNote(EMPTY_USAGE_TOTALS), null);
  assert.equal(
    usageConfidenceNote({ ...EMPTY_USAGE_TOTALS, costUsd: 2, estimatedCostUsd: 0.5 }),
    "$0.50 estimated from the price table"
  );
  assert.equal(
    usageConfidenceNote({ ...EMPTY_USAGE_TOTALS, unpricedRuns: 1 }),
    "1 run with unknown cost"
  );
  assert.equal(
    usageConfidenceNote({ ...EMPTY_USAGE_TOTALS, estimatedCostUsd: 0.5, unpricedRuns: 3 }),
    "$0.50 estimated from the price table · 3 runs with unknown cost"
  );
});
