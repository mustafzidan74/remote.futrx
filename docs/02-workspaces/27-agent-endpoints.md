# Third-party agent endpoints

A brochure site, a landing page, or a round of copy edits does not need a
frontier model. Several vendors publish an endpoint that speaks an agent CLI's
own protocol precisely so their models can be driven by that CLI — Zhipu GLM
and Moonshot/Kimi publish Anthropic-compatible endpoints documented for use
with Claude Code, and the Codex CLI documents custom OpenAI-compatible
providers as `[model_providers.<id>]` blocks in `~/.codex/config.toml`.

This platform keeps a **register** of those endpoints. One chat can be pointed
at one of them from the composer's agent pill, and everything else about that
chat — its transcript, its skills, its container, its schedules — works
exactly as before. Claude stays for the hard work.

- **UI:** Settings → **Agent endpoints** (Agents & skills group), admin only.
  Any member points a chat at an enabled endpoint from the composer's agent
  pill.
- **Storage:** `DATA_DIR/agent-endpoints.json`, mode 0600, seeded on first run
  with disabled templates.
- **Code:** [`service/agentendpoints`](../../backend/internal/service/agentendpoints/),
  [`stores/fileagentendpoints`](../../backend/internal/stores/fileagentendpoints/),
  [`integration/containers/agentendpoint`](../../backend/internal/integration/containers/agentendpoint/),
  [`agent/endpoint.go`](../../backend/internal/agent/endpoint.go),
  [`agent_endpoints_handler.go`](../../backend/internal/transport/http/handlers/agent_endpoints_handler.go).

> **The rule, up front.** Only a vendor's **own published compatibility
> endpoint**, reached with **your own API key**, is supported.
>
> This platform never impersonates a first-party CLI to a vendor that has not
> published such an endpoint, never spoofs a user agent, and never touches
> cookies or replays a session. If a vendor has no documented path, we simply
> do not support it — there is no profile for it and no field here that could
> be configured into one. Pointing a profile at a first-party API you are not
> entitled to use is a terms-of-service breach; the warning box in the admin
> panel says so in two lines, and this document says so here.

## A profile

