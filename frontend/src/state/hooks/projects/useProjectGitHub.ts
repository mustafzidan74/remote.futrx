import { useCallback, useEffect, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectMeta } from "../../../models/project";
import type {
  CreateGitHubPRInput,
  CreateGitHubPRResult,
  GitHubPullRequest,
  GitHubSettings,
  GitHubStatus,
  ImportGitHubCommentsResult,
  UpdateGitHubSettingsInput,
} from "../../../models/github";
import type { ProjectDataLoadSignal } from "../../projects/projectContainerRecords";

/** The panel's three records: the link, the automation settings, the open PRs. */
export interface GitHubRecord {
  loading: boolean;
  status?: GitHubStatus;
  settings?: GitHubSettings;
  error?: string;
}

export interface GitHubPullsRecord {
  loading: boolean;
  data?: GitHubPullRequest[];
  error?: string;
}

/**
 * One project's GitHub integration.
 *
 * The webhook secret is held here as `issuedSecret` and never folded back into
 * the stored settings, exactly like the client portal's one-time link: the
 * server returns it once, and re-reading the settings must not resurrect it.
 *
 * Pull requests are loaded on demand rather than with the panel. Listing them
 * shells into the container and talks to github.com, which is far too much
 * work to do every time somebody opens the Settings tab.
 *
 * For the same reason the panel itself only loads while its tab is open:
 * reading the status runs three commands inside the container, and doing that
 * behind a closed tab would make every visit to Project settings slower for no
 * one's benefit.
 */
export function useProjectGitHub(project: ProjectMeta | null, enabled: boolean) {
  const [record, setRecord] = useState<GitHubRecord>({ loading: false });
  const [pulls, setPulls] = useState<GitHubPullsRecord>({ loading: false });
  const [issuedSecret, setIssuedSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Keyed on the id rather than the project object: the sidebar re-fetches
  // projects on a timer, so a new object identity arrives regularly, and
  // depending on it would re-run three container commands every poll.
  const projectId = project?.id ?? null;

  const load = useCallback(
    async (signal?: ProjectDataLoadSignal) => {
      if (!projectId) {
        setRecord({ loading: false });
        setPulls({ loading: false });
        setIssuedSecret(null);
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        // The settings document exists even for an unlinked project (it is
        // empty), so both are read together and a failure of either is one
        // failure of the panel.
        const [status, settings] = await Promise.all([
          projectApi.getGitHubStatus(projectId),
          projectApi.getGitHubSettings(projectId),
        ]);
        if (signal?.cancelled) return;
        setRecord({ loading: false, status, settings });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, error: (error as Error).message });
      }
    },
    [projectId],
  );

  useEffect(() => {
    if (!enabled || !projectId) return;
    const signal = { cancelled: false };
    void load(signal);
    return () => {
      signal.cancelled = true;
    };
  }, [enabled, projectId, load]);

  const link = useCallback(
    async (repo: string) => {
      if (!project) throw new Error("No project selected.");
      setBusy(true);
      try {
        const status = await projectApi.linkGitHub(project.id, repo);
        setRecord((current) => ({ ...current, loading: false, status, error: undefined }));
      } finally {
        setBusy(false);
      }
    },
    [project],
  );

  const unlink = useCallback(async () => {
    if (!project) return;
    setBusy(true);
    try {
      await projectApi.unlinkGitHub(project.id);
      // Unlinking deletes the webhook secret server-side, so the whole panel
      // is re-read rather than patched: nothing local is still true.
      await load();
      setPulls({ loading: false });
      setIssuedSecret(null);
    } finally {
      setBusy(false);
    }
  }, [project, load]);

  const clone = useCallback(async () => {
    if (!project) return;
    setBusy(true);
    try {
      const status = await projectApi.cloneGitHub(project.id);
      setRecord((current) => ({ ...current, loading: false, status, error: undefined }));
    } finally {
      setBusy(false);
    }
  }, [project]);

  const saveSettings = useCallback(
    async (input: UpdateGitHubSettingsInput): Promise<GitHubSettings> => {
      if (!project) throw new Error("No project selected.");
      setBusy(true);
      try {
        const saved = await projectApi.saveGitHubSettings(project.id, input);
        // The secret is the one-time value; strip it before it reaches the
        // record the panel re-reads on every render.
        const { secret, ...stored } = saved;
        setRecord((current) => ({ ...current, loading: false, settings: stored }));
        if (secret) setIssuedSecret(secret);
        return saved;
      } finally {
        setBusy(false);
      }
    },
    [project],
  );

  const loadPulls = useCallback(async () => {
    if (!project) return;
    setPulls((current) => ({ ...current, loading: true, error: undefined }));
    try {
      const data = await projectApi.listGitHubPullRequests(project.id);
      setPulls({ loading: false, data });
    } catch (error) {
      setPulls({ loading: false, error: (error as Error).message });
    }
  }, [project]);

  const createPullRequest = useCallback(
    async (input: CreateGitHubPRInput): Promise<CreateGitHubPRResult> => {
      if (!project) throw new Error("No project selected.");
      setBusy(true);
      try {
        const created = await projectApi.createGitHubPullRequest(project.id, input);
        // The working tree moved: a new branch, possibly a new commit, and
        // certainly a new ahead/behind. Re-read rather than guess.
        void load();
        void loadPulls();
        return created;
      } finally {
        setBusy(false);
      }
    },
    [project, load, loadPulls],
  );

  const importComments = useCallback(
    async (number: number, chatId: string): Promise<ImportGitHubCommentsResult> => {
      if (!project) throw new Error("No project selected.");
      setBusy(true);
      try {
        return await projectApi.importGitHubComments(project.id, number, chatId);
      } finally {
        setBusy(false);
      }
    },
    [project],
  );

  return {
    record,
    pulls,
    issuedSecret,
    busy,
    dismissIssuedSecret: () => setIssuedSecret(null),
    load,
    link,
    unlink,
    clone,
    saveSettings,
    loadPulls,
    createPullRequest,
    importComments,
  };
}

export type ProjectGitHub = ReturnType<typeof useProjectGitHub>;
