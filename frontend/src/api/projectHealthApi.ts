import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type { ProjectHealthReport } from "../models/health";

/**
 * One call for the whole fleet's health. The sidebar does not need it — the
 * workspace socket streams the same rows — but it answers "is the monitor even
 * running?", which is the question behind a silent Project health toggle.
 */
export const projectHealthApi = {
  report: () => requestJson<ProjectHealthReport>("GET", API_ROUTES.projects.health),
};
