import type {
  PoolView,
  ProviderCapability,
  ProviderInput,
  ProviderKind,
  ProviderLimits,
  ProviderModel,
  ProviderStatus,
  ProviderTestResult,
  ProviderView,
  QuotaRow,
  QuotaView,
  UsageMeter,
} from "../../models/aiProviders";
import type { StatusTone } from "../home/dashboardState";

/**
 * Pure state for the "AI providers" panel.
 *
 * Everything the panel decides rather than displays lives here: what a meter
 * with no documented cap reads as, what the operator must fill in before an
 * entry may be enabled, how the little models textarea round-trips, and how
 * the priority list moves. The server validates all of it again — it is the
 * authority — but a form that answers before the round trip is the difference
 * between helping and scolding.
 *
 * No Preact import belongs in this file. Every rule below is a plain function
 * over plain records so it can be pinned by a test instead of discovered in a
 * browser.
 */

/* ------------------------------------------------------------------ *
 * Status
 * ------------------------------------------------------------------ */

/** What each status means in words, because a coloured dot means nothing alone. */
export const PROVIDER_STATUS_LABELS: Record<ProviderStatus, string> = {
  ready: "Ready",
  cooling: "Cooling down",
  exhausted: "Quota used up",
  "no-key": "No key",
  disabled: "Disabled",
};

/**
 * The dot's tone, in the board's shared vocabulary.
 *
 * Cooling and exhausted are amber rather than red on purpose: neither is a
 * fault. A free tier running out on schedule is the pool working, and the
 * platform simply moves to the next provider. Grey is for the two states
 * nobody is waiting on — an entry with no credential, and one switched off.
 */
export const PROVIDER_STATUS_TONE: Record<ProviderStatus, StatusTone> = {
  ready: "green",
  cooling: "amber",
  exhausted: "amber",
  "no-key": "grey",
  disabled: "grey",
};

/** The kinds, named the way an operator recognizes them. */
export const PROVIDER_KIND_LABELS: Record<ProviderKind, string> = {
  openai: "OpenAI-compatible",
  gemini: "Gemini (native)",
  anthropic: "Anthropic (native)",
};

/** The capability tags a model may claim, in panel order. */
export const PROVIDER_CAPABILITIES: ProviderCapability[] = ["text", "code", "bulk"];

/* ------------------------------------------------------------------ *
 * Meters
 * ------------------------------------------------------------------ */

/**
 * A meter's fill, or null when there is nothing to fill against.
 *
 * A missing limit means the vendor documents no cap for that window, which is
 * not the same as a cap of zero. Returning null rather than 0 is what stops
 * the panel drawing a full-looking empty bar for a provider nobody has
 * measured.
 */
export function meterPercent(meter: UsageMeter | undefined): number | null {
  if (!meter) return null;
  if (typeof meter.percent === "number" && Number.isFinite(meter.percent)) {
    return clamp(Math.round(meter.percent), 0, 100);
  }
  if (typeof meter.limit !== "number" || !Number.isFinite(meter.limit) || meter.limit <= 0) {
    return null;
  }
  return clamp(Math.round((meter.used / meter.limit) * 100), 0, 100);
}

/**
 * The number under a bar: "128 / 250" when there is a documented cap, and the
 * bare count when there is not. Never "128 / 0" and never a percentage the
 * vendor never published.
 */
export function meterLabel(meter: UsageMeter | undefined): string {
  if (!meter) return "0";
  const used = formatCount(meter.used);
  if (typeof meter.limit !== "number" || !Number.isFinite(meter.limit) || meter.limit <= 0) {
    return used;
  }
  return `${used} / ${formatCount(meter.limit)}`;
}

/**
 * Where a number came from. It matters: what the provider reports in its own
 * rate-limit headers is the truth, and what we counted is an estimate that
 * misses whatever else is spending the same key.
 */
export function meterSourceLabel(meter: UsageMeter | undefined): string {
  return meter?.source === "reported" ? "reported by provider" : "counted locally";
}

/** The bar's tone. Only the last stretch of a free tier is worth alarm. */
export function meterTone(percent: number | null): StatusTone {
  if (percent == null) return "grey";
  if (percent >= 90) return "red";
  if (percent >= 70) return "amber";
  return "green";
}

