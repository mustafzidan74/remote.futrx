import { Search, X } from "../primitives/icons";

export function WorkspaceSearch({
  query,
  onQueryChange,
  onClear,
}: {
  query: string;
  onQueryChange: (query: string) => void;
  onClear: () => void;
}) {
  return (
    <label class="mt-3 flex items-center gap-2 h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3 focus-within:border-accent-blue/70 transition-colors">
      <Search class="w-4 h-4 text-ink-300 flex-none" />
      <input
        value={query}
        onInput={(event) => onQueryChange((event.currentTarget as HTMLInputElement).value)}
        placeholder="Search projects and chats"
        class="min-w-0 flex-1 bg-transparent text-[14px] text-ink-100 placeholder:text-ink-300 focus:outline-none"
        autocomplete="off"
        spellcheck={false}
      />
      {query && (
        <button
          type="button"
          onClick={onClear}
          class="w-8 h-8 grid place-items-center rounded text-ink-300 hover:bg-white/10 hover:text-ink-100"
          aria-label="Clear search"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      )}
    </label>
  );
}
