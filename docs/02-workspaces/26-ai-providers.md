# AI providers (free-tier pool)

Several vendors give away a real amount of model capacity every day. None of
them gives away enough. Each one runs out on its own schedule, resets on its
own clock, and changes its terms without telling you.

The **provider pool** connects as many of them as you like, spends them in an
order you control, moves to the next one when a quota runs out, and — the part
that is usually missing — shows you how much of each free tier you have
actually consumed.

It lives in **Settings → Agents & skills → AI providers**, and a compact
**Free quota** card on the home dashboard shows the providers closest to their
caps.

> **The numbers in this feature are advisory.**
> Every limit the platform ships was correct at the time it was written and is
> not a fact about the world. Vendors change free tiers without notice, several
> of them publish caps per *model* rather than per account, and the daily
> meters here roll over at UTC midnight while a vendor may roll over on its
> own timezone. Treat the bars as "roughly how hard you have been leaning on
> this" and the vendor's own dashboard as the truth. Every limit is editable,
> and editing one retires the "verify" warning on that row.

## What it is not

It does **not** run your agents. Claude, Codex, Kimi, and Antigravity still
run every prompt you send, through their own CLIs and their own credentials.
The pool serves two things only:

1. The [auxiliary model](24-auxiliary-model.md)'s small chores — chat titles,
   notification summaries, commit subjects, translation, chat summaries — for
   any job you route to it.
2. The **bulk lane** at `POST /api/providers/complete`, the single entry point
   for features that need a lot of cheap text.

Nothing it does is load bearing. A pool with nothing left falls back to your
local endpoint, and a local endpoint that cannot answer falls back to whatever
the platform did before any of this existed.

## Connecting a provider

Each row in the table is one provider. The platform ships eight of them as
**disabled templates with no credentials**, so installing them changes
nothing: base URLs are filled in, models are listed, and the documented free
limits are pre-entered and flagged for you to check.

To turn one on: press **Edit**, paste a key (or name a
[Secrets vault](16-secrets-vault.md) key), switch **Enabled** on, save, and
press **Test**. Test runs a real one-sentence completion and reports the
latency and the answer, so "it works" is something you see rather than assume.

### Where the key lives

Two options, and the vault one is better:

| | Stored where | Good for |
| --- | --- | --- |
| **Secrets-vault key name** (`apiKeyRef`) | `DATA_DIR/globalsecrets.json` | The key lives in one place and can be rotated once. This is the recommended shape. |
| **Inline key** (`apiKey`) | `DATA_DIR/providers.json`, mode 0600 | One less step when you are trying something out. |

Both are **write-only over the API**: reads return `••••` plus the last four
characters. A vault *key name* is not a secret and is shown in full. If both
are set, the vault reference wins.

### Getting a key for each shipped provider

