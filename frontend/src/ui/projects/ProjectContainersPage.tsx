import type { ComponentChildren, ComponentType } from "preact";
import type {
  AccessRecord,
  PortalRecord,
  ProjectContainerRecord,
  SecretsRecord,
  SharesRecord,
} from "../../state/projects/projectContainerRecords";
import { describePortal, portalFormFrom } from "../../state/projects/projectPortalState";
import type { PortalFormState } from "../../state/projects/projectPortalState";
import { buildProjectPreviewUrl } from "../../shared/projectPreviewUrls";
import { PUBLIC_HOSTNAME } from "../../config/runtime";
import { describeShareCount, liveShares } from "../../state/projects/projectShareState";
import { Empty } from "./project-containers/ProjectContainerPrimitives";
import { ProjectActions } from "./project-containers/ProjectActions";
import {
  ContainerStateBadge,
  ProjectInfoSection,
} from "./project-containers/ProjectInfoSection";
import { ProjectSecretsSection } from "./project-containers/ProjectSecretsSection";
import { ProjectSnapshotsSection } from "./project-containers/ProjectSnapshotsSection";
import { ProjectClientMessageSection } from "./project-containers/ProjectClientMessageSection";
import { ProjectClientPortalSection } from "./project-containers/ProjectClientPortalSection";
import { ProjectPreviewSharesSection } from "./project-containers/ProjectPreviewSharesSection";
import { ProjectSharingSection } from "./project-containers/ProjectSharingSection";
import { ProjectResourceLimits } from "./project-containers/ProjectResourceLimits";
import { ProjectUsageLine } from "./project-containers/ProjectUsageLine";
import {
  ProjectHealthBadge,
  ProjectHealthMeters,
} from "./project-containers/ProjectHealthSummary";
import {
  TemplateBadge,
  TemplateStatusBadge,
} from "./project-containers/ProjectTemplateSection";
import { formatRelativeTime as fmtRelative } from "./project-containers/projectContainerFormat";
import type {
  ContainerLimits,
  ProjectContainerInfo,
  ProjectMeta,
  ProjectPortal,
  ProjectShare,
} from "../../models/project";
import type { ProjectHealth } from "../../models/health";
import {
  Archive,
  ChevronLeft,
  ExternalLink,
  Info,
  Key,
  Loader,
  Globe,
  Menu,
  MessageSquare,
  RotateCcw,
  Settings,
  Users,
} from "../primitives/icons";
import type { SnapshotsRecord } from "../../state/hooks/projects/useProjectSnapshots";
import type { UsageSummary } from "../../models/usage";
import type { ProjectResources } from "../../models/resources";
import { projectTemplateName } from "../../models/project";

export type ProjectSettingsTab = "info" | "settings" | "snapshots" | "secrets" | "sharing";

const tabs: Array<{
  id: ProjectSettingsTab;
  label: string;
  description: string;
  Icon: ComponentType<{ class?: string }>;
}> = [
  {
    id: "info",
    label: "Info",
    description: "Inspect the container, operating system, resources, network, and agent tooling.",
    Icon: Info,
  },
  {
    id: "settings",
    label: "Settings",
    description: "Manage this project's container lifecycle and destructive actions.",
    Icon: Settings,
  },
  {
    id: "snapshots",
    label: "Snapshots",
    description:
      "Archive this project's files and database, and roll back to an earlier copy.",
    Icon: Archive,
  },
  {
    id: "secrets",
    label: "Secrets",
    description: "Configure environment secrets passed to agents in this project.",
    Icon: Key,
  },
  {
    id: "sharing",
    label: "Sharing",
    description:
      "Control which registered users can access this project, hand out public preview links, " +
      "and publish a read-only client portal.",
    Icon: Users,
  },
];

export function isProjectSettingsTab(id: string | null): id is ProjectSettingsTab {
  return !!id && tabs.some((tab) => tab.id === id);
}

