/**
 * MCP servers — the external tools an in-container agent may call.
 *
 * A registry entry never carries a credential. Anything secret is a `${KEY}`
 * placeholder plus the Secrets-vault key behind it in `secretRefs`; the value
 * is read from the vault when the config is written into the container and is
 * never returned by the API.
 */

export type MCPTransport = "stdio" | "http";

/** Where an available entry came from, for one project. */
export type MCPSource = "platform" | "project";

export interface MCPScope {
  /** True reaches every project, present and future. */
  all: boolean;
  /** Explicit project ids, used only when `all` is false. */
  projectIds?: string[];
}

/** One registry entry as the API returns it. */
export interface MCPServer {
  name: string;
  transport: MCPTransport;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
  scope: MCPScope;
  /** Agent CLIs this entry is written for. Empty means every supported one. */
  enabledForProviders?: string[];
  description?: string;
  /** Secrets-vault keys behind this entry's `${KEY}` placeholders. */
  secretRefs?: string[];
  updatedAt: number;
  updatedBy?: string;
  /** Providers named on the entry that this platform cannot configure. */
  unsupportedProviders?: string[];
}

/** The write shape. It is the entry itself: there is no write-only field. */
export interface MCPServerDraftPayload {
  name: string;
  transport: MCPTransport;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
  scope: MCPScope;
  enabledForProviders?: string[];
  description?: string;
  secretRefs?: string[];
}

/** The admin collection response. */
export interface MCPRegistry {
  servers: MCPServer[];
  supportedProviders: string[];
  unsupportedProviders: string[];
}

/** One server a project can use, with its origin and whether it is on. */
export interface MCPProjectEntry extends MCPServer {
  source: MCPSource;
  enabled: boolean;
}

/** What the project MCP panel renders. */
export interface MCPProjectSettings {
  available: MCPProjectEntry[];
  /** When the container configs were last written, in milliseconds. */
  materializedAt?: number;
  materializedNames?: string[];
  supportedProviders: string[];
  unsupportedProviders: string[];
}

/** The project write: which entries are off, plus project-only entries. */
export interface MCPProjectPayload {
  disabled: string[];
  servers: MCPServerDraftPayload[];
}

/** The outcome of probing one server inside a chosen project's container. */
export interface MCPTestResult {
  ok: boolean;
  output: string;
  durationMs: number;
}
