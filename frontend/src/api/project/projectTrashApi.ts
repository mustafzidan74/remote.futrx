import { requestJson } from "../apiRequest";
import type { ProjectMeta, TrashedProject } from "../../models/project";
import { API_ROUTES } from "../../config/routes";

export const projectTrashApi = {
  /** Admins see every trashed project; members see the ones they belonged to. */
  listTrash: () => requestJson<TrashedProject[]>("GET", API_ROUTES.projects.trash),

  restoreProject: (id: string) =>
    requestJson<ProjectMeta>("POST", API_ROUTES.projects.restore(id)),

  /** Permanent. Admin only. */
  purgeProject: (id: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.projects.purge(id)),
};
