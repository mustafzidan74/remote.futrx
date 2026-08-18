import { requestJson } from "../apiRequest";
import type { ProjectSecret } from "../../models/project";
import type { InheritedSecret } from "../../models/secretsVault";
import { API_ROUTES } from "../../config/routes";

/**
 * The project's own secrets plus what its container inherits from the
 * platform vault. The inherited list is metadata only — no vault value ever
 * reaches a project member.
 */
export interface ProjectSecretsView {
  secrets: ProjectSecret[];
  inherited: InheritedSecret[];
}

export const projectSecretsApi = {
  listSecrets: async (id: string): Promise<ProjectSecretsView> => {
    const body = await requestJson<ProjectSecretsView | ProjectSecret[]>(
      "GET",
      API_ROUTES.projects.secrets(id)
    );
    // A server that predates the vault answers with a bare array.
    if (Array.isArray(body)) return { secrets: body, inherited: [] };
    return { secrets: body.secrets ?? [], inherited: body.inherited ?? [] };
  },

  setSecret: (id: string, key: string, value: string) =>
    requestJson<ProjectSecret>("PUT", API_ROUTES.projects.secret(id, key), {
      value,
    }),

  deleteSecret: (id: string, key: string) =>
    requestJson<{ ok: boolean }>(
      "DELETE",
      API_ROUTES.projects.secret(id, key)
    ),
};
