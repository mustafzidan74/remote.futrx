import type {
  InheritedSecret,
  SSHTargetDraft,
  SecretKind,
  SecretScope,
  VaultSecret,
  VaultSecretDraft,
} from "../../models/secretsVault";

/**
 * The editable form behind the Add/Edit dialog. It is deliberately flat and
 * always carries every kind's fields, so switching the kind switch keeps what
 * the operator already typed instead of throwing it away.
 */
export interface VaultDraft {
  key: string;
  kind: SecretKind;
  value: string;
  path: string;
  description: string;
  scopeAll: boolean;
  projectIds: string[];
  ssh: SSHTargetDraft;
  /** Set by the "Remove stored value" control; only then is the value wiped. */
  clear: boolean;
}

export const DEFAULT_SSH_PORT = 22;

/** The two container roots a `file` entry may be written into. */
export const FILE_ROOTS = ["/root/", "/workspace/.secrets/"];

const KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const SSH_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

/**
 * SecretsVaultState holds the pure transitions of the admin vault editor:
 * building and validating a draft, keeping the table sorted after a write,
 * and describing an entry in words. Keeping them here — not in the component —
 * is what makes them testable, and mirrors the backend's own rules so a form
 * error arrives before the round trip.
 */
class SecretsVaultState {
  /** A blank draft for the Add dialog. */
  emptyDraft(): VaultDraft {
    return {
      key: "",
      kind: "env",
      value: "",
      path: "",
      description: "",
      scopeAll: true,
      projectIds: [],
      ssh: {
        name: "",
        host: "",
        port: DEFAULT_SSH_PORT,
        user: "root",
        privateKey: "",
        knownHostsLine: "",
      },
      clear: false,
    };
  }

  /** The draft for editing an existing entry. The value starts blank because
   *  the API never returns it, and blank means "keep what is stored". */
  draftFrom(secret: VaultSecret): VaultDraft {
    return {
      key: secret.key,
      kind: secret.kind,
      value: "",
      path: secret.path ?? "",
      description: secret.description ?? "",
      scopeAll: secret.scope?.all ?? false,
      projectIds: [...(secret.scope?.projectIds ?? [])],
      ssh: {
        name: secret.ssh?.name ?? "",
        host: secret.ssh?.host ?? "",
        port: secret.ssh?.port ?? DEFAULT_SSH_PORT,
        user: secret.ssh?.user ?? "root",
        privateKey: "",
        knownHostsLine: secret.ssh?.knownHostsLine ?? "",
      },
      clear: false,
    };
  }

  /** The first problem with a draft, or null when it is submittable. */
  validate(draft: VaultDraft, options: { creating: boolean }): string | null {
    const key = draft.key.trim();
    if (!key) return "Key is required.";
    if (!KEY_PATTERN.test(key)) {
      return "Key must match [A-Za-z_][A-Za-z0-9_]*";
    }
    if (!draft.scopeAll && draft.projectIds.length === 0) {
      return "Pick at least one project, or scope the entry to all projects.";
    }
    if (draft.kind === "env") {
      if (/[\r\n]/.test(draft.value)) {
        return "An environment value cannot span lines. Use a file entry instead.";
      }
      if (options.creating && !draft.value.trim()) {
        return "Value is required.";
      }
    }
    if (draft.kind === "file") {
      const path = draft.path.trim();
      if (!this.isValidFilePath(path)) {
        return `File path must be an absolute path under ${FILE_ROOTS.join(" or ")}`;
      }
      if (options.creating && !draft.value) {
        return "File contents are required.";
      }
    }
    if (draft.kind === "ssh") {
      const { name, host, user, port, privateKey } = draft.ssh;
      if (!SSH_NAME_PATTERN.test(name.trim())) {
        return "Target name must start with a letter or digit and use only letters, digits, '.', '_' or '-'.";
      }
      if (!host.trim()) return "Host is required.";
      if (!user.trim()) return "User is required.";
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        return "Port must be between 1 and 65535.";
      }
      if (/[\r\n]/.test(draft.ssh.knownHostsLine ?? "")) {
        return "The known_hosts entry must be a single line.";
      }
      if (options.creating && !(privateKey ?? "").trim()) {
        return "A private key is required.";
      }
    }
    return null;
  }

  /** Mirrors the backend's file-root rule, including the `..` escape. */
  isValidFilePath(path: string): boolean {
    if (!path.startsWith("/") || /[\r\n\0]/.test(path)) return false;
    if (path.split("/").includes("..")) return false;
    return FILE_ROOTS.some(
      (root) => path.startsWith(root) && path.length > root.length
    );
  }

  /** The wire payload for a draft: only the fields its kind actually uses. */
  toPayload(draft: VaultDraft): VaultSecretDraft {
    const scope: SecretScope = draft.scopeAll
      ? { all: true }
      : { all: false, projectIds: [...draft.projectIds] };
    const payload: VaultSecretDraft = {
      key: draft.key.trim(),
      kind: draft.kind,
      scope,
      description: draft.description.trim(),
      clear: draft.clear,
    };
    if (draft.kind === "ssh") {
      payload.ssh = {
        name: draft.ssh.name.trim(),
        host: draft.ssh.host.trim(),
        port: draft.ssh.port || DEFAULT_SSH_PORT,
        user: draft.ssh.user.trim(),
        privateKey: draft.clear ? "" : (draft.ssh.privateKey ?? ""),
        knownHostsLine: (draft.ssh.knownHostsLine ?? "").trim(),
      };
      return payload;
    }
    if (draft.kind === "file") payload.path = draft.path.trim();
    payload.value = draft.clear ? "" : draft.value;
    return payload;
  }

  /** Insert or replace one entry, keeping the table in key order. */
  upsert(list: VaultSecret[], secret: VaultSecret): VaultSecret[] {
    const next = list.filter((entry) => entry.key !== secret.key);
    next.push(secret);
    return this.sort(next);
  }

  remove(list: VaultSecret[], key: string): VaultSecret[] {
    return list.filter((entry) => entry.key !== key);
  }

  sort(list: VaultSecret[]): VaultSecret[] {
    return [...list].sort((left, right) => left.key.localeCompare(right.key));
  }

  /** "All projects" or "2 projects", for the scope column. */
  scopeLabel(scope: SecretScope | undefined): string {
    if (!scope || scope.all) return "All projects";
    const count = scope.projectIds?.length ?? 0;
    if (count === 0) return "No projects";
    return count === 1 ? "1 project" : `${count} projects`;
  }

  /** What the entry does, in one line, for the table's second column. */
  destinationLabel(secret: VaultSecret): string {
    if (secret.kind === "env") return `$${secret.key}`;
    if (secret.kind === "file") return secret.path ?? "";
    return secret.ssh ? `ssh ${secret.ssh.name}` : "ssh";
  }

  /** The SSH_TARGET_* contract an ssh entry publishes, for the detail row. */
  sshEnvVars(name: string): string[] {
    const segment = name.toUpperCase().replace(/[^A-Z0-9]/g, "_");
    return [
      `SSH_TARGET_${segment}_HOST`,
      `SSH_TARGET_${segment}_USER`,
      `SSH_TARGET_${segment}_PORT`,
    ];
  }

  /** The inherited entries a project's Secrets tab should warn about. */
  shadowedKeys(inherited: InheritedSecret[]): string[] {
    return inherited.filter((entry) => entry.shadowed).map((entry) => entry.key);
  }
}

export const secretsVaultState = new SecretsVaultState();
