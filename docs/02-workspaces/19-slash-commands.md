# Slash commands

Typing `/` in the composer opens a filterable menu of everything this chat can
do without leaving the keyboard: the platform's own verbs, the operator's
playbooks, and every skill the agent can load, merged into one list and
labelled by group.

Nothing here is a second implementation. Every command calls the same handler
the equivalent button already calls — `/autopilot on` arms the loop through the
autopilot popover's own action, `/test` sends the Test menu's prompt — so a
command and its button cannot drift apart.

## Opening the menu

The menu opens when a `/` starts the message or follows whitespace, and filters
as you type the word after it.

| Key | Effect |
| --- | --- |
| `↑` / `↓` | Move the selection |
| `Enter` / `Tab` | Pick the highlighted command |
| `Shift+Enter` | Pick a **playbook** and send it immediately |
| `Esc` | Dismiss the menu for this token |
| `Ctrl`/`Cmd`+`Enter` | Send the draft, menu or no menu |

Picking a command that takes an argument types it into the draft and leaves the
caret after it; picking one that does not runs it straight away.

Matching is prefix-first: `auto` finds `/autopilot` and `/autotest`, `apt`
finds `/autopilot`, and a word that only appears in a command's description
(`backup` → `/snapshot`) matches last.

**Sending a literal slash.** Start the message with `//` or `\/`. Both keep the
menu shut and are unescaped once on send, so `//deploy tomorrow` reaches the
agent as `/deploy tomorrow`.

## Built-in commands

| Command | What it does |
| --- | --- |
| `/test [url or what to check]` | Runs a Playwright check now. No argument tests the last change (the same prompt the auto-test policy uses); an address checks that page; anything else is treated as a journey described in words. |
| `/deploy` | Loads the deploy playbook into the composer. Falls back to the deploy skill, then to a generic deploy prompt, if no playbook is installed. |
| `/snapshot [label]` | Takes a project snapshot. No agent run: the composer's status line confirms it and the record appears under **Project settings → Snapshots**. |
| `/preview` | Opens the project's lowest app port in a new tab. |
| `/screenshot [port]` | Captures the preview and shows a thumbnail card. See [previews](../02-user-guide/06-previews-and-inspector.md#share-a-screenshot). |
| `/autopilot on\|off [rounds]` | Arms or stops the unattended follow-up loop. Without a round count it keeps the chat's current limit. |
| `/autotest on\|off` | Turns the post-run Playwright check on or off. |
| `/review` | Switches the chat to review mode, selects the review skill if one is registered, and loads "Review the last change." |
| `/snippet <shortcut>` | Inserts one of your own snippets. With no argument it lists the shortcuts you have. See [snippets](21-snippets-and-client-messages.md). |
| `/skills` | Re-opens the menu showing only skills. |
| `/browser <url or port>` | Loads an address in the project's Agent Browser and reveals the pane. A bare port becomes `http://127.0.0.1:<port>/`, which is what the in-container browser can actually reach. |
| `/help` | Lists every command available in this chat. |

Commands that need a container (`/snapshot`, `/preview`, `/screenshot`,
`/browser`) report "This chat is not attached to a project container." in a
chat that has none.

## Playbooks

Every playbook in the library is a command. A playbook whose id is unique is
typed as itself — `/security-review` — and `pb-`-prefixed spelling always works
too, so `/pb-security-review` reaches the same entry. A playbook whose id
collides with a built-in keeps only the prefixed form: a playbook called
`review` is `/pb-review`, because `/review` is the built-in.

Picking a playbook does exactly what clicking it in the ⚡ menu does: it applies
the playbook's skills, mode, and provider, then loads its prompt into the
composer. `Shift+Enter` sends it immediately, and a prompt with an unfilled
`{{placeholder}}` is never sent however it was picked.

## Snippets

Every snippet in your personal library is a command under `/s-<shortcut>`, or
the slug of its title when it has no shortcut. The `s-` namespace belongs to
snippets alone, so a private library can never shadow a built-in verb, a
playbook, or a skill — and a skill installed next month can never break a
shortcut you memorized. The bare word is registered as an alias while nothing
else claims it.

Picking one resolves its placeholders and inserts it, exactly as clicking it in
the 📄 Snippets menu does. See
[21-snippets-and-client-messages.md](21-snippets-and-client-messages.md).

## Skills

Every skill the chat can load — global and project — is a command under its own
name. Picking one does two things:

1. Selects the skill for the chat, exactly as the Skills picker does. It appears
   as a chip above the composer.
2. Inserts `Use the <name> skill: ` into the draft.

The second half is the point. Selecting a skill makes it *available*; naming it
in the prompt makes the agent load it deliberately, which is what `/skill` does
in a terminal agent.

A skill whose command collides with a built-in or a playbook is not registered
as a command — it is still reachable from the Skills picker.

## Where the code lives

| Concern | File |
| --- | --- |
| Registry, filter, parser (pure) | `frontend/src/state/chat/slashCommandState.ts` |
| Caret tracking and what each command does | `frontend/src/state/hooks/chat/useSlashCommands.ts` |
| The menu | `frontend/src/ui/chat/composer/SlashCommandMenu.tsx` |
| The status line commands report into | `frontend/src/ui/chat/composer/ComposerStatusNote.tsx` |
| Keystrokes and caret events | `frontend/src/ui/chat/composer/PromptTextarea.tsx` |

The registry, the filter, and the parser are covered by
`frontend/src/state/chat/slashCommandState.test.ts`.
