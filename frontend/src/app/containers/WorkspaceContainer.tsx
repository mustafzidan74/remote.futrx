import { useMemo } from "preact/hooks";
import { AppShell } from "../../ui/layout/AppShell";
import { NoChatSelected } from "../../ui/layout/NoChatSelected";
import { NewProjectDialog } from "../../ui/projects/NewProjectDialog";
import { CommandPalette } from "../../ui/app/CommandPalette";
import { ShortcutsOverlay } from "../../ui/app/ShortcutsOverlay";
import { SETTINGS_TABS } from "../../ui/settings/SettingsPage";
import { buildCommandItems } from "../../state/app/commandPaletteState";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useUserSettingsContext } from "../../state/context/UserSettingsContext";
import { useWorkspaceCommands } from "../../state/hooks/workspace/useWorkspaceCommands";
import { ChatContainer } from "./ChatContainer";
import { ProjectContainersContainer } from "./ProjectContainersContainer";
import { SettingsContainer } from "./SettingsContainer";
import { SidebarContainer } from "./SidebarContainer";

export function WorkspaceContainer() {
  const workspace = useWorkspaceContext();
  const commands = useWorkspaceCommands();
  const userSettings = useUserSettingsContext();

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
        ) : workspace.activeChat ? (
          <ChatContainer
            key={workspace.activeChat.id}
            chat={workspace.activeChat}
            projects={workspace.projects}
            highlightAt={workspace.highlightAt}
            onHamburger={workspace.openSidebar}
            onSelectChat={workspace.selectChat}
          />
        ) : (
          <NoChatSelected
            hasProjects={workspace.projects.length > 0}
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
