import { useCallback, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectMeta } from "../../../models/project";
import type {
  ProjectDataLoadSignal,
  SecretsRecord,
} from "../../projects/projectContainerRecords";

export function useProjectSecrets(project: ProjectMeta | null) {
  const [record, setRecord] = useState<SecretsRecord>({ loading: false });

  const load = useCallback(
    async (signal?: ProjectDataLoadSignal) => {
      if (!project) {
        setRecord({ loading: false });
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const view = await projectApi.listSecrets(project.id);
        if (signal?.cancelled) return;
        setRecord({ loading: false, data: view.secrets, inherited: view.inherited });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, error: (error as Error).message });
      }
    },
    [project]
  );

  const save = useCallback(
    async (key: string, value: string) => {
      if (!project) return;
      const saved = await projectApi.setSecret(project.id, key, value);
      setRecord((current) => {
        const list = current.data ? [...current.data] : [];
        const index = list.findIndex((secret) => secret.key === saved.key);
        if (index >= 0) list[index] = saved;
        else list.push(saved);
        list.sort((left, right) => left.key.localeCompare(right.key));
        return { ...current, loading: false, data: list };
      });
    },
    [project]
  );

  const remove = useCallback(
    async (key: string) => {
      if (!project) return;
      await projectApi.deleteSecret(project.id, key);
      setRecord((current) => ({
        ...current,
        loading: false,
        data: current.data?.filter((secret) => secret.key !== key) ?? [],
      }));
    },
    [project]
  );

  return { record, load, save, remove };
}
