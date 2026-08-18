import { useMemo } from "preact/hooks";
import { Sidebar } from "../../ui/sidebar/Sidebar";
import { useAuthContext } from "../../state/context/AuthContext";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useSidebarState } from "../../state/hooks/workspace/useSidebarState";
import { useWorkspaceCommands } from "../../state/hooks/workspace/useWorkspaceCommands";
import { workspaceSidebarState } from "../../state/workspace/workspaceSidebarState";

export function SidebarContainer() {
  const { auth } = useAuthContext();
  const workspace = useWorkspaceContext();
  const sidebar = useSidebarState(
    workspace.ui.sidebarOpen,
    workspace.closeSidebar,
    workspace.projects,
    workspace.chats
  );
  const commands = useWorkspaceCommands();
  const model = useMemo(
    () => workspaceSidebarState.model(workspace.chats, workspace.projects, sidebar.query),
    [workspace.chats, workspace.projects, sidebar.query]
  );

  /**
   * Clicking a project row resumes it: its newest chat, or a fresh one when
   * the project has none. The gear beside it still goes to the settings page.
   */
  function openProject(projectId: string) {
    const chatId = workspaceSidebarState.mostRecentChatId(workspace.chats, projectId);
    if (chatId) workspace.selectChat(chatId);
    else void commands.newChatInProject(projectId);
  }

  return (
    <Sidebar
      open={workspace.ui.sidebarOpen}
      model={model}
      health={workspace.health}
      query={sidebar.query}
      collapsed={sidebar.collapsed}
      recentOpen={sidebar.recentOpen}
      sidebarCollapsed={sidebar.sidebarCollapsed}
      activeChatId={workspace.ui.activeChatId}
      account={{
        email: auth.email,
        authenticated: auth.authenticated,
      }}
      onClose={workspace.closeSidebar}
      onQueryChange={sidebar.setQuery}
      onClearQuery={() => sidebar.setQuery("")}
      onToggleSidebar={sidebar.toggleSidebarCollapsed}
      onToggleRecent={sidebar.toggleRecent}
      onNewProject={commands.newProject}
      onNewChatInProject={commands.newChatInProject}
      onToggleProject={sidebar.toggleCollapsed}
      onOpenProject={openProject}
      onSelectChat={workspace.selectChat}
      onDeleteChat={commands.deleteChat}
      onToggleChatUnread={commands.toggleChatUnread}
      onForkChat={commands.forkChat}
      onReorderProjects={commands.reorderProjects}
      onOpenProjectContainers={workspace.showProjectContainers}
      onOpenSettings={workspace.showSettings}
    />
  );
}
