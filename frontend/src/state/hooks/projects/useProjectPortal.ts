import { useCallback, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectMeta, ProjectPortal } from "../../../models/project";
import type { PortalRecord, ProjectDataLoadSignal } from "../../projects/projectContainerRecords";
import {
  type PortalFormState,
  portalFormFrom,
  portalUpdateInput,
} from "../../projects/projectPortalState";

/**
 * The client portal for one project. The plaintext link exists only in the
 * response that minted it, so it is held here as `issuedUrl` and never folded
 * back into the stored record.
 */
export function useProjectPortal(project: ProjectMeta | null) {
  const [record, setRecord] = useState<PortalRecord>({ loading: false });
  const [issuedUrl, setIssuedUrl] = useState<string | null>(null);

  const load = useCallback(
    async (signal?: ProjectDataLoadSignal) => {
      if (!project) {
        setRecord({ loading: false });
        setIssuedUrl(null);
        return;
      }
      setRecord((current) => ({ ...current, loading: true, error: undefined }));
      try {
        const data = await projectApi.getPortal(project.id);
        if (signal?.cancelled) return;
        setRecord({ loading: false, data });
      } catch (error) {
        if (signal?.cancelled) return;
        setRecord({ loading: false, error: (error as Error).message });
      }
    },
    [project],
  );

  const save = useCallback(
    async (
      form: PortalFormState,
      overrides: { enabled?: boolean; rotate?: boolean } = {},
    ): Promise<ProjectPortal> => {
      if (!project) throw new Error("No project selected.");
      const saved = await projectApi.savePortal(
        project.id,
        portalUpdateInput(form, overrides),
      );
      // The url is the one-time secret; strip it before it reaches the record
      // the form re-reads on every render.
      const { url, ...stored } = saved;
      setRecord({ loading: false, data: stored });
      setIssuedUrl(url ?? null);
      return saved;
    },
    [project],
  );

  const dismissIssuedUrl = useCallback(() => setIssuedUrl(null), []);

  return {
    record,
    form: portalFormFrom(record.data),
    issuedUrl,
    dismissIssuedUrl,
    load,
    save,
  };
}
