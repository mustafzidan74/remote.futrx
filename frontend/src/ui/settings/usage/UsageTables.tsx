import type { ComponentChildren } from "preact";
import type { UsageGroup, UsageGroupBy, UsageRecord } from "../../../models/usage";
import {
  formatTokens,
  formatUsd,
} from "../../../state/usage/usageChartModel";
import { Activity, ChevronRight, Loader, X } from "../../primitives/icons";
import { EmptyState } from "../../primitives/Feedback";

const GROUP_HEADINGS: Record<UsageGroupBy, string> = {
  project: "Project",
  user: "User",
  provider: "Provider",
  model: "Model",
  day: "Day",
  chat: "Chat",
};

export function UsageGroupTable({
  groups,
  groupBy,
  onDrillDown,
}: {
  groups: UsageGroup[];
  groupBy: UsageGroupBy;
  onDrillDown: (group: UsageGroup) => void;
}) {
  if (groups.length === 0) {
    return (
      <UsagePanel title={GROUP_HEADINGS[groupBy]} description="No usage recorded in this range.">
        <EmptyState
          Icon={Activity}
          title="No usage in this range"
          hint="Widen the date range, or run an agent to record the first tokens."
        />
      </UsagePanel>
    );
  }

  // Drilling down asks for the raw runs of one project, so the affordance is
  // only meaningful while the table is grouped by project.
  const drillable = groupBy === "project";
  return (
    <UsagePanel
      title={`Usage by ${GROUP_HEADINGS[groupBy].toLowerCase()}`}
      description={`${groups.length} row${groups.length === 1 ? "" : "s"}${
        drillable ? " · select a project to see its runs" : ""
      }`}
    >
      <UsageScroller>
        <table class="w-full text-[13px] border-collapse min-w-[560px]">
          <thead>
            <tr class="text-left text-[11.5px] uppercase tracking-wide text-ink-300">
              <th class="font-medium px-2 py-2">{GROUP_HEADINGS[groupBy]}</th>
              <th class="font-medium px-2 py-2 text-right">Runs</th>
              <th class="font-medium px-2 py-2 text-right">Tokens</th>
              <th class="font-medium px-2 py-2 text-right">Cost</th>
              {drillable && <th class="w-8" />}
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <tr
                key={group.key || group.label}
                class={`border-t border-white/[0.06] ${
                  drillable ? "cursor-pointer hover:bg-white/[0.04]" : ""
                }`}
                onClick={drillable ? () => onDrillDown(group) : undefined}
              >
                <td dir="auto" class="bidi-auto px-2 py-2 text-ink-100 truncate max-w-[220px]" title={group.label}>
                  {group.label}
                </td>
                <td class="px-2 py-2 text-right text-ink-200 font-mono tabular-nums">{group.runs}</td>
                <td class="px-2 py-2 text-right text-ink-200 font-mono tabular-nums">
                  {formatTokens(group.totalTokens)}
                </td>
                <td class="px-2 py-2 text-right font-mono tabular-nums text-ink-50">
                  <CostCell
                    cost={group.costUsd}
                    estimated={group.estimatedCostUsd}
                    unpriced={group.unpricedRuns}
                  />
                </td>
                {drillable && (
                  <td class="px-1 py-2 text-ink-400">
                    <ChevronRight class="w-4 h-4" />
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </UsageScroller>
    </UsagePanel>
  );
}

export function UsageRecordsTable({
  label,
  records,
  loading,
  error,
  hasMore,
  onClose,
  onLoadMore,
}: {
  label: string;
  records: UsageRecord[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  onClose: () => void;
  onLoadMore: () => void;
}) {
  return (
    <UsagePanel
      title={`Runs in ${label}`}
      description={
        error
          ? `Could not load runs: ${error}`
          : `${records.length} run${records.length === 1 ? "" : "s"}${hasMore ? " (more available)" : ""}`
      }
      action={
        <button
          type="button"
          onClick={onClose}
          class="h-8 w-8 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] grid place-items-center"
          aria-label="Close run list"
        >
          <X class="w-4 h-4" />
        </button>
      }
    >
      {loading && records.length === 0 ? (
        <div class="px-1 py-6 flex items-center justify-center gap-2 text-[13px] text-ink-300">
          <Loader class="w-4 h-4 animate-spin" /> Loading runs…
        </div>
      ) : records.length === 0 ? (
        <EmptyState Icon={Activity} title="No runs in this range" />
      ) : (
        <>
          <UsageScroller>
            <table class="w-full text-[12.5px] border-collapse min-w-[720px]">
              <thead>
                <tr class="text-left text-[11.5px] uppercase tracking-wide text-ink-300">
                  <th class="font-medium px-2 py-2">When (UTC)</th>
                  <th class="font-medium px-2 py-2">Chat</th>
                  <th class="font-medium px-2 py-2">User</th>
                  <th class="font-medium px-2 py-2">Model</th>
                  <th class="font-medium px-2 py-2 text-right">In</th>
                  <th class="font-medium px-2 py-2 text-right">Out</th>
                  <th class="font-medium px-2 py-2 text-right">Cache</th>
                  <th class="font-medium px-2 py-2 text-right">Cost</th>
                </tr>
              </thead>
              <tbody>
                {records.map((record) => (
                  <tr
                    key={`${record.chatId}-${record.at}`}
                    class="border-t border-white/[0.06] align-top"
                  >
                    <td class="px-2 py-2 text-ink-300 font-mono tabular-nums whitespace-nowrap">
                      {formatUtcDateTime(record.at)}
                    </td>
                    <td class="px-2 py-2 text-ink-200 font-mono">
                      {record.chatId}
                      {record.scheduled && (
                        <span class="ml-1.5 text-[10.5px] text-accent-green">scheduled</span>
                      )}
                    </td>
                    <td class="px-2 py-2 text-ink-200 truncate max-w-[180px]" title={record.userEmail}>
                      {record.userEmail || "—"}
                    </td>
                    <td class="px-2 py-2 text-ink-200 truncate max-w-[180px]" title={record.model || record.provider}>
                      {record.model || record.provider || "—"}
                    </td>
                    <td class="px-2 py-2 text-right text-ink-200 font-mono tabular-nums">
                      {formatTokens(record.inputTokens)}
                    </td>
                    <td class="px-2 py-2 text-right text-ink-200 font-mono tabular-nums">
                      {formatTokens(record.outputTokens)}
                    </td>
                    <td class="px-2 py-2 text-right text-ink-300 font-mono tabular-nums">
                      {formatTokens(record.cacheReadTokens + record.cacheWriteTokens)}
                    </td>
                    <td class="px-2 py-2 text-right font-mono tabular-nums text-ink-50">
                      {record.costUsd == null ? (
                        <span class="text-ink-300" title="No provider price and no matching price-table entry">
                          unknown
                        </span>
                      ) : (
                        <span title={record.estimated ? "Estimated from the price table" : "Reported by the provider"}>
                          {record.estimated ? "~" : ""}
                          {formatUsd(record.costUsd)}
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </UsageScroller>
          {hasMore && (
            <button
              type="button"
              onClick={onLoadMore}
              disabled={loading}
              class="mt-2 h-9 px-3 rounded-md bg-white/[0.08] hover:bg-white/[0.12] text-[13px]
                     text-ink-100 disabled:opacity-60"
            >
              {loading ? "Loading…" : "Load more runs"}
            </button>
          )}
        </>
      )}
    </UsagePanel>
  );
}

function CostCell({
  cost,
  estimated,
  unpriced,
}: {
  cost: number;
  estimated: number;
  unpriced: number;
}) {
  const allEstimated = cost > 0 && estimated >= cost;
  const title = [
    estimated > 0 ? `${formatUsd(estimated)} estimated from the price table` : null,
    unpriced > 0 ? `${unpriced} run${unpriced === 1 ? "" : "s"} with unknown cost` : null,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <span title={title || "Reported by the provider"}>
      {allEstimated ? "~" : ""}
      {formatUsd(cost)}
      {!allEstimated && estimated > 0 && <span class="text-ink-300">*</span>}
    </span>
  );
}

export function UsagePanel({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description: string;
  action?: ComponentChildren;
  children: ComponentChildren;
}) {
  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 border-b border-white/[0.06] flex items-start gap-3">
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">{title}</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">{description}</div>
        </div>
        {action}
      </header>
      <div class="p-3">{children}</div>
    </section>
  );
}

function UsageScroller({ children }: { children: ComponentChildren }) {
  return <div class="overflow-x-auto touch-scroll">{children}</div>;
}

/** Records are bucketed by UTC day server-side, so they are shown in UTC. */
function formatUtcDateTime(at: number): string {
  return new Date(at).toISOString().replace("T", " ").slice(0, 16);
}
