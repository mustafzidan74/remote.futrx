# Snippets and client messages

A **snippet** is a piece of text one user saved to reuse. There are two kinds,
and they live in the same per-user library:

- an **agent snippet** — a prompt, inserted into the chat composer;
- a **client template** — a message for a human, written in Arabic and English,
  used from a project's **Message client** panel.

Playbooks ([13-playbooks.md](13-playbooks.md)) are the operator's shared,
admin-curated prompts. Snippets are the opposite half of the same idea: private,
self-serve, and never visible to another member.

## Why this exists

The prompts a person actually reaches for a dozen times a day are personal —
"fix the PHP fatal in this plugin", "explain this in Arabic for the client" —
and are not worth an admin round trip. The messages a freelancer sends clients
are equally repetitive and considerably more embarrassing to retype badly at
midnight. Both are text with a few holes in it, so both get the same store, the
same placeholder rules, and the same editor.

## Storage

| Path | Contents |
| --- | --- |
| `DATA_DIR/snippets/<owner>-<digest>.json` | One user's whole library, a JSON array, mode 0600 |

The filename is the owner key in a filesystem-safe form plus a short SHA-256
digest of the exact key, so the file is readable in a backup and two owners can
never collide. The owner key is `sub:<subject>` for a Google session and
`email:<address>` for local sign-in — the same rule the per-user settings store
uses.

One entry:

```json
{
  "id": "wp-fix",
  "title": "WordPress fatal",
  "body": "Read the PHP error log in /workspace and fix the fatal. {{selection}}",
  "audience": "agent",
  "variants": { "ar": "", "en": "" },
  "tags": ["wordpress"],
  "shortcut": "wpfix",
  "createdAt": 1755500000000,
  "updatedAt": 1755500000000,
  "uses": 12
}
```

| Field | Rule |
| --- | --- |
| `id` | Generated from the title; lowercase letters, digits, and dashes, unique per user, at most 64 characters. `import` and `export` are reserved because the routes use them |
| `title` | Required, at most 120 characters |
| `body` | The text of an agent snippet, at most 8000 characters |
| `audience` | `agent` (default) or `client` |
| `variants` | `{ar, en}` for a client template, each at most 8000 characters |
| `tags` | At most 10, lowercased and deduplicated |
| `shortcut` | Optional; the word `/s-<shortcut>` types. Lowercase letters, digits, and dashes, unique per user. `/`, `s-`, and mixed case are accepted on input and stored bare |
| `createdAt`, `updatedAt`, `uses` | Server-owned. An edit moves `updatedAt`; an insertion moves `uses` and nothing else |

A snippet needs at least one of `body`, `variants.ar`, `variants.en`. The
library is capped at 200 entries per user.

## Seeding

The first read of a user's library writes four bilingual client templates:
**Site is live**, **Delivery note**, **Request for credentials**, and
**Quotation summary**. Seeding is per user and happens once: a library that
exists — including one the user has deliberately emptied — is never re-seeded,
because the document's existence is what "already seeded" means.

Personal prompts are not seeded. Guessing at somebody's prompts would only
produce four entries to delete.

## Placeholders

Both kinds of text may carry `{{placeholder}}` tokens. They are resolved in the
browser, against whatever the current screen knows:

| Token | Resolves to |
| --- | --- |
| `{{project}}`, `{{projectName}}` | The project's name |
| `{{clientName}}` | The client's name, defaulting to the project name |
| `{{slug}}` | The project slug |
| `{{previewUrl}}` | The newest public preview link, when one is live |
| `{{portalUrl}}` | The client portal link — known only in the session that minted it |
| `{{date}}` | Today, in the reader's locale |
| `{{selection}}` | Whatever is currently in the composer |

A token that cannot be filled in **stays visible** — verbatim, in place — and
the composer selects the first one so the user completes it themselves. This is
the same rule playbooks follow, and the reason a half-resolved prompt is never
sent automatically. An unknown name is left alone too: a snippet that mentions
`{{invoiceNumber}}` is not silently emptied.

`{{selection}}` is the one token that changes how insertion works: a snippet
that names it **replaces** the draft rather than appending to it, because the
draft is already embedded in the result.

## Where snippets appear

### Composer → Snippets

The 📄 **Snippets** pill sits beside ⚡ Playbooks. It lists the user's agent
snippets, most used first, with search over titles, shortcuts, tags, and bodies.

- Clicking one inserts it. It never sends: a snippet is a starting point.
- Each row has edit and delete.
- The footer offers **Save this draft** (or **New snippet** when the composer is
  empty), **Import**, and **Export**.

### Save as snippet

Hovering any message the user wrote offers **Save as snippet** next to
**Rewind**. It opens the Snippets menu with the editor already filled in and a
title proposed from the first line.

### Slash commands

