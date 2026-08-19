# Auxiliary model

The platform does a handful of small text jobs on its own account: it names a
chat, it writes the sentence a phone notification carries, it pre-fills a
commit message, it offers to translate a client message. Today it does these
either crudely — a chat title is the first sixty characters of the first
prompt — or not at all.

The **auxiliary model** is an optional, small, cheap language model that takes
those jobs. It is not a coding agent and never becomes one. Claude, Codex,
Kimi, and Antigravity still run every prompt you send, with their own
credentials and their own models. The auxiliary model never sees a workspace,
never runs a tool, and never touches an agent's answer.

**Everything it does has a fallback.** Switched off, unreachable, misconfigured,
or simply slow, every feature below reverts to exactly what it did before — no
error, no blocked action, no waiting. That property is the reason it can be
turned on without ceremony.

## The jobs

| Job | With the auxiliary model | Without it (the fallback) |
| --- | --- | --- |
| **Chat titles** | A 3–6 word title in the language the first prompt was written in, written after the first run settles. A ↺ button in the chat header rewrites it on demand. | The first 60 characters of the first prompt, truncated |
| **Notification summaries** | One useful sentence in the Telegram / WhatsApp / webhook alert | The raw tail of the agent's last message |
| **Commit messages** | A Conventional Commits subject in the pull-request dialog, behind a **Suggest** button | `Changes from Remote — <date>` |
| **Client message translation** | **Translate to العربية / English** buttons on a client message template | The buttons are not shown |
| **Chat summaries** | A one-line subtitle under a chat in the sidebar and in the dashboard's Recent activity | No subtitle |

Each job has its own toggle in **Settings → Agents & skills → Local /
auxiliary model**, and all of them are on once the service itself is on.
Switching one off restores that feature's original behaviour exactly.

### What the commit-message job is shown

Only the *shape* of the change: the per-file insert/delete counts from `git
diff --stat`, and the changed paths from `git status --porcelain`. Never a
line of any file's contents. This is what makes the job safe to run against a
remote endpoint as well as a local one — and it is pinned by a test, not just
a promise.

## Running Ollama on this host (recommended)

