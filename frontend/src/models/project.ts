import type { InheritedSecret } from "./secretsVault";
import type { MCPProjectSettings } from "./mcp";
import type { GitHubLink } from "./github";
import type { ProjectTemplateStatus } from "./template";

export type ProjectStatus =
  | ""
  | "provisioning"
  | "running"
  | "stopped"
  | "error"
  | "missing";

export type ContainerState =
  | "RUNNING"
  | "STOPPED"
  | "FROZEN"
  | "MISSING"
  | "UNKNOWN";

export interface ProjectMeta {
  id: string;
  name: string;
  slug: string;
  cwd: string;
  containerName: string;
  status: ProjectStatus;
  /**
   * Stack preset the container was created from. Absent on projects created
   * before templates existed, which behave like "blank"; read it through
   * projectTemplateName().
   */
  template?: string;
  order?: number;
  errorMsg?: string;
  resourceLimits?: ContainerLimits;
  /**
   * The GitHub repository this project is linked to, or absent when it is not
   * linked. Identity only — the webhook secret and the automation toggles live
   * behind /api/projects/{id}/github/settings, never on this document.
   */
  github?: GitHubLink;
  createdAt: number;
  updatedAt: number;
  /**
   * Unix-ms instant the project was moved to the Trash. A trashed project is
   * excluded from every normal listing (the sidebar never shows one) but keeps
   * its metadata so it can be restored.
   */
  deletedAt?: number;
  deletedBy?: string;
  /** The automatic snapshot taken on the way to the Trash. */
  trashSnapshotId?: string;
}

/** A project in the Trash, with the instant the janitor will purge it. Zero
 *  when retention is disabled, meaning "kept until an admin purges it". */
export interface TrashedProject extends ProjectMeta {
  expiresAt?: number;
}

/** Backward-compatible read of ProjectMeta.template. */
export function projectTemplateName(project: Pick<ProjectMeta, "template">): string {
  return project.template || "blank";
}

export interface WorkspaceInfo {
  hostSource: string;
  containerPath: string;
}

export interface ResourceInfo {
  processes?: number;
  diskUsageBytes?: number;
  memoryCurrentBytes?: number;
  memoryPeakBytes?: number;
  memoryTotalBytes?: number;
  swapCurrentBytes?: number;
  cpuUsageSeconds?: number;
}

export interface NetworkInterface {
  name: string;
  state?: string;
  type?: string;
  hostName?: string;
  macAddress?: string;
  mtu?: number;
  addresses?: string[];
  bytesReceived?: number;
  bytesSent?: number;
}

export interface OSInfo {
  prettyName?: string;
  kernel?: string;
  uptimeSec?: number;
  cpuCount?: number;
  hostname?: string;
}

export interface DiskUsage {
  mountPath: string;
  filesystem?: string;
  totalBytes?: number;
  usedBytes?: number;
  availBytes?: number;
}

export interface ContainerLimits {
  cpu?: string;
  memory?: string;
  disk?: string;
}

export interface ClaudeContainerStatus {
  installed: boolean;
  version?: string;
  claudeMdInstalled: boolean;
  claudeMdInSync: boolean;
}

export interface CodexContainerStatus {
  installed: boolean;
  version?: string;
}

export interface AgentContainerStatus {
  id: string;
  label: string;
  installed: boolean;
  version?: string;
  instructionsPath?: string;
  instructionsInstalled?: boolean;
  instructionsInSync?: boolean;
}

export interface AuthBundleFileStatus {
  hostPath: string;
  containerPath: string;
  hostExists: boolean;
  hostMtime?: number;
  containerExists: boolean;
  containerMtime?: number;
  hostNewer: boolean;
  containerNewer: boolean;
}

export interface AuthBundleStatus {
  name: string;
  files: AuthBundleFileStatus[];
}

export interface ProjectContainerInfo {
  name: string;
  state: ContainerState;
  bootAutostart: boolean;
  image?: string;
  type?: string;
  architecture?: string;
  pid?: number;
  createdAt?: string;
  lastUsedAt?: string;
  workspace?: WorkspaceInfo;
  resources?: ResourceInfo;
  network?: NetworkInterface[];
  os?: OSInfo;
  disks?: DiskUsage[];
  limits?: ContainerLimits;
  limitOverrides?: ContainerLimits;
  claude: ClaudeContainerStatus;
  codex: CodexContainerStatus;
  agents?: AgentContainerStatus[];
  authBundles: AuthBundleStatus[];
  template?: ProjectTemplateStatus;
}

