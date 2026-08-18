# Notifications

Agent runs are long. This page covers the feature that lets an operator walk
away from one and still learn, on their phone, when it finished, failed, or
stopped to ask a question — plus how a scheduled task reported back.

Notifications are a **global, admin-owned** setting: one configuration for the
whole server, delivered to at most three destinations (a Telegram chat, a
WhatsApp number, and one generic webhook). There is no per-user or per-project
routing yet.

## What triggers a notification

| Event | Fires when | `status` values |
| --- | --- | --- |
| `runFinished` | An interactive agent turn completed without error | `finished` |
| `runFailed` | An interactive agent turn errored or was cancelled | `failed`, `cancelled` |
| `needsAttention` | The agent called a tool that hands control back to a human | `waiting` |
| `scheduledRun` | A scheduled task run settled | `succeeded`, `failed` |
| `projectHealth` | A running project crossed a health threshold, or recovered | `warn`, `crit`, `ok` |
| `digest` | The weekly cost-and-usage schedule came due | `finished` |

Two details worth knowing:

- **A scheduled run never produces both.** Runs injected by the scheduler are
  skipped by the interactive `runFinished`/`runFailed` path so a nightly task
  pings once, not twice.
- **`needsAttention` is tool driven.** Agents launch with permission prompts
  disabled, so there is no interactive approval event to hook. What remains is
  the set of tools that exist precisely to ask the human: `AskUserQuestion`,
  `ExitPlanMode`, `RequestPermission`, and their snake- and kebab-case
  spellings (see `attentionTools` in
  [`backend/internal/service/notify/events.go`](../../backend/internal/service/notify/events.go)).
  Each such call is its own ping — an agent that asks twice notifies twice.

