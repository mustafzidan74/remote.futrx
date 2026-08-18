# Client portal

A share link opens one running application. Sometimes what a client actually
wants is smaller and calmer: *is it running, what changed this week, where do I
look at it?* The **client portal** is that page — one public, read-only status
page per project, on the platform's own hostname, gated by a token and by
nothing else.

The portal renders nothing the platform did not compose itself. It has no
proxy, no workspace access, no chat, no secrets, and no session. A visitor
holding the link sees a summary; a visitor without it sees a plain 404.

## What the page shows

| Section | Source | Controlled by |
| --- | --- | --- |
| Brand title (or project name) and status pill | project metadata (`running` / `stopped` / …) | always shown |
| **Message from your developer** | the note you type in project settings, dated with `noteUpdatedAt` | shown when non-empty |
| **Latest preview** links | ports that currently hold a **live public share link** | *Show preview links* |
| **Activity** | agent runs in the last 7 days — a count, never a cost | *Show activity* (off by default) |
| **Recent changes** | last 15 commits of `/workspace`, grouped by day | *Show recent changes* |
| "Last updated" footer | render time, in UTC | always shown |

Every section degrades on its own. A stopped container, a workspace with no git
repository, or an unreadable usage ledger leaves that section with a short,
friendly sentence instead of failing the page.

### The preview rule

The portal links a preview port **only when that port already has a live public
share link** ([Projects and containers](03-projects-and-containers.md) →
Sharing). That is deliberate and it is the whole rule:

- `<slug>--<port>.dev.<host>` is gated at the Caddy edge. Without a share, the
  only way in is a platform sign-in — which a client does not have.
- So a port with no live share would be an invitation to a login screen. The
  portal never renders one.
- A port **with** a live share is publicly reachable, so listing it tells the
  client where the work is, and the share link you sent them is what opens it.

The IDE host, the agent browser (`:6080`), the DevTools port, and the
application itself are never linked from the portal under any configuration.

### The note

The note is rendered under the heading **"Message from your developer"**, with
the date it was last written under it. That date is `noteUpdatedAt`, and it
moves only when the note text changes — flipping a display toggle must not make
a month-old message look like it was written today — and it is cleared when the
note is emptied. A record written before the field existed prints no date rather
than inventing one.

You can write the note by hand here, or compose it from one of your bilingual
client templates in **Sharing → Message client**
([21-snippets-and-client-messages.md](21-snippets-and-client-messages.md)).

The note is **markdown-lite**, which here means exactly two things: HTML is
escaped, and line breaks are preserved. There is no bold, no links, no lists —
a client-facing page rendered from operator text is not a place to add a markup
parser. The page is direction-aware: if your brand title or note is written in
Arabic or Hebrew, the whole document renders `dir="rtl"`.

## Enabling a portal

**Project settings → Sharing → Client portal.** Pick what to show, optionally
set a brand title and a note, then **Enable client portal**.

The link appears **exactly once**:

```
https://remote.example.com/portal/<projectId>?t=<token>
```

Copy it then — the server stores only a SHA-256 digest of the token, so it can
never show it again. If you lose it, **Rotate link** mints a new one (and stops
the old one working immediately). **Disable** closes the portal and drops the
digest, so re-enabling later always produces a fresh link rather than reviving
the old one.

Any project **member** can enable, rotate, or disable a portal; it is not an
admin-only action, matching how project secrets and share links already work.

## The token

- 32 random bytes, base64url — the same entropy as a preview share link.
- Only its SHA-256 digest is persisted, so a copy of `DATA_DIR` cannot be
  replayed against the public route.
- Compared in constant time.
- Rate limited: **20 checks per minute per client address**, after which the
  route answers `429` with `Retry-After: 60`. The client IP comes from the
  left-most `X-Forwarded-For` entry, which is trustworthy here because only
  Caddy on loopback can reach the backend.
- A missing project, a disabled portal, and a wrong token all answer the
  **same** `404` with the same body, so the route cannot be used to discover
  which project ids exist.

The link is a bearer credential sitting in a URL, so the page is served with
`Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
`X-Robots-Tag: noindex, nofollow`, and a `robots` meta tag. Treat the link
itself like a password: anyone who receives it can open the page.

## Storage

One JSON document per project at `DATA_DIR/portals/<projectId>.json`, mode
`0600`, written by temp-file + rename:

```json
{
  "enabled": true,
  "tokenHash": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "createdAt": 1755500000000,
  "updatedAt": 1755600000000,
  "showPreview": true,
  "showChangelog": true,
  "showUsage": false,
  "brandTitle": "Acme Shop",
  "note": "Checkout is live on staging.\nInvoices land next week.",
  "noteUpdatedAt": 1755600000000
}
```

`showUsage` defaults to `false`: a client portal is not the place to publish
what your agent runs cost, which is why even with the toggle on the page shows
a run count and no money.

## API

| Method | Path | Auth | Body | Response |
| --- | --- | --- | --- | --- |
| `GET` | `/api/projects/{id}/portal` | member or admin | — | Settings, never the token |
| `PUT` | `/api/projects/{id}/portal` | member or admin | Update payload | Settings, plus `url` on enable/rotate |
| `GET` | `/portal/{projectId}?t=<token>` | **none** — token only | — | `text/html` page |

```json
{
  "enabled": true,
  "rotate": false,
  "showPreview": true,
  "showChangelog": true,
  "showUsage": false,
  "brandTitle": "Acme Shop",
  "note": "Checkout is live on staging."
}
```

`rotate: true` on an enabled portal mints a new token. `enabled: false` closes
the portal and drops the stored digest; `rotate` is ignored in that case. The
`url` field is present **only** in the response that minted a token.

The public route is the one page in the application that the session middleware
does not gate — that middleware covers `/api/*` and `/ws*` only, so `/portal/`
reaches its own token check. It is served by the main application host, not by
a preview host, so no request ever enters a container.

## Audit

Three actions reach the [audit log](../04-operations/10-audit-log.md), with the
project as the target:

| Action | Recorded when |
| --- | --- |
| `portal.enable` | a closed portal is opened (a token is minted) |
| `portal.rotate` | an open portal's token is replaced |
| `portal.disable` | a portal is closed |

Editing the note or the toggles on an already-open portal is not a lifecycle
transition and is not recorded. Public page views are **not** audited: the
visitor has no identity to record.

## Security notes

- The portal is **read-only by construction**. It has no write route, and its
  service takes only readers: the project directory, the share list, the git
  log, and the usage ledger.
- Commit **author emails are stripped** before rendering. The client sees a
  subject, a short SHA, an author name, and a time.
- The note, the brand title, and every commit subject are rendered through
  `html/template`, so agent-authored or operator-authored text cannot inject
  markup. The page loads **no external asset** — all CSS is inline, there is no
  JavaScript, and nothing is fetched from another host.
- The page is composed on the server from data the platform already holds. It
  is not a proxy, so enabling a portal does not widen what a container can be
  reached for.

## Related

- [Projects and containers](03-projects-and-containers.md) — public preview
  share links, which the preview section depends on
- [Workspace tools](05-workspace-tools.md) — the git history service behind the
  changelog
- [Usage and cost](10-usage-and-cost.md) — the ledger behind the activity line
- [Audit log](../04-operations/10-audit-log.md) — where the lifecycle actions land
