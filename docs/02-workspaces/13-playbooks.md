# Playbooks

A **playbook** is a saved prompt plus the chat configuration it needs: the
skills to preselect, and optionally the mode and provider to switch to. One
click in the composer applies all of it and loads the prompt, so a routine job
— a security review, an update-and-test pass, a delivery hand-off — is the
same every time and takes no typing.

The library is server-wide and admin-owned. Every signed-in user sees the same
entries; only administrators can change them.

## Why this exists

The same handful of prompts get retyped in every project, slightly differently
each time, with the right skills selected only when someone remembers. A
playbook makes that prompt an artifact: it is written once, reviewed once, and
runs identically in every chat and for every member.

## Storage

| Path | Contents |
| --- | --- |
| `DATA_DIR/playbooks.json` | The whole library, a JSON array, mode 0600 |

One entry:

```json
{
  "id": "security-review",
  "title": "🔒 Security review",
  "icon": "🔒",
  "hint": "Read-only audit of the workspace with the guard skills.",
  "prompt": "Review the code in /workspace for security …",
  "skills": [{ "name": "wp-guard", "command": "wp-guard", "source": "global" }],
  "mode": "review",
  "provider": "",
  "order": 0
}
```

| Field | Rule |
| --- | --- |
| `id` | Lowercase letters, digits, and dashes; unique; at most 64 characters |
| `title` | Required, at most 120 characters |
| `icon` | Optional emoji, at most 8 characters |
| `hint` | Optional one-liner shown under the title in the menu, at most 200 characters |
| `prompt` | Required, at most 8000 characters |
| `skills` | At most 20 refs; `{name, command, provider, source}`, deduplicated |
| `mode` | Empty, or one of `chat`, `plan`, `code`, `review`, `debug`, `full-auto` |
| `provider` | Empty, or one of `claude`, `codex`, `kimi`, `antigravity` |
| `order` | Renumbered to the array index on every write |

An empty `mode` or `provider` means "leave the chat as it is". The library is
capped at 50 entries.

Writes are whole-document: the admin page submits the full array, which is how
deleting and reordering work without extra verbs. Persistence uses the same
temp-file + rename discipline as the other JSON stores.

## Seeding

On the first start that finds no `playbooks.json`, the service writes seven
built-in playbooks and logs how many. That is the only time it writes on its
own:

- an existing document is never rewritten, even after an upgrade;
- a library an admin deliberately emptied stays empty — "no document" and "an
  empty document" are different states in the store, on purpose.

The seeded set covers security review, update-and-test on the staging copy,
delivery preparation, Hestia import and deploy, live-site audit, and writing
end-to-end tests. Titles carry an emoji, and prompts are written in English
because every provider CLI (Claude, Codex, Kimi, Antigravity) follows English
instructions most predictably.

Seeded prompts reference skills by command name — `wp-guard`,
`playwright-e2e`, `deploy-to-hestia`, and so on. Those are the operator's
[global skills](09-global-skills.md), not built-ins: a playbook may name a
skill this server has not published yet. That is deliberate — the reference
becomes live the moment the skill is installed — so the API accepts it and the
admin page flags it instead.

## Placeholders

A prompt may carry `{{placeholder}}` tokens. The client resolves them against
the chat when a playbook is run:

| Token | Resolves to |
| --- | --- |
| `{{project}}` | The chat's project name |
| `{{slug}}` | The project slug (its container name) |
| `{{previewUrl}}` | The most recent preview URL this chat mentioned |

Anything else — and any of the above that has no value right now, such as
`{{previewUrl}}` in a chat where no preview has appeared — is **left in the
text verbatim**. The composer then selects the first unfilled token so the
user types over it, and shows what still needs filling in.

A prompt in that state is never sent automatically, whichever way it was
clicked. `{{askUrl}}` in the seeded "Audit live site" playbook exists exactly
for this: it has no resolver and never will, so that playbook always stops for
the URL.

## Running one

The composer's ⚡ **Playbooks** button lists the library. Clicking an entry:

1. builds a chat-meta patch — the playbook's skills merged into the current
   selection, plus its mode and provider when it names them — and applies it
   through the normal `PATCH /api/chats/{id}` path, so the change is visible in
   the composer's own controls;
2. resolves the prompt's placeholders;
3. **inserts** the prompt into the composer.

Insert is the default because the user should see what is about to run.
**Shift-click** sends immediately instead — unless a placeholder is unresolved,
in which case it still only inserts.

Two details worth knowing:

- **Skills merge, they do not replace.** Whatever was already selected stays,
  except across a provider switch: a playbook that changes the provider clears
  the previous provider's skills, exactly as the composer's own provider toggle
  does, because those skills are no longer loadable.
- **A half-typed draft survives.** An empty composer is replaced by the prompt;
  a composer with text in it gets the prompt appended after a blank line.

## Editing the library

**Settings → Playbooks** (admin only) edits the whole library: title, emoji,
one-line hint, prompt, a skill multi-select drawn from the same catalog the
composer shows, mode, provider, and ordering. Nothing is written until **Save
library**; **Discard changes** restores the stored document.

The page flags any skill reference this server does not currently publish, and
refuses to save an entry with no title or no prompt.

Non-admins see the tab with a note that the library is curated by
administrators — the playbooks themselves are still available to them from the
composer.

## API

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/playbooks` | The library; any signed-in user |
| GET | `/api/admin/playbooks` | The same list through the admin route; admin only |
| PUT | `/api/admin/playbooks` | Replace the whole library, `{"playbooks":[…]}`; admin only |

Both read routes answer `{"playbooks": [...]}` ordered by `order`. A rejected
document answers `400` with the specific reason. A successful `PUT` records a
`settings.playbooks.update` entry in the [audit log](../04-operations/10-audit-log.md).

## Where the code lives

| Concern | Path |
| --- | --- |
| Model, validation, seed | `backend/internal/service/playbooks/` |
| Persistence | `backend/internal/stores/fileplaybooks/` |
| HTTP routes | `backend/internal/transport/http/handlers/playbook_handler.go` |
| Placeholder and patch logic | `frontend/src/state/chat/playbookState.ts` |
| Composer menu | `frontend/src/ui/chat/composer/PlaybookPicker.tsx` |
| Admin editor | `frontend/src/ui/settings/PlaybooksSettings.tsx` |
