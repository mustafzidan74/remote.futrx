export type ScheduledTaskKind = "once" | "cron";

export interface ScheduledTask {
  id: string;
  name: string;
  ownerEmail: string;
  projectId: string;
  chatId: string;
  prompt: string;
  kind: ScheduledTaskKind;
  at?: number;
  cron?: string;
  timezone: string;
  enabled: boolean;
  status: string;
  nextRunAt?: number;
  runCount: number;
  maxRuns?: number;
  lastRunAt?: number;
  lastRunStatus?: string;
  lastError?: string;
  lastRunResult?: string;
  overlapPolicy?: string;
  next?: ChainLink[];
  condition?: ScheduleCondition;
  createdByAgent?: boolean;
  createdAt: number;
  updatedAt: number;
}

export interface CreateScheduledTaskInput {
  name: string;
  prompt: string;
  kind: ScheduledTaskKind;
  at?: number;
  cron?: string;
  timezone: string;
  maxRuns?: number;
  next?: ChainLink[];
  condition?: ScheduleCondition;
}

export interface UpdateScheduledTaskInput {
  enabled?: boolean;
  name?: string;
  prompt?: string;
  at?: number;
  cron?: string;
  timezone?: string;
  maxRuns?: number;
  next?: ChainLink[];
  condition?: ScheduleCondition;
}

export type ChainWhen = "success" | "failure" | "always";

export interface ChainLink {
  taskId: string;
  when: ChainWhen;
  delayMin?: number;
}

export type ScheduleConditionKind =
  | "outputContains"
  | "httpStatus"
  | "commandExitCode"
  | "weekdays"
  | "notIfRanWithin";

// A gate evaluated before every unforced occurrence. Only the fields the kind
// names are meaningful; the backend strips the rest on save.
export interface ScheduleCondition {
  kind: ScheduleConditionKind;
  pattern?: string;
  /** A task id, or "self" for this task's own last run. */
  inLastRunOf?: string;
  url?: string;
  command?: string;
  /** HTTP status (default 200) or exit code (default 0). */
  expect?: number;
  /** 0 = Sunday … 6 = Saturday, read in the task's timezone. */
  weekdays?: number[];
  minutes?: number;
}

export interface ScheduleChainRun {
  fromTaskId?: string;
  depth?: number;
  total?: number;
}

export type ScheduleRunHistoryStatus = "ok" | "failed" | "skipped" | "cancelled";

export interface ScheduleRunRecord {
  runId: string;
  taskId: string;
  chatId?: string;
  startedAt: number;
  finishedAt: number;
  durationMs: number;
  status: ScheduleRunHistoryStatus;
  summary?: string;
  result?: string;
  error?: string;
  skippedByGate?: boolean;
  gateReason?: string;
  chain?: ScheduleChainRun;
  chainTriggered?: string[];
  forced?: boolean;
  tokens?: number;
  costUsd?: number;
  filesChanged?: string[];
  diffStat?: string;
  commitSha?: string;
}

export interface ScheduleRunDiff {
  runId: string;
  taskId: string;
  filesChanged?: string[];
  diffStat?: string;
  commitStat?: string;
  commitSha?: string;
  unavailable?: string;
}
