import { useCallback, useEffect, useMemo, useState } from "preact/hooks";
import type { ProjectMeta } from "../../../models/project";
import { useProjectAccess } from "./useProjectAccess";
import { useProjectContainerInfo } from "./useProjectContainerInfo";
import { useProjectGitHub } from "./useProjectGitHub";
import { useProjectPortal } from "./useProjectPortal";
import { useProjectSecrets } from "./useProjectSecrets";
import { useProjectShares } from "./useProjectShares";
import { useProjectSnapshots } from "./useProjectSnapshots";

export function useProjectContainersController(
  projects: ProjectMeta[],
  selectedProjectId: string | null,
  // Snapshots are only loaded while their tab is open: the list polls while a
  // capture runs, and there is no reason to poll behind another tab.
  snapshotsEnabled = false,
  // The GitHub panel is gated for the same reason: reading its status shells
  // into the container three times.
  githubEnabled = false
) {
  const selectedProject = useMemo(
    () => projects.find((project) => project.id === selectedProjectId) ?? null,
    [projects, selectedProjectId]
  );
  const info = useProjectContainerInfo(selectedProject);
  const secrets = useProjectSecrets(selectedProject);
  const access = useProjectAccess(selectedProject);
  const shares = useProjectShares(selectedProject);
  const snapshots = useProjectSnapshots(selectedProject, snapshotsEnabled);
  const portal = useProjectPortal(selectedProject);
  const github = useProjectGitHub(selectedProject, githubEnabled);
  const [refreshing, setRefreshing] = useState(false);

  const refresh = useCallback(async () => {
    if (!selectedProject) return;
    setRefreshing(true);
    try {
      await Promise.all([
        info.load(),
        secrets.load(),
        access.load(),
        shares.load(),
        snapshots.load(),
      ]);
      await Promise.all([
        info.load(),
        secrets.load(),
        access.load(),
        shares.load(),
        snapshots.load(),
        portal.load(),
        github.load(),
      ]);
    } finally {
      setRefreshing(false);
    }
  }, [
    selectedProject,
    info.load,
    secrets.load,
    access.load,
    shares.load,
    snapshots.load,
    portal.load,
    github.load,
  ]);

  useEffect(() => {
    const signal = { cancelled: false };
    void info.load(signal);
    void secrets.load(signal);
    void access.load(signal);
    void shares.load(signal);
    void portal.load(signal);
    return () => {
      signal.cancelled = true;
    };
  }, [info.load, secrets.load, access.load, shares.load, portal.load]);

  return {
    selectedProject,
    info,
    secrets,
    access,
    shares,
    snapshots,
    portal,
    github,
    refreshing,
    refresh,
  };
}
