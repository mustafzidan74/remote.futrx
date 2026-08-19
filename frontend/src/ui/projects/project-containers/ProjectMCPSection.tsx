import { useState } from "preact/hooks";
import type { ProjectMeta } from "../../../models/project";
import type { MCPProjectEntry } from "../../../models/mcp";
import type { ProjectMCP } from "../../../state/hooks/projects/useProjectMCP";
import {
  MCP_PROVIDER_LABELS,
  mcpServersState,
} from "../../../state/settings/mcpServersState";
import type { MCPDraft } from "../../../state/settings/mcpServersState";
import { ServerDialog } from "../../settings/MCPServersSettings";
import type { VaultKeyOption } from "../../settings/MCPServersSettings";
import { Plus, Trash } from "../../primitives/icons";
import { Empty } from "./ProjectContainerPrimitives";

/**
 * What MCP servers this project's agents will have.
 *
 * Inherited entries can only be switched off here — their definition belongs
 * to the admin registry. Entries added here belong to this project alone and
 * are editable in full.
 */
export function ProjectMCPSection({
  project,
  mcp,
  vaultKeys,
}: {
  project: ProjectMeta;
  mcp: ProjectMCP;
  vaultKeys: VaultKeyOption[] | null;
}) {
  const [draft, setDraft] = useState<MCPDraft | null>(null);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const settings = mcp.record.data;
  const available = settings?.available ?? [];

  function openCreate() {
    setEditingName(null);
    setDraft({ ...mcpServersState.emptyDraft(), scopeAll: false });
  }

  function openEdit(entry: MCPProjectEntry) {
    setEditingName(entry.name);
    setDraft(mcpServersState.draftFrom(entry));
  }

  function close() {
    setDraft(null);
    setEditingName(null);
  }

  async function run(action: () => Promise<void>) {
    setError(null);
    try {
      await action();
    } catch (cause) {
      setError((cause as Error).message);
    }
  }

  if (mcp.record.loading && !settings) {
    return <Empty text="Loading MCP servers…" />;
  }
  if (mcp.record.error) {
    return <Empty text={mcp.record.error} />;
  }

  return (
    <div class="space-y-3">
      <div class="text-[12px] leading-relaxed text-ink-400">
        {mcpServersState.materializedLabel(settings?.materializedAt, Date.now())}{" "}
        {(settings?.materializedNames?.length ?? 0) > 0 && (
          <span class="font-mono">{(settings?.materializedNames ?? []).join(" · ")}</span>
        )}
        <div class="mt-1">
          Configuration is rewritten before the next agent run.{" "}
          {(settings?.unsupportedProviders?.length ?? 0) > 0 && (
            <>
              {(settings?.unsupportedProviders ?? [])
                .map((id) => MCP_PROVIDER_LABELS[id] ?? id)
                .join(" and ")}{" "}
              {(settings?.unsupportedProviders?.length ?? 0) === 1 ? "does" : "do"} not support MCP,
              so runs on {(settings?.unsupportedProviders?.length ?? 0) === 1 ? "it" : "them"} see
              none of these tools.
            </>
          )}
        </div>
      </div>

      {available.length === 0 ? (
        <Empty text="No MCP servers reach this project yet." />
      ) : (
        <div class="divide-y divide-white/[0.05] overflow-hidden rounded-md border border-white/10">
          {available.map((entry) => (
            <div key={entry.name} class="flex items-start gap-3 px-3 py-2.5">
              <input
                type="checkbox"
                checked={entry.enabled}
                disabled={mcp.saving}
                aria-label={`Enable ${entry.name}`}
                onChange={() => void run(() => mcp.toggle(entry.name))}
                class="mt-0.5"
              />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-baseline gap-2">
                  <span class="font-mono text-[13px] text-ink-50">{entry.name}</span>
                  <span class="text-[11px] uppercase tracking-wide text-ink-400">
                    {entry.source === "project" ? "this project" : "platform"}
                  </span>
                  <span class="text-[11px] text-ink-400">
                    {mcpServersState.providerLabels(entry).join(", ")}
                  </span>
                </div>
                <div class="truncate font-mono text-[11.5px] text-ink-400">
                  {mcpServersState.destinationLabel(entry)}
                </div>
                {entry.description && (
                  <div class="text-[11.5px] text-ink-400" dir="auto">
                    {entry.description}
                  </div>
                )}
              </div>
              {entry.source === "project" && (
                <div class="flex flex-none items-center gap-1">
                  <button
                    type="button"
                    onClick={() => openEdit(entry)}
                    class="h-8 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
                  >
                    edit
                  </button>
                  <button
                    type="button"
                    disabled={mcp.saving}
                    aria-label={`Remove ${entry.name}`}
                    onClick={() => void run(() => mcp.removeServer(entry.name))}
                    class="grid h-8 w-8 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-accent-red disabled:opacity-50"
                  >
                    <Trash class="h-3.5 w-3.5" />
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {error && <div class="text-[12px] text-accent-red">{error}</div>}

      {draft ? (
        <ServerDialog
          draft={draft}
          onDraftChange={setDraft}
          editingName={editingName}
          projects={[project]}
          vaultKeys={vaultKeys}
          supportedProviders={settings?.supportedProviders ?? []}
          platform={false}
          onCancel={close}
          onSubmit={async (next) => {
            await mcp.saveServer(next, editingName ?? undefined);
            close();
          }}
        />
      ) : (
        <button
          type="button"
          onClick={openCreate}
          class="inline-flex h-9 items-center gap-1.5 rounded-md border border-white/10 px-3 text-[13px] text-ink-200 hover:bg-white/[0.08]"
        >
          <Plus class="h-4 w-4" /> Add a server for this project
        </button>
      )}
    </div>
  );
}
