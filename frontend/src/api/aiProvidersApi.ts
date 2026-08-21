import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  PoolView,
  ProviderInput,
  ProviderTestResult,
  QuotaView,
} from "../models/aiProviders";

/**
 * The free-tier provider pool's endpoints.
 *
 * Every admin write answers with the whole `PoolView`, so the panel replaces
 * its state from the response instead of patching a local copy — the meters,
 * the statuses and the priority order all move together when one entry
 * changes.
 *
 * Note the collection takes two different verbs: POST creates or updates one
 * provider, PUT stores the pool's own policy. That is the server's contract,
 * not a slip.
 */
export const aiProvidersApi = {
  list: () => requestJson<PoolView>("GET", API_ROUTES.aiProviders.collection),

  save: (input: ProviderInput) =>
    requestJson<PoolView>("POST", API_ROUTES.aiProviders.collection, input),

  update: (id: string, input: ProviderInput) =>
    requestJson<PoolView>("PUT", API_ROUTES.aiProviders.item(id), input),

  remove: (id: string) =>
    requestJson<PoolView>("DELETE", API_ROUTES.aiProviders.item(id)),

  /** The whole priority order in one request, so no row is ever half-moved. */
  reorder: (ids: string[]) =>
    requestJson<PoolView>("POST", API_ROUTES.aiProviders.reorder, { ids }),

  saveSettings: (autoSwitch: boolean, preferredProviderId: string) =>
    requestJson<PoolView>("PUT", API_ROUTES.aiProviders.collection, {
      autoSwitch,
      preferredProviderId,
    }),

  /** Runs one real completion. A refusal comes back 200 with `ok: false`. */
  test: (id: string) =>
    requestJson<ProviderTestResult>("POST", API_ROUTES.aiProviders.test(id)),

  /** The member-facing card: labels and meters, no endpoint and no key. */
  quota: () => requestJson<QuotaView>("GET", API_ROUTES.aiProviders.quota),
};
