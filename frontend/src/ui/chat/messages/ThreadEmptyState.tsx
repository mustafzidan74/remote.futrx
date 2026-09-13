import type { Playbook } from "../../../models/playbook";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { playbookLabel } from "../../../state/hooks/chat/playbookState";
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
    <div class="mx-auto max-w-md px-4 py-16 text-center text-sm text-ink-300">
      <div class="mx-auto mb-4 grid h-11 w-11 place-items-center rounded-card border border-line text-ink-400">
        <MessageSquare class="h-5 w-5" />
      </div>
      <div class="text-[15px] font-semibold tracking-[-0.01em] text-ink-50">Start a conversation</div>
      <div class="mt-2 text-[12.5px] leading-relaxed text-ink-400">
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
                class="flex w-full items-center gap-2 rounded-md border border-line bg-tint px-3 py-2
                       text-left text-ink-100 transition hover:bg-tint-strong
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
