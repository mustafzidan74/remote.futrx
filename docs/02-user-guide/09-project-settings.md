# Project settings

Open a project's settings with the gear icon beside its name in the sidebar. The tabs are **Info**, **Settings**, **GitHub**, **Snapshots**, **Lighthouse**, **Visual**, **Secrets**, and **Sharing**.

![Project container information, agent versions, and credential-bundle state](/assets/docs/screenshots/01-project-container-info-00m37s.webp "The Info tab exposes the project computer rather than hiding it: runtime identity, tools, mounts, resources, network, and provider state are inspectable.")

## Info

Use **Info** to answer “what is actually running?”

The page can report:

- container state, type, image, architecture, PID, creation time, last use, and autostart;
- operating-system distribution, kernel, hostname, uptime, and CPU count;
- process count, CPU time, current and peak memory, swap, and disk usage;
- interfaces, IP addresses, traffic counters, MAC addresses, and MTU;
- workspace and provider-home mounts;
- installed Claude and Codex versions;
- agent instructions and credential-bundle freshness;
- effective CPU, memory, and root-disk limits.

Choose **Refresh** in the page header to collect a new snapshot.

### Repair project networking

Use **Repair network** when the container is running but has no usable IPv4 address or previews cannot reach it.

1. Open the project gear.
2. Select **Info**.
3. Find the network section.
4. Choose **Repair network**.
5. Refresh the page and confirm that a non-loopback address appears.

The host also runs a periodic IPv4 repair timer. Manual repair is the immediate recovery path.

## Settings

The **Settings** tab combines resource limits with project lifecycle controls.

![Per-project CPU, memory, disk, and lifecycle controls](/assets/docs/screenshots/02-project-resources-00m40s.webp)

### Set resource limits

Only an administrator can save project-specific limits.

1. Open **Settings**.
2. Read the **Resources** panel: the effective CPU, memory, and disk quota,
   whether each came from a project override or the fleet default, and usage
   bars showing what the container consumes against those limits.
3. Enter any overrides you need:
   - **CPU cores**;
   - **Memory**;
   - **Disk quota**.
4. Leave a field blank to inherit the fleet default. Each field shows the fleet
   maximum you cannot exceed.
5. Choose **Save limits**.

**Reset to defaults** removes the project overrides. CPU and memory apply live, and lowering memory can terminate container processes. A disk-quota change on a running container is recorded and takes effect on the next restart, which the panel says explicitly. A root-disk quota cannot be smaller than data already stored.

The fleet default envelope is set by an administrator in **Settings → Resources** and derived from this host's capacity on first run. The disk quota covers the container root disk only, not the durable workspace or provider-home bind mounts, and needs a storage pool that can enforce quotas — the panel says so when yours cannot. See [Resource limits](../02-workspaces/11-resource-limits.md).

### Start, stop, and force-restart

| Control | When to use it | What happens |
| --- | --- | --- |
| **Start project** | The project is stopped, missing, or in error | Remote converges mounts and limits, starts or recreates the container, and runs launch provisioning. A start is refused when running containers would commit more memory than the host has; an administrator then sees **Start anyway (oversubscribe the host)** |
| **Stop project** | You want to release active compute | The container stops; durable workspace and provider homes remain |
| **Force restart** | A process is wedged or the project hit its limits | The host kills project processes and boots the container again |

Starting a missing project can recreate its container from the current base image. Files in `/workspace` and provider homes remain; ad-hoc root-filesystem changes do not.

### Delete a project

Only an administrator can delete a project.

1. Confirm that important work is committed and backed up.
2. Open **Settings**.
3. Choose **Delete project**.
4. Read the confirmation carefully and approve it.

Deletion removes the container, project metadata, project access list, secret store, durable workspace, and provider homes. Chat records are stored separately and the backend does not reliably cascade-delete every chat that referenced the project. Do not treat the sidebar confirmation as a backup or complete data-retention policy.

## Secrets

Use **Secrets** for project-scoped environment values such as API tokens, webhook secrets, JSON credentials, or PEM material.

![Project secret editor with support for multiline values](/assets/docs/screenshots/21-project-secrets-17m20s.webp)

### Add a secret

1. Open **Secrets**.
2. Enter a key such as `API_TOKEN`.
3. Enter the value. Multiline values are supported.
4. Choose **Add**.
5. Restart any already-running application that must read the new value.

Keys must match:

```text
[A-Za-z_][A-Za-z0-9_]*
```

