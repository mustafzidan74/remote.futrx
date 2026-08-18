# Uptime monitoring

Every other alert this platform sends is produced *by* the platform. That is
fine until the platform is the thing that broke: a kernel panic, an OOM kill of
the backend, a full disk, a Caddy that stopped renewing, a host that never came
back from a reboot. None of those can page you, because the thing that would
have sent the page is gone.

So the box needs a witness outside it. Remote gives you two, and they cover
different failures:

| Mechanism | Direction | Catches |
| --- | --- | --- |
| **`GET /healthz`** | An external monitor polls in | The host is unreachable, TLS expired, the backend is wedged, LXD stopped answering, `DATA_DIR` went read-only |
| **Heartbeat push** | This server calls out | Everything above **plus** outbound network death and any failure that leaves the port open but the process useless — and it works when the box is behind NAT with no inbound path |

Use both. They are configured in **Settings → Monitoring**, and both are free
to run on the services below.

A third signal is neither: the **"Remote started"** notification. It fires
through the normal [notification](../02-workspaces/07-notifications.md) sinks
when the backend process comes up, so a crash-restart is visible on Telegram or
WhatsApp even though the box is answering again by the time you look.

## `GET /healthz`

```
GET https://<your-host>/healthz
```

**Public. No session, no cookie, no token.** It is the one application route
besides `/portal/*` that the auth middleware does not gate — the middleware
covers `/api/*` and `/ws*` only — and the Caddy site block for the main host
proxies it straight through.

### Response

```json
{
  "status": "ok",
  "version": "v1.4.0",
  "checks": {
    "backend": "ok",
    "lxd": "ok",
    "caddy": "ok"
  }
}
```

| Field | Meaning |
| --- | --- |
| `status` | `ok` or `degraded`. The roll-up, and the only thing that decides the HTTP status code |
| `version` | The running build (`git describe`), or `dev` for an unstamped binary |
| `checks.backend` | The file-backed store under `DATA_DIR`: it exists, is a directory, and accepts a write |
| `checks.lxd` | `lxc info` answered within 2 seconds |
| `checks.caddy` | The public HTTPS edge is accepting connections |

Each check is `ok`, `degraded`, or `skipped`. `skipped` means this deployment
has no such component wired at all — a development box without LXD, for
instance — and is never counted as a failure.

| HTTP status | When |
| --- | --- |
| `200` | `status` is `ok` |
| `503` | The data store or LXD is unusable. The body carries a `details` array naming which |
| `405` | Any method other than `GET` or `HEAD` |
| `429` | More than 60 requests in a minute from one client IP |

`HEAD` is supported and returns the same status code with no body, which is
what most monitors send by default.

### Why a degraded edge is not a 503

Only the store and LXD move `status`. A failing `caddy` check is reported in
`checks` and `details` but does not fail the response, because a request that
*arrived through* the edge is poor evidence that the edge is down. In fact,
when Caddy proxies the request its forwarding headers prove the edge is alive,
and the check short-circuits to `ok` without dialing anything. The TCP probe of
`127.0.0.1:443` only runs for requests that reached the backend some other way
— which, since the backend binds loopback, means someone on the host with
`curl`.

### Cost, and why it is safe to expose

An unauthenticated endpoint that shells out to `lxc` on demand would be a gift
to anyone with a loop. Three things stop that:

- **The LXD probe is cached for 60 seconds** and bounded at 2 seconds, so a
  flood of requests costs at most one `lxc info` per minute.
- **The edge probe is cached the same way**, and usually does not run at all
  (see above).
- **Requests are rate limited to 60 per minute per client IP**, in tumbling
  windows, keyed off the left-most `X-Forwarded-For` entry — trustworthy here
  because only Caddy on loopback can reach the backend. A refused request never
  reaches the probes at all.

The data-store check is not cached: it is a `stat` plus a temp file on a local
filesystem, and it is the one answer nobody wants a minute stale.

### What it reveals

