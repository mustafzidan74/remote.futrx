import { API_ROUTES } from "../config/routes";
import type {
  AgentEndpoint,
  AgentEndpointChoices,
  AgentEndpointPayload,
  AgentEndpointRegistry,
  AgentEndpointTestResult,
} from "../models/agentEndpoints";
import { requestJson } from "./apiRequest";

/**
 * The third-party agent endpoint register.
 *
 * `choices` is the only route an ordinary member calls; everything else is
 * administrator-only and answers 403 otherwise. No route in either direction
 * carries an API key — a profile names a Secrets-vault key and the server
 * resolves it at run time.
 */
export const agentEndpointsApi = {
  /** The composer's read: what a chat may be pointed at. */
  choices: () =>
    requestJson<AgentEndpointChoices>("GET", API_ROUTES.agentEndpoints.choices),
  list: () =>
    requestJson<AgentEndpointRegistry>("GET", API_ROUTES.agentEndpoints.collection),
  create: (payload: AgentEndpointPayload) =>
    requestJson<AgentEndpoint>("POST", API_ROUTES.agentEndpoints.collection, payload),
  update: (id: string, payload: AgentEndpointPayload) =>
    requestJson<AgentEndpoint>("PUT", API_ROUTES.agentEndpoints.item(id), payload),
  remove: (id: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.agentEndpoints.item(id)),
  setEnabled: (id: string, enabled: boolean) =>
    requestJson<AgentEndpoint>("PUT", API_ROUTES.agentEndpoints.enabled(id), { enabled }),
  /**
   * Runs a two-word prompt through the real CLI inside one project's
   * container and returns the raw result, with the resolved key masked out.
   */
  test: (id: string, projectId: string, model: string) =>
    requestJson<AgentEndpointTestResult>("POST", API_ROUTES.agentEndpoints.test(id), {
      projectId,
      model,
    }),
};
