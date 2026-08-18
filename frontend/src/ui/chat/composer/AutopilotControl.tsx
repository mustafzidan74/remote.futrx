import { useEffect, useRef, useState } from "preact/hooks";
import {
  AUTOPILOT_MAX_DURATION_MIN,
  AUTOPILOT_MAX_ROUNDS,
  AUTOPILOT_MIN_DURATION_MIN,
  AUTOPILOT_MIN_ROUNDS,
  autopilotDraftFrom,
  validateAutopilotDraft,
  type AutopilotDraft,
  type AutopilotView,
} from "../../../state/chat/chatPolicyState";
import { Loader, PlaneTakeoff } from "../../primitives/icons";

/**
 * The composer's Autopilot switch and its popover.
 *
 * Arming sends the drafted limits along with the switch, because the server
 * resets the round counter on the enable transition — sending the limits
 * afterwards would budget the loop against the previous numbers.
 */
export function AutopilotControl({
  view,
  busy,
  onArm,
  onDisarm,
  onSaveLimits,
}: {
  view: AutopilotView;
  busy: boolean;
  onArm: (draft: AutopilotDraft) => void;
  onDisarm: () => void;
  onSaveLimits: (draft: AutopilotDraft) => void;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<AutopilotDraft>(() => autopilotDraftFrom(view));
  const rootRef = useRef<HTMLDivElement>(null);

  // Reopening shows what is stored, not what was typed and abandoned last time.
  useEffect(() => {
    if (open) setDraft(autopilotDraftFrom(view));
  }, [open, view.maxRounds, view.maxDurationMin]);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  const validation = validateAutopilotDraft(draft);

  return (
    <div ref={rootRef} class="codex-autopilot-control-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={busy}
        class={`codex-autopilot-control composer-pill ${
          view.enabled ? "composer-pill-active" : ""
        }`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Autopilot — ${view.status}`}
        title="Autopilot — keep the agent working while you are away"
      >
        {busy ? (
          <Loader class="h-4 w-4 flex-none animate-spin" />
        ) : (
          <PlaneTakeoff class="h-4 w-4 flex-none" aria-hidden="true" />
        )}
        <span class="font-semibold">Autopilot</span>
        {view.enabled && (
          <span class="rounded bg-accent-blue/25 px-1 py-0.5 text-[10px] leading-none">
            {view.roundsUsed}/{view.maxRounds}
          </span>
        )}
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute left-0 bottom-full z-40 mb-2
                 w-[min(20rem,calc(100vw-1.5rem))] sm:left-auto sm:right-0"
          role="dialog"
          aria-label="Autopilot settings"
        >
          <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2">
            <div class="text-[12px] font-semibold text-ink-100">Autopilot</div>
            <p class="mt-1 text-[11px] leading-4 text-ink-400">
              When the agent ends a turn without saying it is done, Remote sends it one more
              &ldquo;keep going&rdquo; prompt — until it reports <code>&lt;&lt;DONE&gt;&gt;</code>, gets
              blocked, or runs out of budget.
            </p>
          </div>

          <div class="px-3 py-2.5">
            <div class="mb-2 flex items-center justify-between gap-2">
              <span class="text-[11px] uppercase tracking-[0.1em] text-ink-400">Status</span>
              <span class="truncate text-[11.5px] text-ink-100">{view.status}</span>
            </div>

            <div class="grid grid-cols-2 gap-2">
              <label class="block">
                <span class="mb-1 block text-[11px] text-ink-400">Max rounds</span>
                <input
                  type="number"
                  inputMode="numeric"
                  min={AUTOPILOT_MIN_ROUNDS}
                  max={AUTOPILOT_MAX_ROUNDS}
                  value={draft.maxRounds}
                  onInput={(event) =>
                    setDraft((current) => ({
                      ...current,
                      maxRounds: (event.currentTarget as HTMLInputElement).value,
                    }))
                  }
                  class="h-8 w-full rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12.5px] text-ink-50
                         focus:border-accent-blue/40 focus:outline-none"
                />
              </label>
              <label class="block">
                <span class="mb-1 block text-[11px] text-ink-400">Max minutes</span>
                <input
                  type="number"
                  inputMode="numeric"
                  min={AUTOPILOT_MIN_DURATION_MIN}
                  max={AUTOPILOT_MAX_DURATION_MIN}
                  value={draft.maxDurationMin}
                  onInput={(event) =>
                    setDraft((current) => ({
                      ...current,
                      maxDurationMin: (event.currentTarget as HTMLInputElement).value,
                    }))
                  }
                  class="h-8 w-full rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12.5px] text-ink-50
                         focus:border-accent-blue/40 focus:outline-none"
                />
              </label>
            </div>

            {validation.error && (
              <div class="mt-2 text-[11px] leading-4 text-red-300">{validation.error}</div>
            )}

            <div class="mt-3 flex items-center gap-2">
              {view.enabled ? (
                <>
                  <button
                    type="button"
                    onClick={() => {
                      setOpen(false);
                      onDisarm();
                    }}
                    class="h-8 flex-1 rounded-md border border-white/10 bg-white/[0.05] text-[12px] font-semibold
                           text-ink-100 hover:bg-white/[0.09]"
                  >
                    Stop autopilot
                  </button>
                  <button
                    type="button"
                    disabled={!validation.valid || busy}
                    onClick={() => {
                      setOpen(false);
                      onSaveLimits(draft);
                    }}
                    class="h-8 flex-1 rounded-md bg-accent-blue/20 text-[12px] font-semibold text-accent-blue
                           hover:bg-accent-blue/30 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Save limits
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={!validation.valid || busy}
                  onClick={() => {
                    setOpen(false);
                    onArm(draft);
                  }}
                  class="h-8 w-full rounded-md bg-accent-blue/20 text-[12px] font-semibold text-accent-blue
                         hover:bg-accent-blue/30 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Enable autopilot
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
