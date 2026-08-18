import { API_ROUTES } from "../config/routes";
import type { DashboardSnapshot } from "../models/dashboard";
import { requestJson } from "./apiRequest";

/**
 * One call for the whole home screen. The fan-out across projects, health,
 * usage, schedules and platform state happens on the server, so the landing
 * view costs a single round trip and every tile on it describes one instant.
 */
export const dashboardApi = {
  snapshot: () => requestJson<DashboardSnapshot>("GET", API_ROUTES.dashboard),
};
