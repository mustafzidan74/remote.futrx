# MCP servers

The **Model Context Protocol** is how an agent CLI talks to a tool it did not
ship with: the CLI starts (or dials) a small server, asks it what it can do,
and the model then calls those tools by name. An MCP server is what turns
"guess the schema of the client's database" into "read it".

This platform keeps a **registry** of those servers and writes the right
configuration into each project container before its next agent run, so a
server an admin adds once is available to every project in its scope without
anyone touching a container.

- **UI:** Settings → **MCP servers** (Agents & skills group), admin only.
  Members see and toggle what their project gets under Project → **Settings**
  → *MCP servers*.
- **Storage:** `DATA_DIR/mcpservers.json` (platform) and
  `DATA_DIR/projectmcp/<id>.json` (per project), both mode 0600.
- **Code:** [`service/mcp`](../../backend/internal/service/mcp/),
  [`stores/filemcp`](../../backend/internal/stores/filemcp/),
  [`integration/containers/mcp`](../../backend/internal/integration/containers/mcp/),
  [`mcp_handler.go`](../../backend/internal/transport/http/handlers/mcp_handler.go).

> **Security note, up front.** An MCP server started by an agent runs **inside
> that project's container, with the container's privileges** — root, network
> access to the LXD bridge, and the project's whole workspace. It can reach
> anything the project can reach. A server scoped to "all projects" is
> therefore reachable from every project on the box. Scope narrowly, prefer a
> read-only credential where the tool offers one, and treat adding an entry
> here the same way you would treat installing a package as root.

## An entry

| Field | Meaning |
| --- | --- |
| `name` | `[A-Za-z0-9][A-Za-z0-9_-]*`, max 48. It becomes part of the tool names the model sees, so keep it short and stable — renaming is not supported, because it would orphan the identifier every model already learned. |
| `transport` | `stdio` (a local child process) or `http` (a remote endpoint). |
| `command`, `args`, `env` | Used by `stdio`. |
| `url`, `headers` | Used by `http`. |
| `scope` | `{"all": true}` — every project, present and future — or `{"projectIds": [...]}`. |
| `enabledForProviders` | Which agent CLIs the entry is written for. Empty means every supported one. |
| `description` | One line, shown in both panels. |
| `secretRefs` | Secrets-vault keys whose values fill this entry's `${KEY}` placeholders. |

The fields the other transport uses are dropped on save, so a `stdio` entry
cannot smuggle a URL past the renderer and an `http` entry cannot carry an
environment block that would never be applied.

### Provider support

| Provider | MCP | How it is configured |
| --- | --- | --- |
| Claude Code | yes | `/root/.claude/mcp-servers.json`, handed to the CLI as `--mcp-config` |
| Codex | yes | the managed region of `/root/.codex/config.toml` |
| Kimi Code | **no** | nothing is written; the UI says so |
| Antigravity | **no** | nothing is written; the UI says so |

An entry that names an unsupported provider is refused on write, and the admin
table flags one that reached `DATA_DIR` some other way. Selecting an
unsupported provider is not silently ignored — the whole point is that an
operator is told rather than left wondering why a tool never appeared.

## Adding one

1. **Settings → MCP servers → Add server**, or pick one of the examples
   (Playwright, Postgres, MySQL, Fetch/HTTP, a custom HTTP endpoint). The
   examples are prefills only: nothing is enabled until you save it.
2. Fill in the command (or the URL), the agents it is for, and the **scope**.
3. If it needs a credential, put the credential in the
   [Secrets vault](16-secrets-vault.md) as an `env` entry, reference it in the
   entry as `${KEY}`, and tick that key in **Secret references**.
4. Save, then use **test** on the row: pick a project whose container is
   running and the platform performs one MCP `initialize` handshake inside it
   and shows the raw answer.

The configuration lands in a container **before its next agent run**, on the
same hook that refreshes skills. There is no separate "apply" step and no
container restart.

## Secrets

An entry never stores a credential. It stores a placeholder and the vault key
behind it:

```json
{
  "name": "postgres",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres",
           "postgresql://app:${PG_PASSWORD}@db.example.com:5432/app"],
  "secretRefs": ["PG_PASSWORD"],
  "scope": {"projectIds": ["proj-acme"]}
}
```

At materialization the platform asks the vault for exactly the keys this
project's enabled entries reference. Only `env` entries **whose scope covers
this project** resolve, so the registry can never widen what a project can
read beyond the environment its container is already given.

- A placeholder with no matching `secretRefs` entry is **rejected on write** —
  it would otherwise be written literally into a config.
- A `secretRefs` key that does not resolve for a project causes that entry to
  be **skipped for that project**, not half-written.
- The rendered configs are pushed with `--mode=0600`, never logged, and never
  returned by any API route.
- The **test** probe passes values through `lxc exec --env` rather than in the
  script body, and every resolved value is masked out of the output before it
  reaches the response.

