import type { AppearanceTheme } from "../../models/settings";
import type { CodexDeviceLogin, KimiDeviceLogin } from "../../models/auth";
import type { UserDirectory } from "../../state/hooks/users/useUserDirectory";
import type { GlobalSkillLibrary } from "../../state/hooks/settings/useGlobalSkills";
import type { PlaybookLibraryEditor } from "../../state/hooks/settings/usePlaybookLibrary";
import type { SecretsVault } from "../../state/hooks/settings/useSecretsVault";
import type { ProjectMeta } from "../../models/project";
import type { AuditLog } from "../../state/hooks/admin/useAuditLog";
import type { ProjectTrash } from "../../state/hooks/admin/useProjectTrash";
import type { ServerInfo } from "../../models/serverInfo";
import type { FleetResourcesView, FleetSettings } from "../../models/resources";
import type { SelfUpdateStatus } from "../../models/selfUpdate";
import type { ComponentType } from "preact";
import { Activity, Bell, Bot, ChevronLeft, Code, Cpu, Download, Info, Key, Menu, Mic, Monitor, Trash, Users, Zap } from "../primitives/icons";
import { AppearanceSettings } from "./AppearanceSettings";
import { AuditLogSettings } from "./AuditLogSettings";
import { ClaudeAuthSettings } from "./ClaudeAuthSettings";
import { CodexAuthSettings } from "./CodexAuthSettings";
import { KimiAuthSettings } from "./KimiAuthSettings";
import { GoogleOAuthSettings } from "./GoogleOAuthSettings";
import { NotificationsSettings } from "./NotificationsSettings";
import { ResourcesSettings } from "./ResourcesSettings";
import { SecretsVaultSettings } from "./SecretsVaultSettings";
import { ServerInfoSettings } from "./ServerInfoSettings";
import { TrashSettings } from "./TrashSettings";
import { UpdatesSettings } from "./UpdatesSettings";
import { GlobalSkillsSettings } from "./GlobalSkillsSettings";
import { PlaybooksSettings } from "./PlaybooksSettings";
import { VoiceInputSettings } from "./VoiceInputSettings";
import { UsageSettings } from "./UsageSettings";
import { UsersPanel } from "../account/UsersPanel";
import type { UsageDashboard } from "../../state/hooks/usage/useUsageDashboard";

export type SettingsTab =
  | "appearance"
  | "agents"
  | "users"
  | "notifications"
  | "skills"
  | "playbooks"
  | "voice"
  | "usage"
  | "resources"
  | "secrets"
  | "trash"
  | "audit"
  | "updates"
  | "info";

type SettingsTabGroup = "personal" | "agents" | "insights" | "platform" | "system";

/**
 * A flat column of destinations reads as a wall. The ids are the
 * routing contract and stay exactly as they are; only the rendering groups
 * them, so the operator scans five short lists instead of one long one.
 */
const GROUP_LABELS: Array<{ id: SettingsTabGroup; label: string }> = [
  { id: "personal", label: "Personal" },
  { id: "agents", label: "Agents & skills" },
  { id: "insights", label: "Insights" },
  { id: "platform", label: "Platform" },
  { id: "system", label: "System" },
];

const tabs: Array<{
  id: SettingsTab;
  group: SettingsTabGroup;
  label: string;
  description: string;
  Icon: ComponentType<{ class?: string }>;
}> = [
  {
    id: "appearance",
    group: "personal",
    label: "Appearance",
    description: "Choose how Remote looks on this device.",
    Icon: Monitor,
  },
  {
    id: "agents",
    group: "agents",
    label: "Agents",
    description: "Manage host authentication for coding agents.",
    Icon: Bot,
  },
  {
    id: "skills",
    group: "agents",
    label: "Global skills",
    description: "Publish skills that every project can select.",
    Icon: Code,
  },
  {
    id: "playbooks",
    group: "agents",
    label: "Playbooks",
    description: "Curate the one-click prompt templates every chat composer offers.",
    Icon: Zap,
  },
  {
    id: "voice",
    group: "agents",
    label: "Voice input",
    description: "Configure the optional server-side speech-to-text fallback for dictation.",
    Icon: Mic,
  },
  {
    id: "usage",
    group: "insights",
    label: "Usage",
    description: "Track tokens and estimated cost per project, user, provider, and model.",
    Icon: Activity,
  },
  {
    id: "users",
    group: "platform",
    label: "Users",
    description: "Control who can access this server.",
    Icon: Users,
  },
  {
    id: "notifications",
    group: "platform",
    label: "Notifications",
    description: "Get pinged when an agent finishes, fails, or needs you.",
    Icon: Bell,
  },
  {
    id: "resources",
    group: "platform",
    label: "Resources",
    description: "Set the CPU, memory, and disk envelope every project container inherits.",
    Icon: Cpu,
  },
  {
    id: "secrets",
    group: "platform",
    label: "Secrets vault",
    description: "Store tokens, licence keys, credential files, and SSH targets once for every project.",
    Icon: Key,
  },
  {
    id: "trash",
    group: "platform",
    label: "Trash",
    description: "Restore a deleted project, or purge it before its retention window closes.",
    Icon: Trash,
  },
  {
    id: "audit",
    group: "insights",
    label: "Audit log",
    description: "Review who did what on this server.",
    Icon: Activity,
  },
  {
    id: "updates",
    group: "system",
    label: "Updates",
    description: "Check for new releases and install them.",
    Icon: Download,
  },
  {
    id: "info",
    group: "system",
    label: "Info",
    description: "View details about the main parent server.",
    Icon: Info,
  },
];

