import { requestJson } from "../apiRequest";
import type { ProjectPortal, UpdateProjectPortalInput } from "../../models/project";
import { API_ROUTES } from "../../config/routes";

export const projectPortalApi = {
  getPortal: (id: string) =>
    requestJson<ProjectPortal>("GET", API_ROUTES.projects.portal(id)),

  savePortal: (id: string, body: UpdateProjectPortalInput) =>
    requestJson<ProjectPortal>("PUT", API_ROUTES.projects.portal(id), body),
};
