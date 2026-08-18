import { useCallback, useEffect, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { TrashedProject } from "../../../models/project";
import { snapshotState } from "../../projects/snapshotState";

export interface ProjectTrash {
  projects: TrashedProject[];
  loading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  restore: (projectId: string) => Promise<void>;
  purge: (projectId: string) => Promise<void>;
}

/**
 * The Trash page's data. Loaded on demand: the listing only matters while the
 * tab is open, and a restore recreates a container, which is not something to
 * kick off from a background refresh.
 */
export function useProjectTrash(enabled: boolean): ProjectTrash {
  const [projects, setProjects] = useState<TrashedProject[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setProjects(await projectApi.listTrash());
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void refresh();
  }, [enabled, refresh]);

  const restore = useCallback(
    async (projectId: string) => {
      await projectApi.restoreProject(projectId);
      setProjects((current) => snapshotState.removeProject(current, projectId));
    },
    [],
  );

  const purge = useCallback(async (projectId: string) => {
    await projectApi.purgeProject(projectId);
    setProjects((current) => snapshotState.removeProject(current, projectId));
  }, []);

  return { projects, loading, error, refresh, restore, purge };
}