| Provider | Where to get a key | What the free tier gives (as documented at seeding time — **verify**) |
| --- | --- | --- |
| **Groq** | [console.groq.com](https://console.groq.com) → API Keys | Fast Llama models. Documented around 30 requests/min, 1,000/day, 12,000 tokens/min, 100,000 tokens/day — **per model**, so the row carries the tightest of them. |
| **Cerebras** | [cloud.cerebras.ai](https://cloud.cerebras.ai) → API Keys | The largest daily request allowance of the set: around 30/min and 14,400/day, 60,000 tokens/min, 1M tokens/day. Extremely fast. |
| **Google Gemini** | [aistudio.google.com](https://aistudio.google.com/apikey) | Around 10 requests/min, 250/day and 250,000 tokens/min on Flash; Flash-Lite is documented more generously. Huge context windows. Uses Google's OpenAI-compatible surface at `/v1beta/openai/`. |
| **OpenRouter** | [openrouter.ai/keys](https://openrouter.ai/keys) | Only model ids ending in `:free` are free. Around 20 requests/min and 50/day, documented as rising once credits have been purchased. One key, many vendors behind it. |
| **Mistral** | [console.mistral.ai](https://console.mistral.ai) → activate the experiment plan | Documented as a per-second request rate plus a large monthly token allowance rather than a daily cap. |
| **Zhipu GLM** | [z.ai/manage-apikey/apikey-list](https://z.ai/manage-apikey/apikey-list) | The published throttle is a **concurrency** ceiling rather than a request or token cap, so **no limits are seeded** — the meters show raw counts until you enter your own. Note the two hosts: `api.z.ai` is international, `open.bigmodel.cn` is mainland China, and a key from one is rejected by the other. A **Coding Plan** key needs `https://api.z.ai/api/coding/paas/v4` instead of the general root. |
| **Moonshot (Kimi)** | [platform.kimi.ai/console/api-keys](https://platform.kimi.ai/console/api-keys) | Trial credit rather than a standing free tier, with a tier-dependent concurrency limit. **No limits are seeded**; set your own once you know them. `api.moonshot.ai` is international, `api.moonshot.cn` is mainland China. |

**GitHub Models is gone.** It shipped as a template and was removed: GitHub
retired the playground, catalog and inference API for every customer on 30
July 2026 and points users at Azure AI Foundry. Nothing in this platform
resurrects a deleted template, so an operator who had already connected it
keeps their row until they delete it — but it will only return errors.

You can add any other provider that speaks one of three wire shapes:

- **`openai`** — `POST {baseUrl}/chat/completions` with `Authorization: Bearer`. This covers everything in the table above.
- **`gemini`** — `POST {baseUrl}/models/{model}:generateContent` with `x-goog-api-key`, for Google's native API.
- **`anthropic`** — `POST {baseUrl}/messages` with `x-api-key` and `anthropic-version`.

The base URL is the **complete API root**, exactly as the vendor documents it
(`https://api.groq.com/openai/v1`, not `https://api.groq.com`). Unlike the
auxiliary model panel, nothing here guesses a missing `/v1`.

## Choosing who answers

One switch at the top of the panel decides everything:

**Auto-switch on quota exhaustion — on.** The pool walks the priority order and
uses the first provider that can take the request. It skips anything that is
disabled, has no key, is cooling down after a refusal, or is already over a
documented limit for the current window. Reorder the rows with the ↑↓ buttons.

**Auto-switch — off.** The pool uses exactly the **preferred provider** you
name, and fails if it cannot. A pinned provider is still skipped when it is
disabled, keyless, or cooling down — those calls are certain to fail — but it
is *not* skipped merely for being over a locally counted limit, because that
count is an estimate and you pinned it on purpose.

### Choosing which model

Each model can be tagged `text`, `code`, or `bulk`. A job asks for one of them
— a commit subject asks for `code`, the bulk lane asks for `bulk`, everything
else asks for `text` — and the pool takes the first model on the chosen
provider that claims the tag and is big enough for the request. **A model with
no tags at all suits everything**, so pasting a model id and stopping does not
make it unreachable.

### When a provider refuses

A 429 or a 5xx puts the provider to sleep on an exponential back-off — **30
seconds, then 5 minutes, then 30 minutes** — and the pool moves to the next
one. If the vendor sent a `retry-after` longer than our own step, the vendor's
number wins: they know when their window reopens and we are guessing. A
successful call clears the back-off, and so does pressing **Test**, which is
what makes "fix the key, press Test" put a provider straight back to work.

At most **three** providers are tried for one request. Walking eight dead
providers is a stall, not a failover.

## Reading the meters

Each provider row carries three bars: **requests today**, **tokens today**, and
**tokens this month**, each against the limit on that row. Every bar carries a
badge:

- **counted locally** — this platform added it up. It is always available, it
  is ours, and it knows nothing about requests you made with the same key from
  somewhere else.
- **reported by provider** — read from the vendor's own rate-limit headers on
  the last response. Better than ours whenever it is there, and it wins when it
  is. A reading older than ten minutes is dropped rather than shown, because
  "nothing left" from an hour ago describes a window that has since reopened.

A window with **no documented limit** shows an empty track and the raw count.
The platform will not invent a number to compare against, and a provider with
no documented caps can never be skipped for being "over" one.

Token counts come from the provider's own usage block when the response has
one. When it does not, they are a four-characters-per-token estimate — which is
wrong for Arabic and for code, and is exactly why such a bar says "counted
locally".

The day and month windows are **UTC**. Counters survive a restart: the day and
month totals are rebuilt from the ledger on boot, so restarting the server does
not hand you a comfortable lie about how much free tier is left.

## Routing the auxiliary model's jobs

Each of the five auxiliary-model jobs now chooses between three routes in
**Settings → Agents & skills → Local / auxiliary model**:

| Route | What happens |
| --- | --- |
| **Local** | The job goes to your own endpoint — the local Ollama, or whatever OpenAI-compatible URL you configured. This is what every job did before the pool existed, and it stays the default. |
| **Pool** | The job goes through the provider pool. If the pool cannot serve it, the job silently drops to the local endpoint; if that cannot either, the job falls back to its original non-AI behaviour. |
| **Off** | The job does exactly what the platform did before the auxiliary model existed. |

The failover is invisible to the job. A chat title does not know or care which
of eight providers wrote it.

Documents written before this feature existed still work: a stored `true`
reads as **Local** and a stored `false` reads as **Off**, which is what they
always meant.

## The bulk lane

`POST /api/providers/complete` is the one entry point for features that need a
lot of cheap text — product descriptions, SEO copy, translating a few hundred
UI strings. It exists so none of them grows its own provider client, its own
key handling, and its own idea of a reasonable request.

```bash
curl -sS -b cookies.txt -X POST https://remote.example.com/api/providers/complete \
  -H 'Content-Type: application/json' \
  -d '{"job":"bulk","prompt":"Write a 40-word description of a stainless steel kettle.","system":"You write product copy.","maxTokens":200}'
```

```json
{
  "text": "…",
  "providerId": "groq",
  "providerLabel": "Groq",
  "model": "llama-3.1-8b-instant",
  "promptTokens": 31,
  "completionTokens": 58,
  "latencyMs": 412,
  "failovers": 0
}
```

- **Any signed-in member** may call it; it is **rate limited to 30 requests per
  minute per user** and every call is written to the [audit log](../04-operations/10-audit-log.md)
  with the provider, the model, and the token counts — never the prompt and
  never the answer.
- The prompt is capped at about **8,000 tokens** and the answer at **2,000**. A
  larger prompt is refused with `413` rather than silently truncated.
- `providerId` pins one provider for that call. Omit it to follow the pool's
  own mode.
- An exhausted pool answers **503**, which means "fall back", not "retry".

## Files and API

| Path | What |
| --- | --- |
| `DATA_DIR/providers.json` | The registry: providers, their limits, the pool's policy. Mode 0600 — an entry may carry an inline key. |
| `DATA_DIR/providerpool/usage-YYYY-MM.jsonl` | Append-only ledger, one line per request, failover, and cooldown. Mode 0600. |

| Route | Who | What |
| --- | --- | --- |
| `GET /api/admin/providers` | admin | The whole panel: providers, meters, policy |
| `POST /api/admin/providers` | admin | Create or update one provider |
| `PUT /api/admin/providers` | admin | Save the pool's policy (auto-switch, preferred provider) |
| `PUT /api/admin/providers/{id}` | admin | Update one provider |
| `DELETE /api/admin/providers/{id}` | admin | Remove one provider and forget its counters |
| `POST /api/admin/providers/reorder` | admin | Rewrite the priority order from `{"ids": [...]}` |
| `POST /api/admin/providers/{id}/test` | admin | One real completion; answers `200` with the outcome in the body |
| `GET /api/providers/quota` | member | The dashboard card: labels and meters, no endpoints and no key state |
| `POST /api/providers/complete` | member | The bulk lane |

`reorder` and `settings` are reserved and cannot be used as provider ids.

## Housekeeping

The ledger is **not rotated**. One line per request against small free tiers
is a slow-growing file, but it grows forever; if you lean on the bulk lane
hard, prune old months yourself:

```bash
find /opt/remote.futrx/data/providerpool -name 'usage-*.jsonl' -mtime +180 -delete
```

Deleting a provider forgets its counters, so an id you create again later
starts from zero rather than inheriting a stranger's consumption. Deleting the
provider a manual pin names also clears the pin, so the pool does not end up
declining every request while pointing at something that no longer exists.
