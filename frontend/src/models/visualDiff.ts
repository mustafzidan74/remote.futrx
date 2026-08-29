/**
 * Before/after visual comparison.
 *
 * A baseline is what the project is supposed to look like; a comparison is
 * what it looks like now. Everything here is read-only shape plus the small
 * amount of judgement the UI needs to turn numbers into a sentence — the
 * thresholds themselves live in the service, so the two cannot disagree about
 * whether a page changed.
 */

export type VisualStatus = "running" | "ready" | "failed";

export interface VisualShot {
  path: string;
  file?: string;
  url?: string;
  width?: number;
  height?: number;
  bytes?: number;
  /** Why this one page could not be photographed. The run continues without it. */
  error?: string;
}

export interface VisualBaseline {
  id: string;
  status: VisualStatus;
  error?: string;
  port: number;
  paths: string[];
  width: number;
  height: number;
  fullPage?: boolean;
  threshold: number;
  pages: VisualShot[];
  createdBy?: string;
  createdAt: number;
  finishedAt?: number;
}

export interface VisualPageDiff {
  path: string;
  beforeUrl?: string;
  afterUrl?: string;
  diffUrl?: string;
  changedPercent: number;
  changedPixels: number;
  width?: number;
  height?: number;
  /** The page's own dimensions moved, which the percentage alone understates. */
  sizeChanged?: boolean;
  error?: string;
}

export interface VisualComparison {
  id: string;
  baselineId: string;
  status: VisualStatus;
  error?: string;
  label?: string;
  pages: VisualPageDiff[];
  changedPages: number;
  maxChangedPercent: number;
  createdBy?: string;
  createdAt: number;
  finishedAt?: number;
}

export interface VisualOverview {
  baseline?: VisualBaseline;
  comparisons: VisualComparison[];
  /** A run is in flight, so the buttons are disabled and the panel polls. */
  running: boolean;
}

/** Mirrors visualdiff.ChangedPercentFloor. */
export const CHANGED_PERCENT_FLOOR = 0.1;

/** Mirrors visualdiff.MaxPaths. */
export const MAX_PATHS = 12;

export function pageChanged(page: VisualPageDiff): boolean {
  return !page.error && (!!page.sizeChanged || page.changedPercent >= CHANGED_PERCENT_FLOOR);
}

/**
 * How loudly to render one page.
 *
 * The bands are deliberately coarse. The operator is scanning a list to find
 * the page they did not expect, and a precise gradient would make every row
 * look slightly different from every other one, which is the opposite of
 * scannable.
 */
export type ChangeTone = "none" | "slight" | "notable" | "major" | "failed";

export function changeTone(page: VisualPageDiff): ChangeTone {
  if (page.error) return "failed";
  if (!pageChanged(page)) return "none";
  if (page.sizeChanged || page.changedPercent >= 20) return "major";
  if (page.changedPercent >= 3) return "notable";
  return "slight";
}

/** The change as the operator reads it, not as a float. */
export function changeLabel(page: VisualPageDiff): string {
  if (page.error) return "could not compare";
  if (!pageChanged(page)) return "unchanged";
  const percent = page.changedPercent < 1
    ? page.changedPercent.toFixed(2)
    : page.changedPercent.toFixed(1);
  if (page.sizeChanged) return `${percent}% · page height changed`;
  return `${percent}% changed`;
}

/**
 * The headline for a finished comparison.
 *
 * "Nothing moved" is the answer worth stating plainly: it is the result the
 * operator hopes for and the one a list of twelve green rows communicates
 * badly.
 */
export function comparisonHeadline(comparison: VisualComparison): string {
  if (comparison.status === "running") return "photographing pages…";
  if (comparison.status === "failed") return comparison.error || "the run failed";
  const compared = comparison.pages.length;
  if (compared === 0) return "no pages were compared";
  if (comparison.changedPages === 0) return `nothing moved across ${compared} pages`;
  // The noun belongs to the total, not to the count that moved: "1 of 2 pages
  // changed", never "1 of 2 page changed".
  const noun = compared === 1 ? "page" : "pages";
  return `${comparison.changedPages} of ${compared} ${noun} changed`;
}

/** Sorts the worst offender to the top: that is the row being looked for. */
export function byMostChanged(pages: VisualPageDiff[]): VisualPageDiff[] {
  return [...pages].sort((left, right) => {
    if (!!left.error !== !!right.error) return left.error ? 1 : -1;
    if (left.sizeChanged !== right.sizeChanged) return left.sizeChanged ? -1 : 1;
    return right.changedPercent - left.changedPercent;
  });
}
