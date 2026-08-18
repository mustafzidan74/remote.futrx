import { Folder, Plus, Search } from "../primitives/icons";

export function SidebarEmptyState({ onNewProject }: { onNewProject: () => void }) {
  return (
    <div class="mx-2 rounded-lg border border-dashed border-white/[0.12] bg-white/[0.03] text-center text-ink-300 text-sm py-8 px-4">
      <Folder class="w-8 h-8 mx-auto mb-3 opacity-50" />
      <div class="text-ink-100 font-medium">No projects yet</div>
      <div class="text-[12px] mt-1.5 leading-relaxed">
        Each project gets its own sandboxed container. Agent CLIs run inside.
      </div>
      <button
        type="button"
        onClick={onNewProject}
        class="mt-4 inline-flex items-center gap-1.5 h-10 px-3 rounded-md bg-white/[0.08] hover:bg-white/[0.12] text-ink-100 text-sm"
      >
        <Plus class="w-4 h-4" /> New project
      </button>
    </div>
  );
}

export function SidebarNoMatches() {
  return (
    <div class="mx-2 rounded-lg border border-dashed border-white/[0.12] bg-white/[0.03] text-center text-ink-300 text-sm py-6 px-4">
      <Search class="w-6 h-6 mx-auto mb-2 opacity-50" />
      No matches
    </div>
  );
}
