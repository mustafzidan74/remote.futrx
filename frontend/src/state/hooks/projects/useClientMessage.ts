import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import { snippetApi } from "../../../api/snippetApi";
import type { ClientMessageDelivery, ProjectMeta } from "../../../models/project";
import type { Snippet, SnippetLanguage } from "../../../models/snippet";
import {
  resolveSnippetText,
  snippetText,
  snippetsFor,
  sortSnippets,
  type SnippetContext,
} from "../../chat/snippetState";

export interface ClientMessageState {
  templates: Snippet[];
  loading: boolean;
  error: string | null;
  /** The template currently loaded, or null while nothing is picked. */
  selectedId: string | null;
  language: SnippetLanguage;
  /** The resolved text, editable before it goes anywhere. */
  text: string;
  /** Placeholders the panel could not fill in, for the hint under the box. */
  unresolved: string[];
  /** False when no notification sink is configured on this server. */
  canSend: boolean;
  sending: boolean;
  delivered: ClientMessageDelivery[] | null;
  publishing: boolean;
  published: boolean;
  select: (id: string) => void;
  setLanguage: (language: SnippetLanguage) => void;
  setText: (text: string) => void;
  send: () => Promise<void>;
  publishToPortal: () => Promise<void>;
  mailtoHref: () => string;
}

/**
 * The "Message client" panel: pick one of your own templates, choose the
 * language, and hand the resolved text to whatever channel you actually use.
 *
 * The templates are the client-facing half of the personal snippet library, so
 * a message written once in the composer is available here and the other way
 * round. Resolution happens in the browser, exactly as it does for a snippet
 * or a playbook, and anything that cannot be filled in stays visible.
 */
export function useClientMessage({
  project,
  portalUrl,
  previewUrl,
  onPublishNote,
}: {
  project: ProjectMeta | null;
  /** The portal link, known only in the session that minted it. */
  portalUrl?: string | null;
  previewUrl?: string | null;
  onPublishNote?: (note: string) => Promise<unknown>;
}): ClientMessageState {
  const [templates, setTemplates] = useState<Snippet[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [language, setLanguageState] = useState<SnippetLanguage>("en");
  const [text, setText] = useState("");
  const [canSend, setCanSend] = useState(false);
  const [sending, setSending] = useState(false);
  const [delivered, setDelivered] = useState<ClientMessageDelivery[] | null>(null);
  const [publishing, setPublishing] = useState(false);
  const [published, setPublished] = useState(false);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const context = useMemo<SnippetContext>(
    () => ({
      projectName: project?.name,
      slug: project?.slug,
      previewUrl: previewUrl ?? undefined,
      portalUrl: portalUrl ?? undefined,
    }),
    [project?.name, project?.slug, previewUrl, portalUrl],
  );

  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    setLoading(true);
    snippetApi
      .list()
      .then((list) => {
        if (cancelled) return;
        setTemplates(sortSnippets(snippetsFor(list, "client")));
        setError(null);
      })
      .catch((cause) => {
        if (cancelled) return;
        setTemplates([]);
        setError((cause as Error).message || "Could not load your message templates");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [project?.id]);

  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    projectApi
      .getClientMessageSinks(project.id)
      .then((result) => {
        if (!cancelled) setCanSend(result.configured);
      })
      .catch(() => {
        if (!cancelled) setCanSend(false);
      });
    return () => {
      cancelled = true;
    };
  }, [project?.id]);

  const load = useCallback(
    (template: Snippet | undefined, next: SnippetLanguage) => {
      if (!template) return;
      const resolved = resolveSnippetText(snippetText(template, next), context);
      setText(resolved.text);
      setDelivered(null);
      setPublished(false);
    },
    [context],
  );

  const select = useCallback(
    (id: string) => {
      setSelectedId(id);
      load(
        templates.find((template) => template.id === id),
        language,
      );
    },
    [language, load, templates],
  );

  const setLanguage = useCallback(
    (next: SnippetLanguage) => {
      setLanguageState(next);
      load(
        templates.find((template) => template.id === selectedId),
        next,
      );
    },
    [load, selectedId, templates],
  );

  const send = useCallback(async () => {
    if (!project || !text.trim()) return;
    setSending(true);
    setError(null);
    try {
      const result = await projectApi.sendClientMessage(project.id, text, portalUrl ?? undefined);
      if (!alive.current) return;
      setDelivered(result.delivered ?? []);
      setCanSend(result.configured);
    } catch (cause) {
      if (alive.current) setError((cause as Error).message || "Could not send the message");
    } finally {
      if (alive.current) setSending(false);
    }
  }, [portalUrl, project, text]);

  const publishToPortal = useCallback(async () => {
    if (!onPublishNote || !text.trim()) return;
    setPublishing(true);
    setError(null);
    try {
      await onPublishNote(text);
      if (alive.current) setPublished(true);
    } catch (cause) {
      if (alive.current) setError((cause as Error).message || "Could not update the portal");
    } finally {
      if (alive.current) setPublishing(false);
    }
  }, [onPublishNote, text]);

  const mailtoHref = useCallback(() => {
    const subject = project?.name ? `${project.name}` : "Project update";
    return `mailto:?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(text)}`;
  }, [project?.name, text]);

  const unresolved = useMemo(
    () => [...new Set(resolveSnippetText(text, context).unresolved.map((item) => item.token))],
    [text, context],
  );

  return {
    templates,
    loading,
    error,
    selectedId,
    language,
    text,
    unresolved,
    canSend,
    sending,
    delivered,
    publishing,
    published,
    select,
    setLanguage,
    setText: (value: string) => {
      setText(value);
      setPublished(false);
    },
    send,
    publishToPortal,
    mailtoHref,
  };
}