| Field | Meaning |
| --- | --- |
| `id` | `[a-z0-9][a-z0-9_-]*`, max 40. For the codex CLI it becomes the `model_providers.<id>` table key, which is why the character set is restricted. **Immutable** — it is the handle every chat pointed at this endpoint stores. |
| `label` | What the composer, the pill, and the chat header badge show. |
| `cli` | `claude` or `codex`. These are the two CLIs whose vendors document a compatibility mode; Kimi Code and Antigravity document none, so they are not offered. |
| `baseUrl` | The vendor's published compatibility endpoint. Must be an absolute `http(s)` URL with no query, fragment, or embedded credentials. |
| `apiKeyRef` | The **name** of a Secrets-vault `env` entry scoped to **all projects**, holding your API key. Never the key itself. |
| `models` | The model ids to offer, in picker order. The first is the default. Empty leaves the CLI on whatever the endpoint defaults to. |
| `headers` | Optional extra request headers the vendor asks for. Names are restricted to header tokens because they become part of a config key path. |
| `wireApi` | Codex only: `responses` (default) or `chat`. See [Wire protocol](#wire-protocol). |
| `notes` | Free text. The seeded templates use it to record where their values came from. |
| `enabled` | Whether the profile appears in the composer and may be used by a run. Every seeded template ships **disabled**. |

A profile may sit disabled without naming a vault key — that is how the
templates arrive — but it cannot be switched on until it names one.

## Where the key lives

The register **never stores an API key**. A profile names a Secrets-vault key
and the value is read at run time, handed to the CLI for that one run, and
then forgotten. It is never logged, never written into a chat transcript, and
never returned by any API route.

The vault entry must be an `env` entry **scoped to all projects**
(`PlatformValues` in [`service/globalsecrets`](../../backend/internal/service/globalsecrets/)).
That rule exists because a profile is platform-level and may be used by a chat
with no project at all: there is no project whose scope could authorize the
read, and requiring an all-projects entry keeps this from becoming a way to
read a project-scoped secret from outside that project.

If the key does not resolve, the run **fails before the CLI starts**, with a
sentence naming the profile and the key. The admin table shows the same state
in its Status column ("Key missing") so the problem is visible without a probe.

## How each CLI is configured

Both shapes are **per run**. Nothing is written into a container's
configuration files, because `/root/.claude` and `/root/.codex` are
bind-mounted from the host and shared by every chat in a project — a provider
block written into `config.toml` would silently point the *next* chat at a
third party too.

### Claude Code — Anthropic-compatible

Three environment variables, published through `lxc exec --env` for that one
run:

| Variable | Value |
| --- | --- |
| `ANTHROPIC_BASE_URL` | the profile's `baseUrl` |
| `ANTHROPIC_AUTH_TOKEN` | the resolved vault value |
| `ANTHROPIC_API_KEY` | **empty** |
| `ANTHROPIC_CUSTOM_HEADERS` | `Name: Value` per line, only when the profile declares headers |

`ANTHROPIC_API_KEY` is set empty rather than left alone: the container's own
environment or a project secret could otherwise hold an Anthropic key, and a
run pointed at a third party must not carry one.

The model is selected with the CLI's ordinary `--model` flag — the endpoint
contributes no flags of its own.

### Codex — OpenAI-compatible

The `[model_providers.<id>]` block travels as `-c key=value` overrides on the
CLI's own command line, the same mechanism this repository already uses for
`model_reasoning_effort`, `service_tier`, and the Agent Browser's MCP server:

```
-c model_provider="openrouter"
-c model_providers.openrouter.name="OpenRouter"
-c model_providers.openrouter.base_url="https://openrouter.ai/api/v1"
-c model_providers.openrouter.env_key="REMOTE_ENDPOINT_API_KEY"
-c model_providers.openrouter.wire_api="responses"
-c model_providers.openrouter.http_headers.X-Title="..."     # only if declared
```

plus two environment variables: `REMOTE_ENDPOINT_API_KEY` (the resolved value,
which is what `env_key` points at) and an empty `OPENAI_API_KEY`.

The key is deliberately **never** an argument — a command line is readable by
every process in the container. `env_key` is the vendor-documented way to keep
a credential out of the configuration itself, and this platform uses it.

Every interpolated value is rendered as a TOML basic string, so a label
containing a quote is data rather than syntax.

### Measured against codex-cli 0.145.0 — no codex templates ship

The codex side was tested on the pinned CLI version and does not work, so the
templates for it were removed rather than shipped broken:

| What was tried | Result |
|---|---|
| `wire_api = "chat"` (Gemini, Groq, Cerebras publish Chat Completions only) | Config refused at load: *"`wire_api = "chat"` is no longer supported"* |
| `wire_api = "responses"` + `env_key`, from a real `config.toml` | Reaches the provider, sends no credential: *401 Missing Authentication header* |
| Same, via `-c model_providers.x.env_key=...` overrides | Identical |
| `env_http_headers.Authorization` | Identical |

`codex doctor` reports *"auth is provided by the active model provider"* and
*"model provider requires OpenAI auth false"*, so codex accepts the
configuration — it simply never puts the key on the wire.

One useful thing the same test settled: **codex does not need its own ChatGPT
login to drive a custom provider.** It went straight to the third-party host.
So when the credential path works, these endpoints will not require a Codex
subscription.

`codex` remains a supported CLI so an operator with a working recipe can add a
profile by hand. What changed is that the platform no longer ships templates
that cannot authenticate.

### Wire protocol

Recent codex builds require the OpenAI **Responses** API and no longer speak
the older Chat Completions wire. OpenRouter documents `wire_api = "responses"`.
Google Gemini's, Groq's, and Cerebras's OpenAI-compatibility layers publish
Chat Completions, so their seeded templates carry `wire_api = "chat"` and a
note saying a translating proxy may be needed. **Run Test before enabling one.**

## What a run pointed at an endpoint does *not* do

Two things are deliberately skipped, in both CLIs:

- the operator's own provider credentials are **not seeded** into the
  container for that run (no `~/.claude/.credentials.json` push, no
  `~/.codex/auth.json` push). A run authenticating to a third party with your
  key for that vendor has no business also carrying a first-party token;
- credentials are **not synced back** from the container afterwards, so a run
  that talked to a third party cannot overwrite the operator's stored token
  with whatever it left behind.

A project secret whose name collides with an endpoint variable is also
dropped for that run, so a project cannot redirect a run the platform pointed
somewhere specific, nor substitute its own credential.

### Does the injected token really win over an existing login?

For the `claude` CLI this was the one thing the design could not settle by
reading code: `/root/.claude` is a per-project bind mount, so a container
whose operator has already logged in carries a `.credentials.json`, and
whether the CLI prefers that file or `ANTHROPIC_AUTH_TOKEN` is the vendor's
own behaviour.

Measured on this platform (claude CLI, container `wp-test`, endpoint pointed
at Zhipu with a deliberately wrong key), the CLI answered:

```
⚠ claude.ai connectors are disabled because ANTHROPIC_API_KEY or another
  auth source is set and takes precedence over your claude.ai login
```

So the injected token wins, which is what this feature needs: a chat pinned
to an endpoint cannot silently fall back to spending the operator's Anthropic
subscription. Re-check it after a CLI upgrade — if a future version reverses
the order, the fix is a per-run `CLAUDE_CONFIG_DIR` pointing at a scratch
directory, which keeps the login file out of reach entirely.

Note what the same test showed about failures: a CLI whose key the endpoint
rejects does **not** exit. It retries quietly, prints nothing useful, and is
still going two minutes later. That is why Test reports a timeout as *"the
CLI kept retrying instead of answering, which usually means the endpoint
rejected the key or does not serve the requested model"* rather than as a
network error.

## Selection precedence

A chat stores one field: `endpointId` in its meta (empty = the vendor's own
endpoint, which is what every chat did before this feature and is still the
default).

**An endpoint pins the chat.** It decides which CLI answers and which model
that CLI is asked for, so a chat carrying one is not offered to the automatic
[model router](23-model-routing.md) at all — the routing decision is not
extended with an endpoint field, and the chat's endpoint simply wins.

The reason is that the two vocabularies cannot be reconciled: a routing rule
names a model from the platform's own catalog, and an endpoint offers a
vendor's model ids under one specific CLI, so a routed decision landing on a
chat pinned to GLM could only ever produce a model name the endpoint has never
heard of.

| Chat state | What runs |
| --- | --- |
| no endpoint, `modelPolicy: pinned` | the chat's own provider and model |
| no endpoint, `modelPolicy: auto` | whatever the routing policy decides |
| endpoint set, either policy | the endpoint's CLI and its resolved model |

If the chat's stored provider has drifted away from its endpoint's CLI, the
**endpoint's CLI wins** — otherwise a codex binary would be asked for a model
only an Anthropic-compatible endpoint offers. Picking a first-party model or a
different agent in the pill releases the endpoint, which is how a chat comes
back.

**Team mode** is unaffected: companion chats are created by the team
orchestrator with their own per-seat provider and model and do **not** inherit
the parent's endpoint. A reviewer stays on a real model.

## Guardrails

- **Red header badge.** A chat running on an endpoint carries
  `running on GLM-4.6 via Zhipu GLM — not Anthropic` in its header, and the
  composer pill turns red and names the model and vendor. Whose model produced
  a piece of client code is a commercial fact somebody will eventually be
  asked about; the badge is what stops a GLM-written page being handed over as
  Claude's work.
- **Audit.** `agent.run.start` gains an `endpointId` in its meta. The four
  register edits are recorded as `settings.agent-endpoint.{create,update,delete,test}`
  against a target of type `agent-endpoint`. No entry ever records a key.
- **Test.** Per profile, the admin panel runs a **two-word prompt through the
  real CLI** inside a chosen project's container and shows the raw result. It
  runs the real binary on purpose: a probe that just spoke HTTP to the base URL
  would answer a different question and miss whether *this* CLI, at the version
  this platform pins, accepts *this* vendor's compatibility mode. The resolved
  key is masked out of the output before it leaves the backend, and a disabled
  profile is testable so values can be confirmed *before* switching it on.
- **Clear errors.** A deleted profile, a disabled one, and an unresolved key
  each stop the turn before the CLI starts, with a sentence about the
  configuration rather than a vendor's opaque 401 in the transcript.

## Seeded templates

A fresh install writes six profiles, **all disabled and all without a key
reference**. Seeding happens once — the file's existence is the marker — so an
operator who deletes them keeps an empty register.

| Profile | CLI | Base URL | Notes |
| --- | --- | --- | --- |
| Zhipu GLM | claude | `https://api.z.ai/api/anthropic` | the international host; mainland accounts use `open.bigmodel.cn` and the keys are not interchangeable |
| Moonshot Kimi | claude | `https://api.moonshot.ai/anthropic` | mainland accounts use `api.moonshot.cn` |
| OpenRouter | codex | `https://openrouter.ai/api/v1` | `wire_api = responses`; model ids carry the vendor prefix |
| Google Gemini | codex | `https://generativelanguage.googleapis.com/v1beta/openai/` | Chat Completions only |
| Groq | codex | `https://api.groq.com/openai/v1` | Chat Completions only |
| Cerebras | codex | `https://api.cerebras.ai/v1` | Chat Completions only |

Vendors move URLs and rename models. These are a starting point you confirm
with Test, not a guarantee — which is exactly why they ship switched off.

## Setting one up

1. Settings → **Secrets vault** → add an `env` entry scoped to **all
   projects**, e.g. `ZHIPU_API_KEY`, holding your key for that vendor.
2. Settings → **Agent endpoints** → **Edit** the seeded profile → set its
   *Secrets vault key* to that name → save.
3. Pick a project and press **Run a two-word prompt**. Its container must be
   running.
4. If it answers, press **Enable**.
5. In any chat, open the composer's agent pill → **Third-party endpoints** →
   pick a model. The header badge turns red.

## API

| Route | Method | Who | What |
| --- | --- | --- | --- |
| `/api/agent-endpoints` | GET | any signed-in user | The composer's read: enabled profiles, their labels, CLIs, and model ids. **No base URL, no key reference.** |
| `/api/admin/agent-endpoints` | GET | admin | The whole register, plus whether each key resolves |
| `/api/admin/agent-endpoints` | POST | admin | Create |
| `/api/admin/agent-endpoints/{id}` | PUT | admin | Update (the id is immutable) |
| `/api/admin/agent-endpoints/{id}` | DELETE | admin | Delete; chats pointed at it fall back to the vendor's own endpoint on their next turn |
| `/api/admin/agent-endpoints/{id}/enabled` | PUT | admin | Toggle without restating the profile |
| `/api/admin/agent-endpoints/{id}/test` | POST | admin | Run the two-word probe in a project's container |

A deployment without the register answers `503` on the admin routes and an
empty list on the member route, so the composer simply shows no third-party
section.
