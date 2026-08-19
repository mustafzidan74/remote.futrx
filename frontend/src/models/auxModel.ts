/**
 * The auxiliary model: the platform's own small text model (a local Ollama, or
 * any OpenAI-compatible endpoint) for the cheap internal jobs — chat titles,
 * notification summaries, commit subjects, client-message translation.
 *
 * It never runs a coding agent. Every job it takes falls back to what the
 * platform did before, so nothing in the UI may depend on it being there.
 */

/** The two provider shapes the server speaks. */
export type AuxModelProvider = "ollama" | "openai-compatible";

/** The per-job toggles, keyed by the ids the server publishes. */
export type AuxModelJobId =
  | "chatTitle"
  | "runSummary"
  | "commitMessage"
  | "translate"
  | "chatSummary";

export interface AuxModelJobDescriptor {
  id: AuxModelJobId;
  label: string;
}

/** The values the panel pre-fills and the bounds it validates against. */
export interface AuxModelDefaults {
  ollamaBaseUrl: string;
  model: string;
  timeoutSeconds: number;
  maxTokens: number;
  minTimeoutSeconds: number;
  maxTimeoutSeconds: number;
  minMaxTokens: number;
  maxMaxTokens: number;
}

/** Admin-facing settings. The API key is never echoed back. */
export interface AuxModelSettings {
  enabled: boolean;
  configured: boolean;
  provider: AuxModelProvider;
  baseUrl: string;
  model: string;
  apiKeyMasked?: string;
  keyConfigured: boolean;
  timeoutSeconds: number;
  maxTokens: number;
  jobs: Record<string, boolean>;
  jobLabels: AuxModelJobDescriptor[];
  providers: AuxModelProvider[];
  defaults: AuxModelDefaults;
  updatedAt?: number;
}

/**
 * Write payload. A blank key keeps whatever the server already stores, which
 * is why the form can show a mask instead of the real value; `clearApiKey` is
 * the explicit way to remove one. `jobs` is a partial map: sending one toggle
 * leaves the others exactly as they were.
 */
export interface UpdateAuxModelInput {
  enabled: boolean;
  provider: AuxModelProvider;
  baseUrl: string;
  model: string;
  apiKey: string;
  clearApiKey?: boolean;
  timeoutSeconds: number;
  maxTokens: number;
  jobs: Record<string, boolean>;
}

/** One real completion, run from the Test button. */
export interface AuxModelTestResult {
  ok: boolean;
  provider: string;
  baseUrl: string;
  model: string;
  durationMs: number;
  answer?: string;
  error?: string;
}

/**
 * What every signed-in user may know: which jobs are available right now. It
 * names no endpoint, no model, and no key — a member does not need to know
 * which box answers.
 */
export interface AuxModelAvailability {
  enabled: boolean;
  jobs: Record<string, boolean>;
}

/** The two languages a client message template is written in. */
export type TranslationTarget = "ar" | "en";

export interface TranslationResult {
  text: string;
  target: TranslationTarget;
}
