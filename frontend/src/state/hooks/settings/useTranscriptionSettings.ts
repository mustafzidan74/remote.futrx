import { useCallback, useEffect, useState } from "preact/hooks";
import { transcriptionApi } from "../../../api/transcriptionApi";
import { VOICE_LANGUAGE_OPTIONS } from "../../../config/voice";
import type {
  TranscriptionSettings,
  TranscriptionTestResult,
} from "../../../models/transcription";

/** Language choices the admin panel offers as the server-side default. */
export const TRANSCRIPTION_DEFAULT_LANGUAGE_OPTIONS: ReadonlyArray<{
  value: string;
  label: string;
}> = [
  { value: "", label: "Detect automatically" },
  ...VOICE_LANGUAGE_OPTIONS.filter((option) => option.value !== "auto").map((option) => ({
    value: option.value as string,
    label: option.label,
  })),
];

export interface TranscriptionSettingsEditor {
  settings: TranscriptionSettings | null;
  enabled: boolean;
  apiKey: string;
  model: string;
  defaultLanguage: string;
  models: string[];
  loading: boolean;
  saving: boolean;
  testing: boolean;
  saved: boolean;
  error: string | null;
  testResult: TranscriptionTestResult | null;
  setEnabled: (enabled: boolean) => void;
  setApiKey: (apiKey: string) => void;
  setModel: (model: string) => void;
  setDefaultLanguage: (language: string) => void;
  save: (event: Event) => Promise<void>;
  clearApiKey: () => Promise<void>;
  runTest: () => Promise<void>;
}

/**
 * The Voice input admin panel's state. It mirrors the notifications editor:
 * the key is write-only, so its input stays blank and an empty submission
 * means "keep whatever is stored".
 */
export function useTranscriptionSettings(enabledPanel: boolean): TranscriptionSettingsEditor {
  const [settings, setSettings] = useState<TranscriptionSettings | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("");
  const [defaultLanguage, setDefaultLanguage] = useState("");
  const [models, setModels] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<TranscriptionTestResult | null>(null);

  const adopt = useCallback((value: TranscriptionSettings) => {
    setSettings(value);
    setEnabled(value.enabled);
    setModel(value.model);
    setDefaultLanguage(value.defaultLanguage ?? "");
    setModels(value.models ?? []);
    // The key is never returned, so its input stays empty and an empty
    // submission means "keep what the server already has".
    setApiKey("");
  }, []);

  useEffect(() => {
    if (!enabledPanel) return;
    let cancelled = false;
    setLoading(true);
    transcriptionApi
      .settings()
      .then((value) => !cancelled && adopt(value))
      .catch((cause) => !cancelled && setError((cause as Error).message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [adopt, enabledPanel]);

  // `enabledOverride` exists because "remove the stored key" has to submit
  // `enabled: false` in the same request: a React state update queued in the
  // same tick would not be visible to this closure.
  const submit = useCallback(
    async (clearApiKeyFlag: boolean, enabledOverride?: boolean) => {
      setSaving(true);
      setError(null);
      setSaved(false);
      setTestResult(null);
      try {
        adopt(
          await transcriptionApi.save({
            enabled: enabledOverride ?? enabled,
            provider: settings?.provider || "openai",
            apiKey: clearApiKeyFlag ? "" : apiKey,
            clearApiKey: clearApiKeyFlag,
            model,
            defaultLanguage,
          }),
        );
        setSaved(true);
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setSaving(false);
      }
    },
    [adopt, apiKey, defaultLanguage, enabled, model, settings?.provider],
  );

  return {
    settings,
    enabled,
    apiKey,
    model,
    defaultLanguage,
    models,
    loading,
    saving,
    testing,
    saved,
    error,
    testResult,
    setEnabled,
    setApiKey,
    setModel,
    setDefaultLanguage,
    save: async (event: Event) => {
      event.preventDefault();
      await submit(false);
    },
    // Removing the key also switches the fallback off, because a server the
    // composer offers but cannot use is worse than no button at all.
    clearApiKey: async () => {
      setApiKey("");
      await submit(true, false);
    },
    runTest: async () => {
      setTesting(true);
      setError(null);
      try {
        setTestResult(await transcriptionApi.test());
      } catch (cause) {
        setError((cause as Error).message);
      } finally {
        setTesting(false);
      }
    },
  };
}
