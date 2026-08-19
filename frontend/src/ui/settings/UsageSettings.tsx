import { useState } from "preact/hooks";
import type { UsageGroupBy } from "../../models/usage";
import type { UsageDashboard } from "../../state/hooks/usage/useUsageDashboard";
import type { UsageChartMetric } from "../../state/usage/usageChartModel";
import {
  formatCostWithConfidence,
  formatTokens,
  usageConfidenceNote,
} from "../../state/usage/usageChartModel";
import { USAGE_RANGE_PRESETS, usageRangeLabels } from "../../state/usage/usageRangeState";
import { Loader, RotateCcw } from "../primitives/icons";
import { ErrorBanner } from "../primitives/Feedback";
import { AutoRoutingCard } from "./usage/AutoRoutingCard";
import { UsageBarChart } from "./usage/UsageBarChart";
import { UsageGroupTable, UsageRecordsTable } from "./usage/UsageTables";

const GROUP_OPTIONS: Array<{ id: UsageGroupBy; label: string }> = [
  { id: "project", label: "Project" },
  { id: "user", label: "User" },
  { id: "provider", label: "Provider" },
  { id: "model", label: "Model" },
  { id: "day", label: "Day" },
];

export function UsageSettings({
  dashboard,
  isAdmin,
  onRebuild,
  rebuilding,
  rebuildMessage,
}: {
  dashboard: UsageDashboard;
  isAdmin: boolean;
  onRebuild: () => Promise<void>;
  rebuilding: boolean;
  rebuildMessage: string | null;
}) {
  const [metric, setMetric] = useState<UsageChartMetric>("tokens");
  const { summary, range, groupBy, drillDown } = dashboard;
  const labels = usageRangeLabels(range);
  const totals = summary?.totals;
  const note = totals ? usageConfidenceNote(totals) : null;

  if (dashboard.loading && !summary) {
    return (
      <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-12 flex items-center justify-center gap-2 text-[13px] text-ink-300">
        <Loader class="w-4 h-4 animate-spin" /> Loading usage…
      </div>
    );
  }

  return (
    <div class="space-y-4">
      <section class="rounded-lg border border-white/10 bg-[#101318] px-4 py-3 space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <div class="inline-flex rounded-md border border-white/10 overflow-hidden">
            {USAGE_RANGE_PRESETS.map(({ id, label }) => (
              <button
                key={id}
                type="button"
                onClick={() => dashboard.setPreset(id)}
                aria-pressed={range.preset === id}
                class={`h-9 px-3 text-[12.5px] font-medium transition-colors ${
                  range.preset === id
                    ? "bg-white/[0.10] text-ink-50"
                    : "text-ink-300 hover:text-ink-100 hover:bg-white/[0.05]"
                }`}
              >
                {label}
              </button>
            ))}
          </div>

          {range.preset === "custom" && (
            <div class="flex items-center gap-2 text-[12.5px] text-ink-300">
              <input
                type="date"
                value={labels.fromDate}
                onChange={(event) =>
                  dashboard.setCustomRange(
                    (event.currentTarget as HTMLInputElement).value,
                    labels.toDate
                  )
                }
                aria-label="Usage range start"
                class="h-9 px-2 rounded-md bg-white/[0.06] border border-white/10 text-ink-100"
              />
              <span>to</span>
              <input
                type="date"
                value={labels.toDate}
                onChange={(event) =>
                  dashboard.setCustomRange(
                    labels.fromDate,
                    (event.currentTarget as HTMLInputElement).value
                  )
                }
                aria-label="Usage range end"
                class="h-9 px-2 rounded-md bg-white/[0.06] border border-white/10 text-ink-100"
              />
            </div>
          )}

          <div class="flex-1" />

          <button
            type="button"
            onClick={() => void dashboard.refresh()}
            disabled={dashboard.refreshing}
            class="h-9 px-2.5 rounded-md inline-flex items-center gap-2 text-[12px] text-ink-200
                   hover:text-ink-50 hover:bg-white/[0.08] disabled:opacity-60"
          >
            <RotateCcw class={`w-4 h-4 ${dashboard.refreshing ? "animate-spin" : ""}`} aria-hidden="true" />
            <span class="hidden sm:inline">Refresh</span>
          </button>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <span class="text-[11.5px] uppercase tracking-wide text-ink-300">Group by</span>
          {GROUP_OPTIONS.map(({ id, label }) => (
            <button
              key={id}
              type="button"
              onClick={() => dashboard.setGroupBy(id)}
              aria-pressed={groupBy === id}
              class={`h-8 px-2.5 rounded-md border text-[12.5px] transition-colors ${
                groupBy === id
                  ? "border-white/10 bg-white/[0.08] text-ink-50"
                  : "border-transparent text-ink-300 hover:text-ink-100 hover:bg-white/[0.05]"
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        <div dir="ltr" class="bidi-ltr text-[11.5px] text-ink-300 font-mono tabular-nums">
          {labels.fromDate} → {labels.toDate} (UTC)
        </div>
      </section>

      {dashboard.error && (
        <ErrorBanner
          message={`Could not load usage: ${dashboard.error}`}
          retrying={dashboard.refreshing}
          onRetry={() => void dashboard.refresh()}
        />
      )}

      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <KpiTile label="Total tokens" value={formatTokens(totals?.totalTokens ?? 0)} />
        <KpiTile
          label="Estimated cost"
          value={totals ? formatCostWithConfidence(totals) : "$0.00"}
          detail={note ?? undefined}
        />
        <KpiTile label="Runs" value={String(totals?.runs ?? 0)} />
        <KpiTile label="Active projects" value={String(summary?.projects ?? 0)} />
      </div>

      {summary?.routing && <AutoRoutingCard summary={summary.routing} />}

      <UsageBarChart daily={summary?.daily ?? []} metric={metric} onMetricChange={setMetric} />

      <UsageGroupTable
        groups={summary?.groups ?? []}
        groupBy={groupBy}
        onDrillDown={(group) => void dashboard.openDrillDown(group.key, group.label)}
      />

      {drillDown && (
        <UsageRecordsTable
          label={drillDown.label}
          records={drillDown.records}
          loading={drillDown.loading}
          error={drillDown.error}
          hasMore={drillDown.hasMore}
          onClose={dashboard.closeDrillDown}
          onLoadMore={() => void dashboard.loadMoreDrillDown()}
        />
      )}

      {isAdmin && (
        <section class="rounded-lg border border-white/10 bg-[#101318] px-4 py-3">
          <div class="text-[14.5px] font-semibold text-ink-50">Ledger maintenance</div>
          <p class="mt-1 text-[12.5px] text-ink-300 leading-relaxed">
            Rebuild re-derives every usage record from the stored chat event logs. It is safe to
            run repeatedly and is how an existing install backfills history recorded before this
            page existed. Prices live in <span class="font-mono">DATA_DIR/usage/prices.json</span>.
          </p>
          <div class="mt-3 flex items-center gap-3">
            <button
              type="button"
              onClick={() => void onRebuild()}
              disabled={rebuilding}
              class="h-9 px-3 rounded-md bg-white/[0.08] hover:bg-white/[0.12] text-[13px]
                     text-ink-100 disabled:opacity-60"
            >
              {rebuilding ? "Rebuilding…" : "Rebuild usage ledger"}
            </button>
            {rebuildMessage && <span class="text-[12.5px] text-ink-300">{rebuildMessage}</span>}
          </div>
        </section>
      )}
    </div>
  );
}

function KpiTile({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail?: string;
}) {
  return (
    <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-3">
      <div class="text-[11.5px] uppercase tracking-wide text-ink-300">{label}</div>
      <div class="mt-1 text-[22px] font-semibold text-ink-50 tabular-nums">{value}</div>
      {detail && <div class="mt-1 text-[11.5px] text-ink-300 leading-snug">{detail}</div>}
    </div>
  );
}
