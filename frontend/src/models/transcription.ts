/** Admin-facing transcription settings. The API key is never echoed back. */
export interface TranscriptionSettings {
  enabled: boolean;
  configured: boolean;
  provider: string;
  apiKeyMasked?: string;
  model: string;
  defaultLanguage: string;
  models: string[];
  updatedAt?: number;
}

/**
 * Write payload. A blank key keeps whatever the server already stores, which
 * is why the form can show a mask instead of the real value; `clearApiKey` is
 * the explicit way to remove one.
 */
export interface UpdateTranscriptionSettingsInput {
  enabled: boolean;
  provider: string;
  apiKey: string;
  clearApiKey?: boolean;
  model: string;
  defaultLanguage: string;
}

/** The admin round-trip probe against a one-second silent sample. */
export interface TranscriptionTestResult {
  ok: boolean;
  provider: string;
  model: string;
  durationMs: number;
  text?: string;
  error?: string;
}

/**
 * What the composer is allowed to know: whether the server fallback is
 * available and the limits it enforces. No provider identity, no key.
 */
export interface TranscriptionClientConfig {
  enabled: boolean;
  defaultLanguage: string;
  maxBytes: number;
  maxSeconds: number;
}

/** One finished server transcription. */
export interface TranscriptionResult {
  text: string;
  model?: string;
  language?: string;
}
