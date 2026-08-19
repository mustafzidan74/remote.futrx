import type {
  SiteChecks,
  SiteStatus,
  WatchedSiteInput,
  WatchedSiteView,
} from "../../models/sitewatch";

/**
 * Everything the Client sites panel decides about the payload it was given.
 *
 * The server answers with facts — a status, a response time, three uptime
 * percentages, a certificate expiry. This module turns them into the words,
 * tones, orderings and SVG geometry the table renders, and validates the form
 * the same way the backend does so a mistake is caught before a round trip.
 * It is all pure functions over plain records, so every rule here is testable
 * without a DOM.
 */

/** Fallback bounds for the check cadence. The server sends its own with every
 *  list response; these keep the form usable before the first one lands, and
 *  they mirror `MinIntervalMinutes`/`MaxIntervalMinutes` in
 *  `backend/internal/service/sitewatch/model.go`. */
export const SITE_INTERVAL_BOUNDS = { min: 1, max: 60 } as const;

/** Mirrors `DefaultIntervalMinutes` and `DefaultTLSWarnDays` in the backend. */
export const SITE_DEFAULTS = {
  intervalMinutes: 5,
  tlsWarnDays: 21,
} as const;

/** Mirrors `MaxSites` and `MaxExtraURLs`. */
export const SITE_LIMITS = { maxSites: 200, maxExtraUrls: 5 } as const;

/** Status tones, shared with the home dashboard's vocabulary. */
export type SiteTone = "green" | "amber" | "red" | "grey";

const STATUS_TONE: Record<SiteStatus, SiteTone> = {
  up: "green",
  slow: "amber",
  down: "red",
  unknown: "grey",
};

const STATUS_LABEL: Record<SiteStatus, string> = {
  up: "Up",
  slow: "Slow",
  down: "Down",
  unknown: "Not checked yet",
};

export interface SiteDot {
  tone: SiteTone;
  label: string;
  /** Tooltip: the label plus whatever the last check complained about. */
  title: string;
}

/** The status dot for one row. A disabled site is grey whatever its last
 *  reading was: nothing is watching it, so the old verdict is not news. */
export function siteDot(site: WatchedSiteView): SiteDot {
  if (!site.enabled) {
    return { tone: "grey", label: "Paused", title: "Checks are paused for this site" };
  }
  const label = STATUS_LABEL[site.status] ?? STATUS_LABEL.unknown;
  const detail = (site.lastError ?? "").trim();
  return {
    tone: STATUS_TONE[site.status] ?? "grey",
    label,
    title: detail ? `${label}\n${detail}` : label,
  };
}

/**
 * Row order: whatever needs attention first, then alphabetically.
 *
 * The server already sorts by name, so this only lifts the unwell rows to the
 * top — which is what makes a forty-site table readable at a glance without
 * reshuffling under the cursor on every refresh.
 */
export function sortSites(sites: WatchedSiteView[]): WatchedSiteView[] {
  const rank = (site: WatchedSiteView): number => {
    if (!site.enabled) return 4;
    switch (site.status) {
      case "down":
        return 0;
      case "slow":
        return 1;
      case "unknown":
        return 3;
      default:
        return 2;
    }
  };
  return [...sites].sort((left, right) => {
    const delta = rank(left) - rank(right);
    if (delta !== 0) return delta;
    return siteName(left).localeCompare(siteName(right));
  });
}

/** What a site is called: its label, or the hostname of its URL. */
export function siteName(site: { label?: string; url: string }): string {
  const label = (site.label ?? "").trim();
  if (label) return label;
  return siteHost(site.url);
}

