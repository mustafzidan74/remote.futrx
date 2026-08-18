import { requestJson } from "./apiRequest";
import type {
  SecretTestResult,
  VaultSecret,
  VaultSecretDraft,
} from "../models/secretsVault";
import { API_ROUTES } from "../config/routes";

// Admin-only client for the platform secrets vault. Every route is gated on
// the server; a non-admin session receives 403. Responses never carry a
// value, so nothing here needs to be scrubbed before it reaches state.
export const secretsVaultApi = {
  list: async (): Promise<VaultSecret[]> => {
    const body = await requestJson<{ secrets?: VaultSecret[] }>(
      "GET",
      API_ROUTES.secretsVault.collection
    );
    return body.secrets ?? [];
  },

  create: (draft: VaultSecretDraft) =>
    requestJson<VaultSecret>("POST", API_ROUTES.secretsVault.collection, draft),

  update: (key: string, draft: VaultSecretDraft) =>
    requestJson<VaultSecret>("PUT", API_ROUTES.secretsVault.item(key), draft),

  remove: (key: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.secretsVault.item(key)),

  test: (key: string) =>
    requestJson<SecretTestResult>("POST", API_ROUTES.secretsVault.test(key)),
};
