import {
  MODE_OPTIONS,
  reasoningEffortOptionsForProvider,
  serviceTierOptionsForProvider,
} from "../../../config/chat";
import type { ChatPolicies } from "../../../state/hooks/chat/useChatPolicies";
import { Activity, Cpu, MessageSquare } from "../../primitives/icons";
import { AutoTestControl } from "./AutoTestControl";
import { AutopilotControl } from "./AutopilotControl";
import { ComposerOptionDropdown } from "./ComposerOptionDropdown";
import { TestMenu } from "./TestMenu";
import type { ComposerPreferenceActions, ComposerPreferences } from "./preferences";

export function ComposerExecutionControls({
  preferences,
  preferenceActions,
  policies,
  canSendPrompt,
  streaming,
}: {
  preferences: ComposerPreferences;
  preferenceActions: ComposerPreferenceActions;
  policies: ChatPolicies;
  canSendPrompt: boolean;
  streaming: boolean;
}) {
  const reasoningEffortOptions = reasoningEffortOptionsForProvider(preferences.provider);
  const serviceTierOptions = serviceTierOptionsForProvider(preferences.provider);

  return (
    <div class="codex-composer-execution-controls flex min-w-0 flex-wrap items-center gap-1">
      {reasoningEffortOptions.length > 0 && (
        <ComposerOptionDropdown
          label="Thinking"
          value={preferences.reasoningEffort}
          options={reasoningEffortOptions}
          disabled={streaming}
          Icon={Activity}
          onChange={preferenceActions.changeReasoningEffort}
        />
      )}

      {serviceTierOptions.length > 0 && (
        <ComposerOptionDropdown
          label="Speed"
          value={preferences.serviceTier}
          options={serviceTierOptions}
          disabled={streaming}
          Icon={Cpu}
          onChange={preferenceActions.changeServiceTier}
        />
      )}

      <ComposerOptionDropdown
        label="Mode"
        value={preferences.mode}
        options={MODE_OPTIONS}
        Icon={MessageSquare}
        onChange={preferenceActions.changeMode}
      />

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

      {/* A check is an ordinary prompt, so the menu is dead while the run
          lock is held — exactly like the Send button beside it. */}
      <TestMenu disabled={!canSendPrompt || streaming} onSendTest={policies.sendTest} />
    </div>
  );
}
