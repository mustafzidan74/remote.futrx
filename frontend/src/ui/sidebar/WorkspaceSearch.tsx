import { Search, X } from "../primitives/icons";

/**
 * The sidebar's search box.
 *
 * It drives two things at once: the local title filter (instant) and the
 * server-backed message search (debounced). The arrow keys and Enter belong to
 * the message results, so they are forwarded up rather than handled here.
 */
export function WorkspaceSearch({
  query,
  onQueryChange,
  onClear,
  onNavigate,
  onSubmit,
}: {
  query: string;
  onQueryChange: (query: string) => void;
  onClear: () => void;
  /** Move the highlighted message result. */
  onNavigate?: (direction: -1 | 1) => void;
  /** Open the highlighted message result; returns false when there was none. */
  onSubmit?: () => boolean;
}) {
  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "ArrowDown" && onNavigate) {
      event.preventDefault();
      onNavigate(1);
      return;
    }
    if (event.key === "ArrowUp" && onNavigate) {
      event.preventDefault();
      onNavigate(-1);
      return;
    }
    if (event.key === "Enter" && onSubmit) {
      // Only swallow Enter when a result actually consumed it, so the key
      // keeps its default meaning in an otherwise idle box.
      if (onSubmit()) event.preventDefault();
      return;
    }
    if (event.key === "Escape" && query) {
      event.preventDefault();
      event.stopPropagation();
      onClear();
    }
  }

  return (
    <label class="mt-3 flex items-center gap-2 h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3 focus-within:border-accent-blue/70 transition-colors">
      <Search class="w-4 h-4 text-ink-300 flex-none" />
      <input
        value={query}
        onInput={(event) => onQueryChange((event.currentTarget as HTMLInputElement).value)}
        onKeyDown={handleKeyDown}
        placeholder="Search projects, chats and messages"
        class="min-w-0 flex-1 bg-transparent text-[14px] text-ink-100 placeholder:text-ink-300 focus:outline-none"
        autocomplete="off"
        spellcheck={false}
      />
      {query && (
        <button
          type="button"
          onClick={onClear}
          class="w-7 h-7 grid place-items-center rounded text-ink-300 hover:bg-white/10 hover:text-ink-100"
          aria-label="Clear search"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      )}
    </label>
  );
}
