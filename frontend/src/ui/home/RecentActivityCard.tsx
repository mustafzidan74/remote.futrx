import type { DashboardRun } from "../../models/dashboard";
import { formatRelative } from "../../state/home/dashboardState";
import { formatTokens, formatUsd } from "../../state/usage/usageChartModel";
import { CardEmpty, CardSkeleton, DashboardCard, ToneDot } from "./DashboardCard";
import { Activity, CalendarClock } from "../primitives/icons";

/**
 * Recent activity: the turns in flight, then the last completed runs from the
 * usage ledger. Clicking a row opens the chat it happened in.
 *
 * The ledger records completed runs only, so a failed turn never appears
 * here. That is a deliberate limit of the ledger rather than a claim that
 * nothing failed — the chat itself is the record of that.
 */
export function RecentActivityCard({
  runs,
  loading,
  now,
  onOpenChat,
}: {
  runs: DashboardRun[];
  loading: boolean;
  now: number;
  onOpenChat: (chatId: string) => void;
}) {
  return (
    <DashboardCard
      title="Recent activity"
      subtitle={runs.length === 0 ? "No runs yet" : `Last ${runs.length} agent runs`}
      Icon={Activity}
    >
      {loading ? (
        <CardSkeleton rows={4} />
      ) : runs.length === 0 ? (
        <CardEmpty>
          Nothing has run yet. Start a chat in a project and the turns will be listed here with
          what they cost.
        </CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {runs.map((run) => (
            <li key={run.id}>
              <button
                type="button"
                onClick={() => onOpenChat(run.chatId)}
                class="flex w-full items-center gap-3 px-4 py-2.5 text-left transition hover:bg-white/[0.04]"
              >
                <ToneDot
                  tone={run.status === "running" ? "green" : "grey"}
                  title={run.status === "running" ? "Running now" : "Finished"}
                  pulse={run.status === "running"}
                />
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-1.5">
                    <span class="truncate text-[13px] font-medium text-ink-50">
                      {run.chatTitle || "Untitled chat"}
                    </span>
                    {run.scheduled && (
                      <CalendarClock
                        class="h-3 w-3 flex-none text-ink-400"
                        aria-label="Scheduled run"
                      />
                    )}
                  </div>
                  <div class="mt-0.5 truncate text-[11.5px] text-ink-300">
                    {[run.projectName || "No project", run.provider, run.model]
                      .filter(Boolean)
                      .join(" · ")}
                  </div>
                </div>
                <div dir="ltr" class="bidi-ltr flex-none text-right">
                  <div class="font-mono text-[12px] tabular-nums text-ink-200">
                    {run.status === "running"
                      ? "running"
                      : run.costUsd == null
                        ? "—"
                        : `${run.estimated ? "~" : ""}${formatUsd(run.costUsd)}`}
                  </div>
                  <div class="mt-0.5 text-[11px] tabular-nums text-ink-400">
                    {run.status === "running"
                      ? formatRelative(run.startedAt, now)
                      : run.totalTokens
                        ? `${formatTokens(run.totalTokens)} · ${formatRelative(run.finishedAt, now)}`
                        : formatRelative(run.finishedAt, now)}
                  </div>
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}
