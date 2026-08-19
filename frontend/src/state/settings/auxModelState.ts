import type {
  AuxModelDefaults,
  AuxModelJobId,
  AuxModelProvider,
  AuxModelSettings,
  AuxModelTestResult,
  UpdateAuxModelInput,
} from "../../models/auxModel";

/**
 * Pure state for the "Local / auxiliary model" settings panel.
 *
 * The panel's only non-obvious rules live here so they can be pinned by tests
 * rather than discovered in the browser: what a blank endpoint means for each
 * provider, what the operator has to fill in before the switch may be turned
 * on, and how the Test result reads once it comes back.
 */

/** The editable half of the panel: everything the form holds. */
export interface AuxModelForm {
  enabled: boolean;
  provider: AuxModelProvider;
  baseUrl: string;
  model: string;
  apiKey: string;
  timeoutSeconds: number;
  maxTokens: number;
  jobs: Record<string, boolean>;
}

export const AUX_MODEL_PROVIDER_LABELS: Record<AuxModelProvider, string> = {
  ollama: "Ollama (local)",
  "openai-compatible": "OpenAI-compatible endpoint",
};

/** The defaults the panel falls back to before the first load lands. */
export const AUX_MODEL_FALLBACK_DEFAULTS: AuxModelDefaults = {
  ollamaBaseUrl: "http://127.0.0.1:11434",
  model: "qwen2.5:3b",
  timeoutSeconds: 30,
  maxTokens: 1200,
  minTimeoutSeconds: 3,
  maxTimeoutSeconds: 120,
  minMaxTokens: 16,
  maxMaxTokens: 4096,
};

/** The form a freshly loaded settings document produces. */
export function formFromSettings(settings: AuxModelSettings): AuxModelForm {
  return {
    enabled: settings.enabled,
    provider: settings.provider,
    baseUrl: settings.baseUrl,
    model: settings.model,
    // The key is never returned, so its input stays empty and an empty
    // submission means "keep what the server already has".
    apiKey: "",
    timeoutSeconds: settings.timeoutSeconds,
    maxTokens: settings.maxTokens,
    jobs: { ...settings.jobs },
  };
}

/**
 * Switching provider swaps the endpoint suggestion, but only when the operator
 * has not typed one of their own — retyping a remote URL because you glanced
 * at the Ollama option is the kind of small betrayal that makes people
 * distrust a form.
 */
export function applyProviderChange(
  form: AuxModelForm,
  provider: AuxModelProvider,
  defaults: AuxModelDefaults,
): AuxModelForm {
  if (provider === form.provider) return form;
  const wasSuggestion =
    form.baseUrl.trim() === "" || form.baseUrl.trim() === defaults.ollamaBaseUrl;
  return {
    ...form,
    provider,
    baseUrl: provider === "ollama" && wasSuggestion ? defaults.ollamaBaseUrl : form.baseUrl,
  };
}

/**
 * The reason the form cannot be saved, or null when it can.
 *
 * The server validates all of this again — it is the authority — but saying
 * "paste a base URL" before the round trip is the difference between a form
 * that helps and a form that scolds.
 */
export function validateAuxModelForm(
  form: AuxModelForm,
  settings: AuxModelSettings | null,
  defaults: AuxModelDefaults,
): string | null {
  const baseUrl = form.baseUrl.trim();
  if (baseUrl === "") return "Paste the endpoint's base URL.";
  if (!/^https?:\/\/[^\s]+$/i.test(baseUrl)) {
    return "The base URL must start with http:// or https://.";
  }
  if (form.model.trim() === "") return "Name the model to use.";
  if (
    form.timeoutSeconds < defaults.minTimeoutSeconds ||
    form.timeoutSeconds > defaults.maxTimeoutSeconds
  ) {
    return `The timeout must be between ${defaults.minTimeoutSeconds} and ${defaults.maxTimeoutSeconds} seconds.`;
  }
  if (form.maxTokens < defaults.minMaxTokens || form.maxTokens > defaults.maxMaxTokens) {
    return `The answer cap must be between ${defaults.minMaxTokens} and ${defaults.maxMaxTokens} tokens.`;
  }
  // A hosted endpoint usually needs a key; a loopback one never does. Warning
  // rather than blocking would be wrong here: the switch claims the feature
  // works, and it would not.
  if (
    form.enabled &&
    form.provider === "openai-compatible" &&
    !isLoopback(baseUrl) &&
    form.apiKey.trim() === "" &&
    !settings?.keyConfigured
  ) {
    return "Add an API key for a remote endpoint, or point the base URL at a local one.";
  }
  return null;
}

/** Whether a URL points at this same host, where no credential is expected. */
export function isLoopback(baseUrl: string): boolean {
  try {
    const { hostname } = new URL(baseUrl);
    return hostname === "127.0.0.1" || hostname === "localhost" || hostname === "::1";
  } catch {
    return false;
  }
}

/** The payload the panel submits. */
export function updateInputFromForm(
  form: AuxModelForm,
  options: { clearApiKey?: boolean; enabledOverride?: boolean } = {},
): UpdateAuxModelInput {
  return {
    enabled: options.enabledOverride ?? form.enabled,
    provider: form.provider,
    baseUrl: form.baseUrl.trim(),
    model: form.model.trim(),
    apiKey: options.clearApiKey ? "" : form.apiKey.trim(),
    clearApiKey: options.clearApiKey ?? false,
    timeoutSeconds: form.timeoutSeconds,
    maxTokens: form.maxTokens,
    jobs: { ...form.jobs },
  };
}

/** Flips one job toggle without disturbing the others. */
export function toggleJob(
  form: AuxModelForm,
  job: AuxModelJobId | string,
  enabled: boolean,
): AuxModelForm {
  return { ...form, jobs: { ...form.jobs, [job]: enabled } };
}

/** Whether a job's own toggle is on. An unknown job defaults to on, like the server's. */
export function jobEnabled(jobs: Record<string, boolean>, job: AuxModelJobId | string): boolean {
  return jobs[job] !== false;
}

/** One line describing how the Test went, for the panel's result strip. */
export function describeTestResult(result: AuxModelTestResult): string {
  if (!result.ok) {
    return `Test failed after ${formatLatency(result.durationMs)}: ${result.error || "no answer"}`;
  }
  return `${result.model} answered in ${formatLatency(result.durationMs)}.`;
}

/**
 * Latency the way an operator reads it. Sub-second answers are the point of a
 * local model, and "0.4 s" says that better than "400 ms" once the numbers get
 * long enough to compare.
 */
export function formatLatency(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "—";
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`;
  return `${(durationMs / 1000).toFixed(1)} s`;
}

/**
 * How a latency reads as a verdict. A local 3B model on a small VPS answers a
 * six-word title in well under two seconds; past five it is slower than the
 * truncation it replaces is annoying, and the operator should know.
 */
export function latencyVerdict(durationMs: number): "fast" | "usable" | "slow" {
  if (durationMs <= 2000) return "fast";
  if (durationMs <= 5000) return "usable";
  return "slow";
}