Snippets join the `/` registry ([19-slash-commands.md](19-slash-commands.md)) as
their own group:

- `/s-<shortcut>` inserts that snippet. The `s-` namespace is exclusive, so a
  personal library can never shadow a built-in verb, a playbook, or a skill, and
  installing a skill later can never break a shortcut somebody memorized.
- The bare `<shortcut>` is registered as an alias while nothing else claims it.
- `/snippet <shortcut>` does the same for anyone who remembers the word but not
  the prefix. With no argument it lists the shortcuts that exist.
- A snippet with no shortcut is still reachable by the slug of its title.

## Client message templates

### Project → Sharing → Message client

The panel picks a template, a language (English or العربية), and shows the
resolved text in an editable box. From there:

| Action | What it does |
| --- | --- |
| **Copy** | Clipboard |
| **Open in email** | A `mailto:` draft with the project name as the subject |
| **Send to my sink** | Delivers the text through the notification sinks configured under Settings → Notifications |
| **Show on the portal** | Writes the text as the client portal's note |

"Send to my sink" reaches **the operator's own** Telegram or WhatsApp
destination — the channel already set up for run notifications — not the
client's phone. The event kind is `clientMessage`; like a test send and a
screenshot it bypasses the per-event toggles and the global enable switch,
because a person pressed send. Unlike an agent-run summary it is not clipped to
a few hundred characters: the message *is* the payload, so it gets whatever the
sink can carry (900 characters for WhatsApp, 3500 for Telegram).

### On the portal page

The client portal ([14-client-portal.md](14-client-portal.md)) already had an
operator note. It is now presented as **"Message from your developer"** and
carries its own timestamp, `noteUpdatedAt`, which the page prints as "Written
<date>". The timestamp moves only when the note's text changes — flipping a
display toggle does not make a month-old message look like it was written
today — and is cleared when the note is emptied.

## API

All snippet routes derive the owner from the session and never from the path.
There is no id to pass, no admin view, and an id that belongs to another user is
reported as `404`, not `403`.

| Route | Method | Purpose |
| --- | --- | --- |
| `/api/me/snippets` | `GET` | The whole library, most used first. Seeds on the first call |
| `/api/me/snippets` | `POST` | Create; body is `{title, body, audience, variants, tags, shortcut}`. `201` |
| `/api/me/snippets/{id}` | `PUT` | Replace the editable half. `createdAt` and `uses` survive |
| `/api/me/snippets/{id}` | `DELETE` | Remove one |
| `/api/me/snippets/{id}/use` | `POST` | Increment `uses`. Returns the updated snippet |
| `/api/me/snippets/import` | `POST` | `{snippets: [...], replace?: bool}`. Merges by id; `replace` restores a backup exactly |
| `/api/projects/{id}/client-message` | `GET` | `{configured}` — whether any sink could receive a message |
| `/api/projects/{id}/client-message` | `POST` | `{text, url?}` → `{configured, delivered: [{sink, delivered, error}]}` |

Errors: `400` for a snippet the service refuses (`ErrInvalidSnippet`), `401`
without a session, `404` for an unknown id, `503` when the deployment has no
snippet store.

The project route is dispatched by the existing project handler, so it inherits
the same gate as every other `/api/projects/{id}/…` route: admin **or** project
membership.

### Import and export

**Export** downloads `snippets.json` — `{"version": 1, "snippets": [...]}`.
**Import** accepts that shape and a bare array, coerces every field rather than
trusting the file, merges by id, and renames a colliding shortcut instead of
failing the whole import. Re-importing a file the user just exported is a no-op.

## Layout

| Layer | Path |
| --- | --- |
| Service | `backend/internal/service/snippets` (`model.go`, `seed.go`, `service.go`) |
| Store | `backend/internal/stores/filesnippets` |
| Handlers | `backend/internal/transport/http/handlers/snippet_handler.go`, `client_message_handler.go` |
| Sink verb | `backend/internal/service/notify/message.go` |
| Frontend state | `frontend/src/state/chat/snippetState.ts`, `state/hooks/chat/useSnippets.ts`, `state/hooks/projects/useClientMessage.ts` |
| Frontend UI | `frontend/src/ui/chat/composer/SnippetPicker.tsx`, `SnippetEditor.tsx`, `ui/projects/project-containers/ProjectClientMessageSection.tsx` |

## Limits and non-goals

- Snippets are **private**. There is no sharing, no team library, and no admin
  route that reads them; that is what playbooks are for.
- Resolution happens in the browser. The server stores and delivers text; it
  never fills in a placeholder, which is why the panel sends resolved text.
- `{{portalUrl}}` only resolves in the session that minted the portal link. The
  server keeps a hash of the token and cannot rebuild the URL afterwards.
- A client message goes to the operator's channel. Messaging a client directly
  would need their address and their consent, and this platform has neither.
