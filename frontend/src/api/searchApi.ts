import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type { SearchResponse } from "../models/search";

/**
 * Full-text search across the chats the caller can see. The backend applies
 * membership filtering, so this client never has to.
 *
 * There is no cancellation: the shared request helper has no abort seam, and
 * the caller debounces and discards stale responses by request number, which
 * is what actually matters for a search-as-you-type box.
 */
export const searchApi = {
  search: async (
    query: string,
    options: { projectId?: string; limit?: number } = {},
  ): Promise<SearchResponse> => {
    const params = new URLSearchParams({ q: query });
    if (options.projectId) params.set("projectId", options.projectId);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await requestJson<SearchResponse>(
      "GET",
      API_ROUTES.search(params.toString()),
    );
    return { results: payload?.results ?? [], truncated: payload?.truncated ?? false };
  },
};
