import type { JournalEntry, JournalFilters } from "../../../models/journal";
import type { JournalRecord } from "../../../state/hooks/projects/useProjectJournal";
import { journalState } from "../../../state/projects/journalState";
import { AlertCircle, Clock, Download, FileText, Loader, Search, Terminal, X } from "../../primitives/icons";
import { Empty, Loading } from "./ProjectContainerPrimitives";
import { formatEpochMillis } from "./projectContainerFormat";

/**
 * The project's change history: one card per agent run, newest first, with
 * what was asked, what came back, and the files it wrote. Filtering happens on
 * the server, so the list stays one page per request however long the history.
 */
export function ProjectJournalSection({
  record,
  filters,
  onFiltersChange,
  onClearFilters,
  onLoadMore,
  exportUrl,
  onOpenChat,
}: {
  record: JournalRecord;
  filters: JournalFilters;
  onFiltersChange: (filters: JournalFilters) => void;
  onClearFilters: () => void;
  onLoadMore: () => void;
  exportUrl: string;
  onOpenChat?: (chatId: string) => void;
}) {
  const filtered = journalState.filtered(filters);
  const patch = (change: Partial<JournalFilters>) => onFiltersChange({ ...filters, ...change });

  return (
    <div class="space-y-3">
      <div class="flex flex-wrap items-end gap-2">
        <label class="min-w-[12rem] flex-1 space-y-1">
          <span class="block text-[11px] text-ink-400">Search</span>
          <span class="relative block">
            <Search class="pointer-events-none absolute inset-y-0 my-auto ms-2.5 h-3.5 w-3.5 text-ink-400" />
            <input
              type="search"
              value={filters.text}
              onInput={(event) => patch({ text: (event.currentTarget as HTMLInputElement).value })}
              placeholder="A feature, a file, a command"
              class="h-9 w-full rounded-md border border-line bg-inset ps-8 pe-3 text-[13px] text-ink-100 placeholder:text-ink-400 focus:border-accent-blue focus:outline-none"
            />
          </span>
        </label>
        <label class="space-y-1">
          <span class="block text-[11px] text-ink-400">From</span>
          <input
            type="date"
            value={filters.from}
            onInput={(event) => patch({ from: (event.currentTarget as HTMLInputElement).value })}
            class="h-9 rounded-md border border-line bg-inset px-2.5 text-[13px] text-ink-100 focus:border-accent-blue focus:outline-none"
          />
        </label>
        <label class="space-y-1">
          <span class="block text-[11px] text-ink-400">To</span>
          <input
            type="date"
            value={filters.to}
            onInput={(event) => patch({ to: (event.currentTarget as HTMLInputElement).value })}
            class="h-9 rounded-md border border-line bg-inset px-2.5 text-[13px] text-ink-100 focus:border-accent-blue focus:outline-none"
          />
        </label>
        {filtered && (
          <button
            type="button"
            onClick={onClearFilters}
            class="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] text-ink-300 hover:bg-tint-strong hover:text-ink-100"
          >
            <X class="h-3.5 w-3.5" /> Clear filters
          </button>
        )}
        {exportUrl && (
          <a
            href={exportUrl}
            class="inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] text-ink-300 hover:bg-tint-strong hover:text-ink-100"
          >
            <Download class="h-3.5 w-3.5" /> Download the file
          </a>
        )}
      </div>

      {record.error && (
        <div class="flex items-start gap-2 rounded-md border border-accent-red/40 bg-accent-red/[0.08] p-2.5 text-[12px] text-accent-red">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" />
          <span>{record.error}</span>
        </div>
      )}

      {record.loading && record.entries.length === 0 && <Loading text="Loading the change history…" />}

      {!record.loading && record.entries.length === 0 && !record.error && (
        <Empty
          text={
            filtered
              ? "No run matches these filters."
              : "No agent run has been recorded for this project yet."
          }
        />
      )}

      <div class="space-y-2">
        {record.entries.map((entry) => (
          <JournalCard key={journalState.key(entry)} entry={entry} onOpenChat={onOpenChat} />
        ))}
      </div>

      {record.nextCursor && (
        <button
          type="button"
          onClick={onLoadMore}
          disabled={record.loading}
          class="inline-flex h-8 items-center gap-1.5 rounded-md border border-line px-2.5 text-[12px] text-ink-300 hover:bg-tint-strong hover:text-ink-100 disabled:opacity-50"
        >
          {record.loading && <Loader class="h-3.5 w-3.5 animate-spin" />} Show older
        </button>
      )}
    </div>
  );
}

function JournalCard({
  entry,
  onOpenChat,
}: {
  entry: JournalEntry;
  onOpenChat?: (chatId: string) => void;
}) {
  const tone =
    entry.status === "failed"
      ? "text-accent-red"
      : entry.status === "cancelled"
        ? "text-ink-400"
        : "text-accent-green";

  return (
    <article class="rounded-lg border border-line bg-tint p-3">
      <header class="flex flex-wrap items-center gap-2 text-[11.5px] text-ink-400">
        <Clock class="h-3.5 w-3.5" />
        <span class="tabular-nums">{formatEpochMillis(entry.at)}</span>
        <span class={tone}>{statusLabel(entry.status)}</span>
        {entry.scheduled && <span>Scheduled task</span>}
        {entry.synthetic && <span>{entry.synthetic}</span>}
        {entry.chatTitle && onOpenChat && (
          <button
            type="button"
            onClick={() => onOpenChat(entry.chatId)}
            class="ms-auto max-w-[16rem] truncate text-accent-blue hover:underline"
            title={entry.chatTitle}
          >
            {entry.chatTitle}
          </button>
        )}
      </header>

      <p class="mt-2 whitespace-pre-wrap text-[13px] text-ink-100" dir="auto">
        {entry.request}
      </p>
      {entry.summary && (
        <p class="mt-1.5 whitespace-pre-wrap text-[12.5px] text-ink-300" dir="auto">
          {entry.summary}
        </p>
      )}
      {entry.error && (
        <p class="mt-1.5 text-[12.5px] text-accent-red" dir="auto">
          {entry.error}
        </p>
      )}

      {entry.files && entry.files.length > 0 && (
        <ul class="mt-2 flex flex-wrap gap-1.5">
          {entry.files.map((file) => (
            <li
              key={file}
              class="inline-flex items-center gap-1 rounded border border-line bg-inset px-1.5 py-0.5 text-[11px] text-ink-200"
            >
              <FileText class="h-3 w-3 flex-none text-ink-400" />
              <span class="font-mono" dir="ltr">
                {file}
              </span>
            </li>
          ))}
        </ul>
      )}
      {entry.commands && entry.commands.length > 0 && (
        <ul class="mt-1.5 space-y-1">
          {entry.commands.map((command) => (
            <li key={command} class="flex items-start gap-1.5 text-[11px] text-ink-400">
              <Terminal class="mt-0.5 h-3 w-3 flex-none" />
              <span class="font-mono break-all" dir="ltr">
                {command}
              </span>
            </li>
          ))}
        </ul>
      )}
    </article>
  );
}

function statusLabel(status: JournalEntry["status"]): string {
  switch (status) {
    case "failed":
      return "Failed";
    case "cancelled":
      return "Cancelled";
    default:
      return "Done";
  }
}
