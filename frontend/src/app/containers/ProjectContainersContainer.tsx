import { useCallback, useEffect, useMemo, useState } from "preact/hooks";
import {
  ProjectContainersPage,
  isProjectSettingsTab,
  type ProjectSettingsTab,
} from "../../ui/projects/ProjectContainersPage";
import type { ProjectMeta } from "../../models/project";
import { useProjectContainersController } from "../../state/hooks/projects/useProjectContainersController";
import { useProjectUsage } from "../../state/hooks/usage/useProjectUsage";
import { useProjectResources } from "../../state/hooks/projects/useProjectResources";
import { useProjectMCP } from "../../state/hooks/projects/useProjectMCP";
import { useProjectLighthouse } from "../../state/hooks/projects/useProjectLighthouse";
import { useProjectPreviewLinks } from "../../state/hooks/projects/useProjectPreviewLinks";
import { useProjectVisual } from "../../state/hooks/projects/useProjectVisual";
import type { ProjectHealthMap } from "../../state/workspace/projectHealthState";
import { useAuthContext } from "../../state/context/AuthContext";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";

export function ProjectContainersContainer({
  projects,
  health,
  selectedProjectId,
  requestedTab,
  onBack,
  onHamburger,
  onDeleteProject,
}: {
  projects: ProjectMeta[];
  health: ProjectHealthMap;
  selectedProjectId: string | null;
  /** Tab the caller asked for (the command palette does), or null for the last one. */
  requestedTab: string | null;
  onBack: () => void;
  onHamburger: () => void;
  onDeleteProject: (projectId: string) => Promise<void>;
}) {
  const [activeTab, setActiveTab] = useState<ProjectSettingsTab>("info");
  // The GitHub tab imports a pull request's review comments into a chat, so it
  // needs the chat list and a way to open the one it landed in. Both already
  // live in the workspace context; passing them down through every caller of
  // this container would buy nothing.
  const { chats, selectChat } = useWorkspaceContext();
  const { auth } = useAuthContext();

  useEffect(() => {
    if (isProjectSettingsTab(requestedTab)) setActiveTab(requestedTab);
  }, [requestedTab, selectedProjectId]);

  const controller = useProjectContainersController(
    projects,
    selectedProjectId,
    activeTab === "snapshots",
    activeTab === "github",
  );
  const { selectedProject, info, secrets, access, shares, snapshots, portal } = controller;
  const usage = useProjectUsage(selectedProject?.id);
  const resources = useProjectResources(selectedProject, activeTab === "settings");
  // The MCP panel lives on the Settings tab and is only read while it is open:
  // the list is cheap, but there is no reason to fetch it behind another tab.
  const mcp = useProjectMCP(selectedProject, activeTab === "settings");
  // Both the Visual and Lighthouse tabs point a browser at one of the
  // project's own listening ports, so they share one scan rather than running
  // two identical ones. It is scoped to those tabs: the scan runs `ss` inside
  // the container, which is not worth doing behind a tab nobody opened.
  const browserTab = activeTab === "visual" || activeTab === "lighthouse";
  const previewPorts = useProjectPreviewLinks({
    project: browserTab ? selectedProject : null,
    enabled: browserTab,
    polling: false,
  });
  const previewablePorts = useMemo(
    () => previewPorts.rows.filter((row) => row.shareable).map((row) => row.port),
    [previewPorts.rows],
  );
  const visual = useProjectVisual(selectedProject?.id ?? "");
  const lighthouse = useProjectLighthouse(selectedProject?.id ?? "", activeTab === "lighthouse");

  const githubChats = useMemo(
    () =>
      chats
        .filter((chat) => chat.projectId === selectedProjectId)
        .map((chat) => ({ id: chat.id, title: chat.title || "Untitled chat" })),
    [chats, selectedProjectId],
  );

  const deleteSelectedProject = useCallback(async () => {
    if (!selectedProject) return;
    await onDeleteProject(selectedProject.id);
    onBack();
  }, [selectedProject, onBack, onDeleteProject]);

  return (
    <ProjectContainersPage
      activeTab={activeTab}
      project={selectedProject}
      health={selectedProject ? health[selectedProject.id] : undefined}
      infoRecord={info.record}
      secretsRecord={secrets.record}
      accessRecord={access.record}
      sharesRecord={shares.record}
      snapshotsRecord={snapshots.record}
      snapshotsRunning={snapshots.running}
      lighthouse={lighthouse}
      lighthousePorts={previewablePorts}
      visual={visual}
      visualPorts={previewablePorts}
      portalRecord={portal.record}
      portalIssuedUrl={portal.issuedUrl}
      github={controller.github}
      githubChats={githubChats}
      mcp={mcp}
      isAdmin={auth.isAdmin}
      onOpenChat={selectChat}
      refreshing={controller.refreshing}
      usageSummary={usage.summary}
      usageLoading={usage.loading}
      usageError={usage.error}
      resources={resources.data}
      resourcesLoading={resources.loading}
      resourcesSaving={resources.saving}
      resourcesError={resources.error}
      onRefresh={() => void controller.refresh()}
      onBack={onBack}
      onHamburger={onHamburger}
      onTabChange={setActiveTab}
      onSaveSecret={secrets.save}
      onDeleteSecret={secrets.remove}
      onAddMember={access.add}
      onRemoveMember={access.remove}
      onCreateShare={shares.create}
      onRevokeShare={shares.revoke}
      onCreateSnapshot={snapshots.create}
      onRestoreSnapshot={snapshots.restore}
      onDeleteSnapshot={snapshots.remove}
      onSavePortal={portal.save}
      onDismissPortalUrl={portal.dismissIssuedUrl}
      onRepairNetwork={info.repairNetwork}
      onSetResourceLimits={resources.save}
      onStartProject={info.start}
      onStopProject={info.stop}
      onRestartProject={info.restart}
      onDeleteProject={deleteSelectedProject}
    />
  );
}
