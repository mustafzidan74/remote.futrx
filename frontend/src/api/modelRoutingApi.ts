import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  RoutingDecision,
  RoutingPolicy,
  RoutingTestInput,
  RoutingView,
} from "../models/modelRouting";

/**
 * Automatic model routing. The policy routes are admin-only; `preview` is the
 * one any signed-in user may call, because the composer pill names the model
 * the next turn will use.
 */
export const modelRoutingApi = {
  fetch: () => requestJson<RoutingView>("GET", API_ROUTES.modelRouting.policy),

  save: (policy: RoutingPolicy) =>
    requestJson<RoutingView>("PUT", API_ROUTES.modelRouting.policy, policy),

  test: (input: RoutingTestInput) =>
    requestJson<RoutingDecision>("POST", API_ROUTES.modelRouting.test, input),

  preview: (input: RoutingTestInput) =>
    requestJson<RoutingDecision>("POST", API_ROUTES.modelRouting.preview, input),
};
