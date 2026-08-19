# Client site monitoring

[Uptime monitoring](11-uptime-monitoring.md) is about *this* box. This page is
about everybody else's: the shops, landing pages, and dashboards the projects
on this server were built for. They run on hosts you do not control, they go
down at 3am, and the people who notice first are your client's customers.

**Settings → Insights → Client sites** is an always-on watcher for them. It is
deliberately the cheapest thing that can honestly say a site is down:

- **One `HEAD` request per site per interval.** No browser, no screenshot, no
  DOM. A `GET` happens only when the origin refuses `HEAD` or when you asked
  for a keyword check, which needs a body to look in.
- **No agent runs, ever.** This spends **zero tokens**. It is a timer, an
  HTTP client, and a state machine — nothing on this page ever starts a
  container or a model.
- **No parsing beyond a substring test.** The keyword check is
  `strings.Contains` over the first 256 KiB of the response, case-folded.
  That is the whole content analysis.

It is separate from the weekly agent-driven audit and from the [project
health monitor](../02-workspaces/07-notifications.md#project-health), which
watches *containers on this host* rather than websites anywhere.

## Where it runs, and what it costs you

Every check leaves from **the platform host's own IP, on the platform host's
own bandwidth.** That has three consequences worth stating plainly:

1. **It is real outbound traffic.** A `HEAD` round trip is a few kilobytes.
   At the 200-site cap on the default five-minute interval that is roughly
   one request every 1.5 seconds, all day. At the one-minute floor it is
   about three per second. Keep the default unless a client's contract needs
   faster.
2. **Your server's IP is what the client's WAF sees.** Requests carry a
   browser-shaped `User-Agent` (Go's default is blocked outright by a large
   share of managed WAFs — Cloudflare, Sucuri, Wordfence — which would
   otherwise report healthy sites as down). A client who rate-limits by IP may
   still need to allowlist this box.
3. **A dead platform means silent sites.** If this server is down, nothing is
   watching. That is exactly what [`/healthz` and the
   heartbeat](11-uptime-monitoring.md) are for; the two features cover each
   other.

## What a site is

| Field | Meaning |
| --- | --- |
| `label` | What alerts call it. Blank falls back to the hostname |
| `url` | The page checked first. A bare domain gets `https://`; a hostname with no dot is refused (it is either `localhost` or a typo) |
| `enabled` | Off pauses the checks without losing the history |
| `intervalMinutes` | 1–60, default **5** |
| `checks.status.expect` | An exact HTTP code to require. Blank accepts any **2xx or 3xx** — redirects are followed by the client, so a 3xx here means a redirect it chose not to follow |
| `checks.keyword.mustContain` | Case-insensitive substring that must be present |
| `checks.keyword.mustNotContain` | Substring that must be absent — `Error establishing a database connection` is the classic |
| `checks.tls.warnDays` | Days before certificate expiry to raise the amber alert. Default **21**, `0` disables it |
| `checks.maxResponseMs` | Response-time budget. Over it twice in a row is **slow**, never **down** |
| `extraUrls` | Up to 5 more pages under the same site — the checkout page, the login page — each a full extra request per interval |
| `projectId` | Links the site to a project. **This is also the visibility rule** (see below) |
| `notify` | Whether state changes reach the notification sinks. The table still goes red either way |
| `headers` | Up to 10 extra request headers, for a staging site behind a shared token |
| `method` | `HEAD` (default) or `GET` |

A site's status is the **worst** of its own page and every extra URL: a shop
whose `/checkout` returns 500 is down, because that is the only purpose it
has. The reasons carry the page label, so the row explains itself without a
second request.

### Statuses

| Status | Meaning |
| --- | --- |
| `up` | Every configured check passed |
| `slow` | Answered correctly, but over `maxResponseMs` |
| `down` | The request failed, or a response failed a check, or the certificate has expired |
| `unknown` | Never checked yet |

An **expired** certificate is `down`, not a warning: every browser refuses the
page, so the site is down for its visitors.

## Two consecutive checks, in both directions

A single failed request is a dropped packet, a WAF hiccup, or an origin
restart. It is not an outage, and it must not wake anybody.

So the watcher publishes a state change only after **two consecutive checks
agree**, and it alerts only on a published change:

```
up   up   down  down  down  down  up   up   up
               ▲                        ▲
               │                        └── 🟢 recovered (2nd good check)
               └── 🔴 down (2nd bad check)
```

- A brand-new site that fails its first check stays `unknown` until it fails
  twice. A brand-new site that *passes* goes green immediately — nothing
  recovered, so there is nothing to announce.
- A site that stays down is reported **once**, not once per interval.
- A site flapping between two *different* bad states never settles, and so
  never alerts.
- The debouncer is restored from the history file on restart, so a recovery
  after a redeploy still reports the outage it ended.

The state machine is `stateMachine` in
[`backend/internal/service/sitewatch/evaluate.go`](../../backend/internal/service/sitewatch/evaluate.go);
`consecutiveChecks` is the constant.

## Alerts

State changes are delivered through the existing [notification
sinks](../02-workspaces/07-notifications.md) — Telegram, WhatsApp, webhook —
under the event kind **`siteWatch`**, shown in the events list as **Client
sites**. Turn it off there to silence every site at once, or clear a single
site's `notify` flag to silence just that one.

| Kind | Message | Webhook `status` |
| --- | --- | --- |
| down | `🔴 shop.example.com — 502 in 1.2 s` | `crit` |
| recovered | `🟢 shop.example.com — back after 12 m` | `ok` |
| slow | `🟠 shop.example.com — 3.4 s, over the 2 s budget` | `warn` |
| certificate | `🟡 shop.example.com — certificate expires in 9 days` | `warn` |

**Never more than one alert per state change.** The certificate warning is its
own axis (a site can be perfectly up and three days from a browser-wide TLS
error) with its own latch: one message per crossing, cleared when the
certificate is renewed.

Alerts link to the site's project when it has one, because that is where the
person who can fix it works.

## Uptime and the sparkline

Every check — good or bad — is appended to
`DATA_DIR/sitewatch/history/<siteId>.jsonl` and the file is trimmed to the
newest **500** records. Uptime percentages are computed from exactly that
window:

- **24h / 7d / 30d** = checks that served the customer ÷ checks in the window.
  `slow` counts as served: the site worked, it just took its time.
- A window with **no checks reports nothing** (`—`), never `0%`. "We have not
  looked" and "it was down the whole time" must not render the same.
- At the default five-minute cadence, 500 records is a little under two days.
  The 30-day figure is therefore honest about covering only what it has —
  read `uptime.since` for the real extent.

The table's inline sparkline is the last 40 response times, drawn as SVG with
no charting library. Failed checks arrive as zero and are drawn as red gap
markers rather than as a suspiciously fast response.

## Who sees what

| Caller | Sees |
| --- | --- |
| Admin | Every site; can add, edit, delete, and import |
| Member | Only sites whose `projectId` is a project they belong to; read-only, plus **Check now** |
| Anyone | Nothing without a session — the routes are under `/api`, which the auth middleware gates |

**A site with no `projectId` is admin-only.** That is the default, so an
internal endpoint you paste in does not become visible to every member of
every project.

A site a member may not see answers **404**, not 403 — a member must not be
able to discover which ids exist by probing.

## Bulk import

Adding forty client sites one form at a time is not a feature. **Bulk import**
takes either half of:

- **A pasted list**, one address per line. Blank lines, anything after a `#`,
  and repeats are dropped; comma- and space-separated addresses on one line
  are split. Everything is normalized the way a single add is (bare domain →
  `https://`).
- **The projects' own domains**, read from each project's secrets. Any secret
  named `HESTIA_DOMAIN`, `SITE_DOMAIN`, `SITE_URL`, `PUBLIC_URL`, `APP_URL`,
  `WP_HOME`, `WP_SITEURL`, or `PRIMARY_DOMAIN` becomes a candidate, and the
  site it creates is **linked to the project it came from** — so that
  project's members can see it immediately.

Everything created gets the defaults (5 minutes, any 2xx/3xx, 21-day
certificate warning, `HEAD`). Anything already watched or unusable is reported
in the **skipped** list rather than silently dropped; duplicate detection is
host + path, ignoring the scheme and a trailing slash, so pasting a site twice
as `http` and `https` is caught.

Reading project secrets for the import goes straight to the store rather than
through the project service, so a background scan across every project does
not bury the [audit log](10-audit-log.md) under secret-read entries.

## Limits

| Limit | Value | Why |
| --- | --- | --- |
| Sites | **200** | The most outbound traffic a shared platform box should spend on somebody else's uptime |
| Interval | 1–60 minutes | Below a minute is a load test; above an hour is not monitoring |
| Extra URLs per site | 5 | Each is a full extra request per interval |
| Request timeout | 15 s | Past any reasonable page, short enough that a wedged origin cannot hold a scheduler slot |
| Body read | 256 KiB | Keywords live in the markup, not in the hero image |
| Redirects | 8 | Apex → www → https is three; more is a loop |
| History per site | 500 checks | Bounded without a background sweeper |
| Headers per site | 10 | |

The scheduler runs one goroutine that wakes every 15 seconds, checks at most
25 due sites per tick across 8 workers, and arms each site at a **jittered**
instant: at boot the whole fleet is spread across one interval, so a restarted
backend never fires two hundred requests in the same second.

## API

All routes require a session. Writes are admin-only.

| Method | Route | Who |
| --- | --- | --- |
| `GET` | `/api/sitewatch/sites` | Any signed-in user — filtered to what they may see. Echoes `maxSites`, `minIntervalMinutes`, `maxIntervalMinutes`, `maxExtraUrls` |
| `POST` | `/api/sitewatch/sites` | Admin — create |
| `GET` | `/api/sitewatch/sites/{id}` | Visible to the caller |
| `PUT` | `/api/sitewatch/sites/{id}` | Admin — replace the configuration, keeping the history |
| `DELETE` | `/api/sitewatch/sites/{id}` | Admin — removes the site and its history |
| `POST` | `/api/sitewatch/sites/{id}/check` | Visible to the caller — runs every check **synchronously** and returns the raw per-URL results |
| `GET` | `/api/sitewatch/sites/{id}/history` | Visible to the caller — the raw records, oldest first |
| `POST` | `/api/admin/sitewatch/import` | Admin — bulk import |
| `GET` | `/api/admin/sitewatch/candidates` | Admin — the project domains an import would offer, minus what is already watched |

**Check now counts.** It goes through the same path a timed check does, so it
is recorded in the history and can move the state machine — pressing it twice
on a broken site will raise the alert.

Error codes: `400` invalid site, `404` unknown *or invisible*, `409` the
200-site cap, `503` no sitewatch store on this deployment.

## Storage

| Path | Contents |
| --- | --- |
| `DATA_DIR/sitewatch/sites.json` | The catalog, mode 0600, replaced atomically |
| `DATA_DIR/sitewatch/history/<siteId>.jsonl` | One append-only check log per site, trimmed to 500 records, mode 0600 |

The catalog is 0600 because a site's custom headers can carry a shared token.
Nothing else here is secret.

The live table, the uptime percentages, and the sparklines are all served from
memory: history is read once at boot and kept as a bounded ring per site, so
listing 200 sites never touches the disk.

## Where it appears

- **Settings → Insights → Client sites** — the full table, the editor, bulk
  import, and **Check now**.
- **Home dashboard** — a compact card listing only what is *not* green, and an
  **Attention** row per unwell site (down is critical, slow is a warning) with
  an *Open client sites* button.

## Configuration

There is none. No environment variables, no infra steps, no new ports. The
feature appears as soon as `DATA_DIR` is writable and disappears (routes
answer `503`) if it is not.

## See also

- [Uptime monitoring](11-uptime-monitoring.md) — the mirror image: how the
  outside world learns *this* box died
- [Notifications](../02-workspaces/07-notifications.md) — the sinks the
  `siteWatch` event is delivered through
- [Project health](../02-workspaces/07-notifications.md#project-health) — the
  same traffic-light idea applied to containers on this host
