import type {
  WorkspaceUiAction,
  WorkspaceUiState,
} from "../../models/workspace";

class WorkspaceUiStateTransitions {
  // A notification tap on a cold start arrives as ?chat=<id>, so the first
  // render can open straight into that chat instead of the newest one.
  createInitial(requestedChatId: string | null = null): WorkspaceUiState {
    return {
      activeChatId: requestedChatId,
      containerProjectId: null,
      containerTab: null,
      settingsTab: null,
      sidebarOpen: false,
      createProjectOpen: false,
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
      case "open-create-project":
        return { ...state, createProjectOpen: true };
      case "close-create-project":
        return { ...state, createProjectOpen: false };
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
