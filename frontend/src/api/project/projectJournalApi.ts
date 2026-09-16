import { requestJson } from "../apiRequest";
import type { JournalPage } from "../../models/journal";
import { API_ROUTES } from "../../config/routes";

export interface JournalQuery {
  /** Matches the request, the summary, the chat title, files and commands. */
  q?: string;
  /** RFC3339 bounds on the run time. */
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
}

function searchParams(query: JournalQuery): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === "" || value === null) continue;
    params.set(key, String(value));
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : "";
}

export const projectJournalApi = {
  listJournal: (id: string, query: JournalQuery = {}) =>
    requestJson<JournalPage>("GET", API_ROUTES.projects.journal(id) + searchParams(query)),

  /** The whole history as Markdown, for download. */
  journalExportUrl: (id: string) => API_ROUTES.projects.journalExport(id),
};
