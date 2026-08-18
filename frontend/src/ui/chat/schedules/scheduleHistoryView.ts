import type {
  ChainLink,
  ScheduledTask,
  ScheduleChainRun,
  ScheduleCondition,
  ScheduleRunRecord,
} from "../../../models/schedule";

/** One row of `git --stat` output: a path and its churn. */
export interface DiffStatRow {
  path: string;
  changes: number;
  additions: number;
  deletions: number;
}

export interface DiffStatSummary {
  rows: DiffStatRow[];
  /** The trailing "N files changed, …" line, when git emitted one. */
  total: string;
}

// parseDiffStat reads `git diff --stat` / `git show --stat` output. The stat
// format is not a unified diff, so it cannot go through diffModel's parser;
// this keeps the History drawer showing real rows instead of a raw <pre>.
export function parseDiffStat(stat: string): DiffStatSummary {
  const rows: DiffStatRow[] = [];
  let total = "";

  for (const raw of (stat || "").split("\n")) {
    const line = raw.trimEnd();
    if (!line.trim()) continue;

    const summary = line.match(/^\s*\d+ files? changed.*$/);
    if (summary) {
      total = line.trim();
      continue;
    }

    const separator = line.lastIndexOf("|");
    if (separator < 0) continue;
    const path = line.slice(0, separator).trim();
    const churn = line.slice(separator + 1).trim();
    if (!path) continue;

    const count = Number.parseInt(churn, 10);
    rows.push({
      path,
      changes: Number.isNaN(count) ? 0 : count,
      additions: (churn.match(/\+/g) ?? []).length,
      deletions: (churn.match(/-/g) ?? []).length,
    });
  }

  return { rows, total };
}

export function formatRunDuration(durationMs: number): string {
  if (!durationMs || durationMs < 0) return "—";
  if (durationMs < 1000) return `${durationMs} ms`;
  const seconds = Math.round(durationMs / 100) / 10;
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(durationMs / 60_000);
  const remainder = Math.round((durationMs % 60_000) / 1000);
  return `${minutes}m ${remainder}s`;
}

export function runStatusTone(record: ScheduleRunRecord): string {
  switch (record.status) {
    case "ok":
      return "text-accent-green";
    case "failed":
      return "text-accent-red";
    case "skipped":
      return "text-amber-300";
    default:
      return "text-ink-300";
  }
}

export function runStatusLabel(record: ScheduleRunRecord): string {
  if (record.skippedByGate) return "skipped (gate)";
  return record.status;
}

/** "chain 2/3", or "" when the run was not part of a chain. */
export function chainLabel(chain?: ScheduleChainRun): string {
  if (!chain || !chain.depth) return "";
  const total = Math.max(chain.total ?? chain.depth, chain.depth);
  return `chain ${chain.depth}/${total}`;
}

export function formatRunCost(record: ScheduleRunRecord): string {
  const parts: string[] = [];
  if (record.tokens) parts.push(`${record.tokens.toLocaleString()} tok`);
  if (typeof record.costUsd === "number") parts.push(`$${record.costUsd.toFixed(4)}`);
  return parts.join(" · ");
}

/** Human summary of a gate, for the task card badge and the editor preview. */
export function describeCondition(condition?: ScheduleCondition): string {
  if (!condition) return "";
  switch (condition.kind) {
    case "outputContains": {
      const source = !condition.inLastRunOf || condition.inLastRunOf === "self"
        ? "its own last run"
        : "another task's last run";
      return `only if ${source} matches /${condition.pattern ?? ""}/`;
    }
    case "httpStatus":
      return `only if ${condition.url ?? "the url"} answers ${condition.expect ?? 200}`;
    case "commandExitCode":
      return `only if \`${condition.command ?? ""}\` exits ${condition.expect ?? 0}`;
    case "weekdays":
      return `only on ${(condition.weekdays ?? []).map(weekdayName).join(", ") || "no day"}`;
    case "notIfRanWithin":
      return `not if it ran in the last ${condition.minutes ?? 0} min`;
    default:
      return "";
  }
}

const WEEKDAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

export function weekdayName(day: number): string {
  return WEEKDAY_NAMES[day] ?? String(day);
}

/** The tasks a chain may point at: same chat, never the task being edited. */
export function chainCandidates(
  tasks: ScheduledTask[],
  editingId: string
): ScheduledTask[] {
  return tasks.filter((task) => task.id !== editingId);
}

// describeChain renders the arrow badge shown on a task card.
export function describeChain(
  links: ChainLink[] | undefined,
  tasks: ScheduledTask[]
): string {
  if (!links || links.length === 0) return "";
  const names = links.map((link) => {
    const target = tasks.find((task) => task.id === link.taskId);
    const name = target ? target.name : link.taskId.slice(0, 8);
    const delay = link.delayMin ? ` +${link.delayMin}m` : "";
    return `${name} (${link.when}${delay})`;
  });
  return `→ ${names.join(", ")}`;
}