/** Thousands separated, because six-digit token counts are unreadable raw. */
export function formatCount(value: number | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "0";
  return Math.round(value).toLocaleString("en-US");
}

/* ------------------------------------------------------------------ *
 * The edit form
 * ------------------------------------------------------------------ */

/** The five nullable limits, held as strings so "" can mean "not documented". */
export interface ProviderLimitsForm {
  rpm: string;
  rpd: string;
  tpm: string;
  tpd: string;
  monthlyTokens: string;
}

/** The editable half of one provider: everything the dialog holds. */
export interface ProviderForm {
  id: string;
  label: string;
  kind: ProviderKind;
  baseUrl: string;
  apiKeyRef: string;
  /** Write-only. Blank means "keep whatever the server already stores". */
  apiKey: string;
  clearApiKey: boolean;
  /** One model per line — see `parseModels`. */
  modelsText: string;
  limits: ProviderLimitsForm;
  priority: number;
  enabled: boolean;
  notes: string;
  /** From the response, so the form knows a key exists without ever holding it. */
  keyConfigured: boolean;
  apiKeyMasked: string;
  /** False for an entry being created, which is the only time the id is editable. */
  existing: boolean;
}

/** A blank entry. OpenAI-compatible, because almost everything speaks it. */
export function emptyProviderForm(): ProviderForm {
  return {
    id: "",
    label: "",
    kind: "openai",
    baseUrl: "",
    apiKeyRef: "",
    apiKey: "",
    clearApiKey: false,
    modelsText: "",
    limits: limitsToForm(null),
    priority: 0,
    enabled: true,
    notes: "",
    keyConfigured: false,
    apiKeyMasked: "",
    existing: false,
  };
}

/**
 * The form one row of the table produces.
 *
 * The key input starts empty and stays empty: the server never echoes a
 * credential, only a mask, and putting the mask in the field would submit
 * "••••1234" as the new key the first time somebody saved an unrelated edit.
 */
export function formProviderFrom(view: ProviderView): ProviderForm {
  return {
    id: view.id,
    label: view.label,
    kind: view.kind,
    baseUrl: view.baseUrl,
    apiKeyRef: view.apiKeyRef ?? "",
    apiKey: "",
    clearApiKey: false,
    modelsText: formatModels(view.models),
    limits: limitsToForm(view.limits),
    priority: view.priority,
    enabled: view.enabled,
    notes: view.notes ?? "",
    keyConfigured: view.keyConfigured,
    apiKeyMasked: view.apiKeyMasked ?? "",
    existing: true,
  };
}

/** Mirrors `idPattern` in `backend/internal/service/providerpool/model.go`. */
const ID_PATTERN = /^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$/;

/** Ids the admin routes spend on their own verbs, so no entry may shadow one. */
const RESERVED_IDS = ["reorder", "settings"];

/** Mirrors the Secrets vault's own key shape. */
const VAULT_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * The reason the entry cannot be saved, or null when it can. The order and the
 * wording follow the backend's `validate`, so the operator reads the same
 * sentence whichever side catches the mistake.
 */
export function providerFormProblem(form: ProviderForm): string | null {
  const id = form.id.trim().toLowerCase();
  if (!ID_PATTERN.test(id)) {
    return "The id must be 2–40 lower-case letters, digits or hyphens, starting and ending with a letter or digit.";
  }
  if (RESERVED_IDS.includes(id)) {
    return `“${id}” is reserved for an API route and cannot be a provider id.`;
  }
  const baseUrl = form.baseUrl.trim();
  if (baseUrl === "") return "A base URL is required.";
  if (!isAbsoluteHttpUrl(baseUrl)) return "The base URL must be an absolute http(s) URL.";
  if (parseModels(form.modelsText).length === 0) return "List at least one model id.";
  const apiKeyRef = form.apiKeyRef.trim();
  if (apiKeyRef !== "" && !VAULT_KEY_PATTERN.test(apiKeyRef)) {
    return "A Secrets-vault key name looks like MY_API_KEY.";
  }
  // Enabling an entry with no credential claims a provider the pool would skip
  // on its first attempt, which is worse than an entry that is honestly off.
  if (form.enabled && !formHasKey(form)) {
    return `Add an API key, or a Secrets-vault key name, before enabling ${form.label.trim() || id}.`;
  }
  return null;
}

