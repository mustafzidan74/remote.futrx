import type {
  AgentEndpoint,
  AgentEndpointCLI,
  AgentEndpointChoice,
  AgentEndpointModel,
  AgentEndpointPayload,
  AgentEndpointWireAPI,
} from "../../models/agentEndpoints.ts";

/**
 * Pure transitions for the Agent endpoints admin panel and for the composer's
 * endpoint section.
 *
 * The validation here mirrors the server's, which is the point: the server is
 * the authority, but an operator editing a profile should be told that an id
 * is not a legal provider key *before* the round trip, because that rule is
 * not guessable — the id becomes a `model_providers.<id>` config key on the
 * codex command line.
 */

/** The editable form behind one profile. Everything is a string field. */
export interface AgentEndpointDraft {
  id: string;
  label: string;
  cli: AgentEndpointCLI;
  baseUrl: string;
  apiKeyRef: string;
  /** One `id` or `id = Label` per line, in the order the picker shows them. */
  modelLines: string;
  /** One `Name: Value` per line. */
  headerLines: string;
  wireApi: AgentEndpointWireAPI;
  notes: string;
  enabled: boolean;
}

export const AGENT_ENDPOINT_CLIS: readonly AgentEndpointCLI[] = ["claude", "codex"];

export const AGENT_ENDPOINT_CLI_LABELS: Record<AgentEndpointCLI, string> = {
  claude: "Claude Code",
  codex: "Codex",
};

/**
 * What each CLI's compatibility mode is called, shown next to the picker so
 * an operator knows which of a vendor's endpoints to paste.
 */
export const AGENT_ENDPOINT_CLI_MODES: Record<AgentEndpointCLI, string> = {
  claude: "Anthropic-compatible endpoint",
  codex: "OpenAI-compatible endpoint",
};

export const AGENT_ENDPOINT_WIRE_APIS: readonly AgentEndpointWireAPI[] = [
  "responses",
  "chat",
];

const ID_PATTERN = /^[a-z0-9][a-z0-9_-]*$/;
const HEADER_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const VAULT_KEY_PATTERN = /^[A-Z_][A-Z0-9_]*$/;

const MAX_ID_LENGTH = 40;
const MAX_MODELS = 24;
const MAX_HEADERS = 8;

export function emptyDraft(): AgentEndpointDraft {
  return {
    id: "",
    label: "",
    cli: "claude",
    baseUrl: "",
    apiKeyRef: "",
    modelLines: "",
    headerLines: "",
    wireApi: "responses",
    notes: "",
    enabled: false,
  };
}

/** Builds the form from a stored profile. */
export function draftFrom(endpoint: AgentEndpoint): AgentEndpointDraft {
  return {
    id: endpoint.id,
    label: endpoint.label,
    cli: endpoint.cli,
    baseUrl: endpoint.baseUrl,
    apiKeyRef: endpoint.apiKeyRef ?? "",
    modelLines: formatModels(endpoint.models ?? []),
    headerLines: formatHeaders(endpoint.headers ?? {}),
    wireApi: endpoint.wireApi ?? "responses",
    notes: endpoint.notes ?? "",
    enabled: endpoint.enabled,
  };
}

/** `id` or `id = Label`, one per line. */
export function formatModels(models: AgentEndpointModel[]): string {
  return models
    .map((model) => (model.label ? `${model.id} = ${model.label}` : model.id))
    .join("\n");
}

export function parseModels(text: string): AgentEndpointModel[] {
  const models: AgentEndpointModel[] = [];
  const seen = new Set<string>();
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    const id = (separator >= 0 ? trimmed.slice(0, separator) : trimmed).trim();
    const label = separator >= 0 ? trimmed.slice(separator + 1).trim() : "";
    if (!id || seen.has(id)) continue;
    seen.add(id);
    models.push(label ? { id, label } : { id });
  }
  return models;
}

