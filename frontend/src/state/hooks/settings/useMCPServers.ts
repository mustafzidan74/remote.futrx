import { useCallback, useEffect, useState } from "preact/hooks";
import { mcpApi } from "../../../api/mcpApi";
import type { MCPServer, MCPTestResult } from "../../../models/mcp";
import { MCP_PROVIDERS, mcpServersState } from "../../settings/mcpServersState";
import type { MCPDraft } from "../../settings/mcpServersState";

export interface MCPRegistryEditor {
  servers: MCPServer[] | null;
  supportedProviders: string[];
  unsupportedProviders: string[];
  loading: boolean;
  error: string | null;
  reload: () => Promise<void>;
  create: (draft: MCPDraft) => Promise<void>;
  save: (name: string, draft: MCPDraft) => Promise<void>;
  remove: (name: string) => Promise<void>;
  test: (name: string, projectId: string) => Promise<MCPTestResult>;
}

// useMCPServers owns the admin registry's remote state. Every mutation folds
// the server's response back into the local list through mcpServersState so
// ordering stays identical to a fresh load.
export function useMCPServers(enabled: boolean): MCPRegistryEditor {
  const [servers, setServers] = useState<MCPServer[] | null>(null);
  const [supportedProviders, setSupportedProviders] = useState<string[]>([...MCP_PROVIDERS]);
  const [unsupportedProviders, setUnsupportedProviders] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const registry = await mcpApi.list();
      setServers(mcpServersState.sort(registry.servers ?? []));
      if (registry.supportedProviders?.length) {
        setSupportedProviders(registry.supportedProviders);
      }
      setUnsupportedProviders(registry.unsupportedProviders ?? []);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (enabled) void reload();
  }, [enabled, reload]);

  const create = useCallback(async (draft: MCPDraft) => {
    const created = await mcpApi.create(mcpServersState.toPayload(draft, { platform: true }));
    setServers((current) => mcpServersState.upsert(current ?? [], created));
  }, []);

  const save = useCallback(async (name: string, draft: MCPDraft) => {
    const updated = await mcpApi.update(name, mcpServersState.toPayload(draft, { platform: true }));
    setServers((current) => mcpServersState.upsert(current ?? [], updated));
  }, []);

  const remove = useCallback(async (name: string) => {
    await mcpApi.remove(name);
    setServers((current) => mcpServersState.remove(current ?? [], name));
  }, []);

  const test = useCallback(
    (name: string, projectId: string) => mcpApi.test(name, projectId),
    [],
  );

  return {
    servers,
    supportedProviders,
    unsupportedProviders,
    loading,
    error,
    reload,
    create,
    save,
    remove,
    test,
  };
}
