# Snapshots and Trash

Deleting a project used to be instant and final: the container was destroyed,
`/var/lib/remote/projects/<slug>` was removed, and the metadata was gone. For a
WordPress project that meant losing the install *and* its database in one
click, with nothing to fall back on.

This page describes the two mechanisms that replaced that: **snapshots**, a
point-in-time archive of a project you can roll back to, and the **Trash**, a
retention window that makes a delete reversible.

## What a project's durable state actually is

Two places, and that split is why a snapshot is more than a `tar` of one
directory:

| State | Lives in | Survives a container replacement |
| --- | --- | --- |
| `/workspace` | host bind mount `/var/lib/remote/projects/<slug>/workspace` | yes |
| Agent homes (`.claude`, `.codex`, `.kimi-code`) | host bind mount `/var/lib/remote/projects/<slug>/agent-home/*` | yes |
| Template database (MariaDB for the WordPress stack) | container rootfs | **no** |
| Anything else installed in the rootfs | container rootfs | no |

The WordPress template installs MariaDB inside the container and keeps its data
there ([Project templates](08-project-templates.md)). An archive that only
covered the host directories would restore a WordPress installation with an
empty database, so a snapshot takes a logical dump through the container
runtime and puts it in the same archive.

## Snapshots

### What one contains

```
<snapshot>.tar.zst
├── workspace/        the project's /workspace tree
├── agent-home/       the per-provider agent homes
├── db.sql            logical dump, only when the container has a dump tool
└── meta.json         manifest: project identity, template, label, contents
```

`meta.json` carries enough context to identify an archive without `DATA_DIR`,
which a disaster recovery may not have: project id and name, slug, container
name, template, label, who took it and when, and which directories are inside.

**Secrets are not included by default.** An archive is a plain file an operator
may copy off the box, so the project's secret values are written into the
manifest only when the caller passes `includeSecrets`. A restore never
re-applies them — the live secret store is authoritative.

### Where they live

`/var/lib/remote/snapshots/<projectId>/<timestamp>-<snapshotId>.tar.zst`, mode
`0700` on the directory and `0600` on the archive. `zstd` is used when the host
has the binary and `gzip` otherwise; the stored `format` field records which.
The archives sit next to the workspaces they were taken from rather than under
`DATA_DIR`, because a multi-gigabyte workspace has no business in the metadata
directory.

The index — which snapshots exist, how far each one got, and which archive
backs it — is a small JSON file at `DATA_DIR/snapshots/<projectId>.json`.

### Taking one

Packing a large workspace is minutes of work, so it runs in the background:

1. The record is written with status `pending` and returned immediately (`202`).
2. The background run marks it `running`, dumps the database if the container
   has `mysqldump` or `pg_dumpall`, renders the manifest, and invokes `tar`.
3. On success the record becomes `ready` and gains its archive name, format and
   size; on failure it becomes `failed` and carries the cause.
4. Retention runs: the newest **10** records per project are kept and the
   archives of the rest are deleted. A record that has not settled is never
   evicted, because its archive may still be being written.

**One operation per project at a time.** A second capture, or a restore, while
one is running answers `409` — a restore that swapped directories out from
under a running `tar` would corrupt both.

A failed dump is not a failed snapshot. A stopped container, or a template with
no database at all, logs and continues: the files are still worth archiving.

### Restoring one

Also a background job, and also sequenced against the container lifecycle:

1. Stop the container, so nothing writes into `/workspace` while the directory
   underneath it is replaced.
2. Expand the archive into a staging directory. A truncated archive fails here,
   before anything live has been touched.
3. Move the current `workspace` and `agent-home` to
   `/var/lib/remote/snapshots/<projectId>/.pre-restore-<timestamp>/`. The
   replaced tree is kept, not destroyed — restoring the wrong snapshot has to be
   recoverable too.
4. Move the archive's directories into place and re-chown them to uid/gid
   `1000000`, the unprivileged-container idmap, through the same host adapter
   the launch path uses.
5. Start the container, then import `db.sql` over the current database if the
   archive has one.

If step 2 or 3 fails, the project is started again on whichever tree is on
disk rather than left stopped.

> The `.pre-restore-*` directories are never cleaned up automatically. They are
> the undo of an undo; delete them yourself once you are sure.

## Trash

`DELETE /api/projects/{id}` no longer destroys anything irreversibly:

1. Dump the database — this has to happen while the container still exists.
2. Destroy the container (the rootfs is disposable by design).
3. Move `/var/lib/remote/projects/<slug>` to `/var/lib/remote/trash/<projectId>/`.
   On a normal layout the two roots share a filesystem, so this is a rename and
   effectively instant.
4. Record an automatic snapshot of the trashed copy, with the dump from step 1,
   in the background.
