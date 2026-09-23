import { useCallback, useEffect, useState } from "preact/hooks";
import {
  SettingsPage,
  isSettingsTab,
  type SettingsTab,
} from "../../ui/settings/SettingsPage";
import { useAuthContext } from "../../state/context/AuthContext";
import { useUserSettingsContext } from "../../state/context/UserSettingsContext";
import { useAntigravityAuth } from "../../state/hooks/auth/useAntigravityAuth";
import { useUserDirectory } from "../../state/hooks/users/useUserDirectory";
import { useGlobalSkills } from "../../state/hooks/settings/useGlobalSkills";
import { usePlaybookLibrary } from "../../state/hooks/settings/usePlaybookLibrary";
import { useAgentPreferences } from "../../state/hooks/settings/useAgentPreferences";
import { useSecretsVault } from "../../state/hooks/settings/useSecretsVault";
import { useMCPServers } from "../../state/hooks/settings/useMCPServers";
import { useAgentEndpoints } from "../../state/hooks/settings/useAgentEndpoints";
import { useWorkspaceContext } from "../../state/context/WorkspaceContext";
import { useAuditLog } from "../../state/hooks/admin/useAuditLog";
import { useProjectTrash } from "../../state/hooks/admin/useProjectTrash";
import { useSecuritySettings } from "../../state/hooks/auth/useSecuritySettings";
import { useServerInfo } from "../../state/hooks/server/useServerInfo";
import { useSelfUpdate } from "../../state/hooks/server/useSelfUpdate";
import { usePushNotifications } from "../../state/hooks/push/usePushNotifications";
import { useUsageDashboard } from "../../state/hooks/usage/useUsageDashboard";
import { usageApi } from "../../api/usageApi";
import { useFleetResources } from "../../state/hooks/server/useFleetResources";
import { useModelRouting } from "../../state/hooks/settings/useModelRouting";

export function SettingsContainer({
  onBack,
  onHamburger,
}: {
  onBack: () => void;
  onHamburger: () => void;
}) {
  const { auth } = useAuthContext();
  const userSettings = useUserSettingsContext();
  const userDirectory = useUserDirectory(auth.isAdmin);
  const { projects, ui } = useWorkspaceContext();
  const [activeTab, setActiveTab] = useState<SettingsTab>("appearance");

  // The command palette can name the page it wants. Anything else — the
  // sidebar gear, the account footer — leaves the request null and lands
  // wherever the operator last was.
  const requestedTab = ui.settingsTab;
  useEffect(() => {
    if (isSettingsTab(requestedTab)) setActiveTab(requestedTab);
  }, [requestedTab]);
  const globalSkills = useGlobalSkills(activeTab === "skills" && auth.isAdmin);
  const playbooks = usePlaybookLibrary(activeTab === "playbooks" && auth.isAdmin);
  const agentPreferences = useAgentPreferences(
    activeTab === "reply-preferences" && auth.isAdmin
  );
  const modelRouting = useModelRouting(activeTab === "model-routing" && auth.isAdmin);
  // The vault is also read on the MCP tab: an MCP entry references a vault
  // key, so the secret-ref picker needs the same list the vault screen shows.
  // An agent endpoint references a vault key the same way an MCP entry does,
  // so its editor needs the same list.
  const secretsVault = useSecretsVault(
    (activeTab === "secrets" || activeTab === "mcp" || activeTab === "agent-endpoints") &&
      auth.isAdmin,
  );
  const mcpServers = useMCPServers(activeTab === "mcp" && auth.isAdmin);
  const agentEndpoints = useAgentEndpoints(activeTab === "agent-endpoints" && auth.isAdmin);
  const serverInfo = useServerInfo(activeTab === "info");
  const selfUpdate = useSelfUpdate(activeTab === "updates" && auth.isAdmin);
  const security = useSecuritySettings(activeTab === "security");
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
  const push = usePushNotifications(
    activeTab === "notifications",
    auth.email || auth.adminEmail
  );

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
      agentPreferences={agentPreferences}
      secretsVault={secretsVault}
      modelRouting={modelRouting}
      mcpServers={mcpServers}
      agentEndpoints={agentEndpoints}
      projects={projects}
      usageDashboard={usageDashboard}
      usageRebuilding={usageRebuilding}
      usageRebuildMessage={usageRebuildMessage}
      onRebuildUsage={rebuildUsage}
      auditLog={auditLog}
      projectTrash={projectTrash}
      appearanceTheme={userSettings.settings.appearance.theme}
      appearanceLanguage={userSettings.settings.appearance.language}
      appearanceReplyLanguage={userSettings.settings.agent.replyLanguage}
      appearanceLoading={userSettings.loading}
      appearanceSaving={userSettings.saving}
      appearanceError={userSettings.error}
      push={push}
      onBack={onBack}
      onHamburger={onHamburger}
      onTabChange={setActiveTab}
      onRefreshServerInfo={serverInfo.refresh}
      onCheckForUpdates={selfUpdate.check}
      onApplyUpdate={selfUpdate.apply}
      onAppearanceThemeChange={(theme) => void userSettings.setTheme(theme)}
      onAppearanceLanguageChange={(language) => void userSettings.setLanguage(language)}
      onReplyLanguageChange={(language) => void userSettings.setReplyLanguage(language)}
      security={security}
    />
  );
}