export function formatHeaders(headers: Record<string, string>): string {
  return Object.keys(headers)
    .sort()
    .map((name) => `${name}: ${headers[name]}`)
    .join("\n");
}

export function parseHeaders(text: string): Record<string, string> {
  const headers: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf(":");
    if (separator < 0) continue;
    const name = trimmed.slice(0, separator).trim();
    const value = trimmed.slice(separator + 1).trim();
    if (!name) continue;
    headers[name] = value;
  }
  return headers;
}

/**
 * Reports the first problem with a draft, or null when it is submittable.
 *
 * `platform` distinguishes a new profile (whose id the operator is choosing)
 * from an edit (whose id is immutable, because it is the handle every chat
 * pointed at this endpoint already stores).
 */
export function validate(
  draft: AgentEndpointDraft,
  options: { creating: boolean } = { creating: true },
): string | null {
  const id = draft.id.trim().toLowerCase();
  if (options.creating) {
    if (!id) return "An id is required.";
    if (id.length > MAX_ID_LENGTH) return `An id may be at most ${MAX_ID_LENGTH} characters.`;
    if (!ID_PATTERN.test(id)) {
      return "An id may use only lowercase letters, digits, hyphens and underscores, and must start with a letter or digit.";
    }
  }
  if (!draft.label.trim()) return "A label is required.";
  if (!AGENT_ENDPOINT_CLIS.includes(draft.cli)) return "Choose which agent CLI this endpoint is for.";

  const baseUrl = draft.baseUrl.trim();
  if (!baseUrl) return "A base URL is required.";
  if (!/^https?:\/\/[^\s]+$/.test(baseUrl)) {
    return "The base URL must be an absolute http(s) URL.";
  }
  if (baseUrl.includes("?") || baseUrl.includes("#") || baseUrl.includes("@")) {
    return "The base URL must be a plain endpoint root, with no query, fragment or credentials.";
  }

  const keyRef = draft.apiKeyRef.trim();
  if (keyRef && !VAULT_KEY_PATTERN.test(keyRef)) {
    return "The API key reference must be a Secrets vault key: uppercase letters, digits and underscores.";
  }
  if (draft.enabled && !keyRef) {
    return "An enabled endpoint must name a Secrets vault key holding the operator's API key.";
  }

  const models = parseModels(draft.modelLines);
  if (models.length > MAX_MODELS) return `At most ${MAX_MODELS} models.`;
  for (const model of models) {
    if (/\s/.test(model.id)) return `Model id "${model.id}" contains whitespace.`;
  }

  const headers = parseHeaders(draft.headerLines);
  const headerNames = Object.keys(headers);
  if (headerNames.length > MAX_HEADERS) return `At most ${MAX_HEADERS} headers.`;
  for (const name of headerNames) {
    if (!HEADER_NAME_PATTERN.test(name)) {
      return `Header name "${name}" must use only letters, digits, hyphens and underscores.`;
    }
  }
  return null;
}

/** Renders the draft as the wire payload. */
export function toPayload(draft: AgentEndpointDraft): AgentEndpointPayload {
  return {
    id: draft.id.trim().toLowerCase(),
    label: draft.label.trim(),
    cli: draft.cli,
    baseUrl: draft.baseUrl.trim(),
    apiKeyRef: draft.apiKeyRef.trim(),
    models: parseModels(draft.modelLines),
    headers: parseHeaders(draft.headerLines),
    wireApi: draft.wireApi,
    notes: draft.notes.trim(),
    enabled: draft.enabled,
  };
}

/** Replaces or appends one profile in the loaded list, keeping label order. */
export function upsert(
  endpoints: AgentEndpoint[],
  endpoint: AgentEndpoint,
): AgentEndpoint[] {
  const next = endpoints.filter((candidate) => candidate.id !== endpoint.id);
  next.push(endpoint);
  return sort(next);
}

export function remove(endpoints: AgentEndpoint[], id: string): AgentEndpoint[] {
  return endpoints.filter((endpoint) => endpoint.id !== id);
}

