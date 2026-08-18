import { useCallback, useState } from "preact/hooks";
import {
  ProjectContainersPage,
  type ProjectSettingsTab,
} from "../../ui/projects/ProjectContainersPage";
import type { ProjectMeta } from "../../models/project";
import { useProjectContainersController } from "../../state/hooks/projects/useProjectContainersController";
import { useProjectUsage } from "../../state/hooks/usage/useProjectUsage";
import { useProjectResources } from "../../state/hooks/projects/useProjectResources";

export function ProjectContainersContainer({
  projects,
  selectedProjectId,
  onBack,
  onHamburger,
  onDeleteProject,
}: {
  projects: ProjectMeta[];
  selectedProjectId: string | null;
  onBack: () => void;
  onHamburger: () => void;
  onDeleteProject: (projectId: string) => Promise<void>;
}) {
  const [activeTab, setActiveTab] = useState<ProjectSettingsTab>("info");
  const controller = useProjectContainersController(
    projects,
    selectedProjectId,
    activeTab === "snapshots"
  );
  const { selectedProject, info, secrets, access, shares, snapshots } = controller;
  const usage = useProjectUsage(selectedProject?.id);
  const resources = useProjectResources(selectedProject, activeTab === "settings");

  const deleteSelectedProject = useCallback(async () => {
    if (!selectedProject) return;
    await onDeleteProject(selectedProject.id);
    onBack();
  }, [selectedProject, onBack, onDeleteProject]);

  return (
    <ProjectContainersPage
      activeTab={activeTab}
      project={selectedProject}
      infoRecord={info.record}
      secretsRecord={secrets.record}
      accessRecord={access.record}
      sharesRecord={shares.record}
      snapshotsRecord={snapshots.record}
      snapshotsRunning={snapshots.running}
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
      onRepairNetwork={info.repairNetwork}
      onSetResourceLimits={resources.save}
      onStartProject={info.start}
      onStopProject={info.stop}
      onRestartProject={info.restart}
      onDeleteProject={deleteSelectedProject}
    />
  );
}
