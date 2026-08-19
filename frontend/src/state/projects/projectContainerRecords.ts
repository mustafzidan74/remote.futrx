import type {
  ContainerApp,
  ProjectContainerInfo,
  ProjectPortal,
  ProjectSecret,
  ProjectShare,
} from "../../models/project";
import type { InheritedSecret } from "../../models/secretsVault";
import type { MCPProjectSettings } from "../../models/mcp";

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

export interface SharesRecord {
  loading: boolean;
  data?: ProjectShare[];
  /** Listening ports discovered in the container, used to offer share targets. */
  apps?: ContainerApp[];
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

export interface AccessRecord {
  loading: boolean;
  data?: string[];
  error?: string;
}
