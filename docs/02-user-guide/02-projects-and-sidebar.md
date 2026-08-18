# Projects and the sidebar

A project is the normal unit of work in Remote. It combines a durable workspace,
provider state, membership, secrets, and an isolated LXD container in which
agents, terminals, the IDE, browsers, and development processes run.

## Create a project

1. In the sidebar, select **New project**. On an empty workspace, you can also
   select the **New project** button in the center of the page.
2. Enter a name in the browser prompt **Project name?**.
3. Confirm the prompt.
4. Wait while the project shows its provisioning state.
5. When provisioning finishes, select **New chat in this project** or expand
   the project and select **New chat**.

![Creating a project from the workspace](/assets/docs/screenshots/create-project.webp)

**Outcome:** Remote creates a unique project slug, durable workspace and agent
home directories, a project membership record, and a container based on the
server's development image. Duplicate names receive distinct slugs.

The new-chat control is disabled and shows a spinner while the project is still
provisioning. If provisioning fails, the sidebar displays the project error.
Open **Container info** to inspect the failure or retry lifecycle actions.

## Understand the sidebar

The expanded sidebar is headed **Workspace** → **Remote**, followed by a
**Home** row and then the project list. Selecting the heading or the **Home**
row opens the [Home dashboard](#the-home-dashboard). Each project row
contains:

- an **Expand** or **Collapse** control;
- an unread indicator when one of its chats has unread output;
- **Container info**, shown as a settings control;
- the project name, slug, and chat count; and
- **New chat in this project**.

Each chat row shows its title, model label, last activity time, and one of a
ready, running, or unread indicator. Hover a row on desktop to reveal **Mark
read** or **Mark unread**, **Fork from last message**, and **Delete chat**.

Use **Search projects and chats** to filter both project names and chat titles.
Select **Clear search** to return to the full list. Projects can be dragged into
a new order only while search is empty. Use **Collapse sidebar** to keep a
compact project rail on desktop; use **Open chats** or **Toggle sidebar** on a
small screen.

![Projects, chats, and concurrent agent activity in the sidebar](/assets/docs/screenshots/parallel-agents.webp)

## The Home dashboard

Home is the screen that answers "what is happening on this server?" without
opening anything. It is what you land on when no chat is selected, and you can
return to it at any time by selecting **Home** in the sidebar, selecting the
**Workspace** heading above it, or pressing <kbd>Ctrl</kbd>+<kbd>K</kbd>
(<kbd>Cmd</kbd>+<kbd>K</kbd> on macOS) and choosing **Home**.

A workspace with no projects yet keeps the three-step welcome card instead —
there is nothing for a dashboard to describe until the first project exists.

Home shows the following, all scoped to what you are allowed to see. A project
member's Home never names a project, chat, run, or scheduled task they could
not already open themselves.

| Panel | What it answers |
| --- | --- |
| The four tiles | How many projects are running, how many runs and how much estimated spend in the last seven days (with the change against the seven before), and how many things need attention |
| Platform strip | Whether the platform reports itself healthy, the state of the store, LXD, and Caddy checks, how much of the host container-memory budget is already committed, and when the host was last backed up |
| **Projects** | One card per project: health dot, status, a port chip when its app is listening, when it was last active, and quick actions for the latest chat, the preview, and a snapshot |
| **Attention** | Everything worth a decision, worst first, each with the one control that resolves it |
| **Recent activity** | Turns in flight, then the last completed runs with their token count and cost. Selecting a row opens the chat |
| **Upcoming** | The next armed scheduled tasks with a live countdown, how the previous run went, and **Run now** |
| **Usage** | Seven daily bars of cost or tokens. Selecting the arrow opens the full [Usage and cost](../02-workspaces/10-usage-and-cost.md) report |

Home refreshes itself every 60 seconds while the tab is visible, and once
immediately when you switch back to it. **Refresh** forces one now.

### What Attention reports

| Finding | Fix offered |
| --- | --- |
| A project the health monitor rates degraded or critical | **Open project** |
| The platform's own `/healthz` roll-up is degraded | **Open monitoring** |
| A trashed project within two days of being purged | **Restore from trash** |
| A running project never snapshotted, or not snapshotted in seven days | **Snapshot now** |
| An autopilot loop still running in a chat | **Open chat** |
| 90% or more of the host container-memory budget already committed | **Open resources** |
| Notifications switched off, so nothing announces a finished run or a degraded project | **Enable notifications** |
| No completed host backup in two days, or none ever | None — check `remote-backup.timer` on the host |

Two cases are deliberately silent rather than noisy. A host that never
installed the backup step has no backup directory, so it is never accused of
missing backups; and a project whose snapshot list could not be read is left
alone rather than reported as never snapshotted.

Panels degrade one at a time. A server with no usage ledger shows an
unavailable Usage card — which is not the same claim as zero spend — while
every other panel still works.

## Start work in a project

1. Expand the project.
2. Select an existing chat, or select **New chat**.
3. Choose the agent controls for the task.
4. Send a prompt.

One prompt run is allowed per chat, but separate chats can run concurrently.
The running spinner and unread indicators let you leave a long-running chat,
work elsewhere, and return when output is available.

For agent selection, continue with
[Chat and agent controls](03-chat-and-agent-controls.md). For prompting and
parallel conversation patterns, see
[Prompts, context, and conversation](04-prompts-context-and-conversation.md).

## Open project settings

Select **Container info** beside the project name. The project page has four
sections:

| Section | What it provides | Who can use it |
| --- | --- | --- |
| **Info** | Container state, OS, resources, disks, network, workspace mount, agent versions, credential freshness, and network repair | Project members and administrators |
| **Settings** | Effective CPU, memory, and root-disk limits; **Start project**, **Stop project**, **Force restart**, and **Delete project** | Members can use lifecycle controls; only administrators can change limits or delete |
| **Secrets** | Add, reveal, edit, and delete environment secrets | Project members and administrators |
| **Sharing** | Add or remove registered users | Project members and administrators, subject to final-member safeguards |

Select **Refresh** to reload project information and **Chats** to return to the
workspace.

### Lifecycle outcomes

- **Stop project** stops the container but preserves the durable workspace and
  provider homes.
- **Start project** starts the existing container or recreates it if it is
  missing.
- **Force restart** immediately kills processes inside the container and starts
  it again. Use it to recover a workspace that is unresponsive or at a resource
  limit.
- **Delete project** destroys the container and removes the project's durable
  workspace, provider homes, secrets, access list, and metadata.

Files under `/workspace` and provider homes survive ordinary stop, start,
restart, and image replacement. Packages or files installed elsewhere in the
container root filesystem do not survive container replacement.

> Project deletion is destructive. The current backend does not reliably
> cascade-delete separately stored chat records that refer to the project, even
> though a confirmation may describe chats as being removed. Export or commit
> needed work first, and do not treat chat history as a backup of the workspace.

## Work across projects in parallel

Projects isolate filesystem and process state from one another. A useful
workflow is:

1. Start an implementation in one project chat.
2. Open another project or chat from the sidebar.
3. Start an independent review, research, or test task.
4. Watch running and unread indicators.
5. Return to each chat to review the result.

Concurrency is bounded by the parent server's CPU, memory, storage, provider
limits, and any per-project resource limits. It is not unlimited.

## Loose chats are outside project isolation

When at least one project exists, the no-chat screen also offers **Loose chat**.
Loose chats appear under **Unassigned** and do not have a project container,
project terminal, preview, or project-specific workspace tools.

**Security warning:** the approval-free provider CLI for a loose chat currently
runs directly as the backend service user on the parent host—root in the
production systemd service—with access to the host environment and filesystem.
Use project chats for ordinary work. Reserve loose chats for fully trusted
administrative use until this boundary is redesigned.

## Architecture references

- [Projects and containers](../02-workspaces/03-projects-and-containers.md)
- [System overview](../01-overview/01-system-overview.md)
- [Workspace tools](../02-workspaces/05-workspace-tools.md)
- [Known limitations](../known-limitations.md)
