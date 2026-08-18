import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import {
  DEFAULT_AGENT_PREFERENCES,
  type AgentPreferences,
  type UpdateAgentPreferencesInput,
} from "../models/agentPreferences";

/**
 * The platform agent reply preferences. Both verbs are admin-only; a member
 * never reads this document, because the preference reaches them through the
 * agent's instructions rather than through the UI.
 */
export const agentPreferencesApi = {
  fetch: async (): Promise<AgentPreferences> =>
    normalize(await requestJson<AgentPreferences>("GET", API_ROUTES.agentPreferences)),

  save: async (input: UpdateAgentPreferencesInput): Promise<AgentPreferences> =>
    normalize(await requestJson<AgentPreferences>("PUT", API_ROUTES.agentPreferences, input)),
};

function normalize(prefs: AgentPreferences): AgentPreferences {
  return {
    ...DEFAULT_AGENT_PREFERENCES,
    ...prefs,
    replyLanguage:
      typeof prefs?.replyLanguage === "string" && prefs.replyLanguage
        ? prefs.replyLanguage
        : DEFAULT_AGENT_PREFERENCES.replyLanguage,
    extraInstructions:
      typeof prefs?.extraInstructions === "string" ? prefs.extraInstructions : "",
  };
}
