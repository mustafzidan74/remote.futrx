# WhatsApp on this install

What this deployment is actually running, and the things that cost time getting
there. The full reference for both providers is
[07-notifications.md](../02-workspaces/07-notifications.md); this is the
operational record for **ss-dev.online**.

## What is running

**CallMeBot**, the free personal gateway.

| | |
|---|---|
| Provider | `callmebot` |
| Destination | the operator's own number, in E.164 |
| Credential | an apikey, held in `DATA_DIR/notifications.json` (mode 0600) |
| Configured at | Settings → Platform → Notifications |
| Status | live, verified by the test endpoint |

Telegram runs beside it and receives the same events, which matters more than it
sounds: see [Do not rely on one sink](#do-not-rely-on-one-sink).

## How it delivers

One HTTP GET, nothing else:

```
GET https://api.callmebot.com/whatsapp.php
    ?phone=<number>&apikey=<key>&text=<message>
```

CallMeBot sends from **their** WhatsApp number, not yours. That is the whole
trade: no business account, no template approval, no 24-hour window — and no
control over the sender or the service.

## Activation, and the part that wasted a day

CallMeBot is authorized once, by messaging their number from the phone that
should receive alerts:

> `I allow callmebot to send me messages`

They reply with the apikey, which goes into Settings → Notifications.

**Their activation number rotates.** A number that worked last month is silently
dead: the message simply goes unanswered, which reads as "the service is down"
rather than "you texted the wrong number". Two numbers were tried here before
one answered.

**Always read the current number off CallMeBot's own docs page immediately
before activating.** Do not reuse one from a note, a chat log, or this file —
which is why no number is written here.

## Why messages are capped at 900 characters

The Cloud API allows 4096, and this platform still truncates both providers at
900. CallMeBot carries the message text **in a query string**, so a long summary
runs into URL length limits and is rejected or silently cut.

Sharing the smaller budget means switching providers can never break a message
that was working. The cost is a shorter summary on Meta; the alternative is an
alert that stops arriving the day someone changes a dropdown.

## Do not rely on one sink

CallMeBot is a free service run by one person, with no support contract and no
status page. It is entirely appropriate for "your deploy finished" and entirely
inappropriate as the only path for anything that matters.

Telegram is configured alongside it for exactly this reason. If WhatsApp goes
quiet for a day, the alerts still arrive.

## What not to send through it

Messages pass through a third party's infrastructure. Operational noise is fine:
run finished, run failed, a project's health changed, the weekly digest.

**Client data is not.** No credentials, no customer records, no site content. If
an alert would embarrass you in a stranger's log file, it does not belong on
this path — use the platform's own UI, or Meta's Cloud API where the operator
owns the account.

## Switching to Meta's Cloud API later

Both providers' settings are stored, so switching back and forth loses nothing.
Meta is worth the setup when messages start going to **clients** rather than to
the operator: you send from your own business number, and delivery is a service
with an SLA rather than a favour.

It needs a WhatsApp Business account, a phone number ID, a permanent System User
token, and — for anything outside a 24-hour reply window — a template approved
by name *and* language. [07-notifications.md](../02-workspaces/07-notifications.md)
walks through it.

## Checking it still works

Settings → Platform → Notifications → **Send test**. The reply arrives on the
phone within a few seconds; a failure is reported inline with the provider's own
message rather than a generic error.

If nothing arrives and the test reports success, the apikey is stale: re-run the
activation above with a freshly-read number.
