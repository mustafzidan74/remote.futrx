import { useState } from "preact/hooks";
import type { SnippetInput } from "../../../models/snippet";
import {
  SNIPPET_PLACEHOLDERS,
  normalizeShortcut,
  parseTags,
  validateSnippetInput,
} from "../../../state/chat/snippetState";
import { Loader } from "../../primitives/icons";

/**
 * The snippet form, used for both a new entry and an edit.
 *
 * It is deliberately one panel rather than a modal: it opens inside the
 * Snippets menu, over the list it came from, so saving returns the user to
 * exactly where they were.
 */
export function SnippetEditor({
  initial,
  saving,
  onCancel,
  onSave,
}: {
  initial: SnippetInput;
  saving: boolean;
  onCancel: () => void;
  onSave: (input: SnippetInput) => void;
}) {
  const [input, setInput] = useState<SnippetInput>(initial);
  const [tagText, setTagText] = useState((initial.tags ?? []).join(", "));
  const [error, setError] = useState<string | null>(null);
  const isClient = input.audience === "client";

  function patch(next: Partial<SnippetInput>) {
    setInput((current) => ({ ...current, ...next }));
    setError(null);
  }

  function submit(event: Event) {
    event.preventDefault();
    // The shortcut is stored in the one spelling the server accepts, so
    // whatever the user typed becomes the word `/s-…` will actually match.
    const candidate: SnippetInput = {
      ...input,
      shortcut: normalizeShortcut(input.shortcut),
      tags: parseTags(tagText),
    };
    const problem = validateSnippetInput(candidate);
    if (problem) {
      setError(problem);
      return;
    }
    onSave(candidate);
  }

  return (
    <form onSubmit={submit} class="space-y-2.5 px-3 py-3">
      <label class="block space-y-1">
        <span class="text-[11px] text-ink-400">Title</span>
        <input
          type="text"
          value={input.title}
          onInput={(event) => patch({ title: (event.currentTarget as HTMLInputElement).value })}
          placeholder="What this snippet is for"
          maxLength={120}
          autocomplete="off"
          class="w-full h-9 rounded-md bg-black/30 border border-white/10 px-2.5 text-[13px] text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
        />
      </label>

      <div class="flex gap-2">
        <label class="flex-1 space-y-1">
          <span class="text-[11px] text-ink-400">Kind</span>
          <select
            value={input.audience}
            onChange={(event) =>
              patch({
                audience:
                  (event.currentTarget as HTMLSelectElement).value === "client" ? "client" : "agent",
              })
            }
            class="w-full h-9 rounded-md bg-black/30 border border-white/10 px-2 text-[13px] text-ink-100
                   focus:outline-none focus:border-accent-blue"
          >
            <option value="agent">Prompt for the agent</option>
            <option value="client">Message for a client</option>
          </select>
        </label>
        <label class="flex-1 space-y-1">
          <span class="text-[11px] text-ink-400">Shortcut (optional)</span>
          <input
            type="text"
            value={input.shortcut}
            onInput={(event) =>
              patch({ shortcut: (event.currentTarget as HTMLInputElement).value })
            }
            placeholder="wpfix"
            maxLength={32}
            autocomplete="off"
            class="w-full h-9 rounded-md bg-black/30 border border-white/10 px-2.5 text-[13px] text-ink-100
                   placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
          />
        </label>
      </div>

      {isClient ? (
        <>
          <label class="block space-y-1">
            <span class="text-[11px] text-ink-400">English</span>
            <textarea
              value={input.variants.en ?? ""}
              onInput={(event) =>
                patch({
                  variants: {
                    ...input.variants,
                    en: (event.currentTarget as HTMLTextAreaElement).value,
                  },
                })
              }
              rows={4}
              dir="ltr"
              class="w-full rounded-md bg-black/30 border border-white/10 px-2.5 py-2 text-[13px] text-ink-100
                     placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
            />
          </label>
          <label class="block space-y-1">
            <span class="text-[11px] text-ink-400">العربية</span>
            <textarea
              value={input.variants.ar ?? ""}
              onInput={(event) =>
                patch({
                  variants: {
                    ...input.variants,
                    ar: (event.currentTarget as HTMLTextAreaElement).value,
                  },
                })
              }
              rows={4}
              dir="auto"
              class="w-full rounded-md bg-black/30 border border-white/10 px-2.5 py-2 text-[13px] text-ink-100
                     placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
            />
          </label>
        </>
      ) : (
        <label class="block space-y-1">
          <span class="text-[11px] text-ink-400">Text</span>
          <textarea
            value={input.body}
            onInput={(event) => patch({ body: (event.currentTarget as HTMLTextAreaElement).value })}
            rows={6}
            dir="auto"
            placeholder="The prompt to insert. Use {{selection}} to wrap what is already in the composer."
            class="w-full rounded-md bg-black/30 border border-white/10 px-2.5 py-2 text-[13px] text-ink-100
                   placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
          />
        </label>
      )}

      <label class="block space-y-1">
        <span class="text-[11px] text-ink-400">Tags (optional, comma separated)</span>
        <input
          type="text"
          value={tagText}
          onInput={(event) => setTagText((event.currentTarget as HTMLInputElement).value)}
          placeholder="wordpress, delivery"
          autocomplete="off"
          class="w-full h-9 rounded-md bg-black/30 border border-white/10 px-2.5 text-[13px] text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
        />
      </label>

      <p class="text-[11px] leading-4 text-ink-400">
        Placeholders: {SNIPPET_PLACEHOLDERS.join(" ")} — anything that cannot be filled in stays
        visible in the composer so you can complete it yourself.
      </p>

      {error && <div class="text-[12px] text-red-300">{error}</div>}

      <div class="flex items-center gap-2 pt-0.5">
        <button
          type="submit"
          disabled={saving}
          class="h-9 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[12.5px]
                 font-medium disabled:opacity-50 inline-flex items-center gap-1.5"
        >
          {saving && <Loader class="h-3.5 w-3.5 animate-spin" />}
          Save snippet
        </button>
        <button
          type="button"
          onClick={onCancel}
          class="h-9 px-3 rounded-md border border-white/10 text-ink-200 hover:bg-white/[0.06] text-[12.5px]"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
