import type { Playbook } from "../../../models/playbook";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { playbookLabel } from "../../../state/chat/playbookState";
import { MessageSquare, Zap } from "../../primitives/icons";

/** How many playbooks a blank thread offers before it becomes a menu. */
const SUGGESTION_COUNT = 3;

/**
 * A blank thread is the moment a new user has the least idea what to type, so
 * it offers the first few playbooks as buttons. They load the prompt into the
 * composer rather than sending it — seeing what is about to run is the point.
 */
export function ThreadEmptyState({
  cwd,
  playbooks,
}: {
  cwd?: string;
  playbooks?: PlaybookLibrary;
}) {
  const suggestions: Playbook[] = (playbooks?.playbooks ?? []).slice(0, SUGGESTION_COUNT);

  return (
    <div class="text-center text-ink-300 text-sm py-12 px-4 max-w-md mx-auto">
      <div class="w-14 h-14 mx-auto mb-4 rounded-lg bg-white/[0.06] border border-white/10 grid place-items-center">
        <MessageSquare class="w-7 h-7 opacity-70" />
      </div>
      <div class="font-semibold text-ink-100 text-base">Start a conversation</div>
      <div class="text-xs mt-2 leading-relaxed">
        The selected agent runs with full tool access in{" "}
        <span class="font-mono text-ink-100">{cwd || "~"}</span>.
        Drop, paste, or upload files to reference them.
      </div>

      {suggestions.length > 0 && (
        <div class="mt-6">
          <div class="text-[10.5px] font-semibold uppercase tracking-[0.09em] text-ink-400">
            Or start from a playbook
          </div>
          <div class="mt-2 space-y-1.5">
            {suggestions.map((playbook) => (
              <button
                key={playbook.id}
                type="button"
                onClick={() => void playbooks?.run(playbook)}
                disabled={playbooks?.running !== null}
                class="flex w-full items-center gap-2 rounded-md border border-white/10 bg-white/[0.04] px-3 py-2
                       text-left text-ink-100 transition hover:bg-white/[0.08]
                       disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Zap class="h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-[13px] font-medium">
                    {playbookLabel(playbook)}
                  </span>
                  {playbook.hint && (
                    <span class="block truncate text-[11.5px] text-ink-400">{playbook.hint}</span>
                  )}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
