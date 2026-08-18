import {
  SNIPPET_HIGHLIGHT_END,
  SNIPPET_HIGHLIGHT_START,
  type SearchResult,
} from "../../models/search.ts";

/**
 * Presentation state for "search in messages".
 *
 * The sidebar's own filter already narrows projects and chat titles as you
 * type. This module owns the second, server-backed half: how the hits group,
 * how the keyboard walks them, and how a snippet is split for rendering.
 * Everything here is pure so the interaction is testable without a DOM.
 */

/** Typing fewer than this many characters never reaches the server. */
export const MIN_SEARCH_QUERY_LENGTH = 2;

/** How long the box stays quiet before it asks the server. */
export const SEARCH_DEBOUNCE_MS = 250;

export interface SearchResultGroup {
  chatId: string;
  chatTitle: string;
  projectId?: string;
  projectName?: string;
  results: SearchResult[];
}

/** A snippet split into plain runs and the matched run(s) between them. */
export interface SnippetSegment {
  text: string;
  match: boolean;
}

class MessageSearchState {
  /** Reports whether a query is worth sending. */
  shouldSearch(query: string): boolean {
    return query.trim().length >= MIN_SEARCH_QUERY_LENGTH;
  }

  /**
   * Groups hits by chat, preserving the server's relevance order: a chat takes
   * the position of its first hit, and hits keep their order inside it.
   */
  group(results: SearchResult[]): SearchResultGroup[] {
    const groups: SearchResultGroup[] = [];
    const byChat = new Map<string, SearchResultGroup>();

    for (const result of results) {
      const existing = byChat.get(result.chatId);
      if (existing) {
        existing.results.push(result);
        continue;
      }
      const group: SearchResultGroup = {
        chatId: result.chatId,
        chatTitle: result.chatTitle,
        projectId: result.projectId,
        projectName: result.projectName,
        results: [result],
      };
      byChat.set(result.chatId, group);
      groups.push(group);
    }
    return groups;
  }

  /**
   * The flat navigation order the keyboard walks. It is derived from the
   * groups rather than from the raw list so what ↓ visits is exactly what the
   * eye sees, in the same order.
   */
  flatten(groups: SearchResultGroup[]): SearchResult[] {
    return groups.flatMap((group) => group.results);
  }

  /**
   * Moves the active index. Navigation wraps, because a short result list is
   * faster to cycle than to reverse, and clamps to -1 ("nothing active") when
   * the list is empty.
   */
  move(activeIndex: number, count: number, direction: -1 | 1): number {
    if (count <= 0) return -1;
    if (activeIndex < 0) return direction === 1 ? 0 : count - 1;
    return (activeIndex + direction + count) % count;
  }

  /** Resolves the entry Enter should open, or null when nothing is active. */
  activeResult(results: SearchResult[], activeIndex: number): SearchResult | null {
    if (activeIndex < 0 || activeIndex >= results.length) return null;
    return results[activeIndex];
  }

  /**
   * Splits a snippet on the backend's sentinels. Unpaired sentinels are
   * treated as plain text, so a malformed snippet degrades to something
   * readable rather than to nothing.
   */
  segments(snippet: string): SnippetSegment[] {
    const segments: SnippetSegment[] = [];
    let rest = snippet;

    while (rest.length > 0) {
      const start = rest.indexOf(SNIPPET_HIGHLIGHT_START);
      if (start < 0) break;
      const end = rest.indexOf(SNIPPET_HIGHLIGHT_END, start + 1);
      if (end < 0) break;

      if (start > 0) segments.push({ text: rest.slice(0, start), match: false });
      const matched = rest.slice(start + SNIPPET_HIGHLIGHT_START.length, end);
      if (matched) segments.push({ text: matched, match: true });
      rest = rest.slice(end + SNIPPET_HIGHLIGHT_END.length);
    }

    if (rest.length > 0) {
      segments.push({ text: this.stripSentinels(rest), match: false });
    }
    return segments;
  }

  private stripSentinels(value: string): string {
    return value
      .split(SNIPPET_HIGHLIGHT_START)
      .join("")
      .split(SNIPPET_HIGHLIGHT_END)
      .join("");
  }
}

export const messageSearchState = new MessageSearchState();
