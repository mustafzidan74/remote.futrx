import { useCallback, useEffect, useState } from "preact/hooks";
import { mcpApi } from "../../../api/mcpApi";
import type { ProjectMeta } from "../../../models/project";
import type { ProjectDataLoadSignal, ProjectMCPRecord } from "../../projects/projectContainerRecords";
import { mcpServersState } from "../../settings/mcpServersState";
import type { MCPDraft } from "../../settings/mcpServersState";

export interface ProjectMCP {
  record: ProjectMCPRecord;
  load: (signal?: ProjectDataLoadSignal) => Promise<void>;
  /** Switch one available server on or off for this project. */
  toggle: (name: string) => Promise<void>;
  /** Add or replace one project-only entry. */
  saveServer: (draft: MCPDraft, replacing?: string) => Promise<void>;
  /** Remove one project-only entry. Inherited entries are toggled, not removed. */
  removeServer: (name: string) => Promise<void>;
  saving: boolean;
}

/**
 * One project's MCP configuration.
 *
 * Every write sends the whole document — the disabled list plus the
 * project-only entries — because that is what the API takes, and because
 * rebuilding it from the record the panel is already rendering is the only
 * way two toggles in a row cannot lose each other.
 */
export function useProjectMCP(project: ProjectMeta | null, enabled: boolean): ProjectMCP {
  const [record, setRecord] = useState<ProjectMCPRecord>({ loading: false });
  const [saving, setSaving] = useState(false);

  const load = useCallback(
    async (signal?: ProjectDataLoadSignal) => {
      if (!project) {
        setRecord({ loading: false });
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const data = await mcpApi.projectSettings(project.id);
        if (signal?.cancelled) return;
        setRecord({ loading: false, data });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, error: (error as Error).message });
      }
    },
    [project],
  );

  useEffect(() => {
    if (!enabled) return;
    const signal: ProjectDataLoadSignal = { cancelled: false };
    void load(signal);
    return () => {
      signal.cancelled = true;
    };
  }, [enabled, load]);

  const save = useCallback(
    async (disabled: string[], servers: MCPDraft[]) => {
      if (!project) throw new Error("No project selected.");
      setSaving(true);
      try {
        const data = await mcpApi.saveProjectSettings(project.id, {
          disabled,
          servers: servers.map((draft) => mcpServersState.toPayload(draft, { platform: false })),
        });
        setRecord({ loading: false, data });
      } finally {
        setSaving(false);
      }
    },
    [project],
  );

  const ownedDrafts = useCallback(
    (excluding?: string) =>
      mcpServersState
        .projectOwned(record.data?.available ?? [])
        .filter((entry) => entry.name !== excluding)
        .map((entry) => mcpServersState.draftFrom(entry)),
    [record.data],
  );

  const toggle = useCallback(
    async (name: string) => {
      const available = record.data?.available ?? [];
      await save(mcpServersState.toggle(available, name), ownedDrafts());
    },
    [record.data, save, ownedDrafts],
  );

  const saveServer = useCallback(
    async (draft: MCPDraft, replacing?: string) => {
      const available = record.data?.available ?? [];
      await save(mcpServersState.disabledNames(available), [
        ...ownedDrafts(replacing ?? draft.name.trim()),
        draft,
      ]);
    },
    [record.data, save, ownedDrafts],
  );

  const removeServer = useCallback(
    async (name: string) => {
      const available = record.data?.available ?? [];
      await save(
        mcpServersState.disabledNames(available).filter((entry) => entry !== name),
        ownedDrafts(name),
      );
    },
    [record.data, save, ownedDrafts],
  );

  return { record, load, toggle, saveServer, removeServer, saving };
}