export function SettingsPage({
  activeTab,
  currentEmail,
  isAdmin,
  googleOAuthEnabled,
  serverInfo,
  serverInfoLoading,
  serverInfoRefreshing,
  serverInfoError,
  fleetResources,
  fleetResourcesLoading,
  fleetResourcesSaving,
  fleetResourcesError,
  onSaveFleetResources,
  selfUpdate,
  selfUpdateLoading,
  selfUpdateChecking,
  selfUpdateApplying,
  selfUpdateRestarting,
  selfUpdateError,
  userDirectory,
  globalSkills,
  playbooks,
  secretsVault,
  projects,
  usageDashboard,
  usageRebuilding,
  usageRebuildMessage,
  onRebuildUsage,
  auditLog,
  projectTrash,
  appearanceTheme,
  appearanceLoading,
  appearanceSaving,
  appearanceError,
  codexAuthenticated,
  codexUsesApiKey,
  codexDeviceLogin,
  codexLoading,
  codexStarting,
  codexError,
  kimiAuthenticated,
  kimiDeviceLogin,
  kimiLoading,
  kimiStarting,
  kimiError,
  onBack,
  onHamburger,
  onTabChange,
  onRefreshServerInfo,
  onCheckForUpdates,
  onApplyUpdate,
  onAppearanceThemeChange,
  onStartCodexDeviceLogin,
  onStartKimiDeviceLogin,
}: {
  activeTab: SettingsTab;
  currentEmail: string;
  isAdmin: boolean;
  googleOAuthEnabled: boolean;
  serverInfo: ServerInfo | null;
  serverInfoLoading: boolean;
  serverInfoRefreshing: boolean;
  serverInfoError: string | null;
  fleetResources: FleetResourcesView | null;
  fleetResourcesLoading: boolean;
  fleetResourcesSaving: boolean;
  fleetResourcesError: string | null;
  onSaveFleetResources: (settings: Partial<FleetSettings>) => Promise<void>;
  selfUpdate: SelfUpdateStatus | null;
  selfUpdateLoading: boolean;
  selfUpdateChecking: boolean;
  selfUpdateApplying: boolean;
  selfUpdateRestarting: boolean;
  selfUpdateError: string | null;
  userDirectory: UserDirectory;
  globalSkills: GlobalSkillLibrary;
  playbooks: PlaybookLibraryEditor;
  secretsVault: SecretsVault;
  projects: ProjectMeta[];
  usageDashboard: UsageDashboard;
  usageRebuilding: boolean;
  usageRebuildMessage: string | null;
  onRebuildUsage: () => Promise<void>;
  auditLog: AuditLog;
  projectTrash: ProjectTrash;
  appearanceTheme: AppearanceTheme;
  appearanceLoading: boolean;
  appearanceSaving: boolean;
  appearanceError: string | null;
  codexAuthenticated: boolean;
  codexUsesApiKey: boolean;
  codexDeviceLogin?: CodexDeviceLogin;
  codexLoading: boolean;
  codexStarting: boolean;
  codexError: string | null;
  kimiAuthenticated: boolean;
  kimiDeviceLogin?: KimiDeviceLogin;
  kimiLoading: boolean;
  kimiStarting: boolean;
  kimiError: string | null;
  onBack: () => void;
  onHamburger: () => void;
  onTabChange: (tab: SettingsTab) => void;
  onRefreshServerInfo: () => Promise<void>;
  onCheckForUpdates: () => Promise<void>;
  onApplyUpdate: (tag?: string) => Promise<void>;
  onAppearanceThemeChange: (theme: AppearanceTheme) => void;
  onStartCodexDeviceLogin: () => Promise<void>;
  onStartKimiDeviceLogin: () => Promise<void>;
}) {
  const activeTabDetails = tabs.find((tab) => tab.id === activeTab) ?? tabs[0];

  return (
    <div class="flex-1 flex flex-col min-h-0 overflow-hidden">
      <header class="codex-header top-chrome flex-none z-20 bg-[#101318] border-b border-white/10 px-3 pb-2 flex items-center gap-2 min-h-[52px]">
        <button
          type="button"
          onClick={onHamburger}
          class="md:hidden h-10 w-10 text-ink-100 rounded-md hover:bg-white/[0.08] grid place-items-center"
          aria-label="Toggle sidebar"
        >
          <Menu class="w-5 h-5" />
        </button>
        <button
          type="button"
          onClick={onBack}
          class="hidden md:inline-flex items-center gap-1.5 h-10 px-2 text-ink-200 hover:text-ink-50
                 hover:bg-white/[0.08] rounded-md text-sm"
        >
          <ChevronLeft class="w-4 h-4" /> Chats
        </button>
        <div class="flex-1 min-w-0">
          <div class="text-[11px] text-ink-300">Preferences</div>
          <div class="text-[15px] font-semibold text-ink-50 truncate">Settings</div>
        </div>
      </header>

      <div class="flex-1 min-h-0 flex flex-col md:flex-row overflow-hidden">
        <SettingsNavigation
          activeTab={activeTab}
          onTabChange={onTabChange}
          className="theme-submenu-surface hidden md:flex w-56 flex-none overflow-y-auto touch-scroll scrollbar-thin border-r border-white/10 bg-[#0f1217] p-3"
        />
        <SettingsNavigation
          activeTab={activeTab}
          onTabChange={onTabChange}
          mobile
          className="theme-submenu-surface md:hidden flex-none border-b border-white/10 bg-[#0f1217] px-3 py-2 overflow-x-auto no-scrollbar"
        />

        <main
          id="settings-active-panel"
          role="tabpanel"
          class="flex-1 min-w-0 overflow-y-auto overflow-x-hidden touch-scroll"
        >
          <div class="w-full px-4 py-5 md:px-6 md:py-7">
            <header class="mb-5 max-w-3xl">
              <h1 class="text-xl font-semibold text-ink-50">{activeTabDetails.label}</h1>
              <p class="mt-1 text-[13px] leading-relaxed text-ink-300">
                {activeTabDetails.description}
              </p>
            </header>

            {activeTab === "appearance" && (
              <AppearanceSettings
                theme={appearanceTheme}
                loading={appearanceLoading}
                saving={appearanceSaving}
                error={appearanceError}
                onThemeChange={onAppearanceThemeChange}
              />
            )}

            {activeTab === "agents" && (
              isAdmin ? (
                <div class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
                  <div class="px-4 py-3 border-b border-white/[0.06]">
                    <div class="text-[14.5px] font-semibold text-ink-50">Agent authentication</div>
                    <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
                      Sign in once on the parent host and share the credentials with project containers.
                    </div>
                  </div>
                  <div class="p-3 space-y-3">
                    <ClaudeAuthSettings />
                    <CodexAuthSettings
                      authenticated={codexAuthenticated}
                      usesApiKey={codexUsesApiKey}
                      deviceLogin={codexDeviceLogin}
                      loading={codexLoading}
                      starting={codexStarting}
                      error={codexError}
                      onStartDeviceLogin={onStartCodexDeviceLogin}
                    />
                    <KimiAuthSettings
                      authenticated={kimiAuthenticated}
                      deviceLogin={kimiDeviceLogin}
                      loading={kimiLoading}
                      starting={kimiStarting}
                      error={kimiError}
                      onStartDeviceLogin={onStartKimiDeviceLogin}
                    />
                  </div>
                </div>
              ) : (
                <SettingsNotice>
                  Agent authentication is managed by server administrators.
                </SettingsNotice>
              )
            )}

            {activeTab === "skills" && (
              <GlobalSkillsSettings
                isAdmin={isAdmin}
                library={globalSkills}
                projects={projects}
              />
            )}

            {activeTab === "playbooks" &&
              (isAdmin ? (
                <PlaybooksSettings editor={playbooks} />
              ) : (
                <SettingsNotice>
                  Playbooks are curated by server administrators. You can run them from the ⚡
                  button in any chat composer.
                </SettingsNotice>
              ))}

            {activeTab === "voice" &&
              (isAdmin ? (
                <VoiceInputSettings />
              ) : (
                <SettingsNotice>
                  Dictation works in your browser without any setup — press the microphone in any
                  chat composer. Server-side transcription is configured by server administrators.
                </SettingsNotice>
              ))}

            {activeTab === "usage" && (
              <UsageSettings
                dashboard={usageDashboard}
                isAdmin={isAdmin}
                onRebuild={onRebuildUsage}
                rebuilding={usageRebuilding}
                rebuildMessage={usageRebuildMessage}
              />
            )}

            {activeTab === "users" && (
              <div class="space-y-4">
                {isAdmin && <GoogleOAuthSettings />}
                <UsersPanel
                  currentEmail={currentEmail}
                  isAdmin={isAdmin}
                  users={userDirectory.users}
                  loading={userDirectory.loading}
                  error={userDirectory.error}
                  canAddUsers={googleOAuthEnabled}
                  onAdd={userDirectory.add}
                  onRemove={userDirectory.remove}
                  onSetRole={userDirectory.setRole}
                />
              </div>
            )}

            {activeTab === "notifications" &&
              (isAdmin ? (
                <NotificationsSettings />
              ) : (
                <SettingsNotice>
                  Notifications are managed by server administrators.
                </SettingsNotice>
              ))}

            {activeTab === "resources" &&
              (isAdmin ? (
                <ResourcesSettings
                  view={fleetResources}
                  loading={fleetResourcesLoading}
                  saving={fleetResourcesSaving}
                  error={fleetResourcesError}
                  onSave={onSaveFleetResources}
                />
              ) : (
                <SettingsNotice>
                  Container resource limits are managed by server administrators.
                </SettingsNotice>
              ))}

            {activeTab === "secrets" &&
              (isAdmin ? (
                <SecretsVaultSettings vault={secretsVault} projects={projects} />
              ) : (
                <SettingsNotice>
                  The secrets vault is managed by server administrators. Your project's Secrets tab
                  lists what its container inherits from it.
                </SettingsNotice>
              ))}

            {activeTab === "trash" && (
              <TrashSettings trash={projectTrash} isAdmin={isAdmin} />
            )}

            {activeTab === "audit" &&
              (isAdmin ? (
                <AuditLogSettings
                  entries={auditLog.entries}
                  filters={auditLog.filters}
                  loading={auditLog.loading}
                  loadingMore={auditLog.loadingMore}
                  hasMore={auditLog.hasMore}
                  error={auditLog.error}
                  exportUrl={auditLog.exportUrl}
                  onFiltersChange={auditLog.setFilters}
                  onRefresh={auditLog.refresh}
                  onLoadMore={auditLog.loadMore}
                />
              ) : (
                <SettingsNotice>
                  The audit log is visible to server administrators only.
                </SettingsNotice>
              ))}

            {activeTab === "updates" &&
              (isAdmin ? (
                <UpdatesSettings
                  status={selfUpdate}
                  loading={selfUpdateLoading}
                  checking={selfUpdateChecking}
                  applying={selfUpdateApplying}
                  restarting={selfUpdateRestarting}
                  error={selfUpdateError}
                  onCheck={onCheckForUpdates}
                  onApply={onApplyUpdate}
                />
              ) : (
                <SettingsNotice>
                  Application updates are managed by server administrators.
                </SettingsNotice>
              ))}

            {activeTab === "info" && (
              <ServerInfoSettings
                currentEmail={currentEmail}
                isAdmin={isAdmin}
                info={serverInfo}
                loading={serverInfoLoading}
                refreshing={serverInfoRefreshing}
                error={serverInfoError}
                onRefresh={onRefreshServerInfo}
              />
            )}
          </div>
        </main>
      </div>
    </div>
  );
}

