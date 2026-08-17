export type UsageGroupBy = "project" | "user" | "provider" | "model" | "day" | "chat";

export interface UsageTotals {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  totalTokens: number;
  /** Sum of every run whose cost is known, exact or estimated. */
  costUsd: number;
  /** The part of costUsd that came from the editable price table. */
  estimatedCostUsd: number;
  runs: number;
  /** Runs whose cost could not be determined at all. */
  unpricedRuns: number;
}

export interface UsageGroup extends UsageTotals {
  key: string;
  label: string;
}

export interface UsageDayPoint {
  day: string;
  totalTokens: number;
  costUsd: number;
  runs: number;
}

export interface UsageSummary {
  from: number;
  to: number;
  groupBy: UsageGroupBy;
  totals: UsageTotals;
  projects: number;
  groups: UsageGroup[];
  daily: UsageDayPoint[];
}

export interface UsageRecord {
  at: number;
  projectId?: string;
  projectSlug?: string;
  chatId: string;
  runId?: string;
  userEmail?: string;
  provider: string;
  model?: string;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  /** Absent when the run's cost is unknown — never treat that as zero. */
  costUsd?: number;
  estimated?: boolean;
  durationMs?: number;
  turns?: number;
  scheduled?: boolean;
}

export interface UsageRecordPage {
  records: UsageRecord[];
  nextCursor?: string;
}

export interface UsageModelPrice {
  match: string;
  label?: string;
  inputPerMTok: number;
  outputPerMTok: number;
  cacheReadPerMTok?: number;
  cacheWritePerMTok?: number;
}

export interface UsagePriceTable {
  version: number;
  updatedAt?: number;
  currency: string;
  models: UsageModelPrice[];
}

export interface UsageRebuildResult {
  chats: number;
  records: number;
  months: string[];
  preservedActors: number;
}

export const EMPTY_USAGE_TOTALS: UsageTotals = {
  inputTokens: 0,
  outputTokens: 0,
  cacheReadTokens: 0,
  cacheWriteTokens: 0,
  totalTokens: 0,
  costUsd: 0,
  estimatedCostUsd: 0,
  runs: 0,
  unpricedRuns: 0,
};
