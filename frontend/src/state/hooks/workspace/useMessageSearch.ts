import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { searchApi } from "../../../api/searchApi";
import type { SearchResult } from "../../../models/search";
import {
  SEARCH_DEBOUNCE_MS,
  messageSearchState,
  type SearchResultGroup,
} from "../../workspace/messageSearchState";

export interface MessageSearch {
  /** Hits grouped by chat, in the server's relevance order. */
  groups: SearchResultGroup[];
  /** The same hits flattened, which is what the keyboard walks. */
  results: SearchResult[];
  activeIndex: number;
  loading: boolean;
  error: string | null;
  /** True when the query is long enough that a search is expected. */
  active: boolean;
  /** True when older history fell out of the memory-bounded server index. */
  truncated: boolean;
  moveActive: (direction: -1 | 1) => void;
  setActiveIndex: (index: number) => void;
  /** The entry Enter should open, or null. */
  activeResult: () => SearchResult | null;
}

/**
 * Search-as-you-type over chat messages.
 *
 * Keystrokes are debounced, and responses are matched against a request number
 * so a slow early response can never overwrite a fast later one. There is no
 * request cancellation: the shared fetch helper has no abort seam, and
 * discarding stale responses is what the user actually perceives.
 */
export function useMessageSearch(query: string): MessageSearch {
  const [results, setResults] = useState<SearchResult[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeIndex, setActiveIndex] = useState(-1);
  const requestRef = useRef(0);

  const trimmed = query.trim();
  const active = messageSearchState.shouldSearch(trimmed);

  useEffect(() => {
    if (!active) {
      // Bump the request number so an in-flight response for a longer query
      // cannot repopulate a box the user has just emptied.
      requestRef.current += 1;
      setResults([]);
      setTruncated(false);
      setLoading(false);
      setError(null);
      setActiveIndex(-1);
      return;
    }

    const request = ++requestRef.current;
    setLoading(true);
    const timer = setTimeout(() => {
      searchApi
        .search(trimmed)
        .then((response) => {
          if (requestRef.current !== request) return;
          setResults(response.results);
          setTruncated(response.truncated);
          setError(null);
          setActiveIndex(-1);
        })
        .catch((cause: Error) => {
          if (requestRef.current !== request) return;
          setResults([]);
          setError(cause.message);
        })
        .finally(() => {
          if (requestRef.current === request) setLoading(false);
        });
    }, SEARCH_DEBOUNCE_MS);

    return () => clearTimeout(timer);
  }, [trimmed, active]);

  const groups = useMemo(() => messageSearchState.group(results), [results]);
  const ordered = useMemo(() => messageSearchState.flatten(groups), [groups]);

  const moveActive = useCallback(
    (direction: -1 | 1) => {
      setActiveIndex((current) => messageSearchState.move(current, ordered.length, direction));
    },
    [ordered.length],
  );

  const activeResult = useCallback(
    () => messageSearchState.activeResult(ordered, activeIndex),
    [ordered, activeIndex],
  );

  return {
    groups,
    results: ordered,
    activeIndex,
    loading,
    error,
    active,
    truncated,
    moveActive,
    setActiveIndex,
    activeResult,
  };
}
