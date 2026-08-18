import { useCallback, useEffect, useState } from "preact/hooks";
import type { RefObject } from "preact";
import type { BrowserElementCapture } from "../../../models/browser";
import type { ChatMeta } from "../../../models/chat";
import type { ContainerApp, ProjectMeta } from "../../../models/project";
import type { ChatMessageBlock } from "../../../models/chatMessage";
import { projectApi } from "../../../api/projectApi";
import { projectPreviewPort } from "../../../shared/projectPreviewUrls";
import { chatBrowserState } from "../../chat/chatBrowserState";

export function useChatBrowserController({
  chat,
  projects,
  blocks,
  text,
  setText,
  textareaRef,
}: {
  chat: ChatMeta;
  projects: ProjectMeta[];
  blocks: ChatMessageBlock[];
  text: string;
  setText: (text: string) => void;
  textareaRef: RefObject<HTMLTextAreaElement>;
}) {
  const [browserOpen, setBrowserOpen] = useState(false);
  // Bumped to ask the drawer to switch to the Agent Browser pane. A counter
  // rather than a boolean so a second request re-focuses the pane even if the
  // user has since switched back to the app preview.
  const [agentBrowserRequest, setAgentBrowserRequest] = useState(0);
  const [containerApps, setContainerApps] = useState<ContainerApp[]>([]);
  const [appsLoading, setAppsLoading] = useState(false);
  const [selectedAppPort, setSelectedAppPort] = useState<number | null>(null);
  const browserProject = chat.projectId
    ? projects.find((project) => project.id === chat.projectId) ?? null
    : null;
  const browserUrl = browserProject
    ? chatBrowserState.latestPublicDevUrl(blocks, browserProject.slug)
    : "";

  useEffect(() => {
    setBrowserOpen(false);
    setAgentBrowserRequest(0);
  }, [chat.id]);

  const loadContainerApps = useCallback(async () => {
    if (!chat.projectId) {
      setContainerApps([]);
      setSelectedAppPort(null);
      return;
    }
    setAppsLoading(true);
    try {
      const apps = await projectApi.listApps(chat.projectId);
      setContainerApps(apps);
      setSelectedAppPort((prev) => {
        if (apps.length === 0) return null;
        if (prev != null && apps.some((app) => app.port === prev)) return prev;
        const hinted = projectPreviewPort(browserUrl);
        if (hinted != null && apps.some((app) => app.port === hinted)) return hinted;
        return apps[apps.length - 1].port;
      });
    } catch {
      setContainerApps([]);
      setSelectedAppPort(null);
    } finally {
      setAppsLoading(false);
    }
  }, [chat.projectId, browserUrl]);

  function openBrowserDrawer() {
    if (!chat.projectId) {
      alert("This chat is not attached to a project container.");
      return;
    }
    setBrowserOpen(true);
    void loadContainerApps();
  }

  // Opens the drawer straight onto the Agent Browser pane. Used by the
  // preview popover's "Open in Agent Browser" action, which starts and
  // navigates the shared browser and then wants the user looking at it.
  function openAgentBrowserPane() {
    if (!chat.projectId) return;
    setBrowserOpen(true);
    setAgentBrowserRequest((value) => value + 1);
    void loadContainerApps();
  }

  function insertBrowserElementContext(capture: BrowserElementCapture) {
    const insertion = `\n\n${chatBrowserState.formatElementCapture(capture)}\n\n`;
    const textarea = textareaRef.current;
    const start = textarea?.selectionStart ?? text.length;
    const end = textarea?.selectionEnd ?? start;
    const next = `${text.slice(0, start)}${insertion}${text.slice(end)}`;
    setText(next);
    setTimeout(() => {
      textareaRef.current?.focus();
      const pos = start + insertion.length;
      textareaRef.current?.setSelectionRange(pos, pos);
    }, 0);
  }

  return {
    browserOpen,
    browserProject,
    // The most recent preview URL this chat mentioned, used to resolve a
    // playbook's {{previewUrl}} without paying for a port scan.
    previewUrl: browserUrl,
    agentBrowserRequest,
    openAgentBrowserPane,
    containerApps,
    appsLoading,
    selectedAppPort,
    setSelectedAppPort,
    openBrowserDrawer,
    closeBrowserDrawer: () => setBrowserOpen(false),
    loadContainerApps,
    insertBrowserElementContext,
  };
}
