import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { requestJson } from "../../../api/apiRequest";
import { API_ROUTES } from "../../../config/routes";
import type { DeployTarget } from "../../../models/deploy";

export interface ProjectDeployState {
  targets: DeployTarget[];
  loading: boolean;
  busy: boolean;
  error: string | null;
  setEnabled: (key: string, enabled: boolean) => Promise<void>;
  refresh: () => Promise<void>;
}

/**
 * One project's deploy access.
 *
 * There is no polling: this state only changes when somebody changes it, and
 * the PUT answers with the target's new state after the container has been
 * converged — so the response is the truth rather than a hint to go and check.
 */
export function useProjectDeploy(projectId: string | null): ProjectDeployState {
  const [targets, setTargets] = useState<DeployTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    if (!projectId) {
      setTargets([]);
      setLoading(false);
      return;
    }
    try {
      const body = await requestJson<{ targets?: DeployTarget[] }>(
        "GET",
        API_ROUTES.projects.deploy(projectId),
      );
      if (alive.current) setTargets(body?.targets ?? []);
    } catch {
      // A project on a deployment with no vault simply has no targets. That
      // is not an error worth putting in front of anyone.
      if (alive.current) setTargets([]);
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    setLoading(true);
    void load();
  }, [load]);

  const setEnabled = useCallback(
    async (key: string, enabled: boolean) => {
      if (!projectId) return;
      setBusy(true);
      setError(null);
      try {
        const updated = await requestJson<DeployTarget>(
          "PUT",
          API_ROUTES.projects.deploy(projectId),
          { key, enabled },
        );
        if (alive.current) {
          setTargets((current) =>
            current.map((target) => (target.key === updated.key ? updated : target)),
          );
        }
      } catch (cause) {
        if (alive.current) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [projectId],
  );

  return { targets, loading, busy, error, setEnabled, refresh: load };
}
