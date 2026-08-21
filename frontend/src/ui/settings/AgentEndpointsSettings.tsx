import { useState } from "preact/hooks";
import type { ProjectMeta } from "../../models/project";
import type {
  AgentEndpoint,
  AgentEndpointCLI,
  AgentEndpointTestResult,
} from "../../models/agentEndpoints";
import type { AgentEndpointsEditor } from "../../state/hooks/settings/useAgentEndpoints";
import type { AgentEndpointDraft } from "../../state/settings/agentEndpointsState";
import {
  AGENT_ENDPOINT_CLI_LABELS,
  AGENT_ENDPOINT_CLI_MODES,
  draftFrom,
  emptyDraft,
  lastTestLabel,
  modelSummary,
  statusLabel,
  statusTone,
  toPayload,
  validate,
} from "../../state/settings/agentEndpointsState";
import { AlertCircle, Check, Globe, Loader, Play, Plus, Trash } from "../primitives/icons";
import { EmptyState, ErrorBanner } from "../primitives/Feedback";
import { Field, type VaultKeyOption } from "./MCPServersSettings";

/**
 * Admin editor for the third-party agent endpoint register.
 *
 * The feature exists for cost: a brochure site or a copy edit does not need a
 * frontier model, and several vendors publish an endpoint that speaks an
 * agent CLI's own protocol so their models can be driven by that CLI.
 *
 * The rule the warning box states is not decoration. Only a vendor's own
 * published compatibility endpoint, with the operator's own key, is
 * supportable — and a chat running on one wears a red badge, because whose
 * model produced a piece of client code is a commercial fact somebody will
 * eventually be asked about.
 */
