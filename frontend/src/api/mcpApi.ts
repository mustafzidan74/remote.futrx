import { requestJson } from "./apiRequest";
import type {
  MCPProjectPayload,
  MCPProjectSettings,
  MCPRegistry,
  MCPServer,
  MCPServerDraftPayload,
  MCPTestResult,
} from "../models/mcp";
import { API_ROUTES } from "../config/routes";

// Client for the MCP registry. The admin routes are gated on the server; a
// non-admin session receives 403. The project routes need membership only,
// which the project route has already established by the time they run.
export const mcpApi = {
  list: () => requestJson<MCPRegistry>("GET", API_ROUTES.mcpServers.collection),

  create: (draft: MCPServerDraftPayload) =>
    requestJson<MCPServer>("POST", API_ROUTES.mcpServers.collection, draft),

  update: (name: string, draft: MCPServerDraftPayload) =>
    requestJson<MCPServer>("PUT", API_ROUTES.mcpServers.item(name), draft),

  remove: (name: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.mcpServers.item(name)),

  /** Runs the handshake inside one project's container and returns its raw output. */
  test: (name: string, projectId: string) =>
    requestJson<MCPTestResult>("POST", API_ROUTES.mcpServers.test(name), { projectId }),

  projectSettings: (projectId: string) =>
    requestJson<MCPProjectSettings>("GET", API_ROUTES.projects.mcp(projectId)),

  saveProjectSettings: (projectId: string, payload: MCPProjectPayload) =>
    requestJson<MCPProjectSettings>("PUT", API_ROUTES.projects.mcp(projectId), payload),
};
