/**
 * Third-party agent endpoints: vendor-published compatibility APIs the
 * operator has their own account with, which one chat's coding agent can be
 * pointed at instead of the vendor's own default.
 *
 * Two shapes travel over the wire, and the difference is the whole access
 * model:
 *
 * - {@link AgentEndpointChoice} is what any signed-in user reads so the
 *   composer can list what a chat may be pointed at. It says which vendor and
 *   which models, and nothing about how to reach them.
 * - {@link AgentEndpoint} is the administrator's view: the base URL, the
 *   Secrets-vault key *reference*, and the last test. The key itself never
 *   crosses the wire in either direction — the server resolves it at run time.
 */

/** The two agent CLIs whose vendors document a compatibility mode. */
export type AgentEndpointCLI = "claude" | "codex";

/** The codex `wire_api` protocol selection. Ignored for the claude CLI. */
export type AgentEndpointWireAPI = "responses" | "chat";

export interface AgentEndpointModel {
  id: string;
  label?: string;
}

/** The outcome of the last Test, as the admin table shows it. */
export interface AgentEndpointTestRecord {
  at: number;
  ok: boolean;
  projectId?: string;
  model?: string;
  /** A short, already-masked reason. Empty on success. */
  message?: string;
}

/** One stored profile, as an administrator sees it. */
export interface AgentEndpoint {
  id: string;
  label: string;
  cli: AgentEndpointCLI;
  baseUrl: string;
  /** The *name* of a Secrets-vault entry, never its value. */
  apiKeyRef: string;
  models?: AgentEndpointModel[];
  headers?: Record<string, string>;
  wireApi?: AgentEndpointWireAPI;
  notes?: string;
  enabled: boolean;
  updatedAt?: number;
  updatedBy?: string;
  lastTest?: AgentEndpointTestRecord;
  /**
   * Whether `apiKeyRef` names a vault entry holding a value right now. It is
   * why a run would fail before it started, so the table says so instead of
   * leaving the operator to find out from a probe.
   */
  keyResolved: boolean;
}

/** The admin collection read. */
export interface AgentEndpointRegistry {
  endpoints: AgentEndpoint[];
  supportedCLIs: AgentEndpointCLI[];
  /** CLIs with no documented third-party mode, named so the UI can say so. */
  unsupportedCLIs: string[];
  wireApis: AgentEndpointWireAPI[];
}

/** What the composer needs to offer one endpoint. */
export interface AgentEndpointChoice {
  id: string;
  label: string;
  cli: AgentEndpointCLI;
  models: AgentEndpointModel[];
}

/** The member-facing read. */
export interface AgentEndpointChoices {
  endpoints: AgentEndpointChoice[];
  supportedCLIs: AgentEndpointCLI[];
  unsupportedCLIs: string[];
}

/** One probe's outcome: raw CLI output with the resolved key masked out. */
export interface AgentEndpointTestResult {
  ok: boolean;
  output: string;
  /**
   * Why the probe failed, kept apart from `output`. A CLI whose key the
   * endpoint rejected often prints an unrelated warning and then hangs, so
   * what it printed and why the test failed are two different things.
   */
  error?: string;
  durationMs: number;
}

/** The create/update body. Mirrors the stored profile; carries no secret. */
export interface AgentEndpointPayload {
  id: string;
  label: string;
  cli: AgentEndpointCLI;
  baseUrl: string;
  apiKeyRef: string;
  models: AgentEndpointModel[];
  headers: Record<string, string>;
  wireApi: AgentEndpointWireAPI;
  notes: string;
  enabled: boolean;
}
