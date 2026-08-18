import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  MonitoringPingResult,
  MonitoringSettings,
  UpdateMonitoringSettingsInput,
} from "../models/monitoring";

export const monitoringApi = {
  get: () => requestJson<MonitoringSettings>("GET", API_ROUTES.monitoring.settings),
  save: (input: UpdateMonitoringSettingsInput) =>
    requestJson<MonitoringSettings>("PUT", API_ROUTES.monitoring.settings, input),
  ping: () => requestJson<MonitoringPingResult>("POST", API_ROUTES.monitoring.ping),
};
