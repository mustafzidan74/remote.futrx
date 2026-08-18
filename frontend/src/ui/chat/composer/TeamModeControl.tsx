import { useEffect, useRef, useState } from "preact/hooks";
import type { ChatProvider, TeamRoleName } from "../../../models/chat";
import {
  TEAM_MAX_LOOPS,
  TEAM_MIN_LOOPS,
  teamProviderOptions,
  type TeamRoleView,
  type TeamView,
} from "../../../state/chat/teamState";
import { Loader, Users } from "../../primitives/icons";

/**
 * The composer's Team mode switch and its popover.
 *
 * One button arms the whole workflow, because that is the promise: an operator
 * who wants a reviewed change should not have to pick three providers first.
 * The role pickers below the switch are for the operator who *does* want to,
 * and each one only offers providers with a host-side login — a seat filled by
 * a provider nobody signed in to is a run that fails on its first hop.
 */
export function TeamModeControl({
  view,
  connectedProviders,
  busy,
  onStart,
  onStop,
  onChangeRole,
  onChangeLoops,
  onChangeAutoFix,
}: {
  view: TeamView;
  connectedProviders: ChatProvider[];
  busy: boolean;
  onStart: () => void;
  onStop: () => void;
  onChangeRole: (
    role: TeamRoleName,
    patch: { provider?: ChatProvider; model?: string; enabled?: boolean },
  ) => void;
  onChangeLoops: (loops: number) => void;
  onChangeAutoFix: (autoFix: boolean) => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  const options = teamProviderOptions(connectedProviders);

  return (
    <div ref={rootRef} class="codex-team-control-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={busy}
        class={`codex-team-control composer-pill ${
          view.enabled ? "bg-accent-purple/[0.18] text-accent-purple hover:bg-accent-purple/[0.24]" : ""
        }`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Team mode — ${view.status}`}
        title="Team mode — implementer, reviewer, and tester on one change"
      >
        {busy ? (
          <Loader class="h-4 w-4 flex-none animate-spin" />
        ) : (
          <Users class="h-4 w-4 flex-none" aria-hidden="true" />
        )}
        <span class="font-semibold">Team</span>
        {view.enabled && (
          <span class="rounded bg-accent-purple/25 px-1 py-0.5 text-[10px] leading-none tabular-nums">
            {view.loopsUsed}/{view.maxLoops}
          </span>
        )}
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute left-0 bottom-full z-40 mb-2
                 w-[min(23rem,calc(100vw-1.5rem))] sm:left-auto sm:right-0"
          role="dialog"
          aria-label="Team mode settings"
        >
          <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2">
            <div class="text-[12px] font-semibold text-ink-100">Team mode</div>
            <p class="mt-1 text-[11px] leading-4 text-ink-400">
              After every turn you send, a second agent reviews the diff and a third runs
              Playwright — both in this project, in companion chats of their own. Findings come
              back here automatically. Each loop costs two extra agent runs.
            </p>
          </div>

          <div class="px-3 py-2.5 space-y-2.5">
            <div class="flex items-center justify-between gap-2">
              <span class="text-[11px] uppercase tracking-[0.1em] text-ink-400">Status</span>
              <span class="truncate text-[11.5px] text-ink-100">{view.status}</span>
            </div>

            <RolePicker
              seat={view.implementer}
              options={options}
              busy={busy}
              lockedOn
              hint="The chat you type in."
              onChange={(patch) => onChangeRole("implementer", patch)}
            />
            <RolePicker
              seat={view.reviewer}
              options={options}
              busy={busy}
              hint={
                view.singleProvider
                  ? "Only one provider is connected, so the review runs in a fresh chat on the same one."
                  : "Reads the diff with fresh eyes and returns SHIP or FIX."
              }
              onChange={(patch) => onChangeRole("reviewer", patch)}
            />
            <RolePicker
              seat={view.tester}
              options={options}
              busy={busy}
              hint="Runs a Playwright pass and returns PASS or FAIL."
              onChange={(patch) => onChangeRole("tester", patch)}
            />

            <div class="flex items-center justify-between gap-3 border-t border-white/10 pt-2.5">
              <label class="flex min-w-0 cursor-pointer items-start gap-2">
                <input
                  type="checkbox"
                  checked={view.autoFix}
                  disabled={busy}
                  onChange={(event) =>
                    onChangeAutoFix((event.currentTarget as HTMLInputElement).checked)
                  }
                  class="mt-0.5 h-3.5 w-3.5 flex-none accent-violet-400"
                />
                <span class="text-[12px] leading-4 text-ink-100">
                  Send findings back automatically
                </span>
              </label>
              <label class="flex flex-none items-center gap-1.5">
                <span class="text-[11px] text-ink-400">Loops</span>
                <input
                  type="number"
                  inputMode="numeric"
                  min={TEAM_MIN_LOOPS}
                  max={TEAM_MAX_LOOPS}
                  value={view.maxLoops}
                  disabled={busy}
                  onChange={(event) =>
                    onChangeLoops(Number.parseInt((event.currentTarget as HTMLInputElement).value, 10))
                  }
                  class="h-8 w-14 rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12.5px] text-ink-50
                         focus:border-accent-blue/40 focus:outline-none"
                />
              </label>
            </div>

            <button
              type="button"
              disabled={busy}
              onClick={() => {
                setOpen(false);
                if (view.enabled) onStop();
                else onStart();
              }}
              class={`h-8 w-full rounded-md text-[12px] font-semibold disabled:cursor-not-allowed disabled:opacity-40 ${
                view.enabled
                  ? "border border-white/10 bg-white/[0.05] text-ink-100 hover:bg-white/[0.09]"
                  : "bg-accent-purple/20 text-accent-purple hover:bg-accent-purple/30"
              }`}
            >
              {view.enabled ? "Stop team mode" : "Start team mode"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function RolePicker({
  seat,
  options,
  busy,
  hint,
  lockedOn = false,
  onChange,
}: {
  seat: TeamRoleView;
  options: { value: ChatProvider; label: string }[];
  busy: boolean;
  hint: string;
  lockedOn?: boolean;
  onChange: (patch: { provider?: ChatProvider; model?: string; enabled?: boolean }) => void;
}) {
  return (
    <div class="flex items-start justify-between gap-3">
      <label class="flex min-w-0 items-start gap-2">
        <input
          type="checkbox"
          checked={seat.enabled}
          disabled={busy || lockedOn}
          onChange={(event) =>
            onChange({ enabled: (event.currentTarget as HTMLInputElement).checked })
          }
          class="mt-0.5 h-3.5 w-3.5 flex-none accent-violet-400 disabled:opacity-50"
        />
        <span class="min-w-0">
          <span class="block text-[12.5px] font-medium text-ink-100">{seat.label}</span>
          <span class="block text-[11px] leading-4 text-ink-400">{hint}</span>
        </span>
      </label>
      <select
        value={seat.provider}
        disabled={busy || options.length === 0}
        onChange={(event) =>
          onChange({ provider: (event.currentTarget as HTMLSelectElement).value as ChatProvider })
        }
        aria-label={`${seat.label} provider`}
        class="h-8 flex-none rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12px] text-ink-50
               focus:border-accent-blue/40 focus:outline-none disabled:opacity-50"
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );
}
