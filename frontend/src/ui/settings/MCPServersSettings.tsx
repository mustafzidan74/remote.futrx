import type { ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import type { ProjectMeta } from "../../models/project";
import type { MCPServer, MCPTestResult, MCPTransport } from "../../models/mcp";
import type { MCPRegistryEditor } from "../../state/hooks/settings/useMCPServers";
import {
  MCP_PROVIDER_LABELS,
  MCP_TEMPLATES,
  mcpServersState,
} from "../../state/settings/mcpServersState";
import type { MCPDraft, MCPTemplate } from "../../state/settings/mcpServersState";
import { AlertCircle, Check, Loader, Package, Plus, Trash } from "../primitives/icons";
import { EmptyState, ErrorBanner } from "../primitives/Feedback";

/**
 * The shape the secret-ref picker needs from a vault listing. It is
 * deliberately minimal so the admin screen can feed it the full vault while
 * a project panel feeds it only what that project inherits — a member cannot
 * read the vault, and does not need to.
 */
export interface VaultKeyOption {
  key: string;
  kind: string;
}

const TRANSPORT_LABELS: Record<MCPTransport, string> = {
  stdio: "Local process (stdio)",
  http: "Remote endpoint (http)",
};

/**
 * Admin editor for the platform MCP registry.
 *
 * An MCP server is a tool the agent can call — a client database, a Figma
 * file, a store API. It runs with the container's privileges, so the panel
 * says so plainly and scoping is a first-class field rather than an
 * afterthought. Credentials never live here: an entry references a Secrets
 * vault key and the value is substituted when the config is written.
 */
export function MCPServersSettings({
  registry,
  projects,
  vaultKeys,
}: {
  registry: MCPRegistryEditor;
  projects: ProjectMeta[];
  /** Vault entries an entry may reference, for the secret-ref picker. */
  vaultKeys: VaultKeyOption[] | null;
}) {
  const [draft, setDraft] = useState<MCPDraft | null>(null);
  const [editingName, setEditingName] = useState<string | null>(null);

  function openCreate(template?: MCPTemplate) {
    setEditingName(null);
    setDraft(template ? mcpServersState.draftFromTemplate(template) : mcpServersState.emptyDraft());
  }

  function openEdit(server: MCPServer) {
    setEditingName(server.name);
    setDraft(mcpServersState.draftFrom(server));
  }

  function close() {
    setDraft(null);
    setEditingName(null);
  }

  const servers = registry.servers ?? [];

  return (
    <div class="space-y-4">
      <div class="rounded-lg border border-white/10 bg-[#101318] p-4">
        <div class="flex items-start gap-2">
          <Package class="mt-0.5 h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-300">
            MCP servers give agents real tools instead of guesses — a client database, a Figma
            file, a Google Sheet, a store API, Jira. Entries here are written into every scoped
            project's container before its next run.
            <div class="mt-1.5">
              An MCP server runs with the container's privileges and can reach everything that
              project can reach, so scope narrowly. Put credentials in the{" "}
              <span class="font-medium text-ink-200">Secrets vault</span> and reference them as{" "}
              <span class="font-mono">{"${KEY}"}</span> — the value is substituted when the config
              is written and is never stored or shown here.
            </div>
            {registry.unsupportedProviders.length > 0 && (
              <div class="mt-1.5">
                {registry.unsupportedProviders
                  .map((id) => MCP_PROVIDER_LABELS[id] ?? id)
                  .join(" and ")}{" "}
                {registry.unsupportedProviders.length === 1 ? "does" : "do"} not support MCP on this
                platform, so nothing here reaches{" "}
                {registry.unsupportedProviders.length === 1 ? "it" : "them"}.
              </div>
            )}
          </div>
        </div>
      </div>

      {registry.error && (
        <ErrorBanner message={registry.error} onRetry={() => void registry.reload()} />
      )}

      <div class="flex items-center justify-between gap-2">
        <div class="text-[12.5px] text-ink-300">
          {registry.servers
            ? `${servers.length} server${servers.length === 1 ? "" : "s"}`
            : ""}
        </div>
        <button
          type="button"
          onClick={() => openCreate()}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85"
        >
          <Plus class="h-4 w-4" /> Add server
        </button>
      </div>

      {registry.loading && !registry.servers ? (
        <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
          Loading the registry…
        </div>
      ) : servers.length === 0 ? (
        <EmptyState
          Icon={Package}
          title="No MCP servers yet."
          hint="Add one and every scoped project's agent gains its tools on the next run."
        />
      ) : (
        <div class="overflow-x-auto rounded-lg border border-white/10 bg-[#101318]">
          <table class="w-full min-w-[46rem] text-left text-[12.5px]">
            <thead class="border-b border-white/[0.08] text-[11px] uppercase tracking-wide text-ink-400">
              <tr>
                <th class="px-3 py-2 font-medium">Name</th>
                <th class="px-3 py-2 font-medium">Transport</th>
                <th class="px-3 py-2 font-medium">Connects to</th>
                <th class="px-3 py-2 font-medium">Agents</th>
                <th class="px-3 py-2 font-medium">Scope</th>
                <th class="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {servers.map((server) => (
                <ServerRow
                  key={server.name}
                  server={server}
                  projects={projects}
                  onEdit={() => openEdit(server)}
                  onDelete={() => registry.remove(server.name)}
                  onTest={(projectId) => registry.test(server.name, projectId)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!draft && (
        <div class="rounded-lg border border-white/10 bg-[#101318] p-4">
          <div class="text-[12.5px] font-medium text-ink-200">Examples to start from</div>
          <div class="mt-1 text-[11.5px] text-ink-400">
            Prefills only — nothing is enabled until you save it with a scope.
          </div>
          <div class="mt-2 flex flex-wrap gap-2">
            {MCP_TEMPLATES.map((template) => (
              <button
                key={template.id}
                type="button"
                title={template.hint}
                onClick={() => openCreate(template)}
                class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5 text-[12px] text-ink-200 hover:bg-white/[0.08]"
              >
                <Plus class="h-3.5 w-3.5" />
                {template.label}
              </button>
            ))}
          </div>
        </div>
      )}

      {draft && (
        <ServerDialog
          draft={draft}
          onDraftChange={setDraft}
          editingName={editingName}
          projects={projects}
          vaultKeys={vaultKeys}
          supportedProviders={registry.supportedProviders}
          onCancel={close}
          onSubmit={async (next) => {
            if (editingName) await registry.save(editingName, next);
            else await registry.create(next);
            close();
          }}
        />
      )}
    </div>
  );
}

function ServerRow({
  server,
  projects,
  onEdit,
  onDelete,
  onTest,
}: {
  server: MCPServer;
  projects: ProjectMeta[];
  onEdit: () => void;
  onDelete: () => Promise<void>;
  onTest: (projectId: string) => Promise<MCPTestResult>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<MCPTestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [projectId, setProjectId] = useState(projects[0]?.id ?? "");

  async function remove() {
    if (
      !confirm(
        `Delete ${server.name} from the registry? Scoped containers lose its configuration on their next run.`,
      )
    ) {
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
    if (!projectId) {
      setError("Pick a project whose container should run the probe.");
      return;
    }
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      setResult(await onTest(projectId));
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const unsupported = server.unsupportedProviders ?? [];

  return (
    <>
      <tr class="border-b border-white/[0.05] align-top last:border-b-0">
        <td class="px-3 py-2 font-mono text-ink-50">{server.name}</td>
        <td class="px-3 py-2 text-ink-200">{TRANSPORT_LABELS[server.transport]}</td>
        <td class="max-w-[18rem] truncate px-3 py-2 font-mono text-ink-300">
          {mcpServersState.destinationLabel(server)}
        </td>
        <td class="px-3 py-2 text-ink-300">{mcpServersState.providerLabels(server).join(", ")}</td>
        <td class="px-3 py-2 text-ink-300">
          {mcpServersState.scopeLabel(server.scope)}
          {!server.scope?.all && (server.scope?.projectIds?.length ?? 0) > 0 && (
            <div class="mt-0.5 text-[11px] text-ink-400">
              {projectNames(server.scope?.projectIds ?? [], projects)}
            </div>
          )}
        </td>
        <td class="px-3 py-2">
          <div class="flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={() => setTesting((current) => !current)}
              class="h-8 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
            >
              test
            </button>
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
              aria-label={`Delete ${server.name}`}
              class="grid h-8 w-8 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-accent-red disabled:opacity-50"
            >
              <Trash class="h-3.5 w-3.5" />
            </button>
          </div>
        </td>
      </tr>
      {(server.description ||
        testing ||
        error ||
        result ||
        unsupported.length > 0 ||
        (server.secretRefs?.length ?? 0) > 0) && (
        <tr class="border-b border-white/[0.05] last:border-b-0">
          <td colSpan={6} class="px-3 pb-2 text-[11.5px]">
            {server.description && (
              <div class="text-ink-400" dir="auto">
                {server.description}
              </div>
            )}
            {(server.secretRefs?.length ?? 0) > 0 && (
              <div class="mt-1 font-mono text-[11px] text-ink-400">
                vault keys: {(server.secretRefs ?? []).join(" · ")}
              </div>
            )}
            {unsupported.length > 0 && (
              <div class="mt-1 flex items-start gap-1.5 text-accent-yellow">
                <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
                <span>
                  {unsupported.map((id) => MCP_PROVIDER_LABELS[id] ?? id).join(", ")} cannot be
                  configured for MCP, so this entry never reaches them.
                </span>
              </div>
            )}
            {testing && (
              <div class="mt-1.5 flex flex-wrap items-center gap-2">
                <select
                  value={projectId}
                  onChange={(event) => setProjectId((event.target as HTMLSelectElement).value)}
                  class="h-8 rounded-md border border-white/10 bg-black/30 px-2 text-[12px] text-ink-50 focus:border-accent-blue/50 focus:outline-none"
                >
                  {projects.length === 0 && <option value="">No projects</option>}
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={test}
                  disabled={busy}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5 text-[12px] text-ink-200 hover:bg-white/[0.08] disabled:opacity-50"
                >
                  {busy && <Loader class="h-3.5 w-3.5 animate-spin" />}
                  Run handshake
                </button>
                <span class="text-[11px] text-ink-400">
                  Starts the server inside that project's running container and shows what it
                  answered.
                </span>
              </div>
            )}
            {result && (
              <div class="mt-1.5">
                <div class={result.ok ? "text-accent-green" : "text-accent-red"}>
                  {result.ok ? "Responded" : "Failed"} in {result.durationMs} ms
                </div>
                {result.output && (
                  <pre class="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md border border-white/10 bg-black/30 p-2 font-mono text-[11px] text-ink-300">
                    {result.output}
                  </pre>
                )}
              </div>
            )}
            {error && <div class="mt-1 text-accent-red">{error}</div>}
          </td>
        </tr>
      )}
    </>
  );
}

/**
 * The add/edit form. It is shared by the admin registry and the project
 * panel: `platform` decides whether a scope is asked for at all, because a
 * project-only entry reaches exactly one project by construction.
 */
export function ServerDialog({
  draft,
  onDraftChange,
  editingName,
  projects,
  vaultKeys,
  supportedProviders,
  platform = true,
  onCancel,
  onSubmit,
}: {
  draft: MCPDraft;
  onDraftChange: (draft: MCPDraft) => void;
  editingName: string | null;
  projects: ProjectMeta[];
  vaultKeys: VaultKeyOption[] | null;
  supportedProviders: string[];
  platform?: boolean;
  onCancel: () => void;
  onSubmit: (draft: MCPDraft) => Promise<void>;
}) {
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const creating = editingName === null;

  function patch(changes: Partial<MCPDraft>) {
    onDraftChange({ ...draft, ...changes });
  }

  async function submit(event: Event) {
    event.preventDefault();
    const problem = mcpServersState.validate(draft, { platform });
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

  const referenced = mcpServersState.placeholders(draft);

  return (
    <form
      onSubmit={submit}
      class="space-y-3 rounded-lg border border-white/10 bg-[#101318] p-4"
      aria-label={creating ? "Add MCP server" : `Edit ${editingName}`}
    >
      <div class="text-[14.5px] font-semibold text-ink-50">
        {creating ? "Add MCP server" : `Edit ${editingName}`}
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <Field label="Name" hint="Becomes part of the tool names the model sees.">
          <input
            value={draft.name}
            disabled={!creating}
            onInput={(event) => patch({ name: (event.target as HTMLInputElement).value })}
            placeholder="playwright"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none disabled:opacity-60"
          />
        </Field>
        <Field label="Transport">
          <select
            value={draft.transport}
            onChange={(event) =>
              patch({ transport: (event.target as HTMLSelectElement).value as MCPTransport })
            }
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 text-[13px] text-ink-50 focus:border-accent-blue/50 focus:outline-none"
          >
            {(Object.keys(TRANSPORT_LABELS) as MCPTransport[]).map((transport) => (
              <option key={transport} value={transport}>
                {TRANSPORT_LABELS[transport]}
              </option>
            ))}
          </select>
        </Field>
      </div>

      {draft.transport === "stdio" ? (
        <div class="space-y-3 rounded-md border border-white/10 bg-white/[0.02] p-3">
          <Field label="Command">
            <input
              value={draft.command}
              onInput={(event) => patch({ command: (event.target as HTMLInputElement).value })}
              placeholder="npx"
              class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
            />
          </Field>
          <Field label="Arguments" hint="One per line.">
            <textarea
              value={draft.argsText}
              onInput={(event) => patch({ argsText: (event.target as HTMLTextAreaElement).value })}
              rows={3}
              spellcheck={false}
              placeholder={"@playwright/mcp@latest"}
              class="w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[12.5px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
            />
          </Field>
          <Field label="Environment" hint="NAME=value, one per line. Use ${KEY} for a vault value.">
            <textarea
              value={draft.envText}
              onInput={(event) => patch({ envText: (event.target as HTMLTextAreaElement).value })}
              rows={3}
              spellcheck={false}
              placeholder={"PGPASSWORD=${PG_PASSWORD}"}
              class="w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[12.5px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
            />
          </Field>
        </div>
      ) : (
        <div class="space-y-3 rounded-md border border-white/10 bg-white/[0.02] p-3">
          <Field label="URL">
            <input
              value={draft.url}
              onInput={(event) => patch({ url: (event.target as HTMLInputElement).value })}
              placeholder="https://jira.example.com/mcp"
              class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 font-mono text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
            />
          </Field>
          <Field
            label="Headers"
            hint="Header-Name: value, one per line. Use ${KEY} for a vault value."
          >
            <textarea
              value={draft.headersText}
              onInput={(event) =>
                patch({ headersText: (event.target as HTMLTextAreaElement).value })
              }
              rows={3}
              spellcheck={false}
              placeholder={"Authorization: Bearer ${JIRA_TOKEN}"}
              class="w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[12.5px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
            />
          </Field>
        </div>
      )}

      <Field label="Description">
        <input
          value={draft.description}
          onInput={(event) => patch({ description: (event.target as HTMLInputElement).value })}
          placeholder="What this server is for"
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 text-[13px] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
        />
      </Field>

      <ProviderPicker
        draft={draft}
        patch={patch}
        supportedProviders={supportedProviders}
      />

      <SecretRefPicker draft={draft} patch={patch} vaultKeys={vaultKeys} referenced={referenced} />

      {platform && <ScopePicker draft={draft} patch={patch} projects={projects} />}

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

function ProviderPicker({
  draft,
  patch,
  supportedProviders,
}: {
  draft: MCPDraft;
  patch: (changes: Partial<MCPDraft>) => void;
  supportedProviders: string[];
}) {
  function toggle(id: string) {
    const next = draft.providers.includes(id)
      ? draft.providers.filter((entry) => entry !== id)
      : [...draft.providers, id];
    patch({ providers: next });
  }

  return (
    <fieldset class="space-y-1.5">
      <legend class="text-[11.5px] font-medium text-ink-300">Agents</legend>
      <div class="flex flex-wrap gap-3 text-[12.5px] text-ink-200">
        {supportedProviders.map((id) => (
          <label key={id} class="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={draft.providers.includes(id)}
              onChange={() => toggle(id)}
            />
            {MCP_PROVIDER_LABELS[id] ?? id}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

function SecretRefPicker({
  draft,
  patch,
  vaultKeys,
  referenced,
}: {
  draft: MCPDraft;
  patch: (changes: Partial<MCPDraft>) => void;
  vaultKeys: VaultKeyOption[] | null;
  referenced: string[];
}) {
  // Only `env` entries resolve: those are the values a scoped container is
  // already given, and the registry may not widen that.
  const candidates = (vaultKeys ?? []).filter((secret) => secret.kind === "env");
  const missing = referenced.filter((key) => !draft.secretRefs.includes(key));

  function toggle(key: string) {
    const next = draft.secretRefs.includes(key)
      ? draft.secretRefs.filter((entry) => entry !== key)
      : [...draft.secretRefs, key];
    patch({ secretRefs: next });
  }

  return (
    <fieldset class="space-y-1.5">
      <legend class="text-[11.5px] font-medium text-ink-300">Secret references</legend>
      <div class="text-[11px] text-ink-400">
        Vault keys whose values fill the <span class="font-mono">{"${KEY}"}</span> placeholders
        above when the config is written. The value is never stored in this entry.
      </div>
      {candidates.length === 0 ? (
        <div class="text-[12px] text-ink-400">
          No environment entries in the Secrets vault yet.
        </div>
      ) : (
        <div class="max-h-32 space-y-1 overflow-y-auto rounded-md border border-white/10 bg-black/20 p-2">
          {candidates.map((secret) => (
            <label key={secret.key} class="flex items-center gap-2 text-[12.5px] text-ink-200">
              <input
                type="checkbox"
                checked={draft.secretRefs.includes(secret.key)}
                onChange={() => toggle(secret.key)}
              />
              <span class="truncate font-mono">{secret.key}</span>
            </label>
          ))}
        </div>
      )}
      {missing.length > 0 && (
        <div class="flex items-start gap-1.5 text-[11.5px] text-accent-yellow">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
          <span>
            {missing.map((key) => `\${${key}}`).join(", ")} referenced above but not selected.
          </span>
        </div>
      )}
    </fieldset>
  );
}

function ScopePicker({
  draft,
  patch,
  projects,
}: {
  draft: MCPDraft;
  patch: (changes: Partial<MCPDraft>) => void;
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
            name="mcp-scope"
            checked={draft.scopeAll}
            onChange={() => patch({ scopeAll: true })}
          />
          All projects
        </label>
        <label class="flex items-center gap-1.5">
          <input
            type="radio"
            name="mcp-scope"
            checked={!draft.scopeAll}
            onChange={() => patch({ scopeAll: false })}
          />
          Selected projects
        </label>
      </div>
      {!draft.scopeAll && (
        <div class="max-h-40 space-y-1 overflow-y-auto rounded-md border border-white/10 bg-black/20 p-2">
          {projects.length === 0 && <div class="text-[12px] text-ink-400">No projects yet.</div>}
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

export function Field({
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