/** The hostname of a URL, or the raw string when it will not parse. */
export function siteHost(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

/** "99.95%" for a percentage the server sent, "—" for a window with no
 *  checks. An absent percentage is missing data, never zero availability. */
export function formatUptime(percent: number | undefined): string {
  if (percent === undefined || percent === null || !Number.isFinite(percent)) return "—";
  // Whole numbers read better than "100.00%" in a dense table.
  return Number.isInteger(percent) ? `${percent}%` : `${percent.toFixed(2)}%`;
}

/** Response time, in the largest unit that keeps a digit. */
export function formatMs(ms: number | undefined): string {
  if (!ms || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)} s`;
}

export interface TlsView {
  label: string;
  tone: SiteTone;
}

/** The certificate column: days left, coloured by the site's own warning
 *  window rather than by a fixed threshold. */
export function describeTls(site: WatchedSiteView): TlsView {
  if (site.tlsDaysLeft === undefined || site.tlsDaysLeft === null) {
    return { label: "—", tone: "grey" };
  }
  const warnDays = site.checks?.tls?.warnDays ?? 0;
  if (site.tlsDaysLeft < 0) return { label: "expired", tone: "red" };
  if (site.tlsDaysLeft === 0) return { label: "today", tone: "red" };
  const label = `${site.tlsDaysLeft} d`;
  if (warnDays > 0 && site.tlsDaysLeft <= warnDays) return { label, tone: "amber" };
  return { label, tone: "green" };
}

/** "in 4 min" / "due now" for the next scheduled check. */
export function formatCountdown(at: number | undefined, now: number): string {
  if (!at) return "—";
  const remaining = at - now;
  if (remaining <= 0) return "due now";
  return `in ${formatDuration(remaining)}`;
}

/** "5 min ago" for a past instant; "never" for an absent one. */
export function formatAgo(at: number | undefined, now: number): string {
  if (!at) return "never";
  const elapsed = now - at;
  if (elapsed < 45_000) return "just now";
  return `${formatDuration(elapsed)} ago`;
}

/** Largest useful unit, no false precision. */
export function formatDuration(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h`;
  return `${Math.round(hours / 24)} d`;
}

/**
 * Geometry for the response-time sparkline.
 *
 * The chart is inline SVG with no library: forty points per row across up to
 * two hundred rows is exactly the case a charting dependency would make slow.
 * Failed checks arrive as zero and are dropped from the line rather than
 * drawn at the floor, because a failure is not a fast response — they are
 * returned separately so the view can mark them.
 */
export interface Sparkline {
  /** SVG path for the polyline, empty when there is nothing to draw. */
  path: string;
  /** X positions of the failed checks, for the gap markers. */
  failures: number[];
  width: number;
  height: number;
  /** The slowest sample, for the tooltip. */
  peakMs: number;
}

export function sparkline(points: number[] | undefined, width = 90, height = 20): Sparkline {
  const empty: Sparkline = { path: "", failures: [], width, height, peakMs: 0 };
  if (!points || points.length === 0) return empty;

  const step = points.length > 1 ? width / (points.length - 1) : 0;
  const peak = Math.max(...points.filter((value) => value > 0), 0);
  // A flat line at the top would imply the site is at its worst; a mid-height
  // line reads as "steady", which is what an unvarying response time means.
  const scale = peak > 0 ? (height - 2) / peak : 0;

  const segments: string[] = [];
  const failures: number[] = [];
  let drawing = false;
  points.forEach((value, index) => {
    const x = round1(points.length > 1 ? index * step : width / 2);
    if (value <= 0) {
      failures.push(x);
      drawing = false;
      return;
    }
    const y = round1(height - 1 - value * scale);
    segments.push(`${drawing ? "L" : "M"}${x} ${y}`);
    drawing = true;
  });

  return { path: segments.join(" "), failures, width, height, peakMs: peak };
}

function round1(value: number): number {
  return Math.round(value * 10) / 10;
}

