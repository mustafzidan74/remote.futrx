import type { ComponentChildren } from "preact";
import { createContext } from "preact";
import {
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "preact/hooks";
import type { ChatMeta } from "../../models/chat";
import type { ProjectMeta } from "../../models/project";
import type { ProjectHealthMap } from "../workspace/projectHealthState";
import { chatApi } from "../../api/chatApi";
import { createChatInput } from "./createChatInput";
import { projectApi } from "../../api/projectApi";
import { templateApi } from "../../api/templateApi";
import { useWorkspaceData } from "../hooks/workspace/useWorkspaceData";
import { useWorkspacePushLifecycle } from "../hooks/push/useWorkspacePushLifecycle";
import { useUserSettingsContext } from "./UserSettingsContext";
import type { WorkspaceUiState } from "../../models/workspace";
import { workspaceUiState } from "./workspaceUiState";
import { workspaceSidebarService } from "../../services/workspace/workspaceSidebarService.ts";
import { agentCapabilityCatalogStore } from "../stores/agents/agentCapabilityCatalogStore";
import { takePushNotificationChatId } from "./pushNotificationNavigation";
import { useAuthContext } from "./AuthContext";
import { chatDeepLinkState } from "../workspace/chatDeepLink";
import { projectDeepLinkState } from "../workspace/projectDeepLink";
import {
  newProjectState,
  type NewProjectState,
} from "../projects/newProjectState";

interface WorkspaceContextValue {
  chats: ChatMeta[];
  projects: ProjectMeta[];
  /** Live health verdicts keyed by project id; empty when the monitor is off. */
  health: ProjectHealthMap;
  activeChat: ChatMeta | null;
  /** False until the first workspace snapshot lands. An empty list before that
   *  means "not known yet" — surfaces must show placeholders, not empty states. */
  loaded: boolean;
  ui: WorkspaceUiState;
  selectChat: (chatId: string | null) => void;
  /**
   * Opens a chat and asks the thread to scroll to, and briefly flash, the
   * message at `at` (unix ms). Used by search hits and by `?chat=&at=` links.
   */
  selectChatAt: (chatId: string, at: number) => void;
  /** The message instant the thread should reveal, or null. */
  highlightAt: number | null;
  openSidebar: () => void;
  closeSidebar: () => void;
  showChat: () => void;
  showHome: () => void;
  showSettings: (tab?: string) => void;
  showProjectContainers: (projectId: string | null, tab?: string) => void;
  openCreateProject: () => void;
  closeCreateProject: () => void;
  createProject: (name: string) => Promise<ProjectMeta>;
  newProject: NewProjectState;
  openNewProject: () => void;
  closeNewProject: () => void;
  setNewProjectName: (name: string) => void;
  selectNewProjectTemplate: (template: string) => void;
  setNewProjectInput: (key: string, value: string) => void;
  submitNewProject: () => Promise<void>;
  createChat: (projectId?: string) => Promise<ChatMeta>;
  deleteChat: (chatId: string) => Promise<void>;
  forkChat: (chatId: string) => Promise<ChatMeta>;
  deleteProject: (projectId: string) => Promise<void>;
  reorderProjects: (projectIds: string[]) => Promise<void>;
  startProject: (projectId: string) => Promise<void>;
  stopProject: (projectId: string) => Promise<void>;
}

const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

export function WorkspaceProvider({
  enabled,
  children,
}: {
  enabled: boolean;
  children: ComponentChildren;
}) {
  ////////////////
  // Local State
  ////////////////
  const data = useWorkspaceData(enabled);
  const { auth } = useAuthContext();
  const { settings } = useUserSettingsContext();
  // The message instant a `?chat=&at=` link asked for is read before the
  // reducer initializer strips the chat parameter from the address bar.
  const [highlightAt, setHighlightAt] = useState<number | null>(
    () => chatDeepLinkState.parseAt(location.search)
  );
  const [ui, dispatch] = useReducer(
    workspaceUiState.reduce,
    null,
    () => workspaceUiState.createInitial(takePushNotificationChatId())
  );
  const [newProject, dispatchNewProject] = useReducer(
    newProjectState.reduce,
    newProjectState.createInitial()
  );
  const activeChat = workspaceSidebarService.activeChat(data.chats, ui.activeChatId);
  const account = auth.email || auth.adminEmail;
  const capabilityUserId = account || "anonymous";
  const activeCapabilityProjectId = activeChat?.projectId;
  // A health notification links to `/?project=<id>`. It opens the project's
  // settings page, whose Info tab is the same view the sidebar dot opens.
  const deepLinkProjectId = useRef<string | null>(projectDeepLinkState.parse(location.search));

  ////////////////
  // Handlers
  ////////////////
  const openPushChat = useCallback((chatId: string) => {
    dispatch({ type: "select-chat", chatId });
  }, []);

  const activateNewChat = useCallback((chat: ChatMeta): ChatMeta => {
    data.seedChat(chat);
    dispatch({ type: "select-chat", chatId: chat.id });
    return chat;
  }, [data.seedChat]);

  const createProject = useCallback(async (name: string): Promise<ProjectMeta> => {
    const project = await projectApi.create(name);
    return project;
  }, []);

  const createChat = useCallback(async (projectId?: string): Promise<ChatMeta> => {
    const chat = await chatApi.create(createChatInput(settings, projectId));
    return activateNewChat(chat);
  }, [settings, activateNewChat]);

  const deleteChat = useCallback(async (chatId: string) => {
    await chatApi.delete(chatId);
  }, []);

  const forkChat = useCallback(async (chatId: string): Promise<ChatMeta> => {
    const chat = await chatApi.fork(chatId);
    return activateNewChat(chat);
  }, [activateNewChat]);

  const deleteProject = useCallback(async (projectId: string) => {
    await projectApi.delete(projectId);
    agentCapabilityCatalogStore.getState().removeProject(capabilityUserId, projectId);
  }, [capabilityUserId]);

  const reorderProjects = useCallback(async (projectIds: string[]) => {
    await projectApi.reorder(projectIds);
  }, []);

  const startProject = useCallback(async (projectId: string) => {
    await projectApi.start(projectId);
  }, []);

  const stopProject = useCallback(async (projectId: string) => {
    await projectApi.stop(projectId);
  }, []);

  // The template catalog is fetched once the dialog is first opened, not on
  // mount: it is a static list only this dialog needs.
  const openNewProject = useCallback(() => {
    dispatchNewProject({ type: "open" });
    if (newProject.templates.length > 0 || newProject.templatesLoading) return;
    dispatchNewProject({ type: "templates-loading" });
    templateApi
      .list()
      .then((templates) => dispatchNewProject({ type: "templates-loaded", templates }))
      .catch((error: Error) =>
        dispatchNewProject({ type: "templates-failed", error: error.message })
      );
  }, [newProject.templates.length, newProject.templatesLoading]);

  const submitNewProject = useCallback(async (): Promise<void> => {
    const name = newProjectState.submittedName(newProject);
    if (!name) return;
    dispatchNewProject({ type: "submit" });
    try {
      await projectApi.create(
        name,
        newProject.template,
        newProjectState.submittedInputs(newProject)
      );
      dispatchNewProject({ type: "close" });
    } catch (error) {
      dispatchNewProject({ type: "submit-failed", error: (error as Error).message });
    }
  }, [newProject]);

  // Dispatch-only commands. preact creates `dispatch` once, so these close over
  // nothing that can go stale and need no dependencies.
  const selectChat = useCallback((chatId: string | null) => {
    setHighlightAt(null);
    dispatch({ type: "select-chat", chatId });
  }, []);
  const selectChatAt = useCallback((chatId: string, at: number) => {
    setHighlightAt(at > 0 ? at : null);
    dispatch({ type: "select-chat", chatId });
  }, []);
  const openSidebar = useCallback(() => dispatch({ type: "open-sidebar" }), []);
  const closeSidebar = useCallback(() => dispatch({ type: "close-sidebar" }), []);
  const showChat = useCallback(() => dispatch({ type: "show-chat" }), []);
  const showHome = useCallback(() => dispatch({ type: "show-home" }), []);
  const showSettings = useCallback((tab?: string) => dispatch({ type: "show-settings", tab }), []);
  const showProjectContainers = useCallback((projectId: string | null, tab?: string) => {
    dispatch({ type: "show-project-containers", projectId, tab });
  }, []);
  const openCreateProject = useCallback(() => dispatch({ type: "open-create-project" }), []);
  const closeCreateProject = useCallback(() => dispatch({ type: "close-create-project" }), []);
  const closeNewProject = useCallback(() => dispatchNewProject({ type: "close" }), []);
  const setNewProjectName = useCallback(
    (name: string) => dispatchNewProject({ type: "set-name", name }),
    []
  );
  const selectNewProjectTemplate = useCallback(
    (template: string) => dispatchNewProject({ type: "select-template", template }),
    []
  );
  const setNewProjectInput = useCallback(
    (key: string, value: string) => dispatchNewProject({ type: "set-input", key, value }),
    []
  );

  ////////////////
  // Effects
  ////////////////
  useEffect(() => {
    if (!enabled || !activeChat) return;
    void agentCapabilityCatalogStore.getState()
      .load(capabilityUserId, activeCapabilityProjectId)
      .catch(() => undefined);
  }, [enabled, capabilityUserId, activeCapabilityProjectId, activeChat?.id]);

  useWorkspacePushLifecycle({
    account: enabled ? account : "",
    activeChatId: ui.activeChatId,
    view: ui.view,
    openChat: openPushChat,
  });

  // The `at` half of a deep link outlives the chat parameter the reducer
  // initializer consumed; strip it too so a reload does not re-scroll.
  useEffect(() => {
    if (chatDeepLinkState.parseAt(location.search) === null) return;
    history.replaceState(
      null,
      "",
      chatDeepLinkState.withoutChatParam(location.pathname, location.search, location.hash)
    );
  }, []);

  useEffect(() => {
    const chatId = workspaceSidebarService.initialChatId(enabled, ui.activeChatId, data.chats);
    if (chatId) dispatch({ type: "select-chat", chatId });
  }, [data.chats, enabled, ui.activeChatId]);

  // Layout effect, not a passive one: the render that drops the chat from the
  // list already resolves activeChat to null, so a passive effect would let the
  // browser paint the "no chat selected" screen before the handover lands.
  useLayoutEffect(() => {
    // Wait for the first snapshot: a chat id handed over by a notification tap
    // would otherwise be discarded against a not-yet-populated list.
    if (!data.loaded) return;
    if (workspaceSidebarService.isActiveChatMissing(data.chats, ui.activeChatId)) {
      // Hand straight over to the next chat instead of clearing the selection:
      // clearing renders the "no chat selected" empty state for the one frame
      // before the initial-chat effect picks a replacement, which reads as a
      // flash of the New project screen after deleting a chat.
      dispatch({
        type: "select-chat",
        chatId: workspaceSidebarService.replacementChatId(data.chats),
      });
    }
  }, [data.chats, data.loaded, ui.activeChatId]);

  // Applied after the chat effects above: their "most recent chat" fallback
  // would otherwise switch the view straight back to the chat.
  useEffect(() => {
    if (!enabled || data.projects.length === 0) return;
    const requested = deepLinkProjectId.current;
    if (!requested) return;
    deepLinkProjectId.current = null;
    history.replaceState(
      null,
      "",
      projectDeepLinkState.withoutProjectParam(location.pathname, location.search, location.hash)
    );
    if (data.projects.some((project) => project.id === requested)) {
      dispatch({ type: "show-project-containers", projectId: requested });
    }
  }, [data.projects, enabled]);

  ////////////////
  // Context Value
  ////////////////
  // preact force-renders every subscriber whenever the provider's value fails a
  // `!=` check, so a fresh literal here repainted the whole workspace subtree on
  // any render of this provider — including ones driven by upstream auth or
  // settings ticks this tree does not read.
  const value = useMemo<WorkspaceContextValue>(() => ({
    chats: data.chats,
    projects: data.projects,
    health: data.health,
    activeChat,
    loaded: data.loaded,
    ui,
    selectChat,
    selectChatAt,
    highlightAt,
    openSidebar,
    closeSidebar,
    showChat,
    showHome,
    showSettings,
    showProjectContainers,
    openCreateProject,
    closeCreateProject,
    createProject,
    newProject,
    openNewProject,
    closeNewProject,
    setNewProjectName,
    selectNewProjectTemplate,
    setNewProjectInput,
    submitNewProject,
    createChat,
    deleteChat,
    forkChat,
    deleteProject,
    reorderProjects,
    startProject,
    stopProject,
  }), [
    data.chats,
    data.projects,
    data.health,
    data.loaded,
    activeChat,
    ui,
    selectChat,
    selectChatAt,
    highlightAt,
    openSidebar,
    closeSidebar,
    showChat,
    showHome,
    showSettings,
    showProjectContainers,
    openCreateProject,
    closeCreateProject,
    createProject,
    newProject,
    openNewProject,
    closeNewProject,
    setNewProjectName,
    selectNewProjectTemplate,
    setNewProjectInput,
    submitNewProject,
    createChat,
    deleteChat,
    forkChat,
    deleteProject,
    reorderProjects,
    startProject,
    stopProject,
  ]);

  return (
    <WorkspaceContext.Provider value={value}>
      {children}
    </WorkspaceContext.Provider>
  );
}

export function useWorkspaceContext(): WorkspaceContextValue {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error("useWorkspaceContext must be used inside WorkspaceProvider");
  return value;
}
