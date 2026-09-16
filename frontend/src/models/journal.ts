/** One agent run as the project's change history records it. */
export interface JournalEntry {
  /** When the run started, in unix milliseconds. */
  at: number;
  projectId: string;
  chatId: string;
  chatTitle?: string;
  provider?: string;
  model?: string;
  /** The message that started the run, as the user wrote it. */
  request: string;
  /** What the agent reported when it finished. */
  summary?: string;
  /** Workspace-relative paths the run wrote or edited. */
  files?: string[];
  /** Shell commands the run executed, first line only. */
  commands?: string[];
  status: "done" | "failed" | "cancelled";
  error?: string;
  durationMs?: number;
  scheduled?: boolean;
  /** The platform loop behind a run nobody typed (autopilot, auto-test, team). */
  synthetic?: string;
}

export interface JournalPage {
  entries: JournalEntry[];
  /** Absent on the last page. */
  nextCursor?: string;
}

/** What the journal is filtered by. Empty strings mean "no bound". */
export interface JournalFilters {
  text: string;
  /** `yyyy-mm-dd`, as the date inputs produce them. */
  from: string;
  to: string;
}

export const EMPTY_JOURNAL_FILTERS: JournalFilters = { text: "", from: "", to: "" };
