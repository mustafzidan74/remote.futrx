import type { JournalEntry, JournalFilters } from "../../models/journal";
import type { JournalQuery } from "../../api/project/projectJournalApi";

/** A complete `yyyy-mm-dd`, the shape the date inputs produce. */
const LOCAL_DATE = /^\d{4}-\d{2}-\d{2}$/;

export const journalState = {
  /**
   * The query for a set of filters. A half-typed date leaves its bound unset
   * rather than erroring, so the list never blanks while someone types; the
   * shape is checked explicitly because Date parsing would accept "2026-09-".
   */
  query(filters: JournalFilters, options: { limit?: number; cursor?: string } = {}): JournalQuery {
    const query: JournalQuery = {};
    const text = filters.text.trim();
    if (text) query.q = text;
    const from = this.startOfDay(filters.from);
    if (from) query.from = from;
    const to = this.endOfDay(filters.to);
    if (to) query.to = to;
    if (options.limit) query.limit = options.limit;
    if (options.cursor) query.cursor = options.cursor;
    return query;
  },

  /** The first instant of a local day, as RFC3339. */
  startOfDay(date: string): string {
    return this.instant(date, 0, 0, 0, 0);
  },

  /** The last instant of a local day, so "to" includes that whole day. */
  endOfDay(date: string): string {
    return this.instant(date, 23, 59, 59, 999);
  },

  instant(date: string, hours: number, minutes: number, seconds: number, ms: number): string {
    const trimmed = date.trim();
    if (!LOCAL_DATE.test(trimmed)) return "";
    const [year, month, day] = trimmed.split("-").map(Number);
    const parsed = new Date(year, month - 1, day, hours, minutes, seconds, ms);
    if (Number.isNaN(parsed.getTime())) return "";
    return parsed.toISOString();
  },

  /**
   * Appends a page, dropping entries already listed. Runs append while someone
   * reads, so the same entry can arrive twice across pages.
   */
  appendPage(current: JournalEntry[], next: JournalEntry[]): JournalEntry[] {
    const seen = new Set(current.map(journalState.key));
    return current.concat(next.filter((entry) => !seen.has(journalState.key(entry))));
  },

  /** What makes an entry distinct: one run of one chat at one instant. */
  key(entry: JournalEntry): string {
    return `${entry.chatId}:${entry.at}:${entry.request.length}`;
  },

  /** True when any filter is set, so the empty state can say which it is. */
  filtered(filters: JournalFilters): boolean {
    return Boolean(filters.text.trim() || filters.from.trim() || filters.to.trim());
  },
};