export function ProjectContainersPage({
  activeTab,
  project,
  health,
  infoRecord,
  secretsRecord,
  accessRecord,
  sharesRecord,
  snapshotsRecord,
  snapshotsRunning,
  portalRecord,
  portalIssuedUrl,
  refreshing,
  usageSummary,
  usageLoading,
  usageError,
  resources,
  resourcesLoading,
  resourcesSaving,
  resourcesError,
  onRefresh,
  onBack,
  onHamburger,
  onTabChange,
  onSaveSecret,
  onDeleteSecret,
  onAddMember,
  onRemoveMember,
  onCreateShare,
  onRevokeShare,
  onCreateSnapshot,
  onRestoreSnapshot,
  onDeleteSnapshot,
  onSavePortal,
  onDismissPortalUrl,
  onRepairNetwork,
  onSetResourceLimits,
  onStartProject,
  onStopProject,
  onRestartProject,
  onDeleteProject,
}: {
  activeTab: ProjectSettingsTab;
  project: ProjectMeta | null;
  health?: ProjectHealth;
  infoRecord: ProjectContainerRecord;
  secretsRecord: SecretsRecord;
  accessRecord: AccessRecord;
  sharesRecord: SharesRecord;
  snapshotsRecord: SnapshotsRecord;
  snapshotsRunning: boolean;
  portalRecord: PortalRecord;
  portalIssuedUrl: string | null;
  refreshing: boolean;
  usageSummary: UsageSummary | null;
  usageLoading: boolean;
  usageError: string | null;
  resources: ProjectResources | null;
  resourcesLoading: boolean;
  resourcesSaving: boolean;
  resourcesError: string | null;
  onRefresh: () => void;
  onBack: () => void;
  onHamburger: () => void;
  onTabChange: (tab: ProjectSettingsTab) => void;
  onSaveSecret: (key: string, value: string) => Promise<void>;
  onDeleteSecret: (key: string) => Promise<void>;
  onAddMember: (email: string) => Promise<void>;
  onRemoveMember: (email: string) => Promise<void>;
  onCreateShare: (port: number, ttlHours: number, label?: string) => Promise<ProjectShare>;
  onRevokeShare: (shareId: string) => Promise<void>;
  onCreateSnapshot: (label: string, includeSecrets: boolean) => Promise<void>;
  onRestoreSnapshot: (snapshotId: string) => Promise<void>;
  onDeleteSnapshot: (snapshotId: string) => Promise<void>;
  onSavePortal: (
    form: PortalFormState,
    overrides?: { enabled?: boolean; rotate?: boolean },
  ) => Promise<ProjectPortal>;
  onDismissPortalUrl: () => void;
  onRepairNetwork: () => Promise<void>;
  onSetResourceLimits: (limits: ContainerLimits) => Promise<void>;
  onStartProject: (force?: boolean) => Promise<void>;
  onStopProject: () => Promise<void>;
  onRestartProject: () => Promise<void>;
  onDeleteProject: () => Promise<void>;
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
          <div class="text-[11px] text-ink-300">Projects</div>
          <div class="text-[15px] font-semibold text-ink-50 truncate">
            {project ? `${project.name} — settings` : "Project settings"}
          </div>
        </div>
        <button
          type="button"
          onClick={onRefresh}
          disabled={refreshing}
          class="h-10 w-10 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08]
                 disabled:cursor-wait grid place-items-center"
          aria-label="Refresh"
          title="Refresh"
        >
          {refreshing ? <Loader class="w-4 h-4 animate-spin" /> : <RotateCcw class="w-4 h-4" />}
        </button>
      </header>

      <div class="flex-1 min-h-0 flex flex-col md:flex-row overflow-hidden">
        <ProjectSettingsNavigation
          activeTab={activeTab}
          onTabChange={onTabChange}
          className="theme-submenu-surface hidden md:flex w-56 flex-none border-r border-white/10 bg-[#0f1217] p-3"
        />
        <ProjectSettingsNavigation
          activeTab={activeTab}
          onTabChange={onTabChange}
          mobile
          className="theme-submenu-surface md:hidden flex-none border-b border-white/10 bg-[#0f1217] px-3 py-2 overflow-x-auto no-scrollbar"
        />

        <main
          id="project-settings-active-panel"
          role="tabpanel"
          class="flex-1 min-w-0 overflow-y-auto touch-scroll"
        >
          <div class="w-full px-4 py-5 md:px-6 md:py-7">
            <header class="mb-5">
              <h1 class="text-xl font-semibold text-ink-50">{activeTabDetails.label}</h1>
              <p class="mt-1 text-[13px] leading-relaxed text-ink-300">
                {activeTabDetails.description}
              </p>
            </header>

            {!project ? (
              <Empty text="Select a project from the sidebar." />
            ) : (
              <>
                {activeTab === "info" && (
                  <div class="space-y-3">
                    <ProjectHeader
                      project={project}
                      health={health}
                      info={infoRecord.data}
                      refreshedAt={infoRecord.refreshedAt}
                      usageSummary={usageSummary}
                      usageLoading={usageLoading}
                      usageError={usageError}
                    />
                    <ProjectInfoSection
                      project={project}
                      record={infoRecord}
                      secretsRecord={secretsRecord}
                      onRepairNetwork={onRepairNetwork}
                    />
                  </div>
                )}

                {activeTab === "settings" && (
                  <div class="space-y-4">
                    <ProjectResourceLimits
                      resources={resources}
                      loading={resourcesLoading}
                      saving={resourcesSaving}
                      error={resourcesError}
                      onSave={onSetResourceLimits}
                    />
                    <ProjectSettingsPanel
                      title="Project lifecycle"
                      description={`Current status: ${project.status || "unknown"}`}
                      Icon={Settings}
                    >
                      <ProjectActions
                        project={project}
                        onStart={onStartProject}
                        onStop={onStopProject}
                        onRestart={onRestartProject}
                        onDelete={onDeleteProject}
                      />
                    </ProjectSettingsPanel>
                  </div>
                )}

                {activeTab === "snapshots" && (
                  <ProjectSettingsPanel
                    title="Snapshots"
                    description={snapshotsDescription(snapshotsRecord)}
                    Icon={Archive}
                  >
                    <ProjectSnapshotsSection
                      record={snapshotsRecord}
                      running={snapshotsRunning}
                      onCreate={onCreateSnapshot}
                      onRestore={onRestoreSnapshot}
                      onDelete={onDeleteSnapshot}
                    />
                  </ProjectSettingsPanel>
                )}

                {activeTab === "secrets" && (
                  <ProjectSettingsPanel
                    title="Project secrets"
                    description={secretsDescription(secretsRecord)}
                    Icon={Key}
                  >
                    <ProjectSecretsSection
                      record={secretsRecord}
                      onSave={onSaveSecret}
                      onDelete={onDeleteSecret}
                    />
                  </ProjectSettingsPanel>
                )}

                {activeTab === "sharing" && (
                  <div class="space-y-4">
                    <ProjectSettingsPanel
                      title="Project access"
                      description={accessDescription(accessRecord)}
                      Icon={Users}
                    >
                      <ProjectSharingSection
                        record={accessRecord}
                        onAdd={onAddMember}
                        onRemove={onRemoveMember}
                      />
                    </ProjectSettingsPanel>
                    <ProjectSettingsPanel
                      title="Public preview links"
                      description={sharesDescription(sharesRecord)}
                      Icon={ExternalLink}
                    >
                      <ProjectPreviewSharesSection
                        record={sharesRecord}
                        onCreate={onCreateShare}
                        onRevoke={onRevokeShare}
                      />
                    </ProjectSettingsPanel>
                    <ProjectSettingsPanel
                      title="Client portal"
                      description={describePortal(portalRecord.data, portalRecord.loading)}
                      Icon={Globe}
                    >
                      <ProjectClientPortalSection
                        record={portalRecord}
                        issuedUrl={portalIssuedUrl}
                        onDismissIssuedUrl={onDismissPortalUrl}
                        onSave={onSavePortal}
                      />
                    </ProjectSettingsPanel>
                    <ProjectSettingsPanel
                      title="Message client"
                      description={
                        "Write to the client from one of your own templates, in Arabic or English."
                      }
                      Icon={MessageSquare}
                    >
                      <ProjectClientMessageSection
                        project={project}
                        portalUrl={portalIssuedUrl}
                        previewUrl={firstPreviewUrl(project, sharesRecord.data)}
                        portalEnabled={portalRecord.data?.enabled === true}
                        onPublishNote={(note) =>
                          onSavePortal({ ...portalFormFrom(portalRecord.data), note })
                        }
                      />
                    </ProjectSettingsPanel>
                  </div>
                )}
              </>
            )}
          </div>
        </main>
      </div>
    </div>
  );
}

