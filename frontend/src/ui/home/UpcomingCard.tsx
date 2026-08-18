import { useState } from "preact/hooks";
import type { DashboardTask } from "../../models/dashboard";
import { formatCountdown, formatRelative, type StatusTone } from "../../state/home/dashboardState";
import { CardEmpty, CardSkeleton, DashboardCard, TONE_TEXT } from "./DashboardCard";
import { CalendarClock, Play } from "../primitives/icons";

/** How the previous run of a task reads, and what colour says so. */
const OUTCOME: Record<string, { label: string; tone: StatusTone }> = {
  succeeded: { label: "last run succeeded", tone: "green" },
  failed: { label: "last run failed", tone: "red" },
  skipped: { label: "last run skipped", tone: "amber" },
  abandoned: { label: "last run abandoned", tone: "amber" },
  queued: { label: "last run queued", tone: "grey" },
  running: { label: "running now", tone: "green" },
};

/**
 * Upcoming: the next armed scheduled tasks with a live countdown and how the
 * previous run went, because "runs nightly" is only reassuring if the last
 * one actually worked.
 */
export function UpcomingCard({
  tasks,
  loading,
  now,
  onRunNow,
  onOpenChat,
}: {
  tasks: DashboardTask[];
  loading: boolean;
  now: number;
  onRunNow: (taskId: string) => Promise<void>;
  onOpenChat: (chatId: string) => void;
}) {
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function runNow(task: DashboardTask) {
    setBusyId(task.id);
    setError(null);
    try {
      await onRunNow(task.id);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusyId(null);
    }
  }

  return (
    <DashboardCard
      title="Upcoming"
      subtitle={tasks.length === 0 ? "Nothing scheduled" : `Next ${tasks.length} scheduled tasks`}
      Icon={CalendarClock}
    >
      {loading ? (
        <CardSkeleton rows={3} />
      ) : tasks.length === 0 ? (
        <CardEmpty>
          No armed schedules. A scheduled task sends a prompt into one of its project's chats on a
          cron or at a fixed time.
        </CardEmpty>
      ) : (
        <>
          {error && (
            <p class="px-4 pt-3 text-[12px] text-accent-red" role="alert">
              {error}
            </p>
          )}
          <ul class="divide-y divide-white/[0.06]">
            {tasks.map((task) => {
              const outcome = task.lastRunStatus ? OUTCOME[task.lastRunStatus] : undefined;
              return (
                <li key={task.id} class="flex items-center gap-3 px-4 py-3">
                  <div class="min-w-0 flex-1">
                    <button
                      type="button"
                      onClick={() => task.chatId && onOpenChat(task.chatId)}
                      disabled={!task.chatId}
                      class="block max-w-full truncate text-left text-[13px] font-medium text-ink-50
                             disabled:cursor-default"
                    >
                      {task.name || "Untitled task"}
                    </button>
                    <div class="mt-0.5 truncate text-[11.5px] text-ink-300">
                      {[
                        task.projectName,
                        task.kind === "cron" && task.cron ? task.cron : undefined,
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                      {outcome && (
                        <>
                          {" · "}
                          <span class={TONE_TEXT[outcome.tone]}>{outcome.label}</span>
                          {task.lastRunAt ? ` ${formatRelative(task.lastRunAt, now)}` : ""}
                        </>
                      )}
                    </div>
                  </div>

                  <span
                    dir="ltr"
                    class="bidi-ltr flex-none font-mono text-[12px] tabular-nums text-ink-200"
                    title={new Date(task.nextRunAt).toLocaleString()}
                  >
                    {formatCountdown(task.nextRunAt, now)}
                  </span>

                  <button
                    type="button"
                    onClick={() => void runNow(task)}
                    disabled={busyId === task.id}
                    class="inline-flex h-7 flex-none items-center gap-1 rounded-md bg-white/[0.08] px-2
                           text-[12px] font-medium text-ink-100 transition hover:bg-white/[0.12]
                           disabled:opacity-50"
                    title={`Run ${task.name} now`}
                  >
                    <Play class="h-3 w-3" />
                    {busyId === task.id ? "Starting" : "Run now"}
                  </button>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </DashboardCard>
  );
}
