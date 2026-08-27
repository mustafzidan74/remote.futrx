import { useCallback, useEffect, useState } from "preact/hooks";
import { aiProvidersApi } from "../../../api/aiProvidersApi";
import type {
  ProviderDiscovery, PoolView, ProviderTestResult } from "../../../models/aiProviders";
import {
  moveProvider,
  providerIds,
  providerInputFromForm,
  type ProviderForm,
} from "../../settings/aiProvidersState";

export interface AiProvidersEditor {
  view: PoolView | null;
  loading: boolean;
  saving: boolean;
  /** The id being probed, so one row spins rather than the whole table. */
  testing: string | null;
  error: string | null;
  testResult: ProviderTestResult | null;
  /** The last model listing, so the row can show what is broken. */
  discovery: ProviderDiscovery | null;
  refresh: () => Promise<void>;
  /** Resolves true when the entry landed, so a dialog knows whether to close. */
  saveProvider: (form: ProviderForm) => Promise<boolean>;
  deleteProvider: (id: string) => Promise<void>;
  reorder: (id: string, offset: number) => Promise<void>;
  saveSettings: (autoSwitch: boolean, preferredProviderId: string) => Promise<void>;
  runTest: (id: string) => Promise<void>;
  /** Asks the provider what it serves. */
  discoverModels: (id: string) => Promise<void>;
  /** Replaces the provider's list with the ids it just reported. */
  adoptModels: (id: string, models: string[]) => Promise<void>;
}

/**
 * The "AI providers" panel's state.
 *
 * Every write answers with the whole pool, so this hook never patches a local
 * copy: it replaces `view` from the response. That is what keeps the meters,
 * the statuses and the priority order honest after a save — a provider that
 * just went from `no-key` to `ready` says so without a second request.
 *
 * The rules worth pinning live in `aiProvidersState.ts`; this owns the
 * plumbing only.
 */
export function useAiProviders(active: boolean): AiProvidersEditor {
  const [view, setView] = useState<PoolView | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [discovery, setDiscovery] = useState<ProviderDiscovery | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<ProviderTestResult | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setView(await aiProvidersApi.list());
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    setLoading(true);
    aiProvidersApi
      .list()
      .then((value) => !cancelled && setView(value))
      .catch((cause) => !cancelled && setError((cause as Error).message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [active]);

  // One write path: every admin route answers with the whole PoolView, so the
  // only thing that differs between saving, deleting and reordering is which
  // request is sent.
  const write = useCallback(async (run: () => Promise<PoolView>): Promise<boolean> => {
    setSaving(true);
    setError(null);
    try {
      setView(await run());
      return true;
    } catch (cause) {
      setError((cause as Error).message);
      return false;
    } finally {
      setSaving(false);
    }
  }, []);

  return {
    view,
    loading,
    saving,
    testing,
    error,
    testResult,
    refresh: load,
    saveProvider: (form) => {
      const input = providerInputFromForm(form);
      // POST creates or updates; PUT on the item exists so the path wins over
      // the body. An edit uses it, because that is the request that cannot
      // rename anything by accident.
      return write(() =>
        form.existing ? aiProvidersApi.update(input.id, input) : aiProvidersApi.save(input),
      );
    },
    deleteProvider: async (id) => {
      await write(() => aiProvidersApi.remove(id));
    },
    reorder: async (id, offset) => {
      const current = providerIds(view);
      const next = moveProvider(current, id, offset);
      // The same reference back means the click asked for something the list
      // cannot do — the top entry moving up — and is not worth a round trip.
      if (next === current) return;
      await write(() => aiProvidersApi.reorder(next));
    },
    saveSettings: async (autoSwitch, preferredProviderId) => {
      await write(() => aiProvidersApi.saveSettings(autoSwitch, preferredProviderId));
    },
    runTest: async (id) => {
      setTesting(id);
      setError(null);
      setTestResult(null);
      try {
        setTestResult(await aiProvidersApi.test(id));
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setTesting(null);
      }
    },
    discovery,
    discoverModels: async (id) => {
      // Shares the `testing` spinner: both are one round trip to the same
      // provider from the same row, and two spinners in one row is noise.
      setTesting(id);
      setError(null);
      setTestResult(null);
      setDiscovery(null);
      try {
        setDiscovery(await aiProvidersApi.discoverModels(id));
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setTesting(null);
      }
    },
    adoptModels: async (id, models) => {
      setDiscovery(null);
      await write(() => aiProvidersApi.adoptModels(id, models));
    },
  };
}