/** The editable half of one site's form. */
export interface SiteForm {
  label: string;
  url: string;
  enabled: boolean;
  intervalMinutes: number;
  method: "HEAD" | "GET";
  expectStatus: string;
  mustContain: string;
  mustNotContain: string;
  tlsWarnDays: number;
  maxResponseMs: string;
  projectId: string;
  notify: boolean;
  extraUrls: Array<{ label: string; url: string }>;
  headers: Array<{ name: string; value: string }>;
}

/** A blank form: the defaults a pasted URL is watched with. */
export function emptySiteForm(): SiteForm {
  return {
    label: "",
    url: "",
    enabled: true,
    intervalMinutes: SITE_DEFAULTS.intervalMinutes,
    method: "HEAD",
    expectStatus: "",
    mustContain: "",
    mustNotContain: "",
    tlsWarnDays: SITE_DEFAULTS.tlsWarnDays,
    maxResponseMs: "",
    projectId: "",
    notify: true,
    extraUrls: [],
    headers: [],
  };
}

/** The form for an existing row. */
export function siteToForm(site: WatchedSiteView): SiteForm {
  return {
    label: site.label ?? "",
    url: site.url,
    enabled: site.enabled,
    intervalMinutes: site.intervalMinutes,
    method: site.method === "GET" ? "GET" : "HEAD",
    expectStatus: site.checks?.status?.expect ? String(site.checks.status.expect) : "",
    mustContain: site.checks?.keyword?.mustContain ?? "",
    mustNotContain: site.checks?.keyword?.mustNotContain ?? "",
    tlsWarnDays: site.checks?.tls?.warnDays ?? 0,
    maxResponseMs: site.checks?.maxResponseMs ? String(site.checks.maxResponseMs) : "",
    projectId: site.projectId ?? "",
    notify: site.notify,
    extraUrls: (site.extraUrls ?? []).map((extra) => ({
      label: extra.label ?? "",
      url: extra.url,
    })),
    headers: Object.entries(site.headers ?? {}).map(([name, value]) => ({ name, value })),
  };
}

/** The request body for a form. Extra URLs inherit the site's own rules,
 *  which is what makes "also watch /checkout" a one-field decision. */
export function formToInput(form: SiteForm): WatchedSiteInput {
  const checks = formChecks(form);
  const headers: Record<string, string> = {};
  for (const header of form.headers) {
    const name = header.name.trim();
    const value = header.value.trim();
    if (name && value) headers[name] = value;
  }
  return {
    label: form.label.trim(),
    url: form.url.trim(),
    enabled: form.enabled,
    intervalMinutes: form.intervalMinutes,
    checks,
    extraUrls: form.extraUrls
      .filter((extra) => extra.url.trim() !== "")
      .map((extra) => ({ label: extra.label.trim(), url: extra.url.trim(), checks })),
    projectId: form.projectId.trim(),
    notify: form.notify,
    headers: Object.keys(headers).length > 0 ? headers : undefined,
    method: form.method,
  };
}

function formChecks(form: SiteForm): SiteChecks {
  const checks: SiteChecks = {
    status: {},
    tls: { warnDays: form.tlsWarnDays },
  };
  const expect = Number.parseInt(form.expectStatus, 10);
  if (Number.isInteger(expect) && expect > 0) checks.status.expect = expect;
  const budget = Number.parseInt(form.maxResponseMs, 10);
  if (Number.isInteger(budget) && budget > 0) checks.maxResponseMs = budget;
  const mustContain = form.mustContain.trim();
  const mustNotContain = form.mustNotContain.trim();
  if (mustContain || mustNotContain) {
    checks.keyword = {
      ...(mustContain ? { mustContain } : {}),
      ...(mustNotContain ? { mustNotContain } : {}),
    };
  }
  return checks;
}

export interface IntervalBounds {
  min: number;
  max: number;
}

/**
 * Validates a form the way the backend does, so the panel refuses what the
 * API would refuse and the operator learns it without a round trip.
 * Returning the first problem keeps the single error line honest about what
 * to fix next.
 */
