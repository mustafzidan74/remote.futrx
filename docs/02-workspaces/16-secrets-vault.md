# Secrets vault

Project secrets solve one problem: this project needs this value. They do not
solve the other one — *every* project needs the GitHub token, the plugin
licence keys, the private registry credentials, and SSH access to the servers
this fleet deploys to. Re-entering those per project is how they drift, how one
gets missed, and how a rotation becomes an afternoon.

The **secrets vault** is the platform-level answer: one admin-only place whose
entries are injected into every container in their scope. It sits beside the
per-project store rather than replacing it — a project secret of the same name
always wins, and the vault reports the shadowing instead of hiding it.

- **UI:** Settings → **Secrets vault** (Platform group), admin only. Members see
  what their project inherits under Project → **Secrets**.
- **Storage:** `DATA_DIR/globalsecrets.json`, mode 0600, **plaintext**.
- **Code:** [`service/globalsecrets`](../../backend/internal/service/globalsecrets/),
  [`stores/fileglobalsecrets`](../../backend/internal/stores/fileglobalsecrets/),
  [`integration/containers/secrets`](../../backend/internal/integration/containers/secrets/),
  [`integration/sshprobe`](../../backend/internal/integration/sshprobe/),
  [`global_secrets_handler.go`](../../backend/internal/transport/http/handlers/global_secrets_handler.go).

## The three kinds

Every entry has a `key` (a POSIX environment-variable name: `[A-Za-z_][A-Za-z0-9_]*`),
a `kind`, a `scope`, an optional `description`, and `updatedAt` / `updatedBy`.
What the `kind` decides is *how the value reaches a container*.