### Reveal, edit, or delete

- Choose **show** or **hide** to control the visible value.
- Choose **edit**, change the value, then **Save**.
- Choose the delete icon and confirm to remove it.

### Where values go

The current backend:

- stores the authoritative secret file on the host in plaintext with mode `0600`;
- passes project secrets explicitly to agent runs, except that Codex clears or does not forward `OPENAI_API_KEY` so it uses subscription authentication;
- writes all values to managed `/workspace/.env` with mode `0600`;
- also puts single-line values into LXD `environment.KEY` for new inherited processes;
- omits multiline values from LXD config, while still passing them to agents and `.env`.

Propagation after a change is best-effort. A stale copy can remain after a partial failure, and an already-running process keeps its old environment. Restart affected processes and verify from inside the project.

> Project members can read, reveal, change, and delete project secrets. Agents can read them during execution. Sharing the project shares this authority.

The current Secrets UI contains older helper text claiming values never land in the container filesystem. That statement is no longer accurate; the behavior above reflects the current backend.

## Lighthouse

Use **Lighthouse** to measure this project's own pages — the real Lighthouse, running inside the container, with no API key and no rate limit.

1. Pick a preview port and list up to six page paths.
2. Choose **mobile** or **desktop**. Mobile is the default because it is what Google ranks on.
3. Choose **Audit now**.

Each page reports the four category scores, the Core Web Vitals, and the failing audits worth acting on — with the change since the last run of the same page at the same form factor beside each score. A dash means Lighthouse could not compute that category; it is not a zero.

A container built before Lighthouse shipped in the base image offers a one-off **Install Lighthouse here** button. It takes about a minute and only has to happen once.

## Visual

Use **Visual** to see what a change actually did to the pages you were not looking at.

1. Before the work, list up to twelve page paths and choose **Take baseline**.
2. Make your change.
3. Choose **Compare now**.

Every page reports how far it moved, worst first, with **before**, **after**, and a **diff** overlay that paints the changed pixels red over a faded copy of the page. A page nobody touched reports 0%.

Taking a new baseline discards the comparisons below it: they measure against the old pictures. Re-baseline when you have accepted the change as correct.

> Dynamic content — a carousel, a relative timestamp, a randomised testimonial — differs between any two page loads and will show as changed. Leave those pages out, or read the diff image rather than the number.

## Sharing

Use **Sharing** to decide which registered users can enter this project.

### Add a member

1. Ask an administrator to add the person's email in **Settings → Users** first.
2. Open the project gear.
3. Select **Sharing**.
4. Enter the exact registered email.
5. Choose **Add**.

The member can now use the project's chats, uploads, terminal, files, preview, Agent Browser, and secrets.

### Remove a member

1. Open **Sharing**.
2. Find the email.
3. Choose the remove icon.
4. Confirm removal.

Any current project member can add or remove registered members. A non-admin cannot remove the final project member. Administrators bypass the membership list and can recover access.

### Client portal

The **Client portal** panel publishes a public, read-only status page for this
project — project name or brand title, running/stopped, the preview ports that
already have a live public link, the last 15 commits grouped by day, and a note
you write. It is for the person paying for the work, not for a teammate.

1. Tick what the client should see. *Show activity* (run counts, never costs) is
   off by default.
2. Optionally set a brand title and a note. The note keeps line breaks and
   nothing else; Arabic text flips the whole page to right-to-left.
3. Choose **Enable client portal** and copy the link — it is shown **once**.

**Rotate link** issues a new link and stops the old one immediately.
**Disable** closes the portal; re-enabling later always produces a fresh link.
Anyone holding the link can open the page, so treat it like a password. The
portal never exposes the workspace, the IDE, the Agent Browser, chats, or
secrets. See [Client portal](../02-workspaces/14-client-portal.md).

## Project-settings permissions

| Action | Admin | Project member |
| --- | ---: | ---: |
| View Info and refresh diagnostics | Yes | Yes |
| Repair network | Yes | Yes |
| Start, stop, or force-restart | Yes | Yes |
| Read or change secrets | Yes | Yes |
| Add or remove project members | Yes | Yes |
| Enable, rotate, or disable the client portal | Yes | Yes |
| Save or reset resource limits | Yes | No |
| Delete project | Yes | No |

For storage and lifecycle details, see [Projects and containers](../02-workspaces/03-projects-and-containers.md). For security implications, see the [Threat model](../threat-model.md).
