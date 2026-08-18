import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type { Snippet, SnippetInput } from "../models/snippet";

interface SnippetCollection {
  snippets: Snippet[];
}

/**
 * The signed-in user's own snippet library. Every route derives the owner from
 * the session, so there is no id to pass and no way to read anybody else's.
 */
export const snippetApi = {
  list: async (): Promise<Snippet[]> => {
    const payload = await requestJson<SnippetCollection>("GET", API_ROUTES.snippets.collection);
    return payload.snippets ?? [];
  },

  create: (input: SnippetInput): Promise<Snippet> =>
    requestJson<Snippet>("POST", API_ROUTES.snippets.collection, input),

  update: (id: string, input: SnippetInput): Promise<Snippet> =>
    requestJson<Snippet>("PUT", API_ROUTES.snippets.item(id), input),

  remove: (id: string): Promise<void> =>
    requestJson<void>("DELETE", API_ROUTES.snippets.item(id)),

  /** Records one insertion, which is what "most used first" sorts on. */
  markUsed: (id: string): Promise<Snippet> =>
    requestJson<Snippet>("POST", API_ROUTES.snippets.use(id)),

  import: async (snippets: Snippet[], replace = false): Promise<Snippet[]> => {
    const payload = await requestJson<SnippetCollection>("POST", API_ROUTES.snippets.import, {
      snippets,
      replace,
    });
    return payload.snippets ?? [];
  },
};