| Kind | Reaches the container as | Notes |
| --- | --- | --- |
| `env` | LXD `environment.<KEY>` config, so every `lxc exec` session inherits it | The value **may not contain line breaks** — LXD rejects them in persistent config, so the vault refuses them up front rather than dropping the value silently. Use a `file` entry for PEM keys and JSON blobs. |
| `file` | A file written at a declared path, mode 0600 | The path must be absolute and under `/root` or `/workspace/.secrets/`. Typical destinations: `/root/.npmrc`, `/root/.composer/auth.json`. |
| `ssh` | A private key at `/root/.ssh/<name>_key` (0600) plus a `Host` block in `/root/.ssh/config` | The agent then simply runs `ssh <name>` or `rsync -e ssh … <name>:`. See [SSH targets](#ssh-targets). |

### Scope

`scope` is either `{"all": true}` — every project, present and future — or an
explicit `{"projectIds": [...]}` list. Narrow scopes matter: **anything in a
project's scope is readable by any agent running in that project**, and agents
run as root with safety rails off.

### Values are write-only

A `GET` never returns a value. It returns `masked` (`••••••••` plus the last
four characters) and `hasValue`. Consequently:

- A create requires a value.
- An update with a **blank** value keeps the stored one, so an admin can retitle
  an entry or change its scope without re-pasting a key.
- Removing a value takes an explicit `clear: true`. The entry survives with
  `hasValue: false` and stops being materialized on the next sync.
- `DELETE` removes the entry entirely.

## Injection and cleanup

Injection is an extension of the existing project env sync
([`syncContainerEnv`](../../backend/internal/service/project/service.go)), which
runs on project create, container start after a missing container, workspace
upgrade, trash restore, and every project-secret write:

1. The **vault converges first** — files, SSH material, and its own environment
   variables.
2. The **project's own secrets are applied second**, so for any key both define,
   the project is the last writer and wins.

An admin edit does not wait for that: creating, updating, or deleting an entry
kicks off a background re-sync of every **running** container in the affected
scope (both the old and the new scope, so narrowing one strips the entry from
the projects that just lost it). The pass logs
`secrets vault: resynced N running container(s), M failed`. A stopped container
converges on its next start.

### The manifest

Removal has to be exact — a `file` entry re-pointed at another path must not
leave the old file behind. Each sync therefore writes
**`/root/.remote-secrets.json`** (0600) inside the container:

```json
{
  "version": 1,
  "envKeys": ["GITHUB_TOKEN", "SSH_TARGET_HESTIA_HOST"],
  "files": ["/root/.npmrc", "/root/.ssh/hestia_key"],
  "ssh": ["hestia"]
}
```

The next sync reads it, computes what it owned then and does not own now, and
removes exactly that: stale files with `rm -f`, stale environment names with
`lxc config unset`. A container with no manifest (a fresh one, or one recycled
onto a new base image) is simply written fresh.

`/root/.ssh/config` and `/root/.ssh/known_hosts` are **shared** files, so the
vault owns only the region between its markers:

```
# BEGIN remote.futrx managed secrets vault
…
# END remote.futrx managed secrets vault
```

Anything an agent added to those files by hand survives a re-sync, and
regenerating from unchanged input is byte-identical.

## SSH targets

An `ssh` entry stores `{name, host, port, user, privateKey, knownHostsLine?}`
and materializes as:

- `/root/.ssh/<name>_key`, mode 0600, with a trailing newline (OpenSSH refuses a
  key file without one).
- One `Host` block per target, in name order:

```
Host hestia
    HostName 203.0.113.10
    User admin
    Port 22
    IdentityFile /root/.ssh/hestia_key
    IdentitiesOnly yes
    StrictHostKeyChecking accept-new
```

`StrictHostKeyChecking` is `accept-new` when no `knownHostsLine` is given (the
first connection is accepted and pinned), and `yes` when one is — the line is
written into the managed region of `/root/.ssh/known_hosts` and the host key is
verified against it from the first connection onwards.

### Environment contract

Every `ssh` target also publishes three environment variables, so a skill can
read connection details without parsing `~/.ssh/config`:

| Variable | Value |
| --- | --- |
| `SSH_TARGET_<NAME>_HOST` | the target's host |
| `SSH_TARGET_<NAME>_USER` | the login user |
| `SSH_TARGET_<NAME>_PORT` | the port (`22` unless overridden) |

`<NAME>` is the target name uppercased, with every character outside `A-Z0-9`
replaced by `_`. A target named `hestia-prod.eu` publishes
`SSH_TARGET_HESTIA_PROD_EU_HOST`, `…_USER`, `…_PORT`.

> **For skill authors.** Server-side skills such as `deploy-to-hestia` and
> `client-site-import` live on the operator's own server, not in this
> repository. To adopt the vault, point them at `ssh <name>` (the config block
> is already there) or at the `SSH_TARGET_*` variables above, instead of
> carrying their own host, user, port, and key path.

### Testing a target

`POST /api/admin/secrets/{key}/test` probes the target **from the host**, not
from a container, so an admin gets an answer while adding it — before any
project has been synced. The backend writes the private key to a temp file at
mode 0600, runs

```
ssh -i <tmp> -o BatchMode=yes -o ConnectTimeout=8 -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=<accept-new|yes> -p <port> <user>@<host> 'echo ok'
```

and deletes the temp file immediately. The response is
`{ok, output, latencyMs}`. A refused or unauthenticated connection is a normal
answer with `ok:false`, not an error; only a host with no `ssh` client answers
`503`. The key never appears in the output, and the temp path is redacted from
anything returned.

## API

Admin only. Every route answers `401` without a session and `403` for a member.

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/admin/secrets` | `{"secrets":[…]}` — masked entries with metadata and, for `env` entries, the `shadowedIn` project ids |
| POST | `/api/admin/secrets` | Create an entry; `409` when the key exists |
| PUT | `/api/admin/secrets/{key}` | Update an entry; blank value keeps the stored one, `clear:true` wipes it |
| DELETE | `/api/admin/secrets/{key}` | Remove the entry; scoped containers lose its material on the next sync |
| POST | `/api/admin/secrets/{key}/test` | Probe an `ssh` entry; `400` for any other kind |

Members read the vault indirectly, through the project route they already use:

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/projects/{id}/secrets` | `{"secrets":[…], "inherited":[{key, kind, source:"global", shadowed, path?, description?}]}` |

`inherited` is metadata only — no vault value ever reaches a project member —
and `shadowed` marks an `env` entry this project overrides with a secret of its
own.

## Audit

| Action | Recorded when |
| --- | --- |
| `settings.secret.create` | An entry is created |
| `settings.secret.update` | An entry is updated |
| `settings.secret.delete` | An entry is removed |
| `settings.secret.test` | An SSH target is probed |

The target is `{"type":"secret","id":"<key>"}`. Meta carries the kind, how wide
the scope is (`all` or `projects:<count>`), whether the write cleared the value,
and for a test the outcome and latency. **No value, path content, key material,
or command output is ever recorded.**

## Security notes

- **Plaintext at rest.** `globalsecrets.json` is mode 0600 and unencrypted —
  the same posture as `projectsecrets/<id>.json`, `notifications.json`, and
  `oauth.json`. Anyone with root on the host, or a host backup, has the values.
  See the [threat model](../threat-model.md).
- **Admin-only writes, fleet-wide blast radius.** One entry can reach every
  container on the box. Scope narrowly, and treat the vault as a
  production-credential store only if you already trust every agent running on
  this host.
- **Visible to agents.** An agent in a scoped project can read its own
  environment, `/root/.ssh/`, and any materialized file. An SSH target is
  therefore a standing grant of access to that server for every agent in scope.
  Prefer a dedicated, least-privileged deploy account and a per-target key.
- **Never logged.** The store, the container adapter, the SSH probe, and the
  handler are all written so no value, no pushed content, and no key path
  reaches a log line or an error message.
- **Container boundary unchanged.** Material is pushed with
  `lxc file push --mode=0600` into `/root` or `/workspace/.secrets/`; the vault
  cannot write anywhere else in a rootfs.

## Known gaps

- Values are not encrypted at rest, and there is no envelope key or KMS hook.
- A stopped container is converged on its next start, not on the vault edit, so
  a rotation is not instantaneous fleet-wide.
- `env` values may not span lines. Multi-line credentials belong in a `file`
  entry, which is materialized inside the container rather than exported into
  the environment.
- A key is immutable once created: renaming means delete + create, which is what
  makes cleanup on the container side unambiguous.
