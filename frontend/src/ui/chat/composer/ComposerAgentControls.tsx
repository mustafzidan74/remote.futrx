import type { ChatProvider, SelectedSkill } from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { MoreHorizontal } from "../../primitives/icons";
import { PlaybookPicker } from "./PlaybookPicker";
import { ProviderModelPill } from "./ProviderModelPill";
import { SkillPicker } from "./SkillPicker";
import { TestMenu } from "./TestMenu";

/**
 * The always-visible half of the composer toolbar: who is answering, what it
 * has been given to work with, and the one-click checks.
 *
 * A phone only has room for the agent pill, so everything after it collapses
 * behind an overflow button rather than wrapping the toolbar onto three rows.
 */
export function ComposerAgentControls({
  projectId,
  model,
  provider,
  streaming,
  selectedSkills,
  playbooks,
  testDisabled,
  secondaryOpen,
  onToggleSecondary,
  onSendTest,
  onSelectSkill,
  onProviderChange,
  onModelChange,
}: {
  projectId?: string;
  model: string;
  provider: ChatProvider;
  streaming: boolean;
  selectedSkills: SelectedSkill[];
  playbooks: PlaybookLibrary;
  testDisabled: boolean;
  /** Whether the phone-sized overflow group is showing. Ignored from `md` up. */
  secondaryOpen: boolean;
  onToggleSecondary: () => void;
  onSendTest: (prompt: string) => void;
  onSelectSkill: (skill: RegisteredSkill) => void;
  onProviderChange: (provider: ChatProvider) => void;
  onModelChange: (model: string) => void;
}) {
  return (
    <div class="codex-composer-agent-controls flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
      <ProviderModelPill
        provider={provider}
        model={model}
        streaming={streaming}
        onProviderChange={onProviderChange}
        onModelChange={onModelChange}
      />

      <div
        id="composer-overflow-controls"
        class={`min-w-0 flex-wrap items-center gap-1.5 md:flex ${secondaryOpen ? "flex" : "hidden"}`}
      >
        <SkillPicker
          provider={provider}
          projectId={projectId}
          selectedCount={selectedSkills.length}
          onSelect={(skill) => onSelectSkill(skill)}
        />
        <PlaybookPicker library={playbooks} disabled={playbooks.running !== null} />
        {/* A check is an ordinary prompt, so the menu is dead while the run
            lock is held — exactly like the Send button beside it. */}
        <TestMenu disabled={testDisabled} onSendTest={onSendTest} />
      </div>

      <button
        type="button"
        onClick={onToggleSecondary}
        class={`composer-pill composer-pill-icon md:hidden ${secondaryOpen ? "composer-pill-active" : ""}`}
        aria-controls="composer-overflow-controls"
        aria-expanded={secondaryOpen}
        aria-label={secondaryOpen ? "Hide skills, playbooks and tests" : "Show skills, playbooks and tests"}
        title="Skills, playbooks and tests"
      >
        <MoreHorizontal class="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  );
}
