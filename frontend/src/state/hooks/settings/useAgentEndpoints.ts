import { useCallback, useEffect, useState } from "preact/hooks";
import { agentEndpointsApi } from "../../../api/agentEndpointsApi";
import type {
  AgentEndpoint,
  AgentEndpointCLI,
  AgentEndpointTestResult,
  AgentEndpointWireAPI,
} from "../../../models/agentEndpoints";
import type { AgentEndpointDraft } from "../../settings/agentEndpointsState";
import {
  AGENT_ENDPOINT_CLIS,
  AGENT_ENDPOINT_WIRE_APIS,
  remove as removeEndpoint,
  sort,
  toPayload,
  upsert,
} from "../../settings/agentEndpointsState";

export interface AgentEndpointsEditor {
  endpoints: AgentEndpoint[] | null;
  supportedCLIs: AgentEndpointCLI[];
  unsupportedCLIs: string[];
  wireApis: AgentEndpointWireAPI[];
  loading: boolean;
  error: string | null;
  reload: () => Promise<void>;
  create: (draft: AgentEndpointDraft) => Promise<void>;
  save: (id: string, draft: AgentEndpointDraft) => Promise<void>;
  remove: (id: string) => Promise<void>;
  setEnabled: (id: string, enabled: boolean) => Promise<void>;
  test: (id: string, projectId: string, model: string) => Promise<AgentEndpointTestResult>;
}

/**
 * Owns the admin register's remote state. Every mutation folds the server's
 * response back into the local list, so ordering stays identical to a fresh
 * load — including after a Test, whose response carries the new stamp.
 */
export function useAgentEndpoints(enabled: boolean): AgentEndpointsEditor {
  const [endpoints, setEndpoints] = useState<AgentEndpoint[] | null>(null);
  const [supportedCLIs, setSupportedCLIs] = useState<AgentEndpointCLI[]>([
    ...AGENT_ENDPOINT_CLIS,
  ]);
  const [unsupportedCLIs, setUnsupportedCLIs] = useState<string[]>([]);
  const [wireApis, setWireApis] = useState<AgentEndpointWireAPI[]>([
    ...AGENT_ENDPOINT_WIRE_APIS,
  ]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const registry = await agentEndpointsApi.list();
      setEndpoints(sort(registry.endpoints ?? []));
      if (registry.supportedCLIs?.length) setSupportedCLIs(registry.supportedCLIs);
      setUnsupportedCLIs(registry.unsupportedCLIs ?? []);
      if (registry.wireApis?.length) setWireApis(registry.wireApis);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (enabled) void reload();
  }, [enabled, reload]);

  const create = useCallback(async (draft: AgentEndpointDraft) => {
    const created = await agentEndpointsApi.create(toPayload(draft));
    setEndpoints((current) => upsert(current ?? [], created));
  }, []);

  const save = useCallback(async (id: string, draft: AgentEndpointDraft) => {
    const updated = await agentEndpointsApi.update(id, toPayload(draft));
    setEndpoints((current) => upsert(current ?? [], updated));
  }, []);

  const remove = useCallback(async (id: string) => {
    await agentEndpointsApi.remove(id);
    setEndpoints((current) => removeEndpoint(current ?? [], id));
  }, []);

  const setEnabledFor = useCallback(async (id: string, next: boolean) => {
    const updated = await agentEndpointsApi.setEnabled(id, next);
    setEndpoints((current) => upsert(current ?? [], updated));
  }, []);

  const test = useCallback(
    async (id: string, projectId: string, model: string) => {
      const result = await agentEndpointsApi.test(id, projectId, model);
      // The probe stamps the profile server-side; reload so the table's "last
      // test" column agrees with what the operator just saw.
      void reload();
      return result;
    },
    [reload],
  );

  return {
    endpoints,
    supportedCLIs,
    unsupportedCLIs,
    wireApis,
    loading,
    error,
    reload,
    create,
    save,
    remove,
    setEnabled: setEnabledFor,
    test,
  };
}