Anything you type directly into `env` or `headers` instead of referencing the
vault is stored in `mcpservers.json` in plaintext and is readable through the
admin API. Use `${KEY}`.

## What lands in a container

### Claude Code

`/root/.claude/mcp-servers.json`, mode 0600:

```json
{
  "mcpServers": {
    "postgres": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-postgres", "postgresql://…"]
    }
  }
}
```

The run adds it to the CLI's `--mcp-config` flag. That flag is **variadic** —
it takes space-separated files — so when the Agent Browser is also enabled the
two configs share one flag rather than the second replacing the first, and the
group is always emitted last on the command line.

### Codex

The managed region of `/root/.codex/config.toml`, mode 0600:

```toml
# BEGIN remote.futrx managed MCP servers
[mcp_servers."postgres"]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-postgres", "postgresql://…"]

[mcp_servers."postgres".env]
"PGPASSWORD" = "…"
# END remote.futrx managed MCP servers
```

The rest of the file is left alone: the merge strips the previous region and
appends the current one, so anything the operator or the CLI put there
survives, and regenerating from unchanged input is byte-identical. Every
string is TOML-escaped, so a value containing a newline and a `[table]` header
cannot end the region and start a table of its own.

### The manifest

Removal has to be exact — a config whose last entry was deleted must not sit
in the container still advertising a tool. Each pass writes
**`/root/.remote-mcp.json`** (0600):

```json
{
  "version": 1,
  "signature": "9f2c…",
  "files": ["/root/.claude/mcp-servers.json"],
  "names": ["postgres"],
  "claudeConfig": "/root/.claude/mcp-servers.json"
}
```

The next pass reads it and does one of two things:

- **Signature matches** → nothing is written. The steady-state cost of a
  prompt is one `lxc exec cat`.
- **Signature differs** → the new configs are pushed, the codex region is
  re-merged, and every path the previous manifest owned that this one does not
  is removed with `rm -f`.

The signature covers the rendered bytes, resolved values included, so rotating
a vault value re-materializes the container instead of leaving the old
credential in place. A container with no manifest — a fresh one, or one
recycled onto a new base image — is simply written fresh.

## Per-project overrides

`DATA_DIR/projectmcp/<id>.json` holds three things: the names this project has
switched **off**, the entries only this project has, and the record of the
last materialization that the panel's "Materialized …" line reads.

Merge order is: platform entries whose scope covers the project, then the
project's own entries, which **shadow** a platform entry of the same name
outright — the same rule a project-local skill follows against a global one.

A project entry is editable by any project **member**, not only an admin. That
is deliberate and not a widening: an agent in that container already runs as
root and could start the same process by hand, and the vault keys such an
entry may reference resolve only to values the container already receives.

## API

Platform routes are admin-only; every one answers `401` without a session and
`403` for a member.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/admin/mcp-servers` | `{"servers":[…], "supportedProviders":[…], "unsupportedProviders":[…]}` |
| POST | `/api/admin/mcp-servers` | Create an entry; `409` when the name exists |
| PUT | `/api/admin/mcp-servers/{name}` | Update an entry; the name is immutable |
| DELETE | `/api/admin/mcp-servers/{name}` | Remove it; scoped containers lose its config on their next run |
| POST | `/api/admin/mcp-servers/{name}/test` | `{"projectId":"…"}` → `{ok, output, durationMs}`; runs the handshake inside that project's **running** container |

Project routes need membership, which the project route has already
established:

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/projects/{id}/mcp` | `{"available":[{…, source, enabled}], "materializedAt", "materializedNames", "supportedProviders", "unsupportedProviders"}` |
| PUT | `/api/projects/{id}/mcp` | `{"disabled":[…], "servers":[…]}` — the whole document, replacing what was there |

## Audit

| Action | Recorded when |
| --- | --- |
| `settings.mcp.create` | An entry is created |
| `settings.mcp.update` | An entry is updated |
| `settings.mcp.delete` | An entry is removed |
| `settings.mcp.test` | The probe is run |
| `settings.mcp.project-update` | A project's overrides are saved |

The target is `{"type":"mcp-server","id":"<name>"}`, or the project id for the
project action. Meta carries the transport and how wide the scope is
(`all` or `projects:<count>`) — never a command line, a URL, or a value.

## Limits and behaviour worth knowing

- Nothing here restarts a container or interrupts a running turn. A change
  takes effect on the **next** run of that project.
- A deployment without an MCP store answers `503` on these routes and writes
  nothing into any container; agent runs are unaffected.
- Materialization failure is logged and the run continues without the servers.
  Losing a tool is better than losing the prompt.
- The examples in the Add dialog are illustrative, not a curated catalogue.
  Package names come from their upstream projects and are not pinned or
  verified by this platform — the same supply-chain caveat as any `npx`.
