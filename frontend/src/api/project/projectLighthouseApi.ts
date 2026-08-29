import { requestJson } from "../apiRequest";
import type { LighthouseOverview, LighthouseRun } from "../../models/lighthouse";
import { API_ROUTES } from "../../config/routes";

export const projectLighthouseApi = {
  overview: (id: string) =>
    requestJson<LighthouseOverview>("GET", API_ROUTES.projects.lighthouse(id)),

  /** Answers 202 with a running record; poll overview for the outcome. */
  run: (
    id: string,
    body: { port: number; paths: string[]; formFactor: string; label?: string },
  ) => requestJson<LighthouseRun>("POST", API_ROUTES.projects.lighthouse(id), body),

  /** Synchronous: it is under a minute, and answers with the fresh overview. */
  install: (id: string) =>
    requestJson<LighthouseOverview>("POST", API_ROUTES.projects.lighthouseInstall(id)),

  deleteRun: (id: string, runId: string) =>
    requestJson<void>("DELETE", API_ROUTES.projects.lighthouseRun(id, runId)),
};
