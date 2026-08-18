/**
 * The platform secrets vault: one place for the tokens, licence keys,
 * credential files, and SSH targets every project container should have.
 *
 * Values are write-only. The API returns `masked` and never the value itself,
 * so nothing in this model carries plaintext except the draft a form is
 * currently submitting.
 */

export type SecretKind = "env" | "file" | "ssh";

export interface SecretScope {
  /** True reaches every project, present and future. */
  all: boolean;
  /** Explicit project ids, used only when `all` is false. */
  projectIds?: string[];
}

/** An SSH target as the API returns it — connection detail, never the key. */
export interface SSHTargetView {
  name: string;
  host: string;
  port: number;
  user: string;
  knownHostsLine?: string;
}

/** One vault entry as the admin table reads it. */
export interface VaultSecret {
  key: string;
  kind: SecretKind;
  /** Container destination of a `file` entry. */
  path?: string;
  ssh?: SSHTargetView;
  scope: SecretScope;
  description?: string;
  updatedAt: number;
  updatedBy?: string;
  /** `••••••••` plus the last four characters, or empty for no value. */
  masked: string;
  /** False once the value has been cleared: the material stops being synced. */
  hasValue: boolean;
  /** Project ids whose own secret of the same key overrides this `env` entry. */
  shadowedIn?: string[];
  /** The SSH_TARGET_* variables an `ssh` entry publishes. */
  envVars?: string[];
}

/** The write shape. A blank `value` keeps the stored one; `clear` removes it. */
export interface VaultSecretDraft {
  key: string;
  kind: SecretKind;
  value?: string;
  path?: string;
  ssh?: SSHTargetDraft;
  scope: SecretScope;
  description?: string;
  clear?: boolean;
}

export interface SSHTargetDraft {
  name: string;
  host: string;
  port: number;
  user: string;
  privateKey?: string;
  knownHostsLine?: string;
}

/** The outcome of probing one SSH target from the host. */
export interface SecretTestResult {
  ok: boolean;
  output: string;
  latencyMs: number;
}

/**
 * One vault entry a project's container inherits, as the project Secrets tab
 * reads it. `shadowed` means the project defines a secret of the same name,
 * which wins.
 */
export interface InheritedSecret {
  key: string;
  kind: SecretKind;
  source: string;
  shadowed: boolean;
  path?: string;
  description?: string;
}
