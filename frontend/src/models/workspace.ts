import type { ChatMeta } from "./chat";
import type { ProjectMeta } from "./project";
import type { ProjectHealth } from "./health";
import type { WorkspaceMessage } from "../types/workspaceApi";

/** What the workspace feed has delivered so far. */
export interface WorkspaceSnapshot {
  chats: ChatMeta[];
  projects: ProjectMeta[];
  /** Live health verdicts keyed by project id; empty when the monitor is off. */
  health: Record<string, ProjectHealth>;
  /** False until the first snapshot lands. An empty list before that means
   *  "not known yet", not "none" — callers must not act on the difference. */
  loaded: boolean;
}

/** Opens the workspace feed and reports messages until the returned call. */
export type SubscribeToWorkspace = (
  onMessage: (message: WorkspaceMessage) => void,
) => () => void;

export interface WorkspaceStoreState {
  snapshot: WorkspaceSnapshot;
}

export interface WorkspaceStoreActions {
  setConnected: (connected: boolean) => void;
  /** Applies a chat this client just created, ahead of the server's own
   *  `chat.upsert` for it. */
  seedChat: (chat: ChatMeta) => void;
}

/**
 * "home" is the dashboard. It is a destination of its own rather than a
 * flavour of "chat" with nothing selected, because it has to be reachable
 * while a chat is open — from the sidebar, the app title and the palette —
 * and going back must return to the chat that was already selected.
 */
export type WorkspaceView = "chat" | "settings" | "project-containers" | "home";

export interface WorkspaceUiState {
  activeChatId: string | null;
  containerProjectId: string | null;
  /**
   * Sub-tab a caller asked the destination page to open on, or null to leave
   * the page wherever it was left. Kept as plain ids so this module stays
   * independent of the pages that name them.
   */
  containerTab: string | null;
  settingsTab: string | null;
  sidebarOpen: boolean;
  createProjectOpen: boolean;
  view: WorkspaceView;
}

export type WorkspaceUiAction =
  | { type: "select-chat"; chatId: string | null }
  | { type: "open-sidebar" }
  | { type: "close-sidebar" }
  | { type: "open-create-project" }
  | { type: "close-create-project" }
  | { type: "show-chat" }
  | { type: "show-home" }
  | { type: "show-settings"; tab?: string }
  | { type: "show-project-containers"; projectId: string | null; tab?: string };

export type DropPosition = "before" | "after";

export interface ProjectSidebarNode {
  project: ProjectMeta;
  chats: ChatMeta[];
}

/** The project tree. Ranked search results are owned by the search state. */
export interface WorkspaceSidebarModel {
  visibleProjects: ProjectSidebarNode[];
  visibleLooseChats: ChatMeta[];
  totalChats: number;
  totalProjects: number;
}