- **Project health is debounced, not sampled.** The monitor evaluates each
  running project once a minute, but a status must hold for **two consecutive
  sweeps** before it is published, so a container that touches 80% for one
  allocation spike never pings. See [Project health](#project-health).

Every notification carries a deep link back to the chat:
`https://<your-host>/?chat=<chatId>`. The SPA reads that parameter once on
boot, selects the chat, and strips it from the address bar.

## Project health

The health monitor is the source of the `projectHealth` event. Once a minute
(jittered) it sweeps every **running** project and reduces it to one traffic
light:

| Status | Raised when |
| --- | --- |
| `ok` | Every measured signal is inside its threshold |
| `warn` | Memory at or above **80%** of the container limit, or the project's app answered with a 5xx |
| `crit` | Memory at or above **92%**, the project is in an error state, the container has gone missing, or a port that is listening refuses the request |
| `unknown` | Nothing could be measured — usually LXD was briefly unreachable |

What one sweep costs: three `lxc query` calls for state, limits, and live
counters; one `lxc exec` for the listener scan; and one `HEAD
http://<slug>.lxd:<port>/` with a 3 second timeout against the **lowest
non-platform** listening port. The agent browser (`6080`), code-server (`8842`,
`8081`), and the DevTools endpoint (`9222`) are never probed: they answer
whether or not the user's application is up.

A message reads like this, and links to the project rather than to a chat:

```
Project health critical
wp-project memory 94% (1.41/1.5 GiB) — agent browser + code-server running
https://remote.example.com/?project=1a2b3c4d
```

Two behaviours are worth knowing:

- **One message per settled transition.** Every step into `warn` or `crit`
  sends one, and so does the recovery back to `ok`. A project that sits
  critical for an hour is reported once, not sixty times.
- **`unknown` is never announced.** Losing contact with LXD for a sweep is an
  operational blip, not a project event; the sidebar dot turns grey and the
  phone stays quiet.

The same verdict drives the coloured dot beside each project in the sidebar
(click it to open the project's Info tab) and the health pill plus memory bar
in the project header. Both read the workspace WebSocket, so they update
without polling.

The monitor is controlled by `HEALTH_MONITOR_INTERVAL`; setting it to `0`
switches it off entirely, which also silences this event no matter how the
toggle is set. See
[Project health monitor](../04-operations/09-deployment-and-operations.md#project-health-monitor).

## Delivery guarantees (or the lack of them)

Delivery is deliberately best effort, because a notification must never be able
to break a run:

- Events go onto a **bounded in-memory queue** (256 entries) drained by a single
  worker goroutine. A full queue drops the newest event and logs it.
- Each sink gets **three attempts** with a 1s then 3s backoff, and every outbound
  HTTP call has a **10 second timeout**.
- **One "finished" per run.** Events carry an internal dedupe key
  (`run:<chatId>:<runId>`, `schedule:<taskId>:<lastRunAt>`,
  `digest:<occurrenceMillis>`); the last 512 keys are remembered.
- The queue is **not durable**. Events pending at a restart are lost.

## Enabling notifications

Settings → **Notifications** (admin only). Fill in at least one destination,
tick the events you want, turn **Send notifications** on, save, then use
**Send test** — it reports per-sink success or the exact error, which is the
fastest way to find a wrong token or a chat the bot was never added to.

Configuration lives at `DATA_DIR/notifications.json`, mode `0600` because it
holds the bot token, the WhatsApp credential, and the webhook secret. There are
no environment variables to set; the deep link reuses `BASE_URL`.

Secrets are **write-only over the API**. `GET /api/admin/notifications` returns
`••••` plus the last four characters, never the value. Saving with a blank
secret field keeps whatever is stored, which is why the form can show a mask
instead of the real credential; the **Remove stored …** buttons clear one.

## Telegram

### 1. Create a bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather).
2. Send `/newbot`, pick a display name and a username ending in `bot`.
3. BotFather replies with a token shaped like
   `123456789:AAHk9y...`. That is the **bot token**.

### 2. Find the chat ID

A bot cannot message you first, so you must open the conversation:

- **Direct messages:** send your new bot any message (`/start` works).
- **Group:** add the bot to the group, then send a message mentioning it.
  For groups, also send `/setprivacy` → `Disable` to BotFather if the bot does
  not appear to see messages.

Then read the chat ID:

```bash
curl -s "https://api.telegram.org/bot<TOKEN>/getUpdates" \
  | grep -o '"chat":{"id":[-0-9]*'
```

Personal chats have positive IDs (`123456789`); groups and supergroups have
negative ones (`-1001234567890`). Paste it into the **Chat ID** field.

### Message format

Messages are sent with `parse_mode: HTML`. Everything interpolated — project
name, chat title, agent output — is HTML-escaped first, so agent output cannot
inject markup into your Telegram client. Summaries are trimmed to 900
characters and the whole message to 3500.

## WhatsApp

WhatsApp is one sink with two selectable providers. Pick the one that matches
your situation:

| | **Meta WhatsApp Cloud API** | **CallMeBot** |
| --- | --- | --- |
| Who it is for | a business number you control | one person, one phone |
| Cost | free tier, then per-conversation pricing | free |
| Setup | Meta developer app, phone number ID, access token | one WhatsApp message |
| Free-form text | only inside a 24-hour window (see below) | always |
| Sender shown to you | your own business number | the CallMeBot number |

Only the selected provider is used, but both sets of credentials stay stored,
so switching back does not mean re-entering them.

Messages are **plain text** — WhatsApp renders no HTML — and deliberately
short: an emoji, a one-line headline with the project name, the summary, and
the deep link, capped at 900 characters.

### Meta WhatsApp Cloud API

1. Create a Meta app at [developers.facebook.com](https://developers.facebook.com/)
   and add the **WhatsApp** product to it.
2. In **WhatsApp → API Setup**, note the **Phone number ID** of your sending
   number. This is the long numeric ID, *not* the phone number itself.
3. Copy the access token. The temporary token on that page expires in 24 hours
   — for a server, create a **System User** in Business Settings, give it the
   `whatsapp_business_messaging` permission, and generate a permanent token.
4. Add the destination number under **To** so Meta will accept it while your
   app is unverified.
5. In Remote, choose **Meta WhatsApp Cloud API** and fill in the phone number
   ID, the access token, and the recipient in E.164 **without** a leading plus
   (`2010xxxxxxxx`). Punctuation and spaces are stripped for you.

Remote posts to `https://graph.facebook.com/v20.0/{phoneNumberId}/messages`
with `Authorization: Bearer <token>`.

#### The 24-hour window, and why the template field exists

Meta only delivers **free-form text** inside a *customer service window*: the
24 hours after the recipient last messaged your business number. Outside that
window a free-form message is rejected (error `131047`), and only a
**pre-approved template** goes through.

Remote handles this with one setting:

- **Template name empty** → the message is sent as `type: "text"`. Good for
  testing, and for a recipient who messages the business number regularly.
  Expect failures once the window closes.
- **Template name set** → the message is sent as `type: "template"` with the
  rendered summary as the **first body parameter**. This works at any time,
  which is what you want for a server that pings you at 3 a.m.

Create the template in **WhatsApp Manager → Message templates** with exactly
one `{{1}}` placeholder in its body, for example:

```
Remote: {{1}}
```

Then put its name in **Template name**. Meta matches a template by name *and*
language, so if yours was approved as anything other than `en_US`, put that
code in **Template language** (for example `en` or `ar`). Getting it wrong
shows up as error `132001` in the **Send test** result.

### CallMeBot

CallMeBot is a free personal gateway: no Meta app, no business number, no
template. It is the fastest path for a solo operator.

1. Follow the activation steps at
   [callmebot.com](https://www.callmebot.com/blog/free-api-whatsapp-messages/):
   add the bot's number to your contacts and send it the activation phrase from
   that page.
2. The bot replies with your personal **API key**.
3. In Remote, choose **CallMeBot** and enter your own WhatsApp number in E.164
   **with** the leading plus (`+2010xxxxxxxx`) plus the API key.

Remote issues `GET https://api.callmebot.com/whatsapp.php?phone=…&text=…&apikey=…`
with every parameter URL-encoded, so a summary containing `&`, `=`, or a
newline cannot break out of the query string.

Two things to know: messages arrive from the CallMeBot number rather than your
own, and the service is a free third party with no delivery guarantee — the
message text (project name, chat title, a slice of agent output) passes through
it. Use the Cloud API if that matters.

### Removing a credential

Both secrets follow the same write-only rules as the Telegram token: `GET`
returns `••••` plus the last four characters, saving with the field blank keeps
what is stored, and **Remove stored access token** / **Remove stored API key**
clears one. Clearing a provider's destination (the Cloud recipient or phone
number ID, or the CallMeBot number) also drops that provider's retained secret,
so a credential never outlives the address it was for.

## Weekly cost and usage report

A scheduled digest that answers "what did last week cost?" without opening the
dashboard. It goes out through **every configured sink** — Telegram, WhatsApp,
and the webhook — as one message:

```
📊 Weekly usage report
Weekly usage 11–18 Aug — total $12.35 (56 runs) · shop $7.10 (30 runs) ·
wp-project $5.24 (26 runs) · top model claude-sonnet-4
Open Settings → Usage in Remote for the full breakdown.
```

Figures come from the [usage ledger](10-usage-and-cost.md), aggregated across
**every** project on the server (the digest is an operator report, not a
per-user one). At most five projects appear by name; the rest collapse into
`+N more projects`. A week with no runs says so rather than reporting `$0.00`.

### Scheduling it

**Settings → Notifications → Weekly cost report.** Defaults are **Sunday
09:00 Africa/Cairo**; the time zone accepts any IANA name and the zone database
is compiled into the binary, so no host `tzdata` package is required. An
unknown name falls back to UTC rather than stopping the schedule.

The digest needs notifications turned on: saving it enabled while the master
switch is off answers `400`.

### Idempotency

A loop wakes every five minutes and asks whether the most recent scheduled
occurrence has already been covered. `lastDigestSentAt` in
`notifications.json` records the occurrence — not the send time — so:

- Two ticks in the same week produce one message.
- A restart mid-week produces no extra message.
- A server that was down over Sunday 09:00 sends the digest as soon as it is
  back, still exactly once.
- Enabling the digest **arms** the schedule instead of firing: the first pass
  records the current occurrence without sending, so switching it on (or
  upgrading a running server) never dumps last week's report unannounced. The
  first real digest arrives at the next scheduled hour.

If aggregation fails, the occurrence stays claimed and the error is logged: a
broken ledger produces one missed digest, not a retry storm against your phone.

### Sending one now

**Send digest now** in the settings page, or:

```bash
curl -sS -b cookies.txt -X POST https://remote.example.com/api/admin/notifications/digest/send-now
```

It builds the digest for the last seven days ending *now*, delivers it
synchronously, and reports per-sink results exactly like **Send test**. It does
**not** advance `lastDigestSentAt`, so using it never costs you the real
report.

## Generic webhook

Remote sends `POST <your URL>` with `Content-Type: application/json` and this
body:

```json
{
  "event": "runFinished",
  "projectId": "9f2a1c04",
  "projectSlug": "acme-api",
  "projectName": "Acme API",
  "chatId": "abc123",
  "chatTitle": "Fix the login redirect",
  "provider": "claude",
  "status": "finished",
  "summary": "Updated the session cookie domain and added a regression test.",
  "url": "https://remote.example.com/?chat=abc123",
  "at": 1700000000000
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `event` | string | `runFinished`, `runFailed`, `needsAttention`, `scheduledRun`, `projectHealth`, or `test` |
| `event` | string | `runFinished`, `runFailed`, `needsAttention`, `scheduledRun`, `digest`, or `test` |
| `projectId`, `projectSlug`, `projectName` | string | Omitted for loose (project-less) chats |
| `chatId`, `chatTitle` | string | `chatTitle` omitted if the chat has no title yet; both are absent on `projectHealth` |
| `provider` | string | `claude`, `codex`, `kimi`, or `antigravity`; absent on `projectHealth` |
| `status` | string | See the trigger table above |
| `summary` | string | Agent output or failure reason, trimmed to 600 characters |
| `url` | string | Deep link to the chat, or to the project for `projectHealth` |
| `at` | number | Unix milliseconds |

Any `2xx` response counts as delivered. Anything else is retried up to the
three-attempt limit.

### Verifying the signature

When a shared secret is configured, each request carries

```
X-Remote-Signature: sha256=<hex hmac-sha256 of the raw body>
```

Compute the HMAC over the **exact bytes received**, before any JSON parsing or
re-serialization, and compare in constant time.

```js
// Node.js / Express — note express.raw(), not express.json()
import crypto from "node:crypto";

app.post("/remote", express.raw({ type: "application/json" }), (req, res) => {
  const expected =
    "sha256=" +
    crypto.createHmac("sha256", process.env.REMOTE_WEBHOOK_SECRET).update(req.body).digest("hex");
  const received = req.get("X-Remote-Signature") ?? "";

  const a = Buffer.from(expected);
  const b = Buffer.from(received);
  if (a.length !== b.length || !crypto.timingSafeEqual(a, b)) {
    return res.sendStatus(401);
  }

  const event = JSON.parse(req.body.toString("utf8"));
  console.log(event.event, event.chatTitle, event.url);
  res.sendStatus(204);
});
```

```python
# Python / Flask
import hmac, hashlib, os
from flask import request, abort

@app.post("/remote")
def remote():
    body = request.get_data()  # raw bytes
    expected = "sha256=" + hmac.new(
        os.environ["REMOTE_WEBHOOK_SECRET"].encode(), body, hashlib.sha256
    ).hexdigest()
    if not hmac.compare_digest(expected, request.headers.get("X-Remote-Signature", "")):
        abort(401)
    event = request.get_json()
    return "", 204
```

## API reference

All three routes are admin-only; a registered non-admin gets `403`.

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| `GET` | `/api/admin/notifications` | — | Masked configuration |
| `PUT` | `/api/admin/notifications` | Update payload (below) | Masked configuration |
| `POST` | `/api/admin/notifications/test` | — | `{"results":[{"sink","configured","delivered","error"}]}` |
| `POST` | `/api/admin/notifications/digest/send-now` | — | Same shape; `503` when no usage ledger is configured |

```json
{
  "enabled": true,
  "telegram": { "botToken": "", "clearBotToken": false, "chatId": "-1001234567890" },
  "webhook": { "url": "https://hooks.example.com/remote", "secret": "", "clearSecret": false },
  "events": {
    "runFinished": true,
    "runFailed": true,
    "needsAttention": true,
    "scheduledRun": true,
    "projectHealth": true
  }
  "whatsapp": {
    "provider": "callmebot",
    "cloud": {
      "phoneNumberId": "", "accessToken": "", "clearAccessToken": false,
      "recipient": "", "templateName": "", "templateLanguage": ""
    },
    "callmebot": { "phone": "+2010xxxxxxxx", "apikey": "", "clearApikey": false }
  },
  "events": { "runFinished": true, "runFailed": true, "needsAttention": true, "scheduledRun": true },
  "digest": { "enabled": true, "weekday": 0, "hour": 9, "timezone": "Africa/Cairo" }
}
```

A blank `botToken`, `secret`, `accessToken`, or `apikey` keeps the stored value;
the matching `clear*` flag removes it. `weekday` is `time.Weekday` — `0` is
Sunday. `PUT` returns `400` when the configuration cannot work — enabling with
no destination, a bot token without a chat ID, a webhook secret without a URL, a
webhook URL that is not absolute `http(s)`, a WhatsApp provider whose fields are
incomplete, a Cloud access token without a phone number ID and recipient, a
CallMeBot key without a number, or a digest scheduled while notifications are
off.

Test events ignore both the master switch and the event toggles, so an operator
can always debug a sink.

## Security notes

- The bot token, WhatsApp credential, and webhook secret sit in
  `notifications.json` **in plaintext**,
  mode `0600`, exactly like the other secrets under `DATA_DIR` (see the
  [threat model](../threat-model.md)). Anyone with root on the host can read
  them.
- Telegram, Cloud API, and CallMeBot errors are redacted before they reach the
  log or the admin UI, so a failing request never prints the credential — this
  matters most for CallMeBot, whose API key travels in the URL.
- CallMeBot is a **third-party relay**: message text passes through servers you
  do not control. The Cloud API talks to Meta directly.
- The weekly digest aggregates **every** project on the server, so anyone who
  can read the destination learns the whole fleet's cost shape.
- The webhook URL is operator-supplied and **not restricted to public
  addresses** — an admin can point it at a host on the internal network. Treat
  it as an admin-level capability.
- Summaries contain agent output. Anyone who can read the Telegram chat or the
  webhook endpoint can read a slice of every agent run.

## Related

- [Chat and agents](04-chat-and-agents.md) — where run events come from
- [Scheduled tasks](06-scheduled-tasks.md) — the `scheduledRun` source
- [Deployment and operations](../04-operations/09-deployment-and-operations.md#project-health-monitor) — the `projectHealth` source and its kill switch
- [Usage and cost](10-usage-and-cost.md) — the ledger behind the weekly digest
- [API and realtime](../03-platform/08-api-and-realtime.md) — the rest of the HTTP surface
