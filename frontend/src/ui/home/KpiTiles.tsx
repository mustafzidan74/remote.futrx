import type { ComponentType } from "preact";
import type { DashboardKpis, DashboardAlert } from "../../models/dashboard";
import {
  summarizeAlerts,
  weekOverWeek,
  type StatusTone,
  type TrendDelta,
} from "../../state/home/dashboardState";
import { formatCostWithConfidence } from "../../state/usage/usageChartModel";
import { TONE_TEXT } from "./DashboardCard";
import { Activity, AlertCircle, Layers, Zap } from "../primitives/icons";

/**
 * The top row: four numbers that answer "what is happening?" before anything
 * is scrolled. Each carries its own comparison rather than a colour — a rise
 * in runs and a rise in cost are the same arithmetic and opposite news, so
 * the arrow stays in neutral ink and only the alert tile takes a status
 * colour, because only it describes something being wrong.
 */
export function KpiTiles({
  kpis,
  alerts,
  windowDays,
  onOpenUsage,
  onOpenAlerts,
}: {
  kpis: DashboardKpis;
  alerts: DashboardAlert[];
  windowDays: number;
  onOpenUsage: () => void;
  onOpenAlerts: () => void;
}) {
  const summary = summarizeAlerts(alerts);
  const alertTone: StatusTone =
    summary.crit > 0 ? "red" : summary.warn > 0 ? "amber" : summary.total > 0 ? "grey" : "green";

  return (
    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Tile
        Icon={Layers}
        label="Projects running"
        value={`${kpis.runningProjects}`}
        unit={`of ${kpis.totalProjects}`}
        note={
          kpis.activeRuns > 0
            ? `${kpis.activeRuns} agent turn${kpis.activeRuns === 1 ? "" : "s"} in flight`
            : "No agent turns in flight"
        }
      />
      <Tile
        Icon={Activity}
        label={`Runs · ${windowDays} days`}
        value={formatCount(kpis.runsThisWeek)}
        delta={weekOverWeek(kpis.runsThisWeek, kpis.runsLastWeek, "runs")}
        onClick={onOpenUsage}
      />
      <Tile
        Icon={Zap}
        label={`Est. cost · ${windowDays} days`}
        value={formatCostWithConfidence({
          costUsd: kpis.costThisWeek,
          estimatedCostUsd: kpis.estimatedCostThisWeek,
          unpricedRuns: kpis.unpricedRunsThisWeek,
          inputTokens: 0,
          outputTokens: 0,
          cacheReadTokens: 0,
          cacheWriteTokens: 0,
          totalTokens: kpis.tokensThisWeek,
          runs: kpis.runsThisWeek,
        })}
        delta={weekOverWeek(kpis.costThisWeek, kpis.costLastWeek, "spend")}
        note={
          kpis.unpricedRunsThisWeek > 0
            ? `${kpis.unpricedRunsThisWeek} run${kpis.unpricedRunsThisWeek === 1 ? "" : "s"} could not be priced`
            : undefined
        }
        onClick={onOpenUsage}
      />
      <Tile
        Icon={AlertCircle}
        label="Needs attention"
        value={`${summary.total}`}
        note={summary.headline}
        tone={alertTone}
        onClick={onOpenAlerts}
      />
    </div>
  );
}

function Tile({
  Icon,
  label,
  value,
  unit,
  note,
  delta,
  tone,
  onClick,
}: {
  Icon: ComponentType<{ class?: string }>;
  label: string;
  value: string;
  unit?: string;
  note?: string;
  delta?: TrendDelta;
  tone?: StatusTone;
  onClick?: () => void;
}) {
  const body = (
    <>
      <div class="flex items-center gap-1.5 text-[11.5px] font-medium uppercase tracking-wide text-ink-300">
        <Icon class={`h-3.5 w-3.5 ${tone ? TONE_TEXT[tone] : ""}`} />
        <span class="truncate">{label}</span>
      </div>
      <div class="mt-2 flex items-baseline gap-1.5">
        <span
          dir="ltr"
          class={`bidi-ltr font-mono text-[26px] font-semibold leading-none tabular-nums ${
            tone ? TONE_TEXT[tone] : "text-ink-50"
          }`}
        >
          {value}
        </span>
        {unit && <span class="text-[12px] tabular-nums text-ink-300">{unit}</span>}
        {delta && (
          <span
            class="ml-auto inline-flex items-center gap-0.5 text-[12px] tabular-nums text-ink-300"
            title={delta.title}
          >
            <span aria-hidden="true">
              {delta.direction === "up" ? "▲" : delta.direction === "down" ? "▼" : "–"}
            </span>
            {delta.label}
          </span>
        )}
      </div>
      {note && <p class="mt-1.5 truncate text-[12px] leading-snug text-ink-300">{note}</p>}
    </>
  );

  // `block` matters on the button variant: a button centres its content
  // vertically, which would float a tile with no note half a line lower than
  // its neighbours and break the row's shared baseline.
  const shell = "block rounded-lg border border-white/10 bg-[#101318] p-3.5 text-left";
  if (!onClick) return <div class={shell}>{body}</div>;
  return (
    <button type="button" onClick={onClick} class={`${shell} transition hover:bg-white/[0.04]`}>
      {body}
    </button>
  );
}

/** Compact counts: 1.2K rather than 1234, which is noise at tile size. */
function formatCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0";
  if (value >= 10_000) return `${Math.round(value / 1000)}K`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(Math.round(value));
}