/** Whether a credential would exist after this form was saved. */
export function formHasKey(form: ProviderForm): boolean {
  if (form.apiKeyRef.trim() !== "") return true;
  if (form.apiKey.trim() !== "") return true;
  return form.keyConfigured && !form.clearApiKey;
}

/** The payload the dialog submits. */
export function providerInputFromForm(form: ProviderForm): ProviderInput {
  return {
    id: form.id.trim().toLowerCase(),
    label: form.label.trim(),
    kind: form.kind,
    baseUrl: form.baseUrl.trim(),
    apiKeyRef: form.apiKeyRef.trim(),
    apiKey: form.clearApiKey ? "" : form.apiKey.trim(),
    clearApiKey: form.clearApiKey,
    models: parseModels(form.modelsText),
    limits: limitsFromForm(form.limits),
    priority: form.priority,
    enabled: form.enabled,
    notes: form.notes.trim(),
  };
}

function isAbsoluteHttpUrl(value: string): boolean {
  try {
    const parsed = new URL(value);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.host !== "";
  } catch {
    return false;
  }
}

/* ------------------------------------------------------------------ *
 * Models, as text
 * ------------------------------------------------------------------ */

/**
 * Reads the models textarea: one model per line, as
 * `id | label | contextTokens | text,code,bulk`, every field after the id
 * optional.
 *
 * A textarea rather than a repeating sub-form because a provider's model list
 * is the field an operator most often pastes from the vendor's docs, and five
 * inputs per line would make that a chore. Anything unreadable is dropped
 * rather than refused: a stray blank line is not a mistake worth a red box.
 */
export function parseModels(text: string): ProviderModel[] {
  const models: ProviderModel[] = [];
  const seen = new Set<string>();
  for (const line of text.split("\n")) {
    const fields = line.split("|").map((field) => field.trim());
    const id = fields[0] ?? "";
    if (id === "" || seen.has(id)) continue;
    seen.add(id);
    const model: ProviderModel = { id };
    // A label equal to the id carries nothing: the server fills a blank label
    // in with the id anyway, so keeping it would break the round trip.
    const label = fields[1] ?? "";
    if (label !== "" && label !== id) model.label = label;
    const contextTokens = Number.parseInt(fields[2] ?? "", 10);
    if (Number.isFinite(contextTokens) && contextTokens > 0) model.contextTokens = contextTokens;
    const capabilities = parseCapabilities(fields[3] ?? "");
    if (capabilities.length > 0) model.good_for = capabilities;
    models.push(model);
  }
  return models;
}

/**
 * Writes the models textarea back. Trailing empty fields are dropped so the
 * common case — a bare list of model ids — stays a bare list, and
 * `parseModels(formatModels(models))` returns what it was given.
 */
export function formatModels(models: ProviderModel[]): string {
  return models
    .map((model) => {
      const fields = [
        model.id,
        model.label && model.label !== model.id ? model.label : "",
        model.contextTokens ? String(model.contextTokens) : "",
        (model.good_for ?? []).join(","),
      ];
      while (fields.length > 1 && fields[fields.length - 1] === "") fields.pop();
      return fields.join(" | ");
    })
    .join("\n");
}

function parseCapabilities(text: string): ProviderCapability[] {
  const out: ProviderCapability[] = [];
  for (const raw of text.split(",")) {
    const value = raw.trim().toLowerCase() as ProviderCapability;
    if (PROVIDER_CAPABILITIES.includes(value) && !out.includes(value)) out.push(value);
  }
  return out;
}

/** The models column: a few ids and a count for the rest, not a wall of text. */
export function modelsSummary(models: ProviderModel[], shown = 2): string {
  if (models.length === 0) return "no models";
  const ids = models.slice(0, shown).map((model) => model.id);
  const rest = models.length - ids.length;
  return rest > 0 ? `${ids.join(", ")} +${rest}` : ids.join(", ");
}

/* ------------------------------------------------------------------ *
 * Limits
 * ------------------------------------------------------------------ */

