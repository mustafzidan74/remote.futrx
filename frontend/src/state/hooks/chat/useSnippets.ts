import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { snippetApi } from "../../../api/snippetApi";
import type { Snippet, SnippetInput } from "../../../models/snippet";
import { firstUnresolvedRange, unresolvedSummary } from "../../chat/playbookState";
import {
  exportSnippets,
  parseSnippetImport,
  resolveSnippetText,
  snippetsFor,
  snippetText,
  sortSnippets,
  usesSelection,
  type SnippetContext,
} from "../../chat/snippetState";

export interface SnippetLibrary {
  /** The personal prompts, most used first. */
  snippets: Snippet[];
  /** The client message templates, most used first. */
  clientTemplates: Snippet[];
  /** Both kinds, for export and for the slash registry. */
  all: Snippet[];
  loading: boolean;
  error: string | null;
  /** Id of the snippet a write is in flight for, or "new"/"import". */
  busy: string | null;
  /** Set when an insertion left placeholders for the user to fill in. */
  notice: string | null;
  dismissNotice: () => void;
  reload: () => Promise<void>;
  /** Resolves a snippet and puts it in the composer. */
  insert: (snippet: Snippet) => Promise<void>;
  save: (input: SnippetInput, id?: string) => Promise<Snippet | null>;
  remove: (id: string) => Promise<void>;
  /** Merges an exported document; returns how many entries the library holds. */
  importDocument: (source: string, replace?: boolean) => Promise<number>;
  /** Hands the whole library to the browser as a download. */
  exportDocument: () => void;
}

/**
 * The composer's Snippets menu.
 *
 * The library is private to the signed-in user and changes only when they edit
 * it, so it is fetched once per composer mount rather than polled. Inserting
 * resolves the placeholders against the chat's project and the current draft,
 * then records the use — which is what sorts the menu the next time it opens.
 */
export function useSnippets({
  enabled,
  context,
  draft,
  insertText,
  setText,
}: {
  enabled: boolean;
  context: SnippetContext;
  /** The composer's current text, substituted for `{{selection}}`. */
  draft: string;
  insertText: (text: string, select?: { start: number; end: number }) => void;
  setText: (text: string) => void;
}): SnippetLibrary {
  const [snippets, setSnippets] = useState<Snippet[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const list = await snippetApi.list();
      if (!alive.current) return;
      setSnippets(sortSnippets(list));
      setError(null);
    } catch (cause) {
      if (!alive.current) return;
      setSnippets([]);
      setError((cause as Error).message || "Could not load your snippets");
    } finally {
      if (alive.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void load();
  }, [enabled, load]);

  const insert = useCallback(
    async (snippet: Snippet) => {
      setNotice(null);
      const body = snippetText(snippet, "en");
      const embedsDraft = usesSelection(body);
      const resolved = resolveSnippetText(body, { ...context, selection: draft });
      // A snippet that names {{selection}} wraps the draft rather than adding
      // to it, so inserting it must replace what is in the composer instead of
      // leaving the same words in twice.
      if (embedsDraft && draft.trim()) {
        setText(resolved.text);
      } else {
        insertText(resolved.text, firstUnresolvedRange(resolved) ?? undefined);
      }
      if (!resolved.ready) {
        setNotice(`Fill in ${unresolvedSummary(resolved)} before sending.`);
      }
      // The counter is a convenience, not part of the insertion: a failed
      // increment must never make the text the user asked for disappear.
      try {
        const updated = await snippetApi.markUsed(snippet.id);
        if (!alive.current) return;
        setSnippets((current) =>
          sortSnippets(current.map((item) => (item.id === updated.id ? updated : item)))
        );
      } catch {
        if (alive.current) {
          setSnippets((current) =>
            sortSnippets(
              current.map((item) =>
                item.id === snippet.id ? { ...item, uses: (item.uses ?? 0) + 1 } : item
              )
            )
          );
        }
      }
    },
    [context, draft, insertText, setText]
  );

  const save = useCallback(async (input: SnippetInput, id?: string): Promise<Snippet | null> => {
    setBusy(id ?? "new");
    setError(null);
    try {
      const saved = id ? await snippetApi.update(id, input) : await snippetApi.create(input);
      if (!alive.current) return saved;
      setSnippets((current) =>
        sortSnippets(
          id
            ? current.map((item) => (item.id === saved.id ? saved : item))
            : [...current, saved]
        )
      );
      return saved;
    } catch (cause) {
      if (alive.current) setError((cause as Error).message || "Could not save the snippet");
      return null;
    } finally {
      if (alive.current) setBusy(null);
    }
  }, []);

  const remove = useCallback(async (id: string) => {
    setBusy(id);
    setError(null);
    try {
      await snippetApi.remove(id);
      if (!alive.current) return;
      setSnippets((current) => current.filter((item) => item.id !== id));
    } catch (cause) {
      if (alive.current) setError((cause as Error).message || "Could not delete the snippet");
    } finally {
      if (alive.current) setBusy(null);
    }
  }, []);

  const importDocument = useCallback(async (source: string, replace = false) => {
    setBusy("import");
    setError(null);
    try {
      const parsed = parseSnippetImport(source);
      const list = await snippetApi.import(parsed, replace);
      if (alive.current) setSnippets(sortSnippets(list));
      return list.length;
    } catch (cause) {
      if (alive.current) setError((cause as Error).message || "Could not import that file");
      throw cause;
    } finally {
      if (alive.current) setBusy(null);
    }
  }, []);

  const exportDocument = useCallback(() => {
    const blob = new Blob([exportSnippets(snippets)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "snippets.json";
    anchor.click();
    URL.revokeObjectURL(url);
  }, [snippets]);

  const agentSnippets = useMemo(() => snippetsFor(snippets, "agent"), [snippets]);
  const clientTemplates = useMemo(() => snippetsFor(snippets, "client"), [snippets]);

  return {
    snippets: agentSnippets,
    clientTemplates,
    all: snippets,
    loading,
    error,
    busy,
    notice,
    dismissNotice: () => setNotice(null),
    reload: load,
    insert,
    save,
    remove,
    importDocument,
    exportDocument,
  };
}