export function validateSiteForm(
  form: SiteForm,
  bounds: IntervalBounds = SITE_INTERVAL_BOUNDS,
  maxExtraUrls: number = SITE_LIMITS.maxExtraUrls,
): string | undefined {
  if (!normalizeSiteUrl(form.url)) {
    return "Enter the site's address, for example shop.example.com.";
  }
  if (
    !Number.isInteger(form.intervalMinutes) ||
    form.intervalMinutes < bounds.min ||
    form.intervalMinutes > bounds.max
  ) {
    return `The check interval must be between ${bounds.min} and ${bounds.max} minutes.`;
  }
  if (form.expectStatus.trim()) {
    const expect = Number.parseInt(form.expectStatus, 10);
    if (!Number.isInteger(expect) || expect < 100 || expect > 599) {
      return "The expected status must be an HTTP code between 100 and 599.";
    }
  }
  if (form.maxResponseMs.trim()) {
    const budget = Number.parseInt(form.maxResponseMs, 10);
    if (!Number.isInteger(budget) || budget <= 0) {
      return "The response-time budget must be a positive number of milliseconds.";
    }
  }
  const extras = form.extraUrls.filter((extra) => extra.url.trim() !== "");
  if (extras.length > maxExtraUrls) {
    return `A site can carry at most ${maxExtraUrls} extra URLs.`;
  }
  for (const extra of extras) {
    if (!normalizeSiteUrl(extra.url)) {
      return `"${extra.url}" is not a usable address.`;
    }
  }
  for (const header of form.headers) {
    if (header.name.trim() && !/^[A-Za-z0-9_-]+$/.test(header.name.trim())) {
      return `"${header.name}" is not a valid header name.`;
    }
  }
  return undefined;
}

/**
 * Mirrors the backend's URL rule: a bare domain gets https, the scheme must
 * be http(s), and the hostname must contain a dot — a hostname without one is
 * either localhost or a typo, and neither is a client website.
 */
export function normalizeSiteUrl(raw: string): string | undefined {
  const trimmed = raw.trim();
  if (!trimmed) return undefined;
  const candidate = trimmed.includes("://") ? trimmed : `https://${trimmed}`;
  let parsed: URL;
  try {
    parsed = new URL(candidate);
  } catch {
    return undefined;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return undefined;
  if (!parsed.hostname.includes(".")) return undefined;
  if (!parsed.pathname) parsed.pathname = "/";
  parsed.hash = "";
  return parsed.toString();
}

/**
 * The URLs a pasted block would import. It mirrors `ParseURLList` in the
 * backend so the panel can say "this will add 12 sites" before it does.
 */
export function parsePastedUrls(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const rawLine of text.split("\n")) {
    const hash = rawLine.indexOf("#");
    const line = hash >= 0 ? rawLine.slice(0, hash) : rawLine;
    for (const field of line.split(/[,;\s]+/)) {
      const normalized = normalizeSiteUrl(field);
      if (!normalized || seen.has(normalized)) continue;
      seen.add(normalized);
      out.push(normalized);
    }
  }
  return out;
}

/** The one-line summary above the table: how many sites, and how many are
 *  unwell. A green fleet says so rather than printing four zeroes. */
export function summarizeSites(sites: WatchedSiteView[]): string {
  if (sites.length === 0) return "No sites are being watched yet.";
  let down = 0;
  let slow = 0;
  let paused = 0;
  for (const site of sites) {
    if (!site.enabled) {
      paused++;
      continue;
    }
    if (site.status === "down") down++;
    else if (site.status === "slow") slow++;
  }
  const parts: string[] = [`${sites.length} site${sites.length === 1 ? "" : "s"}`];
  if (down > 0) parts.push(`${down} down`);
  if (slow > 0) parts.push(`${slow} slow`);
  if (paused > 0) parts.push(`${paused} paused`);
  if (down === 0 && slow === 0) parts.push("all up");
  return parts.join(" · ");
}
