/**
 * Full-text chat search.
 *
 * A snippet arrives with the matched span bracketed by two control characters
 * (STX/ETX) rather than by markup: a transcript can contain any printable
 * sequence, so only something unprintable is an unambiguous sentinel. The
 * renderer splits on them and emits vnodes; nothing is ever interpolated as
 * HTML.
 */

export const SNIPPET_HIGHLIGHT_START = "\u0002";
export const SNIPPET_HIGHLIGHT_END = "\u0003";

export type SearchResultRole = "user" | "assistant" | "title";

export interface SearchResult {
  chatId: string;
  chatTitle: string;
  projectId?: string;
  projectName?: string;
  role: SearchResultRole;
  at: number;
  snippet: string;
}

export interface SearchResponse {
  results: SearchResult[];
  /** True when older history fell out of the memory-bounded index. */
  truncated: boolean;
}
