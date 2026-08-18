import type { ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import type { ProjectMeta } from "../../models/project";
import type {
  SecretKind,
  SecretTestResult,
  VaultSecret,
} from "../../models/secretsVault";
import type { SecretsVault } from "../../state/hooks/settings/useSecretsVault";
import { secretsVaultState } from "../../state/settings/secretsVaultState";
import type { VaultDraft } from "../../state/settings/secretsVaultState";
import { AlertCircle, Check, Key, Loader, Plus, Trash, X } from "../primitives/icons";
import { EmptyState, ErrorBanner } from "../primitives/Feedback";

const KIND_LABELS: Record<SecretKind, string> = {
  env: "Environment variable",
  file: "File",
  ssh: "SSH target",
};

/**
 * Admin editor for the platform secrets vault.
 *
 * Values are write-only: the table shows a mask, and the dialog's value field
 * starts blank on an edit, meaning "keep what is stored". Removing a value is
 * a separate, explicit action so a distracted save cannot wipe a token.
 */
export function SecretsVaultSettings({
  vault,
  projects,
}: {
  vault: SecretsVault;
  projects: ProjectMeta[];
}) {
  const [draft, setDraft] = useState<VaultDraft | null>(null);
  const [editingKey, setEditingKey] = useState<string | null>(null);

  function openCreate() {
    setEditingKey(null);
    setDraft(secretsVaultState.emptyDraft());
  }

  function openEdit(secret: VaultSecret) {
    setEditingKey(secret.key);
    setDraft(secretsVaultState.draftFrom(secret));
  }

  function close() {
    setDraft(null);
    setEditingKey(null);
  }

  return (
    <div class="space-y-4">
      <div class="rounded-lg border border-white/10 bg-[#101318] p-4">
        <div class="flex items-start gap-2">
          <Key class="mt-0.5 h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-300">
            One place for the tokens, licence keys, credential files, and SSH access every project
            should have. Entries are injected into each scoped project's container on start and
            whenever you change them here.
            <div class="mt-1.5">
              A project's own secret of the same name always wins over an environment entry — the
              table flags it. Everything stored here is readable by any agent running in a scoped
              project, so scope narrowly when a credential is not meant to be universal.
            </div>
          </div>
        </div>
      </div>

      {vault.error && <ErrorBanner message={vault.error} onRetry={() => void vault.reload()} />}

      <div class="flex items-center justify-between gap-2">
        <div class="text-[12.5px] text-ink-300">
          {vault.secrets ? `${vault.secrets.length} entr${vault.secrets.length === 1 ? "y" : "ies"}` : ""}
        </div>
        <button
          type="button"
          onClick={openCreate}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85"
        >
          <Plus class="h-4 w-4" /> Add secret
        </button>
      </div>

      {vault.loading && !vault.secrets ? (
        <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
          Loading the vault…
        </div>
      ) : (vault.secrets ?? []).length === 0 ? (
        <EmptyState
          Icon={Key}
          title="The vault is empty."
          hint="Add a GitHub token, a plugin licence key, an .npmrc, or an SSH target once and every project inherits it."
        />
      ) : (
        <div class="overflow-x-auto rounded-lg border border-white/10 bg-[#101318]">
          <table class="w-full min-w-[46rem] text-left text-[12.5px]">
            <thead class="border-b border-white/[0.08] text-[11px] uppercase tracking-wide text-ink-400">
              <tr>
                <th class="px-3 py-2 font-medium">Key</th>
                <th class="px-3 py-2 font-medium">Kind</th>
                <th class="px-3 py-2 font-medium">Destination</th>
                <th class="px-3 py-2 font-medium">Scope</th>
                <th class="px-3 py-2 font-medium">Value</th>
                <th class="px-3 py-2 font-medium">Updated</th>
                <th class="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {(vault.secrets ?? []).map((secret) => (
                <SecretRow
                  key={secret.key}
                  secret={secret}
                  projects={projects}
                  onEdit={() => openEdit(secret)}
                  onDelete={() => vault.remove(secret.key)}
                  onTest={() => vault.test(secret.key)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {draft && (
        <SecretDialog
          draft={draft}
          onDraftChange={setDraft}
          editingKey={editingKey}
          projects={projects}
          onCancel={close}
          onSubmit={async (next) => {
            if (editingKey) await vault.save(editingKey, next);
            else await vault.create(next);
            close();
          }}
          onTest={editingKey ? () => vault.test(editingKey) : undefined}
        />
      )}
    </div>
  );
}

function SecretRow({
  secret,
  projects,
  onEdit,
  onDelete,
  onTest,
}: {
  secret: VaultSecret;
  projects: ProjectMeta[];
  onEdit: () => void;
  onDelete: () => Promise<void>;
  onTest: () => Promise<SecretTestResult>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<SecretTestResult | null>(null);

  async function remove() {
    if (!confirm(`Delete ${secret.key} from the vault? Scoped containers lose it on the next sync.`)) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onDelete();
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function test() {
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      setResult(await onTest());
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const shadowed = secret.shadowedIn ?? [];

  return (
    <>
      <tr class="border-b border-white/[0.05] align-top last:border-b-0">
        <td class="px-3 py-2 font-mono text-ink-50">{secret.key}</td>
        <td class="px-3 py-2 text-ink-200">{KIND_LABELS[secret.kind]}</td>
        <td class="px-3 py-2 font-mono text-ink-300">
          {secretsVaultState.destinationLabel(secret)}
        </td>
        <td class="px-3 py-2 text-ink-300">
          {secretsVaultState.scopeLabel(secret.scope)}
          {!secret.scope?.all && (secret.scope?.projectIds?.length ?? 0) > 0 && (
            <div class="mt-0.5 text-[11px] text-ink-400">
              {projectNames(secret.scope?.projectIds ?? [], projects)}
            </div>
          )}
        </td>
        <td class="px-3 py-2 font-mono text-ink-300">
          {secret.hasValue ? secret.masked : <span class="text-accent-red">not set</span>}
        </td>
        <td class="whitespace-nowrap px-3 py-2 text-[11.5px] text-ink-400">
          {formatUpdated(secret)}
        </td>
        <td class="px-3 py-2">
          <div class="flex items-center justify-end gap-1">
            {secret.kind === "ssh" && (
              <button
                type="button"
                onClick={test}
                disabled={busy}
                class="h-8 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100 disabled:opacity-50"
              >
                {busy ? "testing…" : "test"}
              </button>
            )}
            <button
              type="button"
              onClick={onEdit}
              class="h-8 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
            >
              edit
            </button>
            <button
              type="button"
              onClick={remove}
              disabled={busy}
              aria-label={`Delete ${secret.key}`}
              class="grid h-8 w-8 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-accent-red disabled:opacity-50"
            >
              <Trash class="h-3.5 w-3.5" />
            </button>
          </div>
        </td>
      </tr>
      {(shadowed.length > 0 || secret.description || error || result || secret.envVars) && (
        <tr class="border-b border-white/[0.05] last:border-b-0">
          <td colSpan={7} class="px-3 pb-2 text-[11.5px]">
            {secret.description && (
              <div class="text-ink-400" dir="auto">
                {secret.description}
              </div>
            )}
            {secret.envVars && (
              <div class="mt-1 font-mono text-[11px] text-ink-400">
                {secret.envVars.join(" · ")}
              </div>
            )}
            {shadowed.length > 0 && (
              <div class="mt-1 flex items-start gap-1.5 text-accent-yellow">
                <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
                <span>
                  Overridden by a project secret of the same name in{" "}
                  {projectNames(shadowed, projects)}. Those containers keep their own value.
                </span>
              </div>
            )}
            {result && (
              <div class={`mt-1 flex items-start gap-1.5 ${result.ok ? "text-accent-green" : "text-accent-red"}`}>
                {result.ok ? (
                  <Check class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
                ) : (
                  <X class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
                )}
                <span class="break-all">
                  {result.ok ? "Connected" : "Failed"} in {result.latencyMs} ms
                  {result.output ? ` — ${result.output}` : ""}
                </span>
              </div>
            )}
            {error && <div class="mt-1 text-accent-red">{error}</div>}
          </td>
        </tr>
      )}
    </>
  );
}

function SecretDialog({
  draft,
  onDraftChange,
  editingKey,
  projects,
  onCancel,
  onSubmit,
  onTest,
}: {
  draft: VaultDraft;
  onDraftChange: (draft: VaultDraft) => void;
  editingKey: string | null;
  projects: ProjectMeta[];
  onCancel: () => void;
  onSubmit: (draft: VaultDraft) => Promise<void>;
  onTest?: () => Promise<SecretTestResult>;
}) {
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<SecretTestResult | null>(null);
  const creating = editingKey === null;

  function patch(changes: Partial<VaultDraft>) {
    onDraftChange({ ...draft, ...changes });
  }

  async function submit(event: Event) {
    event.preventDefault();
    const problem = secretsVaultState.validate(draft, { creating });
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      await onSubmit(draft);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setSubmitting(false);
    }
  }

  async function test() {
    if (!onTest) return;
    setResult(null);
    setError(null);
    try {
      setResult(await onTest());
    } catch (cause) {
      setError((cause as Error).message);
    }
  }

  return (
    <form
      onSubmit={submit}
      class="space-y-3 rounded-lg border border-white/10 bg-[#101318] p-4"
      aria-label={creating ? "Add secret" : `Edit ${editingKey}`}
    >
      <div class="text-[14.5px] font-semibold text-ink-50">
        {creating ? "Add secret" : `Edit ${editingKey}`}
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <Field label="Key">
          <input
            value={draft.key}
            disabled={!creating}
            onInput={(event) => patch({ key: (event.target as HTMLInputElement).value })}
            placeholder="GITHUB_TOKEN"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none disabled:opacity-60"
          />
        </Field>
        <Field label="Kind">
          <select
            value={draft.kind}
            disabled={!creating}
            onChange={(event) =>
              patch({ kind: (event.target as HTMLSelectElement).value as SecretKind })
            }
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 text-[13px] text-ink-50 focus:border-accent-blue/50 focus:outline-none disabled:opacity-60"
          >
            {(Object.keys(KIND_LABELS) as SecretKind[]).map((kind) => (
              <option key={kind} value={kind}>
                {KIND_LABELS[kind]}
              </option>
            ))}
          </select>
        </Field>
      </div>

      {draft.kind === "file" && (
        <Field label="Container path" hint="Must sit under /root or /workspace/.secrets. Written with mode 0600.">
          <input
            value={draft.path}
            onInput={(event) => patch({ path: (event.target as HTMLInputElement).value })}
            placeholder="/root/.npmrc"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
          />
        </Field>
      )}

      {draft.kind === "ssh" ? (
        <SSHFields draft={draft} patch={patch} onTest={onTest ? test : undefined} />
      ) : (
        <Field
          label={draft.kind === "file" ? "File contents" : "Value"}
          hint={
            creating
              ? undefined
              : "Leave blank to keep the stored value."
          }
        >
          <textarea
            value={draft.value}
            disabled={draft.clear}
            onInput={(event) => patch({ value: (event.target as HTMLTextAreaElement).value })}
            rows={draft.kind === "file" ? 5 : 2}
            spellcheck={false}
            autoComplete="off"
            placeholder={draft.kind === "file" ? "//registry.npmjs.org/:_authToken=…" : "ghp_…"}
            class="w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[13px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none disabled:opacity-50"
          />
        </Field>
      )}

      <Field label="Description">
        <input
          value={draft.description}
          onInput={(event) => patch({ description: (event.target as HTMLInputElement).value })}
          placeholder="What this is for"
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
        />
      </Field>

      <ScopePicker draft={draft} patch={patch} projects={projects} />

      {!creating && (
        <label class="flex items-center gap-2 text-[12.5px] text-ink-300">
          <input
            type="checkbox"
            checked={draft.clear}
            onChange={(event) => patch({ clear: (event.target as HTMLInputElement).checked })}
          />
          Remove the stored value — scoped containers drop this material on the next sync.
        </label>
      )}

      {result && (
        <div class={`text-[12px] ${result.ok ? "text-accent-green" : "text-accent-red"}`}>
          {result.ok ? "Connected" : "Failed"} in {result.latencyMs} ms
          {result.output ? ` — ${result.output}` : ""}
        </div>
      )}
      {error && <div class="text-[12px] text-accent-red">{error}</div>}

      <div class="flex items-center gap-2">
        <button
          type="submit"
          disabled={submitting}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85 disabled:opacity-50"
        >
          {submitting ? <Loader class="h-4 w-4 animate-spin" /> : <Check class="h-4 w-4" />}
          {creating ? "Add" : "Save"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          class="h-9 rounded-md px-3 text-[13px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function SSHFields({
  draft,
  patch,
  onTest,
}: {
  draft: VaultDraft;
  patch: (changes: Partial<VaultDraft>) => void;
  onTest?: () => Promise<void>;
}) {
  function patchSSH(changes: Partial<VaultDraft["ssh"]>) {
    patch({ ssh: { ...draft.ssh, ...changes } });
  }

  return (
    <div class="space-y-3 rounded-md border border-white/10 bg-white/[0.02] p-3">
      <div class="grid gap-3 sm:grid-cols-4">
        <Field label="Name">
          <input
            value={draft.ssh.name}
            onInput={(event) => patchSSH({ name: (event.target as HTMLInputElement).value })}
            placeholder="hestia"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
          />
        </Field>
        <Field label="Host">
          <input
            value={draft.ssh.host}
            onInput={(event) => patchSSH({ host: (event.target as HTMLInputElement).value })}
            placeholder="203.0.113.10"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
          />
        </Field>
        <Field label="User">
          <input
            value={draft.ssh.user}
            onInput={(event) => patchSSH({ user: (event.target as HTMLInputElement).value })}
            placeholder="root"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
          />
        </Field>
        <Field label="Port">
          <input
            type="number"
            value={String(draft.ssh.port)}
            onInput={(event) =>
              patchSSH({ port: Number((event.target as HTMLInputElement).value) || 0 })
            }
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 focus:border-accent-blue/50 focus:outline-none"
          />
        </Field>
      </div>

      <Field label="Private key" hint="Leave blank to keep the stored key. Materialized at /root/.ssh/<name>_key with mode 0600.">
        <textarea
          value={draft.ssh.privateKey ?? ""}
          disabled={draft.clear}
          onInput={(event) =>
            patchSSH({ privateKey: (event.target as HTMLTextAreaElement).value })
          }
          rows={4}
          spellcheck={false}
          autoComplete="off"
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
          class="w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[12.5px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none disabled:opacity-50"
        />
      </Field>

      <Field
        label="known_hosts entry"
        hint="Optional. With one, host-key checking is strict; without it, the first connection is accepted and pinned."
      >
        <input
          value={draft.ssh.knownHostsLine ?? ""}
          onInput={(event) =>
            patchSSH({ knownHostsLine: (event.target as HTMLInputElement).value })
          }
          placeholder="203.0.113.10 ssh-ed25519 AAAAC3Nz…"
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[12.5px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
        />
      </Field>

      <div class="text-[11.5px] text-ink-400">
        Agents reach this target as <span class="font-mono">ssh {draft.ssh.name || "<name>"}</span>{" "}
        and read{" "}
        <span class="font-mono">
          {secretsVaultState.sshEnvVars(draft.ssh.name || "name").join(", ")}
        </span>
        .
      </div>

      {onTest && (
        <div class="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void onTest()}
            class="h-8 rounded-md border border-white/10 px-2.5 text-[12px] text-ink-200 hover:bg-white/[0.08]"
          >
            Test connection
          </button>
          <span class="text-[11px] text-ink-400">
            Probes the saved target from the host — save first to test your edits.
          </span>
        </div>
      )}
    </div>
  );
}

function ScopePicker({
  draft,
  patch,
  projects,
}: {
  draft: VaultDraft;
  patch: (changes: Partial<VaultDraft>) => void;
  projects: ProjectMeta[];
}) {
  function toggle(id: string) {
    const next = draft.projectIds.includes(id)
      ? draft.projectIds.filter((entry) => entry !== id)
      : [...draft.projectIds, id];
    patch({ projectIds: next });
  }

  return (
    <fieldset class="space-y-2">
      <legend class="text-[11.5px] font-medium text-ink-300">Scope</legend>
      <div class="flex flex-wrap gap-3 text-[12.5px] text-ink-200">
        <label class="flex items-center gap-1.5">
          <input
            type="radio"
            name="vault-scope"
            checked={draft.scopeAll}
            onChange={() => patch({ scopeAll: true })}
          />
          All projects
        </label>
        <label class="flex items-center gap-1.5">
          <input
            type="radio"
            name="vault-scope"
            checked={!draft.scopeAll}
            onChange={() => patch({ scopeAll: false })}
          />
          Selected projects
        </label>
      </div>
      {!draft.scopeAll && (
        <div class="max-h-40 space-y-1 overflow-y-auto rounded-md border border-white/10 bg-black/20 p-2">
          {projects.length === 0 && (
            <div class="text-[12px] text-ink-400">No projects yet.</div>
          )}
          {projects.map((project) => (
            <label key={project.id} class="flex items-center gap-2 text-[12.5px] text-ink-200">
              <input
                type="checkbox"
                checked={draft.projectIds.includes(project.id)}
                onChange={() => toggle(project.id)}
              />
              <span class="truncate" dir="auto">
                {project.name}
              </span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ComponentChildren;
}) {
  return (
    <label class="block space-y-1">
      <span class="block text-[11.5px] font-medium text-ink-300">{label}</span>
      {children}
      {hint && <span class="block text-[11px] text-ink-400">{hint}</span>}
    </label>
  );
}

function projectNames(ids: string[], projects: ProjectMeta[]): string {
  const byId = new Map(projects.map((project) => [project.id, project.name]));
  return ids.map((id) => byId.get(id) ?? id).join(", ");
}

function formatUpdated(secret: VaultSecret): string {
  if (!secret.updatedAt) return "—";
  const when = new Date(secret.updatedAt).toLocaleDateString();
  return secret.updatedBy ? `${when} · ${secret.updatedBy}` : when;
}
