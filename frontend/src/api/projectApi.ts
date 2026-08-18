import { requestJson } from "./apiRequest";
import { projectAccessApi } from "./project/projectAccessApi";
import { projectAppsApi } from "./project/projectAppsApi";
import { projectContainerApi } from "./project/projectContainerApi";
import { projectSecretsApi } from "./project/projectSecretsApi";
import { projectSharesApi } from "./project/projectSharesApi";
import type { ProjectMeta } from "../models/project";
import { API_ROUTES } from "../config/routes";

export const projectApi = {
  list: () => requestJson<ProjectMeta[]>("GET", API_ROUTES.projects.collection),
  create: (name: string, template?: string, templateInputs?: Record<string, string>) =>
    requestJson<ProjectMeta>("POST", API_ROUTES.projects.collection, {
      name,
      ...(template ? { template } : {}),
      // Omitted entirely for templates that declare no inputs: the server
      // rejects any input a template does not declare.
      ...(templateInputs && Object.keys(templateInputs).length > 0
        ? { templateInputs }
        : {}),
    }),
  fetch: (id: string) =>
    requestJson<ProjectMeta>("GET", API_ROUTES.projects.item(id)),
  update: (id: string, body: { name?: string }) =>
    requestJson<ProjectMeta>("PATCH", API_ROUTES.projects.item(id), body),
  reorder: (ids: string[]) =>
    requestJson<ProjectMeta[]>("POST", API_ROUTES.projects.reorder, { ids }),
  delete: (id: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.projects.item(id)),
  ...projectContainerApi,
  ...projectAppsApi,
  ...projectSecretsApi,
  ...projectSharesApi,
  ...projectAccessApi,
};
