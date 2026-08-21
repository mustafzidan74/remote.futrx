/**
 * The free-tier provider pool: several third-party model APIs — Gemini, Groq,
 * Cerebras, OpenRouter, GLM, Mistral, GitHub Models — connected side by side
 * so the platform's own small text jobs and its bulk lane can move to the next
 * provider when one runs out of quota.
 *
 * Two things are worth knowing before reading any number below.
 *
 * The pool never serves a coding agent. Claude, Codex and Kimi keep running
 * every prompt with their own credentials; this is for chat titles, run
 * summaries, translations, and whatever future feature wants a lot of cheap
 * text. Nothing in the UI may become load bearing on it.
 *
 * And every limit here is *advisory*. The shipped numbers are what the vendor
 * documented when the seed was written, and vendors change them without
 * notice. A `null` limit means "not documented", never "zero" — the panel
 * draws an empty track and prints the raw count rather than inventing a
 * percentage. What a provider's own rate-limit headers report always wins
 * over what we counted, which is what `Meter.source` distinguishes.
 */

/** The wire shape a provider speaks. Everything above it is shared. */
export type ProviderKind = "openai" | "gemini" | "anthropic";

/** What a model is worth using for. Advisory routing metadata, not a promise. */
export type ProviderCapability = "text" | "code" | "bulk";

/** The dot beside a provider's name. */
export type ProviderStatus = "ready" | "cooling" | "no-key" | "disabled" | "exhausted";

/** Where a credential lives. A vault reference is a key *name*, never a value. */
export type ProviderKeySource = "vault" | "inline";

/**
 * Whether a meter is what we counted locally or what the provider itself
 * reported back in its rate-limit headers. Reported beats counted, and the
 * panel says which one it is showing.
 */
export type MeterSource = "counted" | "reported";

/** One model a provider offers. `good_for` keeps the registry's wire name. */
export interface ProviderModel {
  id: string;
  label?: string;
  contextTokens?: number;
  good_for?: ProviderCapability[];
}

/**
 * The documented caps for one provider. Every field is nullable because a
 * vendor that documents no cap for a window is a real and common case.
 */
export interface ProviderLimits {
  rpm: number | null;
  rpd: number | null;
  tpm: number | null;
  tpd: number | null;
  monthlyTokens: number | null;
}

/**
 * One usage window. `limit` and `percent` are absent when the vendor
 * documents no cap for that window — render an empty track and the raw count.
 */
export interface UsageMeter {
  used: number;
  limit?: number;
  percent?: number;
  source: MeterSource;
}

/** Everything the panel knows about how much of one free tier is gone. */
export interface ProviderUsage {
  requestsToday: UsageMeter;
  tokensToday: UsageMeter;
  tokensMonth: UsageMeter;
  requestsMinute: UsageMeter;
  tokensMinute: UsageMeter;
  errors: number;
  lastError?: string;
  lastErrorAt?: number;
  lastUsedAt?: number;
  cooldownUntil?: number;
  /** When the provider itself said its window resets, if it ever said so. */
  reportedResetAt?: number;
}

/** One row of the settings table. No credential crosses this boundary. */
export interface ProviderView {
  id: string;
  label: string;
  kind: ProviderKind;
  baseUrl: string;
  apiKeyRef?: string;
  apiKeyMasked?: string;
  keyConfigured: boolean;
  keySource?: ProviderKeySource;
  models: ProviderModel[];
  limits: ProviderLimits;
  /** Set on a shipped seed, cleared once an operator edits the limits. */
  limitsNote?: string;
  priority: number;
  enabled: boolean;
  notes?: string;
  seed?: boolean;
  updatedAt?: number;
  status: ProviderStatus;
  usage: ProviderUsage;
}

/** The pool's global policy: may it move on, and where does it go if not. */
export interface PoolSettings {
  autoSwitch: boolean;
  preferredProviderId?: string;
  updatedAt?: number;
}

/** The whole admin panel payload, in one request. */
export interface PoolView {
  providers: ProviderView[];
  settings: PoolSettings;
  kinds: ProviderKind[];
  capabilities: ProviderCapability[];
  /** Whether anything at all could take a request right now. */
  available: boolean;
  /** The ledger month the month-to-date meters count against, as "2026-08". */
  month: string;
  /** The seed warning, echoed so the panel keeps no second copy of it. */
  seedLimitsNote: string;
}

/**
 * The create/update payload. `apiKey` is write-only: send "" to keep whatever
 * is stored, and `clearApiKey` to remove it. The server answers every write
 * with the whole `PoolView`, so the panel never reassembles its own state.
 */
export interface ProviderInput {
  id: string;
  label: string;
  kind: ProviderKind;
  baseUrl: string;
  apiKeyRef: string;
  apiKey: string;
  clearApiKey: boolean;
  models: ProviderModel[];
  limits: ProviderLimits;
  priority: number;
  enabled: boolean;
  notes: string;
}

/** One real completion, run from a row's Test button. */
export interface ProviderTestResult {
  ok: boolean;
  providerId: string;
  label: string;
  model: string;
  durationMs: number;
  answer?: string;
  error?: string;
}

/** One line of the home dashboard's Free quota card. */
export interface QuotaRow {
  id: string;
  label: string;
  status: ProviderStatus;
  requestsToday: UsageMeter;
  tokensToday: UsageMeter;
  tokensMonth: UsageMeter;
}

/**
 * What every signed-in member may read: labels and meters, no endpoint and no
 * key. `available` false means nobody has connected anything, which the card
 * renders as "not set up" rather than as an empty list.
 */
export interface QuotaView {
  available: boolean;
  providers: QuotaRow[];
  month: string;
}
