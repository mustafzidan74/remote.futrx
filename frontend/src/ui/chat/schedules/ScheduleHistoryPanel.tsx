import { useEffect, useState } from "preact/hooks";
import { chatApi } from "../../../api/chatApi";
import type {
  ScheduledTask,
  ScheduleRunDiff,
  ScheduleRunRecord,
} from "../../../models/schedule";
import { AlertCircle, FileText, Loader, RotateCcw, X } from "../../primitives/icons";
import { formatTimestamp } from "./scheduledTaskView";
import {
  chainLabel,
  formatRunCost,
  formatRunDuration,
  parseDiffStat,
  runStatusLabel,
  runStatusTone,
} from "./scheduleHistoryView";

// ScheduleHistoryPanel is the "History" drawer of one task: the last 50 runs
// with their outcome, duration, verdict and touched files, plus the stored
// diff stat for any run the operator opens.
export function ScheduleHistoryPanel({
  task,
  onClose,
}: {
  task: ScheduledTask;
  onClose: () => void;
}) {
  const [records, setRecords] = useState<ScheduleRunRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [openRunId, setOpenRunId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    setOpenRunId(null);
    chatApi
      .fetchScheduleHistory(task.id)
      .then((response) => {
        if (active) setRecords(response);
      })
      .catch((err) => {
        if (active) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [task.id]);

  async function reload() {
    setLoading(true);
    setError(null);
    try {
      setRecords(await chatApi.fetchScheduleHistory(task.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  return (
    <section class="mt-3 rounded-md border border-white/[0.08] bg-black/25 p-2.5">
      <header class="flex items-center gap-2">
        <h4 class="flex-1 text-[12px] font-medium text-ink-100">
          Run history — {task.name}
        </h4>
        <button
          type="button"
          onClick={() => void reload()}
          disabled={loading}
          class="h-7 w-7 rounded-md border border-white/10 bg-white/[0.03] text-ink-300 grid place-items-center hover:bg-white/[0.08] disabled:opacity-45"
          title="Reload history"
          aria-label="Reload run history"
        >
          {loading ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <RotateCcw class="w-3.5 h-3.5" />}
        </button>
        <button
          type="button"
          onClick={onClose}
          class="h-7 w-7 rounded-md border border-white/10 bg-white/[0.03] text-ink-300 grid place-items-center hover:bg-white/[0.08]"
          title="Close history"
          aria-label="Close run history"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      </header>

      {error && (
        <div class="mt-2 flex items-start gap-2 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-2.5 py-2 text-[11.5px] text-accent-red">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" />
          <span class="min-w-0 flex-1 break-words">{error}</span>
        </div>
      )}

      {!error && records.length === 0 && !loading && (
        <p class="mt-2 text-[11.5px] leading-5 text-ink-400">
          No runs recorded yet. Every fire — including one skipped by a
          condition — appears here once it settles.
        </p>
      )}

      {records.length > 0 && (
        <div class="mt-2 overflow-x-auto">
          <table class="w-full min-w-[520px] border-collapse text-left text-[11.5px]">
            <thead>
              <tr class="text-[10px] uppercase tracking-wide text-ink-400">
                <th class="py-1 pr-2 font-medium">Time</th>
                <th class="py-1 pr-2 font-medium">Status</th>
                <th class="py-1 pr-2 font-medium">Duration</th>
                <th class="py-1 pr-2 font-medium">Result</th>
                <th class="py-1 pr-2 font-medium">Files</th>
                <th class="py-1 font-medium" />
              </tr>
            </thead>
            <tbody>
              {records.map((record) => (
                <RunRow
                  key={record.runId + record.startedAt}
                  task={task}
                  record={record}
                  open={openRunId === record.runId}
                  onToggle={() =>
                    setOpenRunId((current) =>
                      current === record.runId ? null : record.runId)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function RunRow({
  task,
  record,
  open,
  onToggle,
}: {
  task: ScheduledTask;
  record: ScheduleRunRecord;
  open: boolean;
  onToggle: () => void;
}) {
  const files = record.filesChanged ?? [];
  const chain = chainLabel(record.chain);
  const cost = formatRunCost(record);

  return (
    <>
      <tr class="border-t border-white/[0.06] align-top">
        <td class="py-1.5 pr-2 text-ink-200 whitespace-nowrap">
          {formatTimestamp(record.startedAt)}
        </td>
        <td class={`py-1.5 pr-2 whitespace-nowrap ${runStatusTone(record)}`}>
          {runStatusLabel(record)}
        </td>
        <td class="py-1.5 pr-2 text-ink-300 whitespace-nowrap">
          {formatRunDuration(record.durationMs)}
        </td>
        <td class="py-1.5 pr-2 text-ink-200 max-w-[10rem] truncate" title={record.result || ""}>
          {record.result || "—"}
        </td>
        <td class="py-1.5 pr-2 text-ink-300 whitespace-nowrap">
          {files.length > 0 ? `${files.length}` : "—"}
        </td>
        <td class="py-1.5 text-right">
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={open}
            class={`inline-flex h-6 items-center gap-1 rounded border px-1.5 text-[10.5px]
                    ${open
                      ? "border-accent-blue/35 bg-accent-blue/[0.14] text-accent-blue"
                      : "border-white/10 bg-white/[0.03] text-ink-300 hover:bg-white/[0.08]"}`}
          >
            <FileText class="w-3 h-3" />
            {open ? "Hide" : "Details"}
          </button>
        </td>
      </tr>
      {(chain || cost || record.gateReason) && (
        <tr class="align-top">
          <td colSpan={6} class="pb-1.5 text-[10.5px] text-ink-400">
            {[chain, cost, record.gateReason && `gate: ${record.gateReason}`]
              .filter(Boolean)
              .join(" · ")}
          </td>
        </tr>
      )}
      {open && (
        <tr>
          <td colSpan={6} class="pb-2">
            <RunDetails task={task} record={record} />
          </td>
        </tr>
      )}
    </>
  );
}

function RunDetails({
  task,
  record,
}: {
  task: ScheduledTask;
  record: ScheduleRunRecord;
}) {
  const [diff, setDiff] = useState<ScheduleRunDiff | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    chatApi
      .fetchScheduleRunDiff(task.id, record.runId)
      .then((response) => {
        if (active) setDiff(response);
      })
      .catch((err) => {
        if (active) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [task.id, record.runId]);

  // The commit stat is read live and is the more complete picture; the stored
  // working-tree stat is the fallback for a run that committed nothing.
  const stat = diff?.commitStat || diff?.diffStat || record.diffStat || "";
  const summary = parseDiffStat(stat);

  return (
    <div class="rounded-md border border-white/[0.07] bg-black/30 p-2.5 text-[11.5px] leading-5">
      {record.summary && (
        <p class="whitespace-pre-wrap break-words text-ink-300">{record.summary}</p>
      )}
      {record.error && (
        <p class="mt-1.5 whitespace-pre-wrap break-words text-accent-red">{record.error}</p>
      )}

      {loading && (
        <div class="mt-2 flex items-center gap-1.5 text-ink-400">
          <Loader class="w-3.5 h-3.5 animate-spin" /> Loading diff…
        </div>
      )}
      {error && <p class="mt-2 text-accent-red">{error}</p>}

      {summary.rows.length > 0 && (
        <div class="mt-2 overflow-x-auto">
          <table class="w-full min-w-[320px] border-collapse text-left font-mono text-[11px]">
            <tbody>
              {summary.rows.map((row) => (
                <tr key={row.path}>
                  <td class="py-0.5 pr-3 text-ink-200 break-all">{row.path}</td>
                  <td class="py-0.5 pr-2 text-right text-ink-400">{row.changes || ""}</td>
                  <td class="py-0.5 whitespace-nowrap">
                    <span class="text-accent-green">{"+".repeat(row.additions)}</span>
                    <span class="text-accent-red">{"-".repeat(row.deletions)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {summary.total && (
            <div class="mt-1 font-mono text-[10.5px] text-ink-400">{summary.total}</div>
          )}
        </div>
      )}

      {summary.rows.length === 0 && !loading && (
        <p class="mt-2 text-ink-400">
          {diff?.unavailable || "This run recorded no file changes."}
        </p>
      )}

      {diff?.commitSha && (
        <div class="mt-1.5 font-mono text-[10.5px] text-ink-400">
          commit {diff.commitSha.slice(0, 12)}
        </div>
      )}
      {record.chainTriggered && record.chainTriggered.length > 0 && (
        <div class="mt-1.5 text-[10.5px] text-ink-400">
          Triggered {record.chainTriggered.length} chained task
          {record.chainTriggered.length === 1 ? "" : "s"}.
        </div>
      )}
    </div>
  );
}
