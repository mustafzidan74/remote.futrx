import type { SearchResult } from "../../models/search";
import type { MessageSearch } from "../../state/hooks/workspace/useMessageSearch";
import { messageSearchState } from "../../state/workspace/messageSearchState";
import { Loader, MessageSquare } from "../primitives/icons";

/**
 * The "Search in messages" section of the sidebar.
 *
 * The sidebar's own filter already narrows chat titles as you type; this
 * section is the server-backed half that looks inside transcripts. Hits are
 * grouped by chat so a long conversation does not flood the list.
 */
export function MessageSearchResults({
  search,
  onOpenResult,
}: {
  search: MessageSearch;
  onOpenResult: (result: SearchResult) => void;
}) {
  if (!search.active) return null;

  const empty = !search.loading && !search.error && search.results.length === 0;

  return (
    <section class="pt-2" aria-label="Search in messages">
      <div class="px-3 pt-2 pb-1 flex items-center gap-2">
        <span class="text-[10.5px] uppercase tracking-wider text-ink-400 font-semibold">
          Search in messages
        </span>
        {search.loading && <Loader class="w-3 h-3 text-ink-400 animate-spin" />}
      </div>

      {search.error && (
        <p class="px-3 py-2 text-[12px] text-accent-red">{search.error}</p>
      )}

      {empty && (
        <p class="px-3 py-2 text-[12px] text-ink-400">
          No messages match.
          {search.truncated && " Older history is outside the search index."}
        </p>
      )}

      <ul class="space-y-2" role="listbox">
        {search.groups.map((group) => (
          <li key={group.chatId}>
            <div class="px-3 pb-1 flex items-baseline gap-1.5 min-w-0">
              <span class="text-[12px] text-ink-200 truncate">
                {group.chatTitle || "Untitled chat"}
              </span>
              {group.projectName && (
                <span class="text-[10.5px] text-ink-400 truncate flex-none">
                  {group.projectName}
                </span>
              )}
            </div>
            <ul class="space-y-0.5">
              {group.results.map((result) => {
                const index = search.results.indexOf(result);
                const active = index === search.activeIndex;
                return (
                  <li key={`${result.chatId}-${result.at}-${result.role}`}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={active}
                      onMouseEnter={() => search.setActiveIndex(index)}
                      onClick={() => onOpenResult(result)}
                      class={`w-full text-left rounded-md px-3 py-1.5 flex items-start gap-2 transition
                              ${active ? "bg-white/[0.09]" : "hover:bg-white/[0.06]"}`}
                    >
                      <MessageSquare class="w-3.5 h-3.5 mt-0.5 text-ink-400 flex-none" />
                      <span class="min-w-0 flex-1">
                        <span class="block text-[12px] leading-snug text-ink-300 break-words">
                          <SnippetText snippet={result.snippet} />
                        </span>
                        <span class="mt-0.5 block text-[10.5px] text-ink-400">
                          {result.role === "title"
                            ? "Chat title"
                            : result.role === "user"
                              ? "You"
                              : "Agent"}
                          {result.at ? ` · ${new Date(result.at).toLocaleString()}` : ""}
                        </span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </li>
        ))}
      </ul>
    </section>
  );
}

/**
 * Renders a snippet with its matched span emphasised. The markers are split
 * out into vnodes rather than interpolated as markup, so a transcript can
 * never inject anything into the DOM.
 */
function SnippetText({ snippet }: { snippet: string }) {
  return (
    <>
      {messageSearchState.segments(snippet).map((segment, index) =>
        segment.match ? (
          <mark key={index} class="bg-accent-blue/25 text-ink-50 rounded-[2px] px-0.5">
            {segment.text}
          </mark>
        ) : (
          <span key={index}>{segment.text}</span>
        ),
      )}
    </>
  );
}