/**
 * The newest public preview link for this project, or null when none is live.
 * It is what `{{previewUrl}}` resolves to in a client template — the same link
 * the portal page would show, never the sign-in-gated preview.
 */
function firstPreviewUrl(project: ProjectMeta, shares?: ProjectShare[]): string | null {
  const port = (shares ?? []).map((share) => share.port).sort((a, b) => a - b)[0];
  if (port === undefined) return null;
  return buildProjectPreviewUrl(project.slug, port, PUBLIC_HOSTNAME);
}

function ProjectSettingsNavigation({
  activeTab,
  onTabChange,
  mobile = false,
  className,
}: {
  activeTab: ProjectSettingsTab;
  onTabChange: (tab: ProjectSettingsTab) => void;
  mobile?: boolean;
  className: string;
}) {
  return (
    <aside class={className} aria-label="Project settings sections">
      <nav
        class={mobile ? "flex gap-1 min-w-max" : "w-full space-y-1"}
        role="tablist"
        aria-orientation={mobile ? "horizontal" : "vertical"}
      >
        {tabs.map(({ id, label, Icon }) => {
          const active = id === activeTab;
          return (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={active}
              aria-controls="project-settings-active-panel"
              tabIndex={active ? 0 : -1}
              onClick={() => onTabChange(id)}
              class={`${mobile ? "h-9 px-3" : "w-full h-10 px-3"} rounded-md inline-flex items-center gap-2.5 border text-[13px] font-medium transition-colors ${
                active
                  ? "border-white/10 bg-white/[0.08] text-ink-50"
                  : "border-transparent text-ink-300 hover:text-ink-100 hover:bg-white/[0.05]"
              }`}
            >
              <Icon class={`w-4 h-4 flex-none ${active ? "text-accent-blue" : "text-ink-400"}`} />
              <span>{label}</span>
            </button>
          );
        })}
      </nav>
    </aside>
  );
}

