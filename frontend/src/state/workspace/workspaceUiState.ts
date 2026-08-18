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
  view: WorkspaceView;
}

export type WorkspaceUiAction =
  | { type: "select-chat"; chatId: string | null }
  | { type: "open-sidebar" }
  | { type: "close-sidebar" }
  | { type: "show-chat" }
  | { type: "show-home" }
  | { type: "show-settings"; tab?: string }
  | { type: "show-project-containers"; projectId: string | null; tab?: string };

class WorkspaceUiStateTransitions {
  createInitial(): WorkspaceUiState {
    return {
      activeChatId: null,
      containerProjectId: null,
      containerTab: null,
      settingsTab: null,
      sidebarOpen: false,
      view: "chat",
    };
  }

  readonly reduce = (
    state: WorkspaceUiState,
    action: WorkspaceUiAction
  ): WorkspaceUiState => {
    switch (action.type) {
      case "select-chat":
        return {
          ...state,
          activeChatId: action.chatId,
          sidebarOpen: false,
          view: "chat",
        };
      case "open-sidebar":
        return { ...state, sidebarOpen: true };
      case "close-sidebar":
        return { ...state, sidebarOpen: false };
      case "show-chat":
        return { ...state, view: "chat" };
      // The selected chat is deliberately kept, so leaving Home returns to
      // whatever was open before it.
      case "show-home":
        return { ...state, view: "home", sidebarOpen: false };
      case "show-settings":
        return { ...state, view: "settings", settingsTab: action.tab ?? null, sidebarOpen: false };
      case "show-project-containers":
        return {
          ...state,
          containerProjectId: action.projectId,
          containerTab: action.tab ?? null,
          view: "project-containers",
          sidebarOpen: false,
        };
    }
  };
}

export const workspaceUiState = new WorkspaceUiStateTransitions();