**The application version, and nothing else.** No hostname, no paths, no
project names, no counts, and no probe error text — the `details` strings come
from a fixed vocabulary precisely so a failing probe cannot leak a mount point
or a socket path into a public response.

The version is a real disclosure: it tells an attacker which release you are
on, and therefore which published fixes you may be missing. That is the price
of an endpoint an external monitor can read, and it is the same price every
`Server:` header charges. If you are not willing to pay it, do not expose
`/healthz` — use only the heartbeat push, which requires nothing inbound.

## Heartbeat push

**Settings → Monitoring → Heartbeat push.**

A ticker calls a URL you provide, on the interval you choose, **only while the
platform is healthy**. If `/healthz` would answer `degraded`, the push is
skipped and logged — staying silent is precisely what makes the external
service raise the alarm.

| Setting | Meaning |
| --- | --- |
| Heartbeat URL | The dead man switch URL. Absolute `http(s)` |
| Interval | 1–60 minutes, default 5 |

Operational details:

- The request is a plain `GET` with a **10 second timeout**.
- A failure is logged once and **retried no sooner than the next interval** —
  attempts count whether or not they succeeded, so a wrong URL cannot spin.
- **Ping now** sends one immediately regardless of health, so you can test the
  URL. It records its outcome like any other attempt.
- The panel shows the last attempt, its outcome, and when the next one is due.

### Storage and masking

The URL lives at `DATA_DIR/monitoring.json`, mode `0600`, written temp-file
plus rename like every other settings document:

```json
{
  "enabled": true,
  "heartbeatUrl": "https://hc-ping.com/9f3a1c72-5b6d-4e21-9f0c-2b7ad4e51234",
  "intervalMinutes": 5,
  "lastPingAt": 1755512345678,
  "lastPingStatus": "ok"
}
```

**Treat the URL as a credential.** Anyone holding it can tell your monitoring
service this box is alive, which is exactly the lie you built the heartbeat to
prevent. Accordingly:

- `GET /api/admin/monitoring` never returns it. It returns the host plus the
  last four characters of the token — `hc-ping.com/••••1234` — enough to tell
  two healthchecks.io URLs apart, never enough to ping either.
- The write follows the same write-only rule as every other stored secret: an
  empty `heartbeatUrl` keeps what is stored; `clearHeartbeatUrl: true` removes
  it and switches the heartbeat off.
- Replacing the URL clears the last-ping record, so a stale "delivered" never
  vouches for a URL nothing has tried.
- Transport errors are rewritten before they reach the log or the panel, so the
  URL cannot leak through a `dial tcp` message.

## Free setups

### UptimeRobot — HTTP monitor on `/healthz`

The polling half. The free plan allows 50 monitors at 5-minute intervals.

1. **Dashboard → New monitor**.
2. Monitor type: **HTTP(s)**.
3. URL: `https://<your-host>/healthz`.
4. Monitoring interval: 5 minutes.
5. Open **Advanced settings** and set:
   - Keyword type: **exists**
   - Keyword value: `"status":"ok"`
6. Add your alert contacts and save.

The keyword check is the point. A status-code-only monitor is nearly enough —
`/healthz` does answer 503 when the store or LXD is unusable — but the keyword
also catches the case where something in front of the backend returns a cheerful
200 that is not this endpoint at all: a captive portal, a parked-domain page, a
misrouted proxy. Match the exact substring `"status":"ok"`; the response has no
spaces after its colons.

If your plan does not offer keyword checks, the status code alone is still
worth having — `/healthz` answers 503 on the failures that matter.

### healthchecks.io — heartbeat URL

The push half, and the one that catches a box which cannot reach the internet.
The free plan allows 20 checks.

1. Sign in and press **Add check**.
2. Name it (for example `remote.futrx production`).
3. Set **Period** to the interval you plan to use — start with 5 minutes.
4. Set **Grace Time** to at least twice the period (10 minutes for a 5-minute
   period) so a single slow push does not page you.
5. Copy the ping URL — `https://hc-ping.com/<uuid>`.
6. In Remote: **Settings → Monitoring → Heartbeat push**, paste the URL, set
   the interval to 5 minutes, tick **Push a heartbeat**, and **Save**.
