import { requestJson } from "../apiRequest";
import type { AgentBrowserInfo } from "../../models/project";
import { API_ROUTES } from "../../config/routes";

export const agentBrowserApi = {
  fetchAgentBrowserStatus: (id: string) =>
    requestJson<AgentBrowserInfo>(
      "GET",
      API_ROUTES.projects.agentBrowser(id)
    ),

  startAgentBrowser: (id: string) =>
    requestJson<AgentBrowserInfo>(
      "POST",
      API_ROUTES.projects.startAgentBrowser(id),
      {}
    ),

  // Drives the already-running shared browser to a URL by opening a new tab
  // through the container's loopback DevTools endpoint. The backend refuses
  // anything but container loopback or this project's own preview host.
  navigateAgentBrowser: (id: string, url: string) =>
    requestJson<{ url: string }>(
      "POST",
      API_ROUTES.projects.navigateAgentBrowser(id),
      { url }
    ),

  stopAgentBrowser: (id: string, scope?: "view") =>
    requestJson<AgentBrowserInfo | { status: "stopped" }>(
      "DELETE",
      API_ROUTES.projects.agentBrowser(id, scope)
    ),
};
