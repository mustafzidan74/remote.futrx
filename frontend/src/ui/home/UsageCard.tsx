import { useState } from "preact/hooks";
import type { DashboardUsage } from "../../models/dashboard";
import { weekOverWeek } from "../../state/home/dashboardState";
import {
  buildUsageChart,
  formatTokens,
  formatUsd,
  type UsageChartMetric,
} from "../../state/usage/usageChartModel";
import { CardEmpty, CardSkeleton, DashboardCard } from "./DashboardCard";
import { ExternalLink, Zap } from "../primitives/icons";

const VIEW_WIDTH = 700;
const VIEW_HEIGHT = 120;
/** A day with activity always keeps a sliver of height, so "a little" never
 *  renders identically to "none". */
const MIN_VISIBLE_RATIO = 0.02;
/** Gap between bars, in viewBox units, so adjacent days never merge. */
const BAR_GAP = 14;

/**
 * The usage card: one bar per UTC day over the dashboard's window, drawn as
 * inline SVG with no chart library. The whole card is a link into
 * Settings → Usage, which is where the same numbers can be filtered, grouped
 * and exported.
 */
export function UsageCard({
  usage,
  windowDays,
  loading,
  onOpenUsage,
}: {
  usage: DashboardUsage;
  windowDays: number;
  loading: boolean;
  onOpenUsage: () => void;
}) {
  const [metric, setMetric] = useState<UsageChartMetric>("cost");
  const chart = buildUsageChart(usage.daily, metric);
  const slot = chart.bars.length > 0 ? VIEW_WIDTH / chart.bars.length : VIEW_WIDTH;
  const barWidth = Math.max(2, slot - BAR_GAP);
  const delta =
    metric === "cost"
      ? weekOverWeek(usage.thisWeek.costUsd, usage.lastWeek.costUsd, "spend")
      : weekOverWeek(usage.thisWeek.totalTokens, usage.lastWeek.totalTokens, "tokens");

  return (
    <DashboardCard
      title="Usage"
      subtitle={`Last ${windowDays} UTC days · ${delta.label} vs the ${windowDays} before`}
      Icon={Zap}
      action={
        <div class="flex items-center gap-2">
          <div class="inline-flex overflow-hidden rounded-md border border-white/10">
            {(["cost", "tokens"] as UsageChartMetric[]).map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setMetric(option)}
                aria-pressed={metric === option}
                class={`h-7 px-2.5 text-[11.5px] font-medium transition-colors ${
                  metric === option
                    ? "bg-white/[0.10] text-ink-50"
                    : "text-ink-300 hover:bg-white/[0.05] hover:text-ink-100"
                }`}
              >
                {option === "cost" ? "Cost" : "Tokens"}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={onOpenUsage}
            class="grid h-7 w-7 place-items-center rounded-md text-ink-300 transition
                   hover:bg-white/[0.09] hover:text-ink-100"
            title="Open the full usage report"
            aria-label="Open the full usage report"
          >
            <ExternalLink class="h-3.5 w-3.5" />
          </button>
        </div>
      }
    >
      {loading ? (
        <CardSkeleton rows={2} />
      ) : !usage.available ? (
        <CardEmpty>
          This deployment is not recording usage, so there is nothing to chart. That is not the
          same as zero spend.
        </CardEmpty>
      ) : chart.isEmpty ? (
        <CardEmpty>No agent runs in the last {windowDays} days.</CardEmpty>
      ) : (
        <div class="p-4 pt-3">
          <div class="mb-2 flex items-baseline justify-between gap-3">
            <span dir="ltr" class="bidi-ltr font-mono text-[20px] font-semibold tabular-nums text-ink-50">
              {metric === "cost"
                ? formatUsd(usage.thisWeek.costUsd)
                : formatTokens(usage.thisWeek.totalTokens)}
            </span>
            <span class="text-[12px] tabular-nums text-ink-300" title={delta.title}>
              peak day {chart.peakLabel}
            </span>
          </div>

          <svg
            viewBox={`0 0 ${VIEW_WIDTH} ${VIEW_HEIGHT}`}
            preserveAspectRatio="none"
            class="h-[110px] w-full text-ink-400"
            role="img"
            aria-label={`Daily ${metric === "cost" ? "cost" : "token"} usage over the last ${windowDays} days`}
          >
            {/* The baseline is the only rule drawn: seven bars need no grid,
                and currentColor keeps it legible in both themes. */}
            <line
              x1="0"
              y1={VIEW_HEIGHT - 0.5}
              x2={VIEW_WIDTH}
              y2={VIEW_HEIGHT - 0.5}
              stroke="currentColor"
              stroke-width="1"
              opacity="0.3"
            />
            {chart.bars.map((bar, index) => {
              if (bar.value <= 0) return null;
              const height = Math.max(bar.ratio, MIN_VISIBLE_RATIO) * (VIEW_HEIGHT - 8);
              return (
                <rect
                  key={bar.day}
                  x={index * slot + (slot - barWidth) / 2}
                  y={VIEW_HEIGHT - height}
                  width={barWidth}
                  height={height}
                  rx="3"
                  class="fill-accent-blue"
                >
                  <title>{bar.label}</title>
                </rect>
              );
            })}
          </svg>

          <div
            dir="ltr"
            class="bidi-ltr mt-1 grid text-center font-mono text-[10.5px] tabular-nums text-ink-400"
            style={{ gridTemplateColumns: `repeat(${chart.bars.length || 1}, minmax(0, 1fr))` }}
          >
            {chart.bars.map((bar) => (
              <span key={bar.day} title={bar.label}>
                {weekdayOf(bar.day)}
              </span>
            ))}
          </div>
        </div>
      )}
    </DashboardCard>
  );
}

/**
 * Two-letter weekday for an ISO date. The bars are days, and "Mo Tu We" is
 * read faster under a chart than seven full dates — the exact date stays in
 * every bar's tooltip.
 */
function weekdayOf(day: string): string {
  const parsed = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return "";
  return ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"][parsed.getUTCDay()];
}
