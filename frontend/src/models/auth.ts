export interface AuthSession {
  authenticated: boolean;
  claimed: boolean;
  localAdminConfigured: boolean;
  googleOAuthEnabled: boolean;
  googleClientId: string;
  adminEmail: string;
  email: string;
  isAdmin: boolean;
  isRegistered: boolean;
}

export interface GoogleOAuthSettings {
  configured: boolean;
  clientId: string;
  redirectUrl: string;
}

export type AgentAuthMode =
  | "managed-code"
  | "managed-device"
  | "managed-api-key"
  | "external"
  | "none";

/** How one provider's login stands, as the settings row draws it. */
export type AgentAuthStatusKind = "no-auth" | "authenticated" | "external" | "unconfigured";

export interface AgentAuthLoginSnapshot {
  active: boolean;
  url?: string;
  awaitingCode?: boolean;
  userCode?: string;
  startedAt?: number;
  expiresAt?: number;
  completed?: boolean;
  error?: string;
}

export interface AgentAuthSnapshot {
  authenticated: boolean;
  warning?: string;
  login: AgentAuthLoginSnapshot;
}

export interface AgentAuthProvider {
  provider: string;
  label: string;
  default?: boolean;
  executionScopes: Array<"host" | "project">;
  authentication: {
    mode: AgentAuthMode;
    instructions?: string;
    satisfiesAccessGate: boolean;
    apiKey?: {
      createUrl: string;
      createLabel: string;
      credentialLabel: string;
    };
  };
  status: AgentAuthSnapshot;
}

export interface AgentAuthCatalog {
  providers: AgentAuthProvider[];
}

export type ClaudeLoginPhase =
  | "idle"
  | "starting"
  | "awaiting-code"
  | "submitting"
  | "done"
  | "error";

/**
 * Antigravity's status. `external` marks a flow the platform does not drive,
 * so a settings page renders instructions instead of a Connect button.
 */
export interface AntigravityAuthStatus {
  authenticated: boolean;
  external: boolean;
  hint?: string;
}

/** Which question the local login screen is asking. */
export type LoginMode = "claim" | "login" | "legacy-setup";

export interface ClaudeAuthStatus {
  authenticated: boolean;
  login?: ClaudeLoginState;
}

// Streamed handshake state, mirroring CodexDeviceLogin. Claude's CLI uses the
// authorization-code grant instead of a device grant: it emits an OAuth URL
// and expects a code pasted back, so there's an `authUrl` + `awaitingCode`
// rather than a `userCode` for the user to read off.
export interface ClaudeLoginState {
  active: boolean;
  authUrl?: string;
  awaitingCode?: boolean;
  startedAt?: number;
  completed?: boolean;
  error?: string;
}

export interface ClaudeLoginState {
  active: boolean;
  authUrl?: string;
  awaitingCode?: boolean;
  startedAt?: number;
  completed?: boolean;
  error?: string;
}

export interface ClaudeLoginStart {
  url: string;
  resumed?: boolean;
}

export interface CodexAuthStatus {
  authenticated: boolean;
  authMode?: string;
  usesApiKey?: boolean;
  deviceLogin?: CodexDeviceLogin;
}

export interface CodexDeviceLogin {
  active: boolean;
  verificationUri?: string;
  userCode?: string;
  startedAt?: number;
  expiresAt?: number;
  completed?: boolean;
  error?: string;
}

export interface KimiAuthStatus {
  authenticated: boolean;
  deviceLogin?: KimiDeviceLogin;
}

export interface KimiDeviceLogin {
  active: boolean;
  verificationUri?: string;
  userCode?: string;
  startedAt?: number;
  expiresAt?: number;
  completed?: boolean;
  error?: string;
}