5. Mark the metadata `deletedAt`.

A trashed project keeps its metadata but is excluded from every live listing:
the sidebar, `GET /api/projects`, and the reconcile sweep all skip it, and the
workspace WebSocket publishes it as a deletion so open browsers drop it
immediately. `GetBySlug` answers "not found", which means a trashed slug can no
longer mint a TLS certificate through `/internal/tls-ask` or resolve a public
share link. Start, stop, restart and recycle all answer `409` with "project is
in the Trash".

### The slug is still reserved

The slug **is** the container name and the DNS label its previews answered on.
A new project that would take a trashed project's slug is refused with

```
a project with this name is in the Trash - restore or purge it before reusing the name
```

rather than being silently renamed, because renaming the trashed project would
change the name it is restored under. A collision with a *live* project keeps
the old behaviour of appending `-2`, `-3`, and so on.

### Restore

Moves the directories back, clears `deletedAt`, re-creates the container
through the normal ensure path (same template, same resource overrides, secrets
re-pushed), and re-imports the database from the automatic snapshot. It is not
admin-only: whoever is a member can undo their own accidental delete.

A restore is refused with `409` while the automatic trash snapshot is still
packing, because that snapshot reads from the very directory the restore wants
to move.

### Purge, and the janitor

`DELETE /api/projects/{id}/purge` (admin) is the permanent one: the trashed
directories, every snapshot archive, the membership list, the secrets and the
metadata all go.

A janitor sweeps every 6 hours and purges anything past the retention window.
The default is **7 days**, overridable with `TRASH_RETENTION_DAYS`; `0` disables
the sweep entirely, so trashed projects are kept until an admin purges them.
The sweep skips a project whose snapshot is still being written.

## API

Every route below first requires admin status or project membership.

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/projects/{id}/snapshots` | `{snapshots, jobs}` — records newest first, plus the recent background jobs |
| `POST` | `/api/projects/{id}/snapshots` | `{label?, includeSecrets?}` → `202` with a pending record |
| `POST` | `/api/projects/{id}/snapshots/{sid}/restore` | admin, or a member sending `{"confirm":true}` → `202` with a job |
| `DELETE` | `/api/projects/{id}/snapshots/{sid}` | drops the record and its archive |
| `DELETE` | `/api/projects/{id}` | admin — moves the project to the Trash |
| `GET` | `/api/projects/trash` | admins see every trashed project, members see theirs; each row carries `deletedAt` and `expiresAt` |
| `POST` | `/api/projects/{id}/restore` | admin or member |
| `DELETE` | `/api/projects/{id}/purge` | admin — permanent |

Status codes worth knowing: `409` for "busy", "already/not in the Trash", and a
slug reserved by the Trash; `400` for a restore a member did not confirm and for
a label over 80 characters; `503` when the host has no `tar`.

Jobs live in memory, so the list is empty again after a backend restart. The
snapshot **records** are durable and carry their own status, which is what the
UI actually reads.

## Where it is in the UI

- **Project settings → Snapshots** — take one with a label, see what each holds,
  restore, delete. The list polls itself while anything is running.
- **Settings → Trash** — deleted projects with who deleted them, when, and how
  many days are left; Restore for members, Purge for admins.
- The delete confirmation now says the project moves to Trash for 7 days.

## Audit

`snapshot.create`, `snapshot.restore`, `snapshot.delete`, `project.trash`,
`project.restore` and `project.purge` are recorded in the
[audit log](../04-operations/10-audit-log.md). A retention purge is recorded too,
with `reason: retention` and no actor — the janitor is the server acting on its
own.

## Code map

| Concern | Where |
| --- | --- |
| Snapshot policy, background jobs, retention | `backend/internal/service/snapshot/` |
| Snapshot index | `backend/internal/stores/filesnapshot/` |
| `tar` and the trash directory moves | `backend/internal/integration/hostarchive/` |
| In-container dump/import | `backend/internal/integration/containers/database/` |
| Trash state machine, janitor | `backend/internal/service/project/trash.go` |
| HTTP routes | `backend/internal/transport/http/handlers/project_snapshots_handler.go`, `project_trash_handler.go` |
| UI | `frontend/src/ui/projects/project-containers/ProjectSnapshotsSection.tsx`, `frontend/src/ui/settings/TrashSettings.tsx` |

## Limits

- Retention is a compiled constant (10 snapshots per project); only the Trash
  window is configurable.
- Chats are not part of a snapshot and are not restored with a project.
- Snapshots are local files on the same host and the same disk as the
  workspaces they protect. They are a rollback mechanism, not off-site backup —
  pair them with `remote-backup` or your own copy of `/var/lib/remote`.
- `.pre-restore-*` stashes accumulate until you remove them.
