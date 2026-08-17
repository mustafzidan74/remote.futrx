import type { UsageDayPoint, UsageTotals } from "../../models/usage";

/**
 * Geometry and formatting for the Usage page's inline-SVG bar chart. Kept out
 * of the component so the scaling rules are unit-testable and the view stays
 * a pure renderer.
 */
export type UsageChartMetric = "tokens" | "cost";

export interface UsageChartBar {
  day: string;
  value: number;
  /** Fraction of the tallest bar, 0..1. */
  ratio: number;
  runs: number;
  label: string;
}

export interface UsageChartModel {
  bars: UsageChartBar[];
  peak: number;
  peakLabel: string;
  /** True when every day in the window is empty, so the view can say so. */
  isEmpty: boolean;
}

export function buildUsageChart(
  daily: UsageDayPoint[],
  metric: UsageChartMetric
): UsageChartModel {
  const values = daily.map((point) => (metric === "cost" ? point.costUsd : point.totalTokens));
  const peak = values.reduce((max, value) => Math.max(max, value), 0);
  const bars = daily.map((point, index) => {
    const value = values[index];
    return {
      day: point.day,
      value,
      // A zero peak would divide by zero; an all-empty window draws flat.
      ratio: peak > 0 ? value / peak : 0,
      runs: point.runs,
      label: `${point.day}: ${
        metric === "cost" ? formatUsd(value) : `${formatTokens(value)} tokens`
      } · ${point.runs} run${point.runs === 1 ? "" : "s"}`,
    };
  });
  return {
    bars,
    peak,
    peakLabel: metric === "cost" ? formatUsd(peak) : formatTokens(peak),
    isEmpty: peak === 0,
  };
}

/** Compact token counts: 1.2M / 34.5K / 812. */
export function formatTokens(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return "0";
  if (tokens >= 1_000_000) return `${(tokens / 1_000_000).toFixed(tokens >= 10_000_000 ? 0 : 1)}M`;
  if (tokens >= 1_000) return `${(tokens / 1_000).toFixed(tokens >= 10_000 ? 0 : 1)}K`;
  return String(Math.round(tokens));
}

/**
 * Money is shown with enough precision to be useful at agent-run scale:
 * sub-cent amounts keep four decimals so a $0.0007 turn is not "$0.00".
 */
export function formatUsd(cost: number): string {
  if (!Number.isFinite(cost) || cost === 0) return "$0.00";
  if (Math.abs(cost) < 0.01) return `$${cost.toFixed(4)}`;
  return `$${cost.toFixed(2)}`;
}

/**
 * Cost with its confidence: an all-estimated total is prefixed with `~`, and
 * runs the platform could not price at all are called out separately.
 */
export function formatCostWithConfidence(totals: UsageTotals): string {
  const cost = formatUsd(totals.costUsd);
  if (totals.costUsd > 0 && totals.estimatedCostUsd >= totals.costUsd) return `~${cost}`;
  if (totals.estimatedCostUsd > 0) return `${cost}*`;
  return cost;
}

/** Human note about how much of a total is estimated or missing. */
export function usageConfidenceNote(totals: UsageTotals): string | null {
  const notes: string[] = [];
  if (totals.estimatedCostUsd > 0) {
    notes.push(`${formatUsd(totals.estimatedCostUsd)} estimated from the price table`);
  }
  if (totals.unpricedRuns > 0) {
    notes.push(
      `${totals.unpricedRuns} run${totals.unpricedRuns === 1 ? "" : "s"} with unknown cost`
    );
  }
  return notes.length > 0 ? notes.join(" · ") : null;
}
