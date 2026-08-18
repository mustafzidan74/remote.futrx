import { useCallback, useState } from "preact/hooks";
import {
  SettingsPage,
  type SettingsTab,
} from "../../ui/settings/SettingsPage";
import { useAuthContext } from "../../state/context/AuthContext";
import { useUserSettingsContext } from "../../state/context/UserSettingsContext";
import { useUserDirectory } from "../../state/hooks/users/useUserDirectory";
import { useGlobalSkills } from "../../state/hooks/settings/useGlobalSkills";
import { usePlaybookLibrary } from "../../state/hooks/settings/usePlaybookLibrary";
import { useSecretsVault } from "../../state/hooks/settings/useSecretsVault";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useAuditLog } from "../../state/hooks/admin/useAuditLog";
import { useProjectTrash } from "../../state/hooks/admin/useProjectTrash";
import { useServerInfo } from "../../state/hooks/server/useServerInfo";
import { useSelfUpdate } from "../../state/hooks/server/useSelfUpdate";
import { useUsageDashboard } from "../../state/hooks/usage/useUsageDashboard";
import { usageApi } from "../../api/usageApi";
import { useFleetResources } from "../../state/hooks/server/useFleetResources";

export function SettingsContainer({
  onBack,
  onHamburger,
}: {
  onBack: () => void;
  onHamburger: () => void;
}) {
  const { auth, codexAuth, kimiAuth } = useAuthContext();
  const userSettings = useUserSettingsContext();
  const userDirectory = useUserDirectory(auth.isAdmin);
  const { projects } = useWorkspaceContext();
  const [activeTab, setActiveTab] = useState<SettingsTab>("appearance");
  const globalSkills = useGlobalSkills(activeTab === "skills" && auth.isAdmin);
  const playbooks = usePlaybookLibrary(activeTab === "playbooks" && auth.isAdmin);
  const secretsVault = useSecretsVault(activeTab === "secrets" && auth.isAdmin);
  const serverInfo = useServerInfo(activeTab === "info");
  const selfUpdate = useSelfUpdate(activeTab === "updates" && auth.isAdmin);
  const usageDashboard = useUsageDashboard(activeTab === "usage");
  const [usageRebuilding, setUsageRebuilding] = useState(false);
  const [usageRebuildMessage, setUsageRebuildMessage] = useState<string | null>(null);

  const rebuildUsage = useCallback(async () => {
    setUsageRebuilding(true);
    setUsageRebuildMessage(null);
    try {
      const result = await usageApi.rebuild();
      setUsageRebuildMessage(
        `Rebuilt ${result.records} record${result.records === 1 ? "" : "s"} from ${result.chats} chat${
          result.chats === 1 ? "" : "s"
        }.`
      );
      await usageDashboard.refresh();
    } catch (cause) {
      setUsageRebuildMessage(`Rebuild failed: ${(cause as Error).message}`);
    } finally {
      setUsageRebuilding(false);
    }
  }, [usageDashboard]);
  const fleetResources = useFleetResources(activeTab === "resources" && auth.isAdmin);
  const auditLog = useAuditLog(activeTab === "audit" && auth.isAdmin);
  // Members see their own trashed projects here too, so this is not gated on
  // admin: the backend already scopes the listing to the caller.
  const projectTrash = useProjectTrash(activeTab === "trash");

  return (
    <SettingsPage
      activeTab={activeTab}
      currentEmail={auth.email}
      isAdmin={auth.isAdmin}
      googleOAuthEnabled={auth.googleOAuthEnabled}
      serverInfo={serverInfo.info}
      serverInfoLoading={serverInfo.loading}
      serverInfoRefreshing={serverInfo.refreshing}
      serverInfoError={serverInfo.error}
      fleetResources={fleetResources.view}
      fleetResourcesLoading={fleetResources.loading}
      fleetResourcesSaving={fleetResources.saving}
      fleetResourcesError={fleetResources.error}
      onSaveFleetResources={fleetResources.save}
      selfUpdate={selfUpdate.status}
      selfUpdateLoading={selfUpdate.loading}
      selfUpdateChecking={selfUpdate.checking}
      selfUpdateApplying={selfUpdate.applying}
      selfUpdateRestarting={selfUpdate.restarting}
      selfUpdateError={selfUpdate.error}
      userDirectory={userDirectory}
      globalSkills={globalSkills}
      playbooks={playbooks}
      secretsVault={secretsVault}
      projects={projects}
      usageDashboard={usageDashboard}
      usageRebuilding={usageRebuilding}
      usageRebuildMessage={usageRebuildMessage}
      onRebuildUsage={rebuildUsage}
      auditLog={auditLog}
      projectTrash={projectTrash}
      appearanceTheme={userSettings.settings.appearance.theme}
      appearanceLoading={userSettings.loading}
      appearanceSaving={userSettings.saving}
      appearanceError={userSettings.error}
      codexAuthenticated={codexAuth.authenticated}
      codexUsesApiKey={codexAuth.usesApiKey}
      codexDeviceLogin={codexAuth.deviceLogin}
      codexLoading={codexAuth.loading}
      codexStarting={codexAuth.starting}
      codexError={codexAuth.error}
      onBack={onBack}
      onHamburger={onHamburger}
      onTabChange={setActiveTab}
      onRefreshServerInfo={serverInfo.refresh}
      onCheckForUpdates={selfUpdate.check}
      onApplyUpdate={selfUpdate.apply}
      onAppearanceThemeChange={(theme) => void userSettings.setTheme(theme)}
      onStartCodexDeviceLogin={codexAuth.startDeviceLogin}
      kimiAuthenticated={kimiAuth.authenticated}
      kimiDeviceLogin={kimiAuth.deviceLogin}
      kimiLoading={kimiAuth.loading}
      kimiStarting={kimiAuth.starting}
      kimiError={kimiAuth.error}
      onStartKimiDeviceLogin={kimiAuth.startDeviceLogin}
    />
  );
}
