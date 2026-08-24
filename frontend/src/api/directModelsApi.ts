import { API_ROUTES } from "../config/routes";
import type { DirectModelChoice } from "../models/directModels";

/**
 * Reads which completion-API models are available right now.
 *
 * Fetched rather than cached at boot: an administrator can enable a provider
 * or change the local model while the app is open, and a stale list either
 * offers a model that no longer answers or hides one that does.
 */
export const directModelsApi = {
  async list(): Promise<DirectModelChoice[]> {
    const response = await fetch(API_ROUTES.directModels.choices, { credentials: "same-origin" });
    if (!response.ok) return [];
    const body = (await response.json()) as { models?: DirectModelChoice[] } | null;
    return body?.models ?? [];
  },
};
