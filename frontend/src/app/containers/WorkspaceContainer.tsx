import { useMemo } from "preact/hooks";
import { AppShell } from "../../ui/layout/AppShell";
import { NoChatSelected } from "../../ui/layout/NoChatSelected";
import { Dashboard } from "../../ui/home/Dashboard";
import { NewProjectDialog } from "../../ui/projects/NewProjectDialog";
import { CommandPalette } from "../../ui/app/CommandPalette";
import { ShortcutsOverlay } from "../../ui/app/ShortcutsOverlay";
import { SETTINGS_TABS } from "../../ui/settings/SettingsPage";
import { buildCommandItems } from "../../state/app/commandPaletteState";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useUserSettingsContext } from "../../state/context/UserSettingsContext";
import { useWorkspaceCommands } from "../../state/hooks/workspace/useWorkspaceCommands";
import { useDashboard } from "../../state/hooks/home/useDashboard";
import { chatScheduleApi } from "../../api/chat/chatScheduleApi";
import { ChatContainer } from "./ChatContainer";
import { ProjectContainersContainer } from "./ProjectContainersContainer";
import { SettingsContainer } from "./SettingsContainer";
import { SidebarContainer } from "./SidebarContainer";

export function WorkspaceContainer() {
  const workspace = useWorkspaceContext();
  const commands = useWorkspaceCommands();
  const userSettings = useUserSettingsContext();

  // The dashboard is the landing view when no chat is selected, and a
  // destination of its own from the sidebar, the app title and the palette.
  // A workspace with no projects keeps the first-run welcome card instead:
  // there is nothing yet for a dashboard to describe.
  const hasProjects = workspace.projects.length > 0;
  const showDashboard =
    workspace.ui.view === "home" ||
    (workspace.ui.view === "chat" && !workspace.activeChat && hasProjects);
  const dashboard = useDashboard(showDashboard);
  // `refresh` is stable across renders; depending on the whole hook result
  // would rebuild the handlers on every sixty-second poll.
  const refreshDashboard = dashboard.refresh;
  const dashboardHandlers = useMemo(
    () => ({
      openChat: workspace.selectChat,
      openProject: (projectId: string, tab?: string) =>
        workspace.showProjectContainers(projectId, tab),
      openSettings: (tabId: string) => workspace.showSettings(tabId),
      newProject: commands.newProject,
      runTaskNow: async (taskId: string) => {
        await chatScheduleApi.runSchedule(taskId);
        await refreshDashboard();
      },
      onHamburger: workspace.openSidebar,
    }),
    [workspace, commands, refreshDashboard],
  );

  // The palette is the one place that reaches every destination, so it is
  // assembled here — the only component that already holds the workspace data,
  // the settings catalog, and the theme.
  const theme = userSettings.settings.appearance.theme;
  const nextTheme = theme === "light" ? "dark" : "light";
  const commandItems = useMemo(
    () =>
      buildCommandItems(
        {
          projects: workspace.projects,
          chats: workspace.chats,
          settingsTabs: SETTINGS_TABS.map((tab) => ({
            id: tab.id,
            label: tab.label,
            description: tab.description,
          })),
          nextThemeName: nextTheme,
        },
        {
          openChat: workspace.selectChat,
          newProject: commands.newProject,
          newChat: (projectId) => void commands.newChatInProject(projectId),
          openProject: (projectId) => workspace.showProjectContainers(projectId, "info"),
          openProjectPreview: (projectId) =>
            workspace.showProjectContainers(projectId, "sharing"),
          snapshotProject: (projectId) =>
            workspace.showProjectContainers(projectId, "snapshots"),
          openSettings: (tabId) => workspace.showSettings(tabId),
          openHome: workspace.showHome,
          toggleTheme: () => void userSettings.setTheme(nextTheme),
        },
      ),
    [workspace, commands, nextTheme, userSettings],
  );

  return (
    <>
      <AppShell sidebar={<SidebarContainer />}>
        {workspace.ui.view === "settings" ? (
          <SettingsContainer
            onBack={workspace.showChat}
            onHamburger={workspace.openSidebar}
          />
        ) : workspace.ui.view === "project-containers" ? (
          <ProjectContainersContainer
            projects={workspace.projects}
            health={workspace.health}
            selectedProjectId={workspace.ui.containerProjectId}
            requestedTab={workspace.ui.containerTab}
            onBack={workspace.showChat}
            onHamburger={workspace.openSidebar}
            onDeleteProject={workspace.deleteProject}
          />
        ) : showDashboard ? (
          <Dashboard data={dashboard} handlers={dashboardHandlers} />
        ) : workspace.activeChat ? (
          <ChatContainer
            key={workspace.activeChat.id}
            chat={workspace.activeChat}
            projects={workspace.projects}
            highlightAt={workspace.highlightAt}
            onHamburger={workspace.openSidebar}
          />
        ) : (
          <NoChatSelected
            hasProjects={hasProjects}
            onNewProject={commands.newProject}
            onNewChat={() => commands.newChatInProject(undefined)}
            onOpenAgentSettings={() => workspace.showSettings("agents")}
            onHamburger={workspace.openSidebar}
          />
        )}
      </AppShell>
      <NewProjectDialog
        state={workspace.newProject}
        onNameChange={workspace.setNewProjectName}
        onSelectTemplate={workspace.selectNewProjectTemplate}
        onInputChange={workspace.setNewProjectInput}
        onSubmit={workspace.submitNewProject}
        onClose={workspace.closeNewProject}
      />
      <CommandPalette items={commandItems} />
      <ShortcutsOverlay />
    </>
  );
}
