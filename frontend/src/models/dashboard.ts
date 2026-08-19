import type { UsageDayPoint, UsageTotals } from "./usage";

/**
 * The home dashboard, as `GET /api/dashboard` returns it.
 *
 * The backend fans out across projects, health, chats, the usage ledger,
 * schedules, snapshots, notifications, platform health and host capacity, and
 * answers with one document. The browser therefore makes one request for the
 * landing screen rather than nine, and every number on the page describes the
 * same instant.
 *
 * Everything here is already scoped to the signed-in user: a member never
 * receives a project, chat, run or task they could not reach on their own.
 */
export interface DashboardSnapshot {
  generatedAt: number;
  /** How many UTC days the usage series and the week-over-week delta cover. */
  windowDays: number;
  kpis: DashboardKpis;
  projects: DashboardProject[];
  alerts: DashboardAlert[];
  recent: DashboardRun[];
  upcoming: DashboardTask[];
  usage: DashboardUsage;
  platform: DashboardPlatform;
  /** The watched client websites that are not currently green. */
  sites: DashboardClientSites;
}

/**
 * The home screen's client-site card. `available` is false when this
 * deployment watches nothing at all, which renders as "not set up" rather
 * than as "everything is fine".
 */
export interface DashboardClientSites {
  available: boolean;
  sites: DashboardClientSite[];
}

export interface DashboardClientSite {
  id: string;
  label: string;
  url: string;
  status: string;
  /** The newest failure reason, already trimmed for a card row. */
  detail?: string;
  /** When the current state began, so the card can say how long. */
  since?: number;
  lastCheckedAt?: number;
  projectId?: string;
}

export interface DashboardKpis {
  runningProjects: number;
  totalProjects: number;
  /** Chats with a turn in flight right now. */
  activeRuns: number;
  runsThisWeek: number;
  runsLastWeek: number;
  tokensThisWeek: number;
  tokensLastWeek: number;
  costThisWeek: number;
  costLastWeek: number;
  /** The part of costThisWeek that came from the editable price table. */
  estimatedCostThisWeek: number;
  /** Runs nothing could price at all — never read as zero cost. */
  unpricedRunsThisWeek: number;
  alerts: number;
  criticalAlerts: number;
}

export type DashboardHealth = "ok" | "warn" | "crit" | "unknown";

export interface DashboardProject {
  id: string;
  name: string;
  slug: string;
  status: string;
  health: DashboardHealth;
  healthReasons?: string[];
  memoryPct?: number;
  /** The project's own lowest listening port, absent when nothing is up. */
  previewPort?: number;
  latestChatId?: string;
  lastActivityAt?: number;
  lastSnapshotAt?: number;
  /** True while a chat in this project has a turn in flight. */
  running?: boolean;
}

export type AlertSeverity = "info" | "warn" | "crit";

export type AlertKind =
  | "health"
  | "autopilot"
  | "trash"
  | "snapshot"
  | "notifications"
  | "backup"
  | "platform"
  | "capacity"
  | "siteWatch";

/**
 * The fix an alert offers. A closed vocabulary, so the browser wires a button
 * to it instead of parsing the title — and an alert whose action this client
 * does not recognize simply renders without a button.
 */
export type AlertAction =
  | "open-project"
  | "open-chat"
  | "snapshot-now"
  | "restore-trash"
  | "enable-notifications"
  | "open-monitoring"
  | "open-resources"
  | "open-client-sites";

export interface DashboardAlert {
  id: string;
  severity: AlertSeverity;
  kind: AlertKind;
  title: string;
  detail?: string;
  action?: AlertAction;
  actionLabel?: string;
  projectId?: string;
  chatId?: string;
  /** The watched client site a siteWatch alert is about. */
  siteId?: string;
  at?: number;
}

export type DashboardRunStatus = "running" | "finished";

export interface DashboardRun {
  id: string;
  chatId: string;
  chatTitle: string;
  /** The auxiliary model's one-line summary of the chat, when there is one. */
  chatSummary?: string;
  projectId?: string;
  projectName?: string;
  provider?: string;
  model?: string;
  status: DashboardRunStatus;
  startedAt?: number;
  finishedAt?: number;
  /** Absent when the run's cost is unknown — never treat that as zero. */
  costUsd?: number;
  estimated?: boolean;
  totalTokens?: number;
  scheduled?: boolean;
}

export interface DashboardTask {
  id: string;
  name: string;
  projectId?: string;
  projectName?: string;
  chatId?: string;
  kind: string;
  cron?: string;
  timezone?: string;
  nextRunAt: number;
  lastRunAt?: number;
  lastRunStatus?: string;
  lastError?: string;
}

export interface DashboardUsage {
  /** False when this deployment records no usage — not the same as zero. */
  available: boolean;
  daily: UsageDayPoint[];
  thisWeek: UsageTotals;
  lastWeek: UsageTotals;
}

export interface DashboardPlatform {
  status: string;
  version?: string;
  checks: { backend: string; lxd: string; caddy: string };
  details?: string[];
  memoryBudgetBytes?: number;
  memoryCommittedBytes?: number;
  runningContainers: number;
  maxRunningContainers?: number;
  healthMonitorEnabled: boolean;
  notificationsEnabled: boolean;
  /** False on a host with no backup marker directory — never an alarm. */
  backupReadable: boolean;
  backupAt?: number;
}
