import type { AutopilotView } from "../../../state/chat/chatPolicyState";
import { PlaneTakeoff, X } from "../../primitives/icons";

/**
 * The header's autopilot indicator.
 *
 * Stop disarms the loop; it deliberately does not cancel a run that is already
 * in flight. Killing an agent mid-edit to switch off a scheduling policy would
 * leave the workspace in whatever half-state the turn had reached — the Cancel
 * control in the composer is the one that stops work.
 */
export function AutopilotPill({
  view,
  busy,
  onStop,
}: {
  view: AutopilotView;
  busy: boolean;
  onStop: () => void;
}) {
  if (!view.enabled) return null;
  return (
    <div
      class="codex-autopilot-pill flex flex-none items-center gap-1.5 rounded-full border border-accent-blue/30
             bg-accent-blue/[0.12] py-1 pl-2 pr-1 text-accent-blue"
      title={view.status}
    >
      <PlaneTakeoff class="h-3 w-3 flex-none" aria-hidden="true" />
      <span class="hidden text-[11px] font-semibold sm:inline">{view.pillLabel}</span>
      <span class="text-[11px] font-semibold sm:hidden">
        {view.roundsUsed}/{view.maxRounds}
      </span>
      <button
        type="button"
        onClick={onStop}
        disabled={busy}
        class="grid h-5 w-5 flex-none place-items-center rounded-full text-accent-blue
               hover:bg-accent-blue/25 disabled:cursor-not-allowed disabled:opacity-40"
        aria-label="Stop autopilot"
        title="Stop autopilot — a run already in flight keeps going"
      >
        <X class="h-3 w-3" />
      </button>
    </div>
  );
}
