/**
 * Client site monitoring, as `GET /api/sitewatch/sites` returns it.
 *
 * These are the operator's *clients'* websites — the shops and landing pages
 * the projects on this box were built for — not the platform itself, which
 * Settings → Monitoring covers. The watcher spends no agent tokens: one HEAD
 * request per site per interval from the platform host.
 *
 * Everything here is already scoped to the signed-in user. A member sees only
 * sites linked to a project they belong to; unlinked sites are admin-only.
 */

/** A site's traffic light. `unknown` means it has not been checked yet. */
export type SiteStatus = "unknown" | "up" | "slow" | "down";

/** The HTTP verb a check uses. HEAD is the cheap default. */
export type SiteMethod = "HEAD" | "GET";

export interface SiteStatusCheck {
  /** Exact response code to require, or 0/absent for "any 2xx or 3xx". */
  expect?: number;
}

/** The only thing the watcher knows about a page's contents: two substrings. */
export interface SiteKeywordCheck {
  mustContain?: string;
  mustNotContain?: string;
}

export interface SiteTlsCheck {
  /** Days before expiry that the amber alert fires. 0 disables the check. */
  warnDays: number;
}

export interface SiteChecks {
  status: SiteStatusCheck;
  keyword?: SiteKeywordCheck;
  tls: SiteTlsCheck;
  /** Response-time budget in ms. Absent means a site is never called slow. */
  maxResponseMs?: number;
}

/** A secondary page watched under the same site — checkout, login. */
export interface SiteEndpoint {
  label?: string;
  url: string;
  checks: SiteChecks;
}

export interface WatchedSite {
  id: string;
  label: string;
  url: string;
  enabled: boolean;
  intervalMinutes: number;
  checks: SiteChecks;
  extraUrls?: SiteEndpoint[];
  /** Links the site to a project — and is also the visibility rule. */
  projectId?: string;
  /** Whether state changes are delivered to the notification sinks. */
  notify: boolean;
  headers?: Record<string, string>;
  method: SiteMethod;
  createdAt?: number;
  updatedAt?: number;
}

/** Availability over the three windows. A window with no checks is absent —
 *  never read that as 0%. */
export interface SiteUptime {
  day?: number;
  week?: number;
  month?: number;
  checks: number;
  since?: number;
}

/** One row of the Client sites table: the site plus what the watcher knows. */
export interface WatchedSiteView extends WatchedSite {
  status: SiteStatus;
  /** When the current state began — what "down for 20 min" is measured from. */
  changedAt?: number;
  lastCheckedAt?: number;
  lastDurationMs?: number;
  lastCode?: number;
  lastError?: string;
  lastSizeBytes?: number;
  nextCheckAt?: number;
  tlsExpiresAt?: number;
  tlsDaysLeft?: number;
  uptime: SiteUptime;
  /** Newest response times, oldest first, for the row's sparkline. Failed
   *  checks are zero. */
  spark?: number[];
}

/** One URL's outcome inside a synchronous "Check now". */
export interface SiteEndpointResult {
  label?: string;
  url: string;
  method: string;
  status: SiteStatus;
  code?: number;
  durationMs: number;
  sizeBytes?: number;
  tlsExpiresAt?: number;
  tlsDaysLeft?: number;
  reasons?: string[];
  error?: string;
}

export interface SiteCheckReport {
  site: WatchedSiteView;
  endpoints: SiteEndpointResult[];
  checkedAt: number;
}

/** One recorded check, as the history endpoint returns it. */
export interface SiteCheckRecord {
  at: number;
  st: SiteStatus;
  code?: number;
  ms: number;
  size?: number;
  tls?: number;
  err?: string;
}

/** The collection response: the rows plus the limits the server owns. */
export interface WatchedSiteCollection {
  sites: WatchedSiteView[];
  maxSites: number;
  minIntervalMinutes: number;
  maxIntervalMinutes: number;
  maxExtraUrls: number;
}

/** Create/update body. The server owns the id and the timestamps. */
export interface WatchedSiteInput {
  label: string;
  url: string;
  enabled: boolean;
  intervalMinutes: number;
  checks: SiteChecks;
  extraUrls?: SiteEndpoint[];
  projectId?: string;
  notify: boolean;
  headers?: Record<string, string>;
  method: SiteMethod;
}

export interface SiteImportInput {
  /** Pasted text, one URL per line. Blank lines and `#` comments ignored. */
  urls: string;
  /** Also pull domains out of the projects' own HESTIA_DOMAIN-style secrets. */
  fromProjects: boolean;
  projectId?: string;
  notify: boolean;
}

export interface SiteImportSkipped {
  url: string;
  reason: string;
}

export interface SiteImportResult {
  created: WatchedSiteView[];
  skipped?: SiteImportSkipped[];
}

/** A domain the project catalog suggests watching. */
export interface SiteCandidate {
  projectId: string;
  projectName: string;
  url: string;
  secretKey: string;
}