function SettingsNavigation({
  activeTab,
  onTabChange,
  mobile = false,
  className,
}: {
  activeTab: SettingsTab;
  onTabChange: (tab: SettingsTab) => void;
  mobile?: boolean;
  className: string;
}) {
  const order = orderedTabs();

  // Roving tabindex without arrow keys leaves every tab but the active one
  // unreachable, so the tablist handles the arrows, Home and End itself.
  function onKeyDown(event: KeyboardEvent) {
    const next = mobile ? "ArrowRight" : "ArrowDown";
    const previous = mobile ? "ArrowLeft" : "ArrowUp";
    const index = order.findIndex((tab) => tab.id === activeTab);
    if (index < 0) return;
    let target = index;
    if (event.key === next) target = (index + 1) % order.length;
    else if (event.key === previous) target = (index - 1 + order.length) % order.length;
    else if (event.key === "Home") target = 0;
    else if (event.key === "End") target = order.length - 1;
    else return;
    event.preventDefault();
    // currentTarget is cleared once dispatch finishes, so grab the list now.
    const nav = event.currentTarget as HTMLElement | null;
    const id = order[target].id;
    onTabChange(id);
    requestAnimationFrame(() => {
      nav?.querySelector<HTMLElement>(`[data-settings-tab="${id}"]`)?.focus();
    });
  }

  if (mobile) {
    // One horizontally scrolling segmented strip: the group order still holds,
    // but a phone gets a single swipeable row instead of five stacked lists.
    return (
      <aside class={className} aria-label="Settings sections">
        <nav
          class="flex gap-1 min-w-max"
          role="tablist"
          aria-orientation="horizontal"
          onKeyDown={onKeyDown}
        >
          {order.map((tab) => (
            <SettingsTabButton
              key={tab.id}
              tab={tab}
              active={tab.id === activeTab}
              mobile
              onSelect={onTabChange}
            />
          ))}
        </nav>
      </aside>
    );
  }

  return (
    <aside class={className} aria-label="Settings sections">
      <nav
        class="w-full space-y-3"
        role="tablist"
        aria-orientation="vertical"
        onKeyDown={onKeyDown}
      >
        {GROUP_LABELS.map((group) => {
          const groupTabs = tabsInGroup(group.id);
          if (groupTabs.length === 0) return null;
          // `presentation` keeps the buttons direct children of the tablist in
          // the accessibility tree; the heading is a visual affordance only.
          return (
            <div key={group.id} role="presentation">
              <div
                aria-hidden="true"
                class="px-3 pb-1 text-[10.5px] font-semibold uppercase tracking-[0.09em] text-ink-300"
              >
                {group.label}
              </div>
              <div class="space-y-0.5" role="presentation">
                {groupTabs.map((tab) => (
                  <SettingsTabButton
                    key={tab.id}
                    tab={tab}
                    active={tab.id === activeTab}
                    onSelect={onTabChange}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </nav>
    </aside>
  );
}

function tabsInGroup(group: SettingsTabGroup) {
  return tabs.filter((tab) => tab.group === group);
}

/** Reading order: grouped, and identical on both the desktop and mobile nav. */
function orderedTabs() {
  return GROUP_LABELS.flatMap((group) => tabsInGroup(group.id));
}

function SettingsTabButton({
  tab,
  active,
  mobile = false,
  onSelect,
}: {
  tab: (typeof tabs)[number];
  active: boolean;
  mobile?: boolean;
  onSelect: (tab: SettingsTab) => void;
}) {
  const { id, label, Icon } = tab;
  return (
    <button
      type="button"
      role="tab"
      data-settings-tab={id}
      aria-selected={active}
      aria-controls="settings-active-panel"
      tabIndex={active ? 0 : -1}
      onClick={() => onSelect(id)}
      class={`${mobile ? "h-10 px-3 whitespace-nowrap" : "w-full h-10 px-3"} rounded-md inline-flex items-center gap-2.5 border text-[13px] font-medium transition-colors ${
        active
          ? "border-white/10 bg-white/[0.08] text-ink-50"
          : "border-transparent text-ink-300 hover:text-ink-100 hover:bg-white/[0.05]"
      }`}
    >
      <Icon class={`w-4 h-4 flex-none ${active ? "text-accent-blue" : "text-ink-300"}`} />
      <span class="truncate">{label}</span>
    </button>
  );
}

function SettingsNotice({ children }: { children: string }) {
  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] p-4 text-[13px] leading-relaxed text-ink-300">
      {children}
    </section>
  );
}