export function sort(endpoints: AgentEndpoint[]): AgentEndpoint[] {
  return [...endpoints].sort(
    (left, right) => left.label.localeCompare(right.label) || left.id.localeCompare(right.id),
  );
}

/** The models column of the admin table. */
export function modelSummary(endpoint: AgentEndpoint): string {
  const models = endpoint.models ?? [];
  if (models.length === 0) return "endpoint default";
  const names = models.map((model) => model.label || model.id);
  if (names.length <= 2) return names.join(", ");
  return `${names[0]}, ${names[1]} +${names.length - 2}`;
}

/**
 * The status word for one profile. "Disabled" wins over a missing key: a
 * profile nobody switched on has no problem to report yet.
 */
export function statusLabel(endpoint: AgentEndpoint): string {
  if (!endpoint.enabled) return "Disabled";
  if (!endpoint.keyResolved) return "Key missing";
  return "Enabled";
}

export function statusTone(endpoint: AgentEndpoint): "off" | "warn" | "on" {
  if (!endpoint.enabled) return "off";
  if (!endpoint.keyResolved) return "warn";
  return "on";
}

/** The "last test" cell. */
export function lastTestLabel(endpoint: AgentEndpoint): string {
  const record = endpoint.lastTest;
  if (!record) return "never tested";
  const when = new Date(record.at).toLocaleString();
  return record.ok ? `passed ${when}` : `failed ${when}`;
}

/**
 * How one endpoint choice is named in the composer's picker — "Claude · GLM-4.6".
 */
export function choiceLabel(choice: AgentEndpointChoice, model: AgentEndpointModel): string {
  return `${AGENT_ENDPOINT_CLI_LABELS[choice.cli]} · ${model.label || model.id}`;
}

/**
 * Flattens the enabled endpoints into the rows the composer's picker renders:
 * one row per model, plus one row for an endpoint that lists no model at all
 * and therefore runs on whatever the vendor defaults to.
 */
export interface AgentEndpointOption {
  endpointId: string;
  endpointLabel: string;
  cli: AgentEndpointCLI;
  model: string;
  label: string;
}

export function endpointOptions(choices: AgentEndpointChoice[]): AgentEndpointOption[] {
  const options: AgentEndpointOption[] = [];
  for (const choice of choices) {
    const models = choice.models ?? [];
    if (models.length === 0) {
      options.push({
        endpointId: choice.id,
        endpointLabel: choice.label,
        cli: choice.cli,
        model: "",
        label: `${AGENT_ENDPOINT_CLI_LABELS[choice.cli]} · ${choice.label}`,
      });
      continue;
    }
    for (const model of models) {
      options.push({
        endpointId: choice.id,
        endpointLabel: choice.label,
        cli: choice.cli,
        model: model.id,
        label: choiceLabel(choice, model),
      });
    }
  }
  return options;
}

/**
 * The red header badge's text for a chat pointed at an endpoint, or null when
 * it is on its vendor's own endpoint.
 *
 * Whose model produced a piece of client code is not a detail: the badge is
 * the thing that stops a GLM-written page being handed to a client as
 * Claude's work, so it names the model, the vendor, and the negative.
 */
export function endpointBadge(
  choices: AgentEndpointChoice[],
  endpointId: string | undefined,
  model: string | undefined,
): { short: string; title: string } | null {
  const id = (endpointId ?? "").trim();
  if (!id) return null;
  const choice = choices.find((candidate) => candidate.id === id);
  const label = choice?.label ?? id;
  const modelId = (model ?? "").trim();
  const known = choice?.models?.find((candidate) => candidate.id === modelId);
  const modelName = known?.label || modelId || choice?.models?.[0]?.label || choice?.models?.[0]?.id || "";
  return {
    short: modelName ? `${modelName} via ${label}` : `via ${label}`,
    title: modelName
      ? `running on ${modelName} via ${label} — not Anthropic`
      : `running via ${label} — not Anthropic`,
  };
}
