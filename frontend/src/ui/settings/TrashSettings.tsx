import { useState } from "preact/hooks";
import type { TrashedProject } from "../../models/project";
import type { ProjectTrash } from "../../state/hooks/admin/useProjectTrash";
import { snapshotState } from "../../state/projects/snapshotState";
import { AlertCircle, Clock, Loader, RotateCcw, Trash } from "../primitives/icons";
import { formatEpochMillis } from "../projects/project-containers/projectContainerFormat";

export function TrashSettings({ trash, isAdmin }: { trash: ProjectTrash; isAdmin: boolean }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const now = Date.now();

  async function run(projectId: string, operation: () => Promise<void>) {
    setBusy(projectId);
    setActionError(null);
    try {
      await operation();
    } catch (error) {
      setActionError((error as Error).message);
    } finally {
      setBusy(null);
    }
  }

  const restore = (project: TrashedProject) =>
    run(project.id, () => trash.restore(project.id));

  const purge = (project: TrashedProject) => {
    if (
      !confirm(
        `Purge "${project.name}" permanently?\n\n` +
          "This deletes the trashed workspace, the agent homes, every snapshot of " +
          "this project, its members and its secrets. There is no undo.",
      )
    ) {
      return;
    }
    return run(project.id, () => trash.purge(project.id));
  };

  return (
    <div class="space-y-4">
      <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
        <header class="px-4 py-3 border-b border-white/[0.06] flex items-start gap-3">
          <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
            <Trash class="w-4 h-4 text-ink-200" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-[14.5px] font-semibold text-ink-50">Deleted projects</div>
            <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
              Deleting a project destroys its container and moves its files here. They
              stay recoverable until the retention window closes.
            </div>
          </div>
          <button
            type="button"
            onClick={() => void trash.refresh()}
            disabled={trash.loading}
            class="h-8 px-2.5 rounded-md border border-white/10 bg-white/[0.04] text-[12.5px]
                   text-ink-200 hover:bg-white/[0.08] disabled:opacity-50 flex-none"
          >
            Refresh
          </button>
        </header>

        <div class="p-3 space-y-2">
          {(trash.error || actionError) && (
            <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
              <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
              <div class="text-accent-red break-words">{trash.error ?? actionError}</div>
            </div>
          )}

          {trash.loading && trash.projects.length === 0 ? (
            <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-4 text-center text-[12.5px] text-ink-300">
              Loading the Trash…
            </div>
          ) : trash.projects.length === 0 ? (
            <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-4 text-center text-[12.5px] text-ink-300">
              The Trash is empty.
            </div>
          ) : (
            trash.projects.map((project) => (
              <TrashedProjectRow
                key={project.id}
                project={project}
                now={now}
                isAdmin={isAdmin}
                busy={busy === project.id}
                onRestore={() => void restore(project)}
                onPurge={() => void purge(project)}
              />
            ))
          )}
        </div>
      </section>

      <p class="text-[11.5px] text-ink-400 leading-relaxed px-1">
        Restoring moves the files back, re-creates the container from the project's
        template, and re-imports the database captured when it was deleted. The name of
        a trashed project stays reserved: a new project cannot take it until this one is
        restored or purged.
      </p>
    </div>
  );
}

function TrashedProjectRow({
  project,
  now,
  isAdmin,
  busy,
  onRestore,
  onPurge,
}: {
  project: TrashedProject;
  now: number;
  isAdmin: boolean;
  busy: boolean;
  onRestore: () => void;
  onPurge: () => void;
}) {
  const days = snapshotState.daysLeft(project, now);
  const urgent = days !== null && days <= 1;

  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2.5 flex items-start gap-3">
      <div class="mt-0.5 w-8 h-8 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
        <Trash class="w-3.5 h-3.5 text-ink-300" />
      </div>
      <div class="flex-1 min-w-0">
        <div class="text-[13px] font-medium text-ink-50 truncate">{project.name}</div>
        <div class="mt-0.5 text-[11.5px] text-ink-400 font-mono truncate">{project.slug}</div>
        <div class="mt-0.5 text-[11.5px] text-ink-400 flex items-center gap-1.5 flex-wrap">
          <Clock class="w-3 h-3 flex-none" />
          <span>deleted {formatEpochMillis(project.deletedAt)}</span>
          {project.deletedBy && <span>· by {project.deletedBy}</span>}
        </div>
        <div class={`mt-0.5 text-[11.5px] ${urgent ? "text-accent-orange" : "text-ink-400"}`}>
          {snapshotState.describeRetention(project, now)}
        </div>
      </div>
      <div class="flex-none flex items-center gap-1">
        <button
          type="button"
          onClick={onRestore}
          disabled={busy}
          class="h-8 px-2.5 rounded-md border border-white/10 bg-white/[0.04] text-[12.5px]
                 text-ink-100 hover:bg-white/[0.08] disabled:opacity-45 inline-flex items-center gap-1.5"
        >
          {busy ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <RotateCcw class="w-3.5 h-3.5" />}
          Restore
        </button>
        {isAdmin && (
          <button
            type="button"
            onClick={onPurge}
            disabled={busy}
            class="h-8 px-2.5 rounded-md border border-accent-red/30 bg-accent-red/[0.08] text-[12.5px]
                   font-medium text-accent-red hover:bg-accent-red/[0.14] disabled:opacity-45"
          >
            Purge
          </button>
        )}
      </div>
    </div>
  );
}