The intended setup is [Ollama](https://ollama.com) on the same box, bound to
loopback, with no credential at all.

**Check the RAM first.** A 3B model quantised to 4 bits needs roughly **2–3 GB
of resident memory** while it is answering, on top of everything the box
already runs.

| Host RAM | Verdict |
| --- | --- |
| 4 GB | **Do not.** Project containers, Caddy, and the backend already fill it. Upgrade first, or use a remote endpoint. |
| 8 GB | Fine with a couple of running containers. Use a 3B model. |
| 16 GB or more | Comfortable. A 7B model is an option if you want better Arabic. |

Ollama unloads an idle model after five minutes by default, so the memory is
only held while jobs are actually running — but the *first* call after an idle
period pays the load time, which is why the settings panel's timeout defaults
to 30 seconds rather than to something tight.

### Install

```bash
# 1. Install Ollama. The script installs a systemd unit and starts it.
curl -fsSL https://ollama.com/install.sh | sh

# 2. Pull the recommended model (~2 GB download).
ollama pull qwen2.5:3b

# 3. Confirm it answers.
ollama run qwen2.5:3b "Reply with one short sentence."
```

The installer's systemd unit already listens on `127.0.0.1:11434` and nothing
else, which is what you want: the auxiliary model must not be reachable from
the internet. Confirm it:

```bash
systemctl status ollama
ss -lntp | grep 11434     # expect 127.0.0.1:11434, never 0.0.0.0:11434
```

If a previous install left `OLLAMA_HOST` set to `0.0.0.0`, pin it back:

```bash
sudo systemctl edit ollama
# add:
#   [Service]
#   Environment="OLLAMA_HOST=127.0.0.1:11434"
sudo systemctl restart ollama
```

Do **not** add a Caddy route for port 11434. The backend reaches Ollama over
loopback; the public edge has no business seeing it.

### Point the platform at it

**Settings → Agents & skills → Local / auxiliary model**:

| Field | Value |
| --- | --- |
| Provider | `Ollama (local)` |
| Base URL | `http://127.0.0.1:11434` |
| Model | `qwen2.5:3b` |
| API key | leave empty |
| Timeout | `30` seconds |

Tick **Use an auxiliary model**, press **Save**, then press **Test**. Test runs
a real one-sentence completion and reports the round trip and the model's own
words. A warm 3B model on a modest VPS answers in a few hundred milliseconds;
the first call after an idle period can take several seconds while the model
loads.

### Model choices

| Model | Approx. RAM | Notes |
| --- | --- | --- |
| `qwen2.5:3b` | 2–3 GB | The recommended default. Good multilingual behaviour, including Arabic. |
| `llama3.2:3b` | 2–3 GB | Comparable; weaker Arabic. |
| `qwen2.5:7b` | 5–6 GB | Noticeably better translations. Only on a 16 GB host. |

## Pointing at a remote endpoint instead

Any endpoint that implements `POST /v1/chat/completions` works: OpenAI, Groq,
Together, OpenRouter, or a `llama.cpp` / vLLM / LM Studio server on another
machine.

| Field | Value |
| --- | --- |
| Provider | `OpenAI-compatible endpoint` |
| Base URL | `https://api.groq.com/openai/v1` (a base that already ends in `/v1` is not given a second one) |
| Model | whatever that endpoint calls it, e.g. `llama-3.1-8b-instant` |
| API key | the endpoint's key — stored write-only, echoed back only as `••••1234` |

The key travels as `Authorization: Bearer …` and is sent **only** when one is
stored, so a loopback endpoint never receives an empty bearer header.

The panel refuses to switch the service on for a non-loopback endpoint with no
key, because a switch that claims a feature works when it cannot is worse than
no switch.

### What leaves the box

If you choose a remote endpoint, these strings go to it:

- the **first prompt** of a chat, truncated (chat titles),
- the **tail of an agent's reply**, truncated (notification and chat summaries),
- a **diff stat and changed paths** — never file contents (commit messages),
- the **client message** you pressed Translate on.

A local Ollama sends none of that anywhere. That is the whole argument for the
local setup, and it is why it is the default.

## Storage and safety rails

| | |
| --- | --- |
| Settings file | `DATA_DIR/aux-model.json`, mode `0600` (it can hold an API key) |
| Key handling | Write-only. An empty key field on save keeps what is stored; **Remove stored key** clears it and switches the service off in the same request. |
| Per-call timeout | The configured value (default 30 s, bounded 3–120 s). Hard: past it the job gives up and the fallback is used. |
| Per-job answer cap | 40 tokens for a title, 60 for a commit subject, 160 for a summary, 1200 for a translation — each further capped by the panel's own **Answer cap**. |
| Per-call input cap | Every job trims its own input; 6000 characters is the backstop. |
| Circuit breaker | After **3 consecutive failures** the platform stops calling the endpoint for **5 minutes** and logs the reason once. Saving new settings, or a successful **Test**, closes it immediately. |
| Blocking | None. Chat-title and summary work happens off the run goroutine; a run notification that waits on a summary is published from its own goroutine. No user action ever waits on this model. |

The breaker exists because these jobs run behind ordinary user actions. A
stopped Ollama must not mean a dial-and-timeout on every chat that settles, and
it must not mean a log line per chat either.

## API

| Route | Who | What |
| --- | --- | --- |
| `GET /api/aux-model/config` | any signed-in user | Which jobs are available. Carries no endpoint, model, or key. |
| `POST /api/aux-model/translate` | any signed-in user | `{text, target:"ar"\|"en"}` → `{text, target}` |
| `GET /api/admin/aux-model` | admin | Masked settings |
| `PUT /api/admin/aux-model` | admin | Update. `jobs` is a partial map: sending one toggle leaves the others alone. |
| `POST /api/admin/aux-model/test` | admin | One real completion; latency plus the model's answer. Always `200` — a failure is reported in the body. |
| `POST /api/chats/{id}/title` | chat member | Rewrite this chat's title. Absent on a server with no auxiliary model. |
| `POST /api/projects/{id}/github/commit-message` | project member | Draft a commit subject. Always answers with a usable message. |

Translation is the one job with no silent fallback: it runs because a person
pressed a button, so an unavailable model is reported to them rather than
quietly doing nothing.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| **Test** says `connection refused` | Ollama is not running, or is not on that port | `systemctl status ollama`, `ss -lntp \| grep 11434` |
| **Test** says `model … not found` | The model was never pulled | `ollama pull qwen2.5:3b` |
| **Test** succeeds but titles never change | The chat-title toggle is off, or the title was edited by hand — the automatic pass only replaces a title that is still the exact truncation | Use the ↺ button in the chat header |
| Titles arrive seconds after the run finishes | Expected: the model is asked after the run settles, and a cold model loads first | Nothing to fix; the truncated title is shown meanwhile |
| Everything silently reverts to the old behaviour | The circuit breaker is open after three failures | Fix the endpoint, then press **Test** — a successful probe closes the breaker at once |
| The box starts swapping | A 3B model on a 4 GB host with running containers | Stop Ollama and use a remote endpoint, or add RAM |

## Related

- [Chat and agents](04-chat-and-agents.md) — the coding agents this does *not* replace
- [Notifications](07-notifications.md) — where the summarized sentence goes
- [GitHub integration](22-github-integration.md) — the commit dialog the Suggest button lives in
- [Snippets and client messages](21-snippets-and-client-messages.md) — the templates the Translate buttons work on
