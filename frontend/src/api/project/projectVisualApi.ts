import { requestJson } from "../apiRequest";
import type { VisualBaseline, VisualComparison, VisualOverview } from "../../models/visualDiff";
import { API_ROUTES } from "../../config/routes";

export const projectVisualApi = {
  overview: (id: string) =>
    requestJson<VisualOverview>("GET", API_ROUTES.projects.visual(id)),

  /** Answers 202 with a running record; poll overview for the outcome. */
  setBaseline: (
    id: string,
    body: { port: number; paths: string[]; fullPage?: boolean; width?: number; height?: number },
  ) => requestJson<VisualBaseline>("POST", API_ROUTES.projects.visualBaseline(id), body),

  /** Answers 202 with a running record; poll overview for the outcome. */
  compare: (id: string, body: { label?: string }) =>
    requestJson<VisualComparison>("POST", API_ROUTES.projects.visualCompare(id), body),

  deleteComparison: (id: string, comparisonId: string) =>
    requestJson<void>("DELETE", API_ROUTES.projects.visualComparison(id, comparisonId)),
};