/**
 * Reads the five limit inputs. A blank field is `null` — "the vendor
 * documents no cap for this window" — and never 0, which would mean "you may
 * make no requests" and would exhaust the provider before its first call.
 */
export function limitsFromForm(form: ProviderLimitsForm): ProviderLimits {
  return {
    rpm: limitFromText(form.rpm),
    rpd: limitFromText(form.rpd),
    tpm: limitFromText(form.tpm),
    tpd: limitFromText(form.tpd),
    monthlyTokens: limitFromText(form.monthlyTokens),
  };
}

/** Fills the five limit inputs, leaving an undocumented window blank. */
export function limitsToForm(limits: ProviderLimits | null | undefined): ProviderLimitsForm {
  return {
    rpm: limitToText(limits?.rpm),
    rpd: limitToText(limits?.rpd),
    tpm: limitToText(limits?.tpm),
    tpd: limitToText(limits?.tpd),
    monthlyTokens: limitToText(limits?.monthlyTokens),
  };
}

function limitFromText(text: string): number | null {
  const value = Number.parseInt(text.trim(), 10);
  if (!Number.isFinite(value) || value <= 0) return null;
  return value;
}

function limitToText(value: number | null | undefined): string {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? String(value) : "";
}

/** The five windows, labelled once so the dialog and the table agree. */
export const PROVIDER_LIMIT_FIELDS: Array<{
  key: keyof ProviderLimitsForm;
  label: string;
  hint: string;
}> = [
  { key: "rpm", label: "Requests / minute", hint: "RPM" },
  { key: "rpd", label: "Requests / day", hint: "RPD" },
  { key: "tpm", label: "Tokens / minute", hint: "TPM" },
  { key: "tpd", label: "Tokens / day", hint: "TPD" },
  { key: "monthlyTokens", label: "Tokens / month", hint: "Monthly ceiling" },
];

/* ------------------------------------------------------------------ *
 * Order
 * ------------------------------------------------------------------ */

/**
 * Moves one provider by `offset` places in the priority order, clamped to the
 * ends of the list.
 *
 * It returns the *same array reference* when nothing moved, so the panel can
 * skip a round trip on the click that would have pushed the top entry higher.
 */
export function moveProvider(ids: string[], id: string, offset: number): string[] {
  const from = ids.indexOf(id);
  if (from < 0 || offset === 0) return ids;
  const to = clamp(from + offset, 0, ids.length - 1);
  if (to === from) return ids;
  const next = [...ids];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/** The current priority order, which is simply the order the server sent. */
export function providerIds(view: PoolView | null): string[] {
  return (view?.providers ?? []).map((provider) => provider.id);
}

/* ------------------------------------------------------------------ *
 * Test results
 * ------------------------------------------------------------------ */

/** One line describing how the Test went, for the row's result strip. */
export function describeTestResult(result: ProviderTestResult): string {
  if (!result.ok) {
    return `Test failed after ${formatLatency(result.durationMs)}: ${result.error || "no answer"}`;
  }
  return `${result.model} answered in ${formatLatency(result.durationMs)}.`;
}

/**
 * Latency the way an operator reads it. Free tiers are often slow enough that
 * the difference between 400 ms and 4 s decides which provider goes first, and
 * "0.4 s" says that better than "400 ms" once the numbers get long enough to
 * compare.
 */
export function formatLatency(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "—";
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`;
  return `${(durationMs / 1000).toFixed(1)} s`;
}

/* ------------------------------------------------------------------ *
 * The dashboard card
 * ------------------------------------------------------------------ */

/**
 * The rows the home card shows. The server already ordered them by how close
 * each provider is to its documented cap — the one about to run out is the one
 * worth a glance — so this only trims the list to what fits on a card.
 */
export function topQuotaRows(view: QuotaView | null, count = 4): QuotaRow[] {
  return (view?.providers ?? []).slice(0, count);
}

/**
 * The card's subtitle. It names the month, because "month to date" on a board
 * refreshed once a minute is otherwise a number with no window attached.
 */
export function quotaSubtitle(view: QuotaView | null): string {
  if (!view?.available) return "Not set up";
  const count = view.providers.length;
  if (count === 0) return "No providers connected";
  return `${count} provider${count === 1 ? "" : "s"} · ${view.month}`;
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}
