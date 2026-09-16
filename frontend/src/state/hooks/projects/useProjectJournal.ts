import { useCallback, useEffect, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectDataLoadSignal, ProjectMeta } from "../../../models/project";
import type { JournalEntry, JournalFilters } from "../../../models/journal";
import { EMPTY_JOURNAL_FILTERS } from "../../../models/journal";
import { journalState } from "../../projects/journalState";

/** How many entries one page carries. */
const PAGE_SIZE = 25;

export interface JournalRecord {
  loading: boolean;
  entries: JournalEntry[];
  /** Absent when the list is complete. */
  nextCursor?: string;
  error?: string;
}

/**
 * One project's change history: every agent run, what it was asked for and
 * what it changed. Filtering runs on the server, so a long history stays one
 * request per page rather than a download of everything.
 */
export function useProjectJournal(project: ProjectMeta | null, enabled: boolean) {
  const [record, setRecord] = useState<JournalRecord>({ loading: false, entries: [] });
  const [filters, setFilters] = useState<JournalFilters>(EMPTY_JOURNAL_FILTERS);
  const projectId = project?.id;

  const load = useCallback(
    async (active: JournalFilters, signal?: ProjectDataLoadSignal) => {
      if (!projectId) {
        setRecord({ loading: false, entries: [] });
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const page = await projectApi.listJournal(
          projectId,
          journalState.query(active, { limit: PAGE_SIZE }),
        );
        if (signal?.cancelled) return;
        setRecord({ loading: false, entries: page.entries, nextCursor: page.nextCursor });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, entries: [], error: (error as Error).message });
      }
    },
    [projectId],
  );

  const loadMore = useCallback(async () => {
    if (!projectId || !record.nextCursor || record.loading) return;
    setRecord((current) => ({ ...current, loading: true, error: undefined }));
    try {
      const page = await projectApi.listJournal(
        projectId,
        journalState.query(filters, { limit: PAGE_SIZE, cursor: record.nextCursor }),
      );
      setRecord((current) => ({
        loading: false,
        entries: journalState.appendPage(current.entries, page.entries),
        nextCursor: page.nextCursor,
      }));
    } catch (error) {
      setRecord((current) => ({ ...current, loading: false, error: (error as Error).message }));
    }
  }, [filters, projectId, record.loading, record.nextCursor]);

  // Filters are applied as they change; the debounce keeps typing in the
  // search box from asking the server once per keystroke.
  useEffect(() => {
    if (!enabled || !projectId) return;
    const signal = { cancelled: false };
    const timer = setTimeout(() => void load(filters, signal), filters.text ? 250 : 0);
    return () => {
      signal.cancelled = true;
      clearTimeout(timer);
    };
  }, [enabled, filters, load, projectId]);

  const exportUrl = projectId ? projectApi.journalExportUrl(projectId) : "";

  return {
    record,
    filters,
    setFilters,
    clearFilters: useCallback(() => setFilters(EMPTY_JOURNAL_FILTERS), []),
    loadMore,
    exportUrl,
  };
}