export function AgentEndpointsSettings({
  register,
  projects,
  vaultKeys,
}: {
  register: AgentEndpointsEditor;
  projects: ProjectMeta[];
  /** Vault entries a profile may reference, for the key picker. */
  vaultKeys: VaultKeyOption[] | null;
}) {
  const [draft, setDraft] = useState<AgentEndpointDraft | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);

  function openCreate() {
    setEditingId(null);
    setDraft(emptyDraft());
  }

  function openEdit(endpoint: AgentEndpoint) {
    setEditingId(endpoint.id);
    setDraft(draftFrom(endpoint));
  }

  function close() {
    setDraft(null);
    setEditingId(null);
  }

  const endpoints = register.endpoints ?? [];

  return (
    <div class="space-y-4">
      <div class="rounded-lg border border-white/10 bg-[#101318] p-4">
        <div class="flex items-start gap-2">
          <Globe class="mt-0.5 h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-300">
            Point a chat's coding agent at a vendor you have your own account with, instead of
            at Anthropic or OpenAI. Cheap or free models are enough for brochure sites, landing
            pages and content edits; Claude stays for the hard work.
            <div class="mt-1.5">
              A profile stores the endpoint and the <span class="font-medium text-ink-200">name</span>{" "}
              of a <span class="font-medium text-ink-200">Secrets vault</span> key — never the key
              itself. The value is read at run time, handed to the CLI for that one run, and is
              never logged or written into a chat transcript.
              {register.unsupportedCLIs.length > 0 && (
                <>
                  {" "}
                  {register.unsupportedCLIs.join(" and ")}{" "}
                  {register.unsupportedCLIs.length === 1 ? "has" : "have"} no documented
                  third-party mode, so {register.unsupportedCLIs.length === 1 ? "it is" : "they are"}{" "}
                  not offered here.
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      <div class="rounded-lg border border-accent-red/40 bg-accent-red/[0.08] p-4">
        <div class="flex items-start gap-2">
          <AlertCircle class="mt-0.5 h-4 w-4 flex-none text-accent-red" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-200">
            <div class="font-semibold text-accent-red">Only a vendor's own published endpoint.</div>
            Use these profiles only for a compatibility endpoint the vendor documents for this CLI,
            reached with your own API key. Never point one at a first-party API you are not entitled
            to use: impersonating a CLI, spoofing a user agent, or replaying a session is a terms-of-service
            breach, and this platform will not help you do it.
          </div>
        </div>
      </div>

      {register.error && (
        <ErrorBanner message={register.error} onRetry={() => void register.reload()} />
      )}

      <div class="flex items-center justify-between gap-2">
        <div class="text-[12.5px] text-ink-300">
          {register.endpoints
            ? `${endpoints.length} endpoint${endpoints.length === 1 ? "" : "s"}`
            : ""}
        </div>
        <button
          type="button"
          onClick={openCreate}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85"
        >
          <Plus class="h-4 w-4" /> Add endpoint
        </button>
      </div>

      {register.loading && !register.endpoints ? (
        <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
          Loading the register…
        </div>
      ) : endpoints.length === 0 ? (
        <EmptyState
          Icon={Globe}
          title="No agent endpoints."
          hint="Add one and any chat can be pointed at it from the composer's agent pill."
        />
      ) : (
        <div class="overflow-x-auto rounded-lg border border-white/10 bg-[#101318]">
          <table class="w-full min-w-[48rem] text-left text-[12.5px]">
            <thead class="border-b border-white/[0.08] text-[11px] uppercase tracking-wide text-ink-400">
              <tr>
                <th class="px-3 py-2 font-medium">Endpoint</th>
                <th class="px-3 py-2 font-medium">CLI</th>
                <th class="px-3 py-2 font-medium">Models</th>
                <th class="px-3 py-2 font-medium">Status</th>
                <th class="px-3 py-2 font-medium">Last test</th>
                <th class="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {endpoints.map((endpoint) => (
                <EndpointRow
                  key={endpoint.id}
                  endpoint={endpoint}
                  projects={projects}
                  onEdit={() => openEdit(endpoint)}
                  onDelete={() => register.remove(endpoint.id)}
                  onToggle={(enabled) => register.setEnabled(endpoint.id, enabled)}
                  onTest={(projectId, model) => register.test(endpoint.id, projectId, model)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {draft && (
        <EndpointDialog
          draft={draft}
          onDraftChange={setDraft}
          editingId={editingId}
          vaultKeys={vaultKeys}
          supportedCLIs={register.supportedCLIs}
          onCancel={close}
          onSubmit={async (next) => {
            if (editingId) await register.save(editingId, next);
            else await register.create(next);
            close();
          }}
        />
      )}
    </div>
  );
}

const STATUS_CLASSES: Record<"off" | "warn" | "on", string> = {
  off: "text-ink-400",
  warn: "text-accent-yellow",
  on: "text-accent-green",
};

function EndpointRow({
  endpoint,
  projects,
  onEdit,
  onDelete,
  onToggle,
  onTest,
}: {
  endpoint: AgentEndpoint;
  projects: ProjectMeta[];
  onEdit: () => void;
  onDelete: () => Promise<void>;
  onToggle: (enabled: boolean) => Promise<void>;
  onTest: (projectId: string, model: string) => Promise<AgentEndpointTestResult>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<AgentEndpointTestResult | null>(null);
  const [projectId, setProjectId] = useState(projects[0]?.id ?? "");
  const [model, setModel] = useState(endpoint.models?.[0]?.id ?? "");

  async function remove() {
    if (
      !confirm(
        `Delete ${endpoint.label}? Chats pointed at it fall back to the vendor's own endpoint on their next turn.`,
      )
    ) {
      return;
    }
    await guard(onDelete);
  }

  async function toggle() {
    await guard(() => onToggle(!endpoint.enabled));
  }

  async function test() {
    if (!projectId) {
      setError("Pick a project whose container should run the test.");
      return;
    }
    setResult(null);
    await guard(async () => setResult(await onTest(projectId, model)));
  }

  async function guard(action: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await action();
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const tone = statusTone(endpoint);

  return (
    <>
      <tr class="border-b border-white/[0.05] align-top last:border-b-0">
        <td class="px-3 py-2">
          <div class="font-medium text-ink-50">{endpoint.label}</div>
          <div class="max-w-[20rem] truncate font-mono text-[11px] text-ink-400">
            {endpoint.baseUrl}
          </div>
        </td>
        <td class="px-3 py-2 text-ink-200">{AGENT_ENDPOINT_CLI_LABELS[endpoint.cli]}</td>
        <td class="max-w-[14rem] truncate px-3 py-2 text-ink-300">{modelSummary(endpoint)}</td>
        <td class={`px-3 py-2 font-medium ${STATUS_CLASSES[tone]}`}>
          {statusLabel(endpoint)}
          {endpoint.enabled && !endpoint.keyResolved && (
            <div class="mt-0.5 text-[11px] font-normal text-ink-400">
              {endpoint.apiKeyRef
                ? `${endpoint.apiKeyRef} is not an all-projects vault entry`
                : "no vault key named"}
            </div>
          )}
        </td>
        <td class="px-3 py-2 text-ink-300">{lastTestLabel(endpoint)}</td>
        <td class="px-3 py-2">
          <div class="flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={() => void toggle()}
              disabled={busy}
              class="h-8 rounded-md px-2 text-[12px] text-ink-200 hover:bg-white/[0.08] disabled:opacity-50"
            >
              {endpoint.enabled ? "Disable" : "Enable"}
            </button>
            <button
              type="button"
              onClick={onEdit}
              class="h-8 rounded-md px-2 text-[12px] text-ink-200 hover:bg-white/[0.08]"
            >
              Edit
            </button>
            <button
              type="button"
              onClick={() => void remove()}
              disabled={busy}
              class="grid h-8 w-8 place-items-center rounded-md text-ink-300 hover:bg-white/[0.08] hover:text-accent-red disabled:opacity-50"
              aria-label={`Delete ${endpoint.label}`}
            >
              <Trash class="h-4 w-4" />
            </button>
          </div>
        </td>
      </tr>

      <tr class="border-b border-white/[0.05] last:border-b-0">
        <td colSpan={6} class="px-3 pb-3">
          {endpoint.notes && (
            <div class="mb-2 text-[11.5px] leading-relaxed text-ink-400">{endpoint.notes}</div>
          )}
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-[11px] text-ink-400">
              <span class="block">Test in project</span>
              <select
                value={projectId}
                onChange={(event) => setProjectId((event.target as HTMLSelectElement).value)}
                class="mt-0.5 h-8 rounded-md border border-white/10 bg-black/30 px-2 text-[12px] text-ink-100"
              >
                {projects.length === 0 && <option value="">No projects</option>}
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
            </label>
            {(endpoint.models?.length ?? 0) > 0 && (
              <label class="text-[11px] text-ink-400">
                <span class="block">Model</span>
                <select
                  value={model}
                  onChange={(event) => setModel((event.target as HTMLSelectElement).value)}
                  class="mt-0.5 h-8 rounded-md border border-white/10 bg-black/30 px-2 text-[12px] text-ink-100"
                >
                  {(endpoint.models ?? []).map((candidate) => (
                    <option key={candidate.id} value={candidate.id}>
                      {candidate.label || candidate.id}
                    </option>
                  ))}
                </select>
              </label>
            )}
            <button
              type="button"
              onClick={() => void test()}
              disabled={busy}
              class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 px-2.5 text-[12px] text-ink-200 hover:bg-white/[0.08] disabled:opacity-50"
            >
              {busy ? <Loader class="h-3.5 w-3.5 animate-spin" /> : <Play class="h-3.5 w-3.5" />}
              Run a two-word prompt
            </button>
            <span class="text-[11px] text-ink-500">
              Starts the real {AGENT_ENDPOINT_CLI_LABELS[endpoint.cli]} CLI in that project's
              container. The container must be running.
            </span>
          </div>

          {error && (
            <div class="mt-2 flex items-start gap-1.5 text-[11.5px] text-accent-red">
              <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
              <span>{error}</span>
            </div>
          )}
          {result && (
            <div class="mt-2 rounded-md border border-white/10 bg-black/30 p-2">
              <div
                class={`flex items-center gap-1.5 text-[11.5px] font-medium ${
                  result.ok ? "text-accent-green" : "text-accent-red"
                }`}
              >
                {result.ok ? (
                  <Check class="h-3.5 w-3.5" aria-hidden="true" />
                ) : (
                  <AlertCircle class="h-3.5 w-3.5" aria-hidden="true" />
                )}
                {result.ok ? "The endpoint answered" : "The endpoint refused"} · {result.durationMs}
                ms
              </div>
              <pre class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all text-[11px] leading-relaxed text-ink-300">
                {result.output || "(no output)"}
              </pre>
            </div>
          )}
        </td>
      </tr>
    </>
  );
}

function EndpointDialog({
  draft,
  onDraftChange,
  editingId,
  vaultKeys,
  supportedCLIs,
  onCancel,
  onSubmit,
}: {
  draft: AgentEndpointDraft;
  onDraftChange: (draft: AgentEndpointDraft) => void;
  editingId: string | null;
  vaultKeys: VaultKeyOption[] | null;
  supportedCLIs: AgentEndpointCLI[];
  onCancel: () => void;
  onSubmit: (draft: AgentEndpointDraft) => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const creating = editingId === null;

  function patch(changes: Partial<AgentEndpointDraft>) {
    onDraftChange({ ...draft, ...changes });
  }

  async function submit(event: Event) {
    event.preventDefault();
    const problem = validate(draft, { creating });
    if (problem) {
      setError(problem);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onSubmit(draft);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  // Only `env` entries resolve, and only ones scoped to every project: a
  // profile is platform-level and may be used by a chat with no project.
  const candidates = (vaultKeys ?? []).filter((secret) => secret.kind === "env");

  return (
    <form
      onSubmit={submit}
      class="space-y-3 rounded-lg border border-white/10 bg-[#101318] p-4"
    >
      <div class="text-[13px] font-medium text-ink-100">
        {creating ? "Add an agent endpoint" : `Edit ${draft.label || editingId}`}
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <Field
          label="Id"
          hint="Lowercase letters, digits, hyphens and underscores. Becomes the codex provider key, and cannot be changed later."
        >
          <input
            value={draft.id}
            disabled={!creating}
            onInput={(event) => patch({ id: (event.target as HTMLInputElement).value })}
            placeholder="zhipu-glm"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 font-mono text-[12.5px] text-ink-100 disabled:opacity-60"
          />
        </Field>
        <Field label="Label" hint="Shown in the composer and in the chat's badge.">
          <input
            value={draft.label}
            onInput={(event) => patch({ label: (event.target as HTMLInputElement).value })}
            placeholder="Zhipu GLM"
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 text-[12.5px] text-ink-100"
          />
        </Field>
      </div>

      <Field
        label="Agent CLI"
        hint={`Use the vendor's ${AGENT_ENDPOINT_CLI_MODES[draft.cli]}.`}
      >
        <div class="flex gap-1">
          {supportedCLIs.map((cli) => (
            <button
              key={cli}
              type="button"
              onClick={() => patch({ cli })}
              class={`h-9 flex-1 rounded-md px-2 text-[12.5px] font-medium transition ${
                draft.cli === cli
                  ? "bg-accent-blue text-ink-900"
                  : "bg-white/[0.05] text-ink-200 hover:bg-white/[0.09]"
              }`}
              aria-pressed={draft.cli === cli}
            >
              {AGENT_ENDPOINT_CLI_LABELS[cli]}
            </button>
          ))}
        </div>
      </Field>

      <Field
        label="Base URL"
        hint={
          draft.cli === "claude"
            ? "Becomes ANTHROPIC_BASE_URL for this run only."
            : "Becomes the codex provider's base_url. Usually ends in /v1."
        }
      >
        <input
          value={draft.baseUrl}
          onInput={(event) => patch({ baseUrl: (event.target as HTMLInputElement).value })}
          placeholder="https://open.bigmodel.cn/api/anthropic"
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 font-mono text-[12px] text-ink-100"
        />
      </Field>

      {draft.cli === "codex" && (
        <Field
          label="Wire protocol"
          hint="Recent codex builds require the Responses API. Choose Chat Completions only if the vendor offers nothing else, and run Test."
        >
          <select
            value={draft.wireApi}
            onChange={(event) =>
              patch({ wireApi: (event.target as HTMLSelectElement).value as "responses" | "chat" })
            }
            class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 text-[12.5px] text-ink-100"
          >
            <option value="responses">Responses API</option>
            <option value="chat">Chat Completions</option>
          </select>
        </Field>
      )}

      <Field
        label="Secrets vault key"
        hint="The name of an env entry scoped to all projects, holding your API key for this vendor. The value is never stored here."
      >
        <input
          value={draft.apiKeyRef}
          onInput={(event) => patch({ apiKeyRef: (event.target as HTMLInputElement).value })}
          placeholder="ZHIPU_API_KEY"
          list="agent-endpoint-vault-keys"
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 font-mono text-[12.5px] text-ink-100"
        />
        <datalist id="agent-endpoint-vault-keys">
          {candidates.map((secret) => (
            <option key={secret.key} value={secret.key} />
          ))}
        </datalist>
      </Field>

      <Field
        label="Models"
        hint="One per line, as the vendor names them. Optionally `id = Label`. The first is the default."
      >
        <textarea
          value={draft.modelLines}
          onInput={(event) => patch({ modelLines: (event.target as HTMLTextAreaElement).value })}
          rows={4}
          placeholder={"glm-4.6 = GLM-4.6\nglm-4.5-air = GLM-4.5 Air"}
          class="w-full rounded-md border border-white/10 bg-black/30 p-2 font-mono text-[12px] text-ink-100"
        />
      </Field>

      <Field label="Extra headers" hint="Optional, one `Name: Value` per line.">
        <textarea
          value={draft.headerLines}
          onInput={(event) => patch({ headerLines: (event.target as HTMLTextAreaElement).value })}
          rows={2}
          placeholder="HTTP-Referer: https://example.com"
          class="w-full rounded-md border border-white/10 bg-black/30 p-2 font-mono text-[12px] text-ink-100"
        />
      </Field>

      <Field label="Notes" hint="Where these values came from, and anything the next admin needs.">
        <input
          value={draft.notes}
          onInput={(event) => patch({ notes: (event.target as HTMLInputElement).value })}
          class="h-9 w-full rounded-md border border-white/10 bg-black/30 px-2 text-[12.5px] text-ink-100"
        />
      </Field>

      <label class="flex items-center gap-2 text-[12.5px] text-ink-200">
        <input
          type="checkbox"
          checked={draft.enabled}
          onChange={(event) => patch({ enabled: (event.target as HTMLInputElement).checked })}
        />
        Enabled — offer this endpoint in every chat's agent pill
      </label>

      {error && (
        <div class="flex items-start gap-1.5 text-[12px] text-accent-red">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
          <span>{error}</span>
        </div>
      )}

      <div class="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          class="h-9 rounded-md px-3 text-[13px] text-ink-200 hover:bg-white/[0.08]"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={busy}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85 disabled:opacity-60"
        >
          {busy && <Loader class="h-3.5 w-3.5 animate-spin" />}
          {creating ? "Add endpoint" : "Save changes"}
        </button>
      </div>
    </form>
  );
}
