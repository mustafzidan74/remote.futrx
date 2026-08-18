import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import {
  MODE_OPTIONS,
  reasoningEffortOptionsForProvider,
  serviceTierOptionsForProvider,
} from "../../../config/chat";
import { runOptionsAriaLabel, runOptionsSummary } from "../../../state/chat/runOptionsState";
import type { ChatPolicies } from "../../../state/hooks/chat/useChatPolicies";
import { Activity, ChevronDown, Cpu, MessageSquare, Settings } from "../../primitives/icons";
import { AutoTestControl } from "./AutoTestControl";
import { AutopilotControl } from "./AutopilotControl";
import { TeamModeControl } from "./TeamModeControl";
import { ComposerOptionDropdown } from "./ComposerOptionDropdown";
import type { ComposerPreferenceActions, ComposerPreferences } from "./preferences";

/**
 * Everything about *how* a prompt runs, behind one button.
 *
 * Thinking, Mode, Autopilot and Auto-test were four permanent controls in a
 * toolbar that already had eleven. They are set once and then left alone for
 * hours, so they belong behind a button whose label already says what they are
 * set to — the controls themselves are unchanged and keep their own popovers.
 */
export function RunOptionsPopover({
  preferences,
  preferenceActions,
  policies,
  streaming,
}: {
  preferences: ComposerPreferences;
  preferenceActions: ComposerPreferenceActions;
  policies: ChatPolicies;
  streaming: boolean;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const reasoningEffortOptions = reasoningEffortOptionsForProvider(preferences.provider);
  const serviceTierOptions = serviceTierOptionsForProvider(preferences.provider);
  const summaryState = {
    provider: preferences.provider,
    mode: preferences.mode,
    reasoningEffort: preferences.reasoningEffort,
    serviceTier: preferences.serviceTier,
    autopilot: policies.autopilot.enabled,
    autoTest: policies.autoTest,
    team: policies.team.enabled,
  };
  const active = policies.autopilot.enabled || policies.autoTest || policies.team.enabled;

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    // Captured and swallowed, so Escape closes the panel instead of cancelling
    // whatever the agent is doing.
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    window.addEventListener("keydown", closeOnEscape, true);
    return () => {
      window.removeEventListener("mousedown", closeOnOutsideClick);
      window.removeEventListener("keydown", closeOnEscape, true);
    };
  }, [open]);

  return (
    <div ref={rootRef} class="codex-run-options-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        class={`composer-pill max-w-[9.5rem] sm:max-w-[15rem] ${open ? "composer-pill-active" : ""} ${
          active ? "composer-pill-on" : ""
        }`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={runOptionsAriaLabel(summaryState)}
        title="Run options — thinking, mode, and what happens after a turn"
      >
        <Settings class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
        <span class="min-w-0 truncate font-semibold text-ink-100">
          {runOptionsSummary(summaryState)}
        </span>
        <ChevronDown class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
      </button>

      {open && (
        <div
          /* The controls hosted here open popovers of their own, which the
             shared surface would otherwise clip at its own rounded edge. */
          class="popover-surface theme-menu-surface absolute bottom-full left-0 z-40 mb-2
                 w-[min(23rem,calc(100vw-1.5rem))] overflow-visible sm:left-auto sm:right-0"
          role="dialog"
          aria-label="Run options"
        >
          <div class="popover-section">
            <div class="text-[12px] font-semibold text-ink-100">Run options</div>
            <p class="mt-1 text-[11px] leading-4 text-ink-400">
              How much the agent thinks, how far it may go on its own, and what happens once a
              turn settles.
            </p>
          </div>

          <div class="popover-section space-y-2.5">
            <OptionRow label="Mode" hint="What the agent is allowed to do without asking.">
              <ComposerOptionDropdown
                label="Mode"
                value={preferences.mode}
                options={MODE_OPTIONS}
                Icon={MessageSquare}
                onChange={preferenceActions.changeMode}
              />
            </OptionRow>

            {reasoningEffortOptions.length > 0 && (
              <OptionRow label="Thinking" hint="How long the model reasons before it answers.">
                <ComposerOptionDropdown
                  label="Thinking"
                  value={preferences.reasoningEffort}
                  options={reasoningEffortOptions}
                  disabled={streaming}
                  Icon={Activity}
                  onChange={preferenceActions.changeReasoningEffort}
                />
              </OptionRow>
            )}

            {serviceTierOptions.length > 0 && (
              <OptionRow label="Speed" hint="Which service tier the request is billed at.">
                <ComposerOptionDropdown
                  label="Speed"
                  value={preferences.serviceTier}
                  options={serviceTierOptions}
                  disabled={streaming}
                  Icon={Cpu}
                  onChange={preferenceActions.changeServiceTier}
                />
              </OptionRow>
            )}
          </div>

          <div class="popover-section">
            <div class="popover-label">After every turn</div>
            <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
              <AutopilotControl
                view={policies.autopilot}
                busy={policies.busy}
                onArm={policies.armAutopilot}
                onDisarm={policies.stopAutopilot}
                onSaveLimits={policies.saveAutopilotLimits}
              />
              <AutoTestControl
                enabled={policies.autoTest}
                busy={policies.busy}
                onChange={policies.setAutoTest}
              />
            </div>
          </div>

          <div class="popover-section">
            <div class="popover-label">Who works on it</div>
            <p class="mt-1 text-[11px] leading-4 text-ink-400">
              Turn one chat into a review chain: you brief the implementer, another provider
              reviews the diff, and a third runs the tests.
            </p>
            <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
              <TeamModeControl
                view={policies.team}
                connectedProviders={policies.connectedProviders}
                busy={policies.busy}
                onStart={policies.startTeam}
                onStop={policies.stopTeam}
                onChangeRole={policies.setTeamRole}
                onChangeLoops={policies.setTeamLoops}
                onChangeAutoFix={policies.setTeamAutoFix}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function OptionRow({
  label,
  hint,
  children,
}: {
  label: string;
  hint: string;
  children: ComponentChildren;
}) {
  return (
    <div class="flex items-center justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[12.5px] font-medium text-ink-100">{label}</div>
        <div class="text-[11px] leading-4 text-ink-400">{hint}</div>
      </div>
      <div class="flex-none">{children}</div>
    </div>
  );
}