7. Press **Ping now**. The check on healthchecks.io should go green
   immediately.

healthchecks.io then alerts you when the pings stop — which happens both when
the box dies and when the platform degrades itself into silence.

### Better Stack — either half, or both

Better Stack's free plan covers 10 monitors and includes heartbeats.

**As an HTTP monitor:** create a monitor of type *HTTP(S)*, URL
`https://<your-host>/healthz`, check frequency 3 minutes, and under request
settings add the expectation that the response body **contains**
`"status":"ok"`.

**As a heartbeat:** go to **Monitors → Heartbeats → Create heartbeat**, set the
expected period to 5 minutes and the grace period to 10, copy the URL
(`https://uptime.betterstack.com/api/v1/heartbeat/<token>`), and paste it into
Remote's heartbeat setting exactly as above.

### UptimeRobot heartbeats

UptimeRobot also offers heartbeat monitors on the free plan
(`https://heartbeat.uptimerobot.com/<token>`). Create one, paste its URL into
the same field, and set its expected interval to match. Any dead man switch
service works — the setting is a URL and a timer, nothing more.

## Restart notifications

When the backend process starts it publishes a `system` notification:

```
Remote system event
Remote started (version v1.4.0, uptime reset).
```

It is delivered through the sinks configured in **Settings → Notifications**
and gated by the **System events** toggle there, which is **on by default**.

This closes a real gap. A backend that crashes and is restarted by systemd is
invisible to both mechanisms above if the gap is shorter than the monitoring
interval — the box was down, nobody polled during the window, and everything
looks fine afterwards. The restart notification is the only signal that the
process died at all, and the version it names tells you whether the restart
was a deploy or a crash.

Expect one on every deploy and every host reboot. If they arrive when you did
not deploy anything, something is killing the process — check
`journalctl -u remote.futrx` and the host's OOM killer.

## Admin API

All three routes are admin-only except the first.

| Route | Method | Purpose |
| --- | --- | --- |
| `/healthz` | `GET`, `HEAD` | Public health report (above) |
| `/api/admin/monitoring` | `GET` | Settings, heartbeat URL masked |
| `/api/admin/monitoring` | `PUT` | Update settings |
| `/api/admin/monitoring/ping` | `POST` | Send one heartbeat now |

```bash
# The public endpoint, from anywhere.
curl -sS https://remote.example.com/healthz

# Settings, with an admin session cookie.
curl -sS -b cookies.txt https://remote.example.com/api/admin/monitoring

# Arm the heartbeat.
curl -sS -b cookies.txt -X PUT \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"heartbeatUrl":"https://hc-ping.com/<uuid>","intervalMinutes":5}' \
  https://remote.example.com/api/admin/monitoring

# Send one now.
curl -sS -b cookies.txt -X POST https://remote.example.com/api/admin/monitoring/ping
```

A `PUT` that fails validation answers `400` with the reason. `POST .../ping`
answers `200` even when the push was rejected: the body carries
`{"delivered":false,"status":"failed","error":"the heartbeat URL responded 404"}`,
because "your monitoring service said no" is an answer you want to read, not a
server error.

## Choosing thresholds

| Interval | Grace period | Detects a dead box in |
| --- | --- | --- |
| 1 min | 3 min | Under 5 minutes |
| 5 min (default) | 10 min | Under 15 minutes |
| 15 min | 30 min | Under 45 minutes |

Set the grace period to **at least twice the interval**. One missed push is
normal — a restart during a deploy, a slow DNS lookup, a monitoring provider's
own hiccup — and paging on it trains you to ignore the page.

## Related

- [Notifications](../02-workspaces/07-notifications.md) — the sinks the restart
  event is delivered through, and the rest of the event toggles
- [Deployment and operations](09-deployment-and-operations.md) — the host,
  proxy, and service layout these checks describe
- [Audit log](10-audit-log.md) — what happened, as opposed to whether the box
  is up
