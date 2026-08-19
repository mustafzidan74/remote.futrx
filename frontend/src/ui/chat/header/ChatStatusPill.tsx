import type { ChatProvider } from "../../../models/chat";
import { providerDisplayLabel } from "../../../config/chat";
import {
  PHASE_ABBREVIATION,
  formatElapsed,
  type AgentActivity,
} from "../../../state/chat/agentActivity";
import type { AutopilotView } from "../../../state/chat/chatPolicyState";
import { useActivityClock } from "../../../state/hooks/chat/useAgentActivity";
import { PlaneTakeoff, X } from "../../primitives/icons";

/**
 * One pill for the whole answer to "what is this chat doing?".
 *
 * The header used to say it three times — a coloured dot beside the title, a
 * "Claude · Ready" line under it, and a separate autopilot pill on the right.
 * Three indicators of one fact is three things to reconcile, so this is now
 * the only one: one dot, one line, and the autopilot counter folded in.
 *
 * Stop disarms the loop; it deliberately does not cancel a run already in
 * flight. Killing an agent mid-edit to switch off a scheduling policy would
 * leave the workspace in whatever half-state the turn had reached — the Cancel
 * control in the composer is the one that stops work.
 *
 * While a run is live the state word is the phase the activity strip is
 * showing — "thinking", "tool", "writing" — plus the run's elapsed time, so
 * the header and the strip never disagree about what is happening.
 */
export function ChatStatusPill({
  provider,
  streaming,
  activity,
  autopilot,
  busy = false,
  onStopAutopilot,
}: {
  provider?: ChatProvider;
  streaming: boolean;
  /** The live phase, when the caller is inside a chat thread. */
  activity?: AgentActivity;
  autopilot?: AutopilotView;
  busy?: boolean;
  onStopAutopilot?: () => void;
}) {
  const flying = !!autopilot?.enabled;
  const phase = streaming && activity ? activity.phase : "idle";
  const now = useActivityClock(phase !== "idle");
  const elapsed =
    phase !== "idle" && activity && activity.startedAt > 0
      ? formatElapsed(Math.max(0, now - activity.startedAt))
      : "";
  const state = streaming
    ? phase === "idle"
      ? "Working"
      : `${PHASE_ABBREVIATION[phase]}${elapsed ? ` ${elapsed}` : ""}`
    : "Ready";
  const title = flying
    ? `${providerDisplayLabel(provider)} · ${state} · ${autopilot.status}`
    : `${providerDisplayLabel(provider)} · ${state}`;

  return (
    <div
      class={`codex-chat-status-pill flex h-8 flex-none items-center gap-1.5 rounded-full border py-1 ps-2.5
              ${flying ? "border-accent-blue/30 bg-accent-blue/[0.12] text-accent-blue pe-1" : "border-white/10 bg-white/[0.04] text-ink-200 pe-2.5"}`}
      title={title}
    >
      <span
        class={`h-1.5 w-1.5 flex-none rounded-full ${streaming ? "bg-accent-green animate-pulse" : "bg-ink-400"}`}
        aria-hidden="true"
      />
      <span class="text-[11.5px] font-semibold whitespace-nowrap tabular-nums">
        <span class="hidden sm:inline">{providerDisplayLabel(provider)} · </span>
        {state}
      </span>
      {flying && (
        <>
          <PlaneTakeoff class="h-3.5 w-3.5 flex-none" aria-hidden="true" />
          <span class="text-[11.5px] font-semibold tabular-nums">
            {autopilot.roundsUsed}/{autopilot.maxRounds}
          </span>
          {onStopAutopilot && (
            <button
              type="button"
              onClick={onStopAutopilot}
              disabled={busy}
              class="grid h-6 w-6 flex-none place-items-center rounded-full text-accent-blue
                     hover:bg-accent-blue/25 disabled:cursor-not-allowed disabled:opacity-40"
              aria-label="Stop autopilot"
              title="Stop autopilot — a run already in flight keeps going"
            >
              <X class="h-3.5 w-3.5" />
            </button>
          )}
        </>
      )}
    </div>
  );
}
