import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type { Playbook } from "../models/playbook";

interface PlaybookCollection {
  playbooks: Playbook[];
}

/**
 * The composer's prompt templates. Reading is open to every signed-in user;
 * the PUT is admin-only and replaces the whole library, which is what the
 * Settings page edits.
 */
export const playbookApi = {
  list: async (): Promise<Playbook[]> => {
    const payload = await requestJson<PlaybookCollection>("GET", API_ROUTES.playbooks.collection);
    return payload.playbooks ?? [];
  },

  saveAll: async (playbooks: Playbook[]): Promise<Playbook[]> => {
    const payload = await requestJson<PlaybookCollection>("PUT", API_ROUTES.playbooks.admin, {
      playbooks,
    });
    return payload.playbooks ?? [];
  },
};
