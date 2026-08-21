import { useCallback, useEffect, useState } from "preact/hooks";
import { auxModelApi } from "../../../api/auxModelApi";
import type {
  AuxModelDefaults,
  AuxModelProvider,
  AuxModelSettings,
  AuxModelTestResult,
} from "../../../models/auxModel";
import {
  AUX_MODEL_FALLBACK_DEFAULTS,
  applyProviderChange,
  formFromSettings,
  setJobSource,
  updateInputFromForm,
  validateAuxModelForm,
  type AuxModelForm,
} from "../../settings/auxModelState";

export interface AuxModelEditor {
  settings: AuxModelSettings | null;
  form: AuxModelForm;
  defaults: AuxModelDefaults;
  loading: boolean;
  saving: boolean;
  testing: boolean;
  saved: boolean;
  error: string | null;
  testResult: AuxModelTestResult | null;
  patch: (next: Partial<AuxModelForm>) => void;
  setProvider: (provider: AuxModelProvider) => void;
  setJob: (job: string, source: string) => void;
  save: (event: Event) => Promise<void>;
  clearApiKey: () => Promise<void>;
  runTest: () => Promise<void>;
}

/**
 * The "Local / auxiliary model" panel's state.
 *
 * It mirrors the voice-input editor: the key is write-only, so its input stays
 * blank and an empty submission means "keep whatever is stored". Every rule
 * worth pinning lives in `auxModelState.ts`; this hook only owns the plumbing.
 */
export function useAuxModel(active: boolean): AuxModelEditor {
  const [settings, setSettings] = useState<AuxModelSettings | null>(null);
  const [form, setForm] = useState<AuxModelForm>(() => blankForm());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<AuxModelTestResult | null>(null);

  const defaults = settings?.defaults ?? AUX_MODEL_FALLBACK_DEFAULTS;

  const adopt = useCallback((value: AuxModelSettings) => {
    setSettings(value);
    setForm(formFromSettings(value));
  }, []);

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    setLoading(true);
    auxModelApi
      .settings()
      .then((value) => !cancelled && adopt(value))
      .catch((cause) => !cancelled && setError((cause as Error).message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [active, adopt]);

  // `enabledOverride` exists because "remove the stored key" has to submit
  // `enabled: false` in the same request: a state update queued in this tick
  // would not be visible to the closure that builds the payload.
  const submit = useCallback(
    async (options: { clearApiKey?: boolean; enabledOverride?: boolean } = {}) => {
      const candidate = { ...form, enabled: options.enabledOverride ?? form.enabled };
      const problem = validateAuxModelForm(candidate, settings, defaults);
      if (problem) {
        setError(problem);
        return;
      }
      setSaving(true);
      setError(null);
      setSaved(false);
      try {
        adopt(await auxModelApi.save(updateInputFromForm(form, options)));
        setSaved(true);
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setSaving(false);
      }
    },
    [adopt, defaults, form, settings],
  );

  return {
    settings,
    form,
    defaults,
    loading,
    saving,
    testing,
    saved,
    error,
    testResult,
    patch: (next) => {
      setForm((current) => ({ ...current, ...next }));
      setError(null);
      setSaved(false);
    },
    setProvider: (provider) =>
      setForm((current) => applyProviderChange(current, provider, defaults)),
    setJob: (job, source) => setForm((current) => setJobSource(current, job, source)),
    save: async (event: Event) => {
      event.preventDefault();
      await submit();
    },
    // Removing the credential also switches the model off, because an endpoint
    // the platform advertises but cannot reach is worse than none at all.
    clearApiKey: async () => {
      setForm((current) => ({ ...current, apiKey: "" }));
      await submit({ clearApiKey: true, enabledOverride: false });
    },
    runTest: async () => {
      setTesting(true);
      setError(null);
      try {
        setTestResult(await auxModelApi.test());
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setTesting(false);
      }
    },
  };
}

function blankForm(): AuxModelForm {
  return {
    enabled: false,
    provider: "ollama",
    baseUrl: AUX_MODEL_FALLBACK_DEFAULTS.ollamaBaseUrl,
    model: AUX_MODEL_FALLBACK_DEFAULTS.model,
    apiKey: "",
    timeoutSeconds: AUX_MODEL_FALLBACK_DEFAULTS.timeoutSeconds,
    maxTokens: AUX_MODEL_FALLBACK_DEFAULTS.maxTokens,
    jobs: {},
  };
}
