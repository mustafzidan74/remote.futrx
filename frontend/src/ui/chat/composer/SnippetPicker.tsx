import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Snippet, SnippetInput } from "../../../models/snippet";
import type { SnippetLibrary } from "../../../state/hooks/chat/useSnippets";
import {
  filterSnippets,
  newSnippetInput,
  snippetInputFrom,
  snippetPreview,
} from "../../../state/chat/snippetState";
import {
  ChevronDown,
  Download,
  Edit,
  FileText,
  Loader,
  Plus,
  Trash,
  Upload,
} from "../../primitives/icons";
import { SnippetEditor } from "./SnippetEditor";

/**
 * The composer's Snippets menu: the user's own saved prompts, most used first.
 *
 * Clicking one inserts it — never sends it — because a snippet is a starting
 * point the user is expected to finish. Everything else the library needs
 * (writing one, editing one, moving the whole set between machines) lives in
 * this one menu rather than in a settings page nobody visits mid-conversation.
 */
export function SnippetPicker({
  library,
  draft,
  disabled,
  pendingBody,
  onPendingHandled,
}: {
  library: SnippetLibrary;
  /** The composer's current text, offered as "Save this draft". */
  draft: string;
  disabled: boolean;
  /** Text a message's "Save as snippet" action handed over, if any. */
  pendingBody?: string | null;
  onPendingHandled?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<{ id?: string; input: SnippetInput } | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  // A "Save as snippet" action elsewhere in the thread opens this menu with
  // the editor already filled in, so the user never has to find it.
  useEffect(() => {
    if (!pendingBody) return;
    setOpen(true);
    setEditing({ input: newSnippetInput(pendingBody) });
    onPendingHandled?.();
  }, [pendingBody, onPendingHandled]);

  const visible = useMemo(
    () => filterSnippets(library.snippets, query),
    [library.snippets, query],
  );

  async function choose(snippet: Snippet) {
    setOpen(false);
    await library.insert(snippet);
  }

  async function save(input: SnippetInput) {
    const saved = await library.save(input, editing?.id);
    if (saved) setEditing(null);
  }

  async function importFile(file: File | null) {
    if (!file) return;
    try {
      await library.importDocument(await file.text());
    } catch {
      // The library surfaces the reason; the menu stays open on top of it.
    }
  }

  return (
    <div ref={rootRef} class="codex-snippet-control-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={disabled}
        class={`codex-snippet-control composer-pill ${open ? "composer-pill-active" : ""}`}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Snippets — your own saved prompts and client messages"
      >
        {library.busy ? (
          <Loader class="h-4 w-4 flex-none animate-spin text-accent-blue" />
        ) : (
          <FileText class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
        )}
        <span class="truncate font-semibold text-ink-100">Snippets</span>
        <span class="rounded bg-white/10 px-1 py-0.5 text-[10px] leading-none text-ink-300">
          {library.loading ? "..." : library.snippets.length}
        </span>
        <ChevronDown class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute left-0 bottom-full z-40 mb-2
                 w-[calc(100vw-1.5rem)] sm:left-auto sm:right-0 sm:w-[460px]"
          role="menu"
        >
          {editing ? (
            <>
              <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2 text-[11px] leading-4 text-ink-400">
                {editing.id ? "Edit snippet" : "New snippet"}
              </div>
              <div class="max-h-[420px] overflow-y-auto">
                <SnippetEditor
                  initial={editing.input}
                  saving={library.busy !== null}
                  onCancel={() => setEditing(null)}
                  onSave={(input) => void save(input)}
                />
              </div>
            </>
          ) : (
            <>
              <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2">
                <input
                  type="search"
                  value={query}
                  onInput={(event) => setQuery((event.currentTarget as HTMLInputElement).value)}
                  placeholder="Search your snippets"
                  autocomplete="off"
                  class="w-full h-8 rounded-md bg-black/30 border border-white/10 px-2.5 text-[12.5px]
                         text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
                />
              </div>

              <div class="max-h-[300px] overflow-y-auto py-1">
                {library.error ? (
                  <div class="px-3 py-3 text-[12px] text-red-300">{library.error}</div>
                ) : library.loading ? (
                  <div class="px-3 py-3 text-[12px] text-ink-400">Loading your snippets...</div>
                ) : visible.length === 0 ? (
                  <div class="px-3 py-3 text-[12px] leading-relaxed text-ink-400">
                    {library.snippets.length === 0
                      ? "No snippets yet. Save the current draft, or write one from scratch."
                      : "Nothing matches that search."}
                  </div>
                ) : (
                  visible.map((snippet) => (
                    <div
                      key={snippet.id}
                      class="group/snippet flex items-start gap-1 px-1.5 hover:bg-white/[0.05]"
                    >
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => void choose(snippet)}
                        disabled={library.busy !== null}
                        class="min-w-0 flex-1 rounded px-1.5 py-2 text-left focus:outline-none
                               disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <div class="flex min-w-0 items-center gap-2">
                          <span class="truncate text-[13px] font-medium text-ink-100">
                            {snippet.title}
                          </span>
                          {snippet.shortcut && (
                            <span class="flex-none rounded bg-white/[0.08] px-1.5 py-0.5 font-mono text-[10px] text-ink-400">
                              /s-{snippet.shortcut}
                            </span>
                          )}
                          {(snippet.uses ?? 0) > 0 && (
                            <span
                              class="flex-none rounded bg-accent-blue/[0.16] px-1.5 py-0.5 text-[10px] text-accent-blue"
                              title="Times inserted"
                            >
                              {snippet.uses}×
                            </span>
                          )}
                        </div>
                        <div dir="auto" class="mt-0.5 truncate text-[12px] leading-4 text-ink-400">
                          {snippetPreview(snippet)}
                        </div>
                      </button>
                      <div class="flex flex-none items-center gap-0.5 pt-2 opacity-100 md:opacity-0 md:group-hover/snippet:opacity-100">
                        <button
                          type="button"
                          onClick={() => setEditing({ id: snippet.id, input: snippetInputFrom(snippet) })}
                          class="grid h-7 w-7 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
                          aria-label={`Edit ${snippet.title}`}
                          title="Edit"
                        >
                          <Edit class="h-3.5 w-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            if (!confirm(`Delete the snippet "${snippet.title}"?`)) return;
                            void library.remove(snippet.id);
                          }}
                          class="grid h-7 w-7 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-accent-red"
                          aria-label={`Delete ${snippet.title}`}
                          title="Delete"
                        >
                          <Trash class="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  ))
                )}
              </div>

              <div class="flex flex-wrap items-center gap-1.5 border-t border-white/10 px-2 py-2">
                <button
                  type="button"
                  onClick={() => setEditing({ input: newSnippetInput(draft) })}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5
                         text-[12px] text-ink-100 hover:bg-white/[0.06]"
                >
                  <Plus class="h-3.5 w-3.5" />
                  {draft.trim() ? "Save this draft" : "New snippet"}
                </button>
                <button
                  type="button"
                  onClick={() => fileRef.current?.click()}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5
                         text-[12px] text-ink-200 hover:bg-white/[0.06]"
                  title="Merge a snippets.json file into your library"
                >
                  <Upload class="h-3.5 w-3.5" />
                  Import
                </button>
                <button
                  type="button"
                  onClick={library.exportDocument}
                  disabled={library.all.length === 0}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5
                         text-[12px] text-ink-200 hover:bg-white/[0.06] disabled:opacity-50"
                  title="Download your whole library as JSON"
                >
                  <Download class="h-3.5 w-3.5" />
                  Export
                </button>
                <input
                  ref={fileRef}
                  type="file"
                  accept="application/json,.json"
                  class="hidden"
                  onChange={(event) => {
                    const input = event.currentTarget as HTMLInputElement;
                    void importFile(input.files?.[0] ?? null);
                    input.value = "";
                  }}
                />
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
