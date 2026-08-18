import { useCallback, useEffect, useState } from "preact/hooks";
import { secretsVaultApi } from "../../../api/secretsVaultApi";
import type {
  SecretTestResult,
  VaultSecret,
} from "../../../models/secretsVault";
import { secretsVaultState } from "../../settings/secretsVaultState";
import type { VaultDraft } from "../../settings/secretsVaultState";

export interface SecretsVault {
  secrets: VaultSecret[] | null;
  loading: boolean;
  error: string | null;
  reload: () => Promise<void>;
  create: (draft: VaultDraft) => Promise<void>;
  save: (key: string, draft: VaultDraft) => Promise<void>;
  remove: (key: string) => Promise<void>;
  test: (key: string) => Promise<SecretTestResult>;
}

// useSecretsVault owns the admin vault's remote state. Every mutation folds
// the server's response back into the local list through secretsVaultState so
// ordering stays identical to a fresh load, and no value is ever held here:
// the API answers with a mask.
export function useSecretsVault(enabled: boolean): SecretsVault {
  const [secrets, setSecrets] = useState<VaultSecret[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setSecrets(secretsVaultState.sort(await secretsVaultApi.list()));
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (enabled) void reload();
  }, [enabled, reload]);

  const create = useCallback(async (draft: VaultDraft) => {
    const created = await secretsVaultApi.create(secretsVaultState.toPayload(draft));
    setSecrets((current) => secretsVaultState.upsert(current ?? [], created));
  }, []);

  const save = useCallback(async (key: string, draft: VaultDraft) => {
    const updated = await secretsVaultApi.update(key, secretsVaultState.toPayload(draft));
    setSecrets((current) => secretsVaultState.upsert(current ?? [], updated));
  }, []);

  const remove = useCallback(async (key: string) => {
    await secretsVaultApi.remove(key);
    setSecrets((current) => secretsVaultState.remove(current ?? [], key));
  }, []);

  const test = useCallback((key: string) => secretsVaultApi.test(key), []);

  return { secrets, loading, error, reload, create, save, remove, test };
}
