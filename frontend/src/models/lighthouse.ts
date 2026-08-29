/**
 * Local Lighthouse audits.
 *
 * Every score is optional on purpose. Lighthouse omits a category it could not
 * compute, and a missing score rendered as zero would tell the operator their
 * page failed completely when in fact it was never measured — so "not
 * measured" and "scored badly" stay different states all the way to the pixel.
 */

export type LighthouseStatus = "running" | "ready" | "failed";
export type FormFactor = "mobile" | "desktop";

export interface LighthouseMetric {
  id: string;
  label: string;
  /** Lighthouse's own rendering ("1.2 s"), kept verbatim. */
  display?: string;
  value: number;
  unit?: string;
  /** 0..1, absent where the metric carries no verdict of its own. */
  score?: number;
}

export interface LighthouseFinding {
  id: string;
  title: string;
  category?: string;
  display?: string;
  score?: number;
  savingsMs?: number;
}

export interface LighthouseReport {
  path: string;
  error?: string;
  performance?: number;
  accessibility?: number;
  bestPractices?: number;
  seo?: number;
  metrics?: LighthouseMetric[];
  opportunities?: LighthouseFinding[];
  version?: string;
  fetchedAt?: number;
}

export interface LighthouseRun {
  id: string;
  status: LighthouseStatus;
  error?: string;
  label?: string;
  port: number;
  paths: string[];
  formFactor: FormFactor;
  reports: LighthouseReport[];
  createdBy?: string;
  createdAt: number;
  finishedAt?: number;
}

export interface LighthouseOverview {
  runs: LighthouseRun[];
  running: boolean;
  /** Absent when the container is not up, so the question was never asked. */
  installed?: boolean;
}

/** Mirrors lighthouse.MaxPaths. */
export const MAX_PATHS = 6;

/** The four categories, in the order Lighthouse's own report lists them. */
export const CATEGORIES: Array<{ key: keyof LighthouseReport; label: string }> = [
  { key: "performance", label: "Performance" },
  { key: "accessibility", label: "Accessibility" },
  { key: "bestPractices", label: "Best Practices" },
  { key: "seo", label: "SEO" },
];

export type ScoreBand = "good" | "average" | "poor" | "unknown";

/**
 * Lighthouse's own thresholds: 90 and up is green, 50 and up is orange.
 *
 * They are copied rather than invented so a score here means exactly what the
 * same score means in Chrome's own report and in PageSpeed Insights. An
 * operator comparing the two must not find them disagreeing about a colour.
 */
export function scoreBand(score: number | undefined): ScoreBand {
  if (typeof score !== "number") return "unknown";
  if (score >= 90) return "good";
  if (score >= 50) return "average";
  return "poor";
}

export function reportMeasured(report: LighthouseReport): boolean {
  return !report.error && typeof report.performance === "number";
}

/** The run's headline: the worst performance score across its pages. */
export function runHeadline(run: LighthouseRun): string {
  if (run.status === "running") {
    return `auditing ${run.reports.length} of ${run.paths.length}…`;
  }
  if (run.status === "failed") return run.error || "the run failed";

  const measured = run.reports.filter(reportMeasured);
  if (measured.length === 0) return "no page could be measured";
  const worst = Math.min(...measured.map((report) => report.performance as number));
  const noun = measured.length === 1 ? "page" : "pages";
  return `${measured.length} ${noun} · worst performance ${worst}`;
}

/**
 * The same page in an earlier run, so a score can be shown against last time.
 *
 * The match is on path and form factor together: a mobile score next to a
 * desktop one is not a trend, it is two different measurements, and showing
 * one as a change in the other would be a lie the operator would act on.
 */
export function previousReport(
  runs: LighthouseRun[],
  run: LighthouseRun,
  path: string,
): LighthouseReport | null {
  const index = runs.findIndex((candidate) => candidate.id === run.id);
  if (index < 0) return null;
  // runs arrive newest first, so "earlier" is further down the list.
  for (const older of runs.slice(index + 1)) {
    if (older.formFactor !== run.formFactor) continue;
    const match = older.reports.find((report) => report.path === path);
    if (match && reportMeasured(match)) return match;
  }
  return null;
}

/**
 * The change in one category since the previous comparable run.
 *
 * Returns null when there is nothing honest to say — no earlier run, or a
 * score missing at either end. A delta of zero is a real answer and renders as
 * "no change"; a fabricated one is not.
 */
export function scoreDelta(
  current: LighthouseReport,
  previous: LighthouseReport | null,
  key: keyof LighthouseReport,
): number | null {
  if (!previous) return null;
  const now = current[key];
  const before = previous[key];
  if (typeof now !== "number" || typeof before !== "number") return null;
  return now - before;
}
