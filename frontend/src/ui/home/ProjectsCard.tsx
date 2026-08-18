import { PUBLIC_HOSTNAME } from "../../config/runtime.ts";
import type { DashboardProject } from "../../models/dashboard";
import { formatRelative, projectDot } from "../../state/home/dashboardState";
import { buildProjectPreviewUrl } from "../../shared/projectPreviewUrls.ts";
import { CardEmpty, CardSkeleton, DashboardCard, RowAction, ToneDot } from "./DashboardCard";
import { Archive, Folder, Globe, MessageSquare, Plus } from "../primitives/icons";

/**
 * The Projects column: one card per project with its dot, what it is doing,
 * and the three things somebody opens a project to do — the latest chat, the
 * running preview, and a snapshot before letting an agent loose on it.
 */
export function ProjectsCard({
  projects,
  loading,
  now,
  onOpenChat,
  onOpenProject,
  onSnapshotProject,
  onNewProject,
}: {
  projects: DashboardProject[];
  loading: boolean;
  now: number;
  onOpenChat: (chatId: string) => void;
  onOpenProject: (projectId: string) => void;
  onSnapshotProject: (projectId: string) => void;
  onNewProject: () => void;
}) {
  const running = projects.filter((project) => project.status === "running").length;

  return (
    <DashboardCard
      title="Projects"
      subtitle={
        projects.length === 0
          ? "Nothing provisioned yet"
          : `${running} of ${projects.length} running`
      }
      Icon={Folder}
      action={
        <button
          type="button"
          onClick={onNewProject}
          class="inline-flex h-7 items-center gap-1 rounded-md bg-white/[0.08] px-2 text-[12px]
                 font-medium text-ink-100 transition hover:bg-white/[0.12]"
        >
          <Plus class="h-3.5 w-3.5" /> New
        </button>
      }
    >
      {loading ? (
        <CardSkeleton rows={3} />
      ) : projects.length === 0 ? (
        <CardEmpty>
          A project is a sandboxed container with a stack preinstalled. Create one to start
          driving an agent against real files.
        </CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {projects.map((project) => (
            <ProjectRow
              key={project.id}
              project={project}
              now={now}
              onOpenChat={onOpenChat}
              onOpenProject={onOpenProject}
              onSnapshotProject={onSnapshotProject}
            />
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}

function ProjectRow({
  project,
  now,
  onOpenChat,
  onOpenProject,
  onSnapshotProject,
}: {
  project: DashboardProject;
  now: number;
  onOpenChat: (chatId: string) => void;
  onOpenProject: (projectId: string) => void;
  onSnapshotProject: (projectId: string) => void;
}) {
  const dot = projectDot(project);
  const previewUrl = project.previewPort
    ? buildProjectPreviewUrl(project.slug, project.previewPort, PUBLIC_HOSTNAME)
    : "";
  const activity = formatRelative(project.lastActivityAt, now);

  return (
    <li class="group flex items-center gap-3 px-4 py-3">
      <ToneDot tone={dot.tone} title={dot.title} pulse={project.status === "provisioning"} />

      <button
        type="button"
        onClick={() => onOpenProject(project.id)}
        class="min-w-0 flex-1 text-left"
      >
        <div class="flex items-center gap-2">
          <span class="truncate text-[13.5px] font-medium text-ink-50">{project.name}</span>
          {project.running && (
            <span class="flex-none rounded bg-accent-blue/15 px-1.5 py-0.5 text-[10.5px] font-medium text-accent-blue">
              Running
            </span>
          )}
          {previewUrl && (
            <span class="flex-none rounded bg-white/[0.08] px-1.5 py-0.5 font-mono text-[10.5px] tabular-nums text-ink-200">
              :{project.previewPort}
            </span>
          )}
        </div>
        <div class="mt-0.5 truncate text-[12px] text-ink-300">
          {dot.label}
          {activity ? ` · active ${activity}` : " · no chats yet"}
        </div>
      </button>

      <div class="hover-actions flex flex-none items-center gap-0.5">
        {project.latestChatId && (
          <RowAction
            label={`Open the latest chat in ${project.name}`}
            Icon={MessageSquare}
            onClick={() => onOpenChat(project.latestChatId as string)}
          />
        )}
        {previewUrl && (
          <RowAction label={`Open the preview of ${project.name}`} Icon={Globe} href={previewUrl} />
        )}
        <RowAction
          label={`Snapshot ${project.name}`}
          Icon={Archive}
          onClick={() => onSnapshotProject(project.id)}
        />
      </div>
    </li>
  );
}
