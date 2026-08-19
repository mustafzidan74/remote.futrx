import { useEffect, useRef, useState } from "preact/hooks";
import type { AgentActivity } from "../../../state/chat/agentActivity";
import { activityView } from "../../../state/chat/agentActivity";
import { useActivityClock, useShowThinking } from "../../../state/hooks/chat/useAgentActivity";
import { ChevronDown, ChevronRight, Loader, Square } from "../../primitives/icons";

/**
 * What the agent is doing, pinned above the composer while a run is in flight.
 *
 * A terminal CLI narrates itself — reading this file, running that command —
 * and a chat window that answers a five-minute turn with a silent spinner
 * makes "thinking hard" and "hung" look identical. This strip is that
 * narration: one line, derived entirely from the chat events already on
 * screen, with the Stop button right where the impatience is.
 */
export function AgentActivityStrip({
  activity,
  streaming,
  onCancel,
}: {
  activity: AgentActivity;
  streaming: boolean;
  onCancel: () => void;
}) {
  const active = streaming && activity.phase !== "idle";
  const now = useActivityClock(active);
  const [showThinking, toggleThinking] = useShowThinking();
  const [open, setOpen] = useState(false);
  const reasoningRef = useRef<HTMLPreElement>(null);
  const view = activityView(activity, now);

  // The reasoning pane follows the newest words the way a terminal follows its
  // own output; without this it silently freezes on the first ten lines.
  useEffect(() => {
    if (!open || !showThinking) return;
    const element = reasoningRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [open, showThinking, view.reasoning]);

  if (!active) return null;

  const expandable = showThinking && view.canShowThinking && view.reasoning.length > 0;

  return (
    <div
      class="codex-activity-strip mx-3 mb-1 mt-2 rounded-md border border-accent-blue/25 bg-accent-blue/[0.07]"
      role="status"
      aria-live="polite"
    >
      <div class="flex items-center gap-2 px-2.5 py-1.5">
        {expandable ? (
          <button
            type="button"
            onClick={() => setOpen((current) => !current)}
            class="grid h-5 w-5 flex-none place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
            aria-expanded={open}
            aria-label={open ? "Hide reasoning" : "Show reasoning"}
          >
            {open ? <ChevronDown class="h-3.5 w-3.5" /> : <ChevronRight class="h-3.5 w-3.5" />}
          </button>
        ) : (
          <Loader class="h-3.5 w-3.5 flex-none animate-spin text-accent-blue" aria-hidden="true" />
        )}

        <span class="flex-none text-[13px] leading-none" aria-hidden="true">
          {view.icon}
        </span>

        {/*
          One line on every viewport: label, target and detail share a single
          truncating block, so a long command shortens instead of pushing the
          timer and the Stop button off a phone screen.
        */}
        <span
          class="min-w-0 flex-1 truncate text-[11.5px]"
          title={view.title || view.label}
        >
          <span class="font-semibold text-ink-100">{view.label}</span>
          {view.target && (
            <>
              {" "}
              <code dir="auto" class="bidi-auto font-mono text-ink-200">
                {view.target}
              </code>
            </>
          )}
          {view.detail && (
            <span class="ms-1 font-mono text-[11px] text-ink-300">{view.detail}</span>
          )}
        </span>

        <span class="flex-none tabular-nums text-[11px] text-ink-300">{view.elapsed}</span>
        {view.tokenLabel && (
          <span class="hidden flex-none text-[11px] text-ink-300 sm:inline">
            · {view.tokenLabel} tokens
          </span>
        )}

        {view.canShowThinking && (
          <button
            type="button"
            onClick={toggleThinking}
            class={`h-6 flex-none rounded px-1.5 text-[11px] font-medium sm:px-2
                    ${showThinking ? "bg-white/[0.10] text-ink-100" : "text-ink-300 hover:bg-white/[0.07] hover:text-ink-100"}`}
            aria-pressed={showThinking}
            aria-label="Show thinking"
            title="Stream the model's reasoning into this strip"
          >
            {/* A phone has no room for the words, but must keep the control. */}
            <span class="sm:hidden" aria-hidden="true">💭</span>
            <span class="hidden sm:inline">Show thinking</span>
          </button>
        )}

        <button
          type="button"
          onClick={onCancel}
          class="grid h-6 w-6 flex-none place-items-center rounded text-ink-300
                 hover:bg-accent-red/15 hover:text-accent-red"
          aria-label="Stop the agent"
          title="Stop"
        >
          <Square class="h-3.5 w-3.5" />
        </button>
      </div>

      {view.stuckNote && (
        <p class="px-2.5 pb-1.5 text-[11px] leading-4 text-accent-yellow">{view.stuckNote}</p>
      )}

      {expandable && open && (
        <pre
          ref={reasoningRef}
          dir="auto"
          class="bidi-auto max-h-[10.5rem] overflow-y-auto scrollbar-thin whitespace-pre-wrap break-words
                 border-t border-white/10 px-2.5 py-2 text-[11.5px] leading-[1.05rem] text-ink-300"
        >
          {view.reasoning}
        </pre>
      )}
    </div>
  );
}