function ProjectSettingsPanel({
  title,
  description,
  Icon,
  children,
}: {
  title: string;
  description: string;
  Icon: ComponentType<{ class?: string }>;
  children: ComponentChildren;
}) {
  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Icon class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">{title}</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">{description}</div>
        </div>
      </header>
      <div class="p-3 space-y-3">{children}</div>
    </section>
  );
}

function secretsDescription(record: SecretsRecord): string {
  if (record.loading && !record.data) return "Loading project secrets…";
  if (record.error) return "Project secrets could not be loaded.";
  const count = record.data?.length ?? 0;
  return `${count} configured secret${count === 1 ? "" : "s"}`;
}

function snapshotsDescription(record: SnapshotsRecord): string {
  if (record.loading && !record.data) return "Loading snapshots…";
  if (record.error) return "Snapshots could not be loaded.";
  const count = record.data?.length ?? 0;
  return `${count} snapshot${count === 1 ? "" : "s"} kept for this project`;
}

function accessDescription(record: AccessRecord): string {
  if (record.loading && !record.data) return "Loading project members…";
  if (record.error) return "Project members could not be loaded.";
  const count = record.data?.length ?? 0;
  return `${count} project member${count === 1 ? "" : "s"}`;
}

function sharesDescription(record: SharesRecord): string {
  if (record.loading && !record.data) return "Loading public preview links…";
  if (record.error) return "Public preview links could not be loaded.";
  return describeShareCount(liveShares(record.data ?? [], Date.now()).length);
}

function ProjectHeader({
  project,
  health,
  info,
  refreshedAt,
  usageSummary,
  usageLoading,
  usageError,
}: {
  project: ProjectMeta;
  health?: ProjectHealth;
  info?: ProjectContainerInfo;
  refreshedAt?: number;
  usageSummary: UsageSummary | null;
  usageLoading: boolean;
  usageError: string | null;
}) {
  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] px-4 py-3 flex items-start gap-3">
      <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
        <Settings class="w-4 h-4 text-ink-200" />
      </div>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap min-w-0">
          <span class="text-[14.5px] font-semibold text-ink-50 truncate">{project.name}</span>
          {info && <ContainerStateBadge state={info.state ?? "UNKNOWN"} />}
          <ProjectHealthBadge health={health} />
          {/* The container payload carries the title and provisioning
              status; project metadata alone is enough for the name, so the
              badge appears before the inspection request resolves. */}
          <TemplateBadge
            template={info?.template ?? { name: projectTemplateName(project), status: "none" }}
          />
          {info?.template && <TemplateStatusBadge status={info.template.status} />}
        </div>
        <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug font-mono truncate">
          {project.containerName || project.slug}
        </div>
        <ProjectUsageLine summary={usageSummary} loading={usageLoading} error={usageError} />
        <ProjectHealthMeters health={health} />
      </div>
      {refreshedAt && (
        <div class="text-[11px] text-ink-400 mt-1.5">refreshed {fmtRelative(refreshedAt)}</div>
      )}
    </section>
  );
}
