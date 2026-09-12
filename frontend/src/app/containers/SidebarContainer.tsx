import { useMemo } from "preact/hooks";
import { Sidebar } from "../../ui/sidebar/Sidebar";
import { useAuthContext } from "../../state/context/AuthContext";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useSidebarState } from "../../state/hooks/workspace/useSidebarState";
import { useMessageSearch } from "../../state/hooks/workspace/useMessageSearch";
import { useOpenCommandPalette } from "../../state/hooks/workspace/useCommandPalette";
import { useSidebarSearch } from "../../state/hooks/workspace/useWorkspaceSearch";
import { useWorkspaceCommands } from "../../state/hooks/workspace/useWorkspaceCommands";
import { workspaceSidebarService } from "../../services/workspace/workspaceSidebarService.ts";
import { useAccountSignOut } from "../../state/hooks/auth/useAccountSignOut";

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
  const signOut = useAccountSignOut();
  const search = useSidebarSearch();
  const openPalette = useOpenCommandPalette();
  const messageSearch = useMessageSearch(search.query);
  const model = useMemo(
    () => workspaceSidebarService.model(workspace.chats, workspace.projects),
    [workspace.chats, workspace.projects]
  );

  /**
   * Clicking a project row resumes it: its newest chat, or a fresh one when
   * the project has none. The gear beside it still goes to the settings page.
   */
  function openProject(projectId: string) {
    const chatId = workspaceSidebarService.mostRecentChatId(workspace.chats, projectId);
    if (chatId) workspace.selectChat(chatId);
    else void commands.newChatInProject(projectId);
  }

  return (
    <Sidebar
      open={workspace.ui.sidebarOpen}
      model={model}
      health={workspace.health}
      messageSearch={messageSearch}
      search={search}
      loading={!workspace.loaded}
      collapsed={sidebar.collapsed}
      recentOpen={sidebar.recentOpen}
      sidebarCollapsed={sidebar.sidebarCollapsed}
      activeChatId={workspace.ui.activeChatId}
      account={{
        email: auth.email,
        authenticated: auth.authenticated,
      }}
      onClose={workspace.closeSidebar}
      onOpenSearchResult={(result) => {
        workspace.selectChatAt(result.chatId, result.at);
        workspace.closeSidebar();
      }}
      onOpenPalette={openPalette}
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
      onOpenHome={workspace.showHome}
      homeActive={workspace.ui.view === "home"}
      onSignOut={signOut}
    />
  );
}