export interface ProjectSecret {
  key: string;
  value: string;
  updatedAt: number;
}

/** Metadata for a public preview link returned by list operations. */
export interface ProjectShare {
  id: string;
  port: number;
  label?: string;
  createdBy?: string;
  createdAt: number;
  expiresAt: number;
}

/**
 * A newly-created public preview link. The URL carries the plaintext token and
 * is returned exactly once, while metadata-only list responses never include it.
 */
export interface CreatedProjectShare extends ProjectShare {
  url: string;
}

export interface SharePortRow {
  port: number;
  process?: string;
  /** Number of cached links currently pointing at this port. */
  shareCount: number;
}

/**
 * Client portal settings for one project. The token is never returned; `url`
 * appears only in the response that minted it (enable or rotate).
 */
export interface ProjectPortal {
  enabled: boolean;
  showPreview: boolean;
  showChangelog: boolean;
  showUsage: boolean;
  brandTitle?: string;
  note?: string;
  /** When the note last changed; the portal page prints it. */
  noteUpdatedAt?: number;
  createdAt?: number;
  updatedAt?: number;
  url?: string;
}

export interface UpdateProjectPortalInput {
  enabled: boolean;
  /** Mints a fresh token, invalidating the link the client currently holds. */
  rotate?: boolean;
  showPreview: boolean;
  showChangelog: boolean;
  showUsage: boolean;
  brandTitle: string;
  note: string;
}

/** One sink's outcome when a client message was handed to the notifiers. */
export interface ClientMessageDelivery {
  sink: string;
  configured: boolean;
  delivered: boolean;
  error?: string;
}

export interface ClientMessageResult {
  /** False when no sink is set up, which is what hides the send button. */
  configured: boolean;
  delivered: ClientMessageDelivery[];
}

export interface ContainerApp {
  port: number;
  address?: string;
  process?: string;
  pid?: number;
}

export type AgentBrowserStatus = "idle" | "starting" | "ready" | "core-ready" | "error" | "stopped";
export type AgentBrowserServerStatus = Exclude<AgentBrowserStatus, "idle">;

export interface AgentBrowserInfo {
  status: AgentBrowserServerStatus;
  url?: string;
  slug?: string;
  port?: number;
  error?: string;
  core?: string;
  view?: string;
  viewerCount?: number;
  uptimeSec?: number;
  lastActivity?: number;
}

/** What one agent-browser status report means for the drawer. */
export interface AgentBrowserView {
  status: AgentBrowserStatus;
  guiUrl: string;
  error: string | null;
  keepPolling: boolean;
}

export interface CreateProjectValidation {
  ok: boolean;
  slug: string;
  // Error text when ok is false; informational "Saved as <slug>" when the
  // slug differs from what was typed.
  message: string;
}

/** Lets an in-flight project load see that its caller has moved on. */
export interface ProjectDataLoadSignal {
  cancelled: boolean;
}

export interface ProjectContainerRecord {
  loading: boolean;
  data?: ProjectContainerInfo;
  error?: string;
  refreshedAt?: number;
}

export interface SecretsRecord {
  loading: boolean;
  data?: ProjectSecret[];
  /**
   * What the platform vault also puts into this project's container. Read
   * only here: values live in Settings -> Secrets vault, and an entry marked
   * `shadowed` is overridden by the project's own secret of the same name.
   */
  inherited?: InheritedSecret[];
  error?: string;
}

/** The client portal record. It never carries the plaintext link. */
export interface PortalRecord {
  loading: boolean;
  data?: ProjectPortal;
  error?: string;
}

/**
 * What MCP servers this project's containers will be configured with, plus
 * when they were last written in. Nothing here is a credential: an entry
 * carries `${KEY}` placeholders, never a value.
 */
export interface ProjectMCPRecord {
  loading: boolean;
  data?: MCPProjectSettings;
  error?: string;
}

export interface SharesRecord {
  loading: boolean;
  data?: ProjectShare[];
  /** Listening ports discovered in the container, used to offer share targets. */
  apps?: ContainerApp[];
  error?: string;
}

export interface AccessRecord {
  loading: boolean;
  data?: string[];
  error?: string;
}
