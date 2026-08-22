import type { ChatModelPolicy, ChatProvider, SelectedSkill } from "../../../models/chat";
import type { DirectModelChoice, DirectModelRef } from "../../../models/directModels";
import type { AgentEndpointChoice } from "../../../models/agentEndpoints";
import type { ComposerRoutingHint } from "./preferences";
import type { RegisteredSkill } from "../../../models/skill";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import type { SnippetLibrary } from "../../../state/hooks/chat/useSnippets";
import { MoreHorizontal } from "../../primitives/icons";
import { PlaybookPicker } from "./PlaybookPicker";
import { SnippetPicker } from "./SnippetPicker";
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
  modelPolicy,
  endpointId,
  endpointChoices,
  directModel,
  directChoices,
  onDirectModelChange,
  routing,
  streaming,
  selectedSkills,
  playbooks,
  snippets,
  draft,
  pendingSnippet,
  onPendingSnippetHandled,
  testDisabled,
  secondaryOpen,
  onToggleSecondary,
  onSendTest,
  onSelectSkill,
  onProviderChange,
  onModelChange,
  onModelPolicyChange,
  onEndpointChange,
}: {
  projectId?: string;
  model: string;
  provider: ChatProvider;
  modelPolicy: ChatModelPolicy;
  /** The third-party endpoint this chat is pointed at, "" for the default. */
  endpointId: string;
  /** Enabled third-party endpoints; empty hides the pill's section. */
  endpointChoices: AgentEndpointChoice[];
  directModel: DirectModelRef;
  directChoices: DirectModelChoice[];
  onDirectModelChange: (choice: DirectModelChoice | null) => void;
  /** What the next turn would be routed to, and whether Auto is on offer. */
  routing: ComposerRoutingHint;
  streaming: boolean;
  selectedSkills: SelectedSkill[];
  playbooks: PlaybookLibrary;
  /** This user's own saved prompts. */
  snippets: SnippetLibrary;
  /** The composer's current text, offered to the snippet editor. */
  draft: string;
  /** Text a message's "Save as snippet" action handed over, if any. */
  pendingSnippet?: string | null;
  onPendingSnippetHandled?: () => void;
  testDisabled: boolean;
  /** Whether the phone-sized overflow group is showing. Ignored from `md` up. */
  secondaryOpen: boolean;
  onToggleSecondary: () => void;
  onSendTest: (prompt: string) => void;
  onSelectSkill: (skill: RegisteredSkill) => void;
  onProviderChange: (provider: ChatProvider) => void;
  onModelChange: (model: string) => void;
  onModelPolicyChange: (policy: ChatModelPolicy) => void;
  onEndpointChange: (endpointId: string, cli: ChatProvider, model: string) => void;
}) {
  return (
    <div class="codex-composer-agent-controls flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
      <ProviderModelPill
        provider={provider}
        model={model}
        modelPolicy={modelPolicy}
        endpointId={endpointId}
        endpointChoices={endpointChoices}
        directModel={directModel}
        directChoices={directChoices}
        onDirectModelChange={onDirectModelChange}
        routedNext={routing.decision}
        routingAvailable={routing.available}
        streaming={streaming}
        onProviderChange={onProviderChange}
        onModelChange={onModelChange}
        onModelPolicyChange={onModelPolicyChange}
        onEndpointChange={onEndpointChange}
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
        <SnippetPicker
          library={snippets}
          draft={draft}
          disabled={snippets.busy !== null}
          pendingBody={pendingSnippet}
          onPendingHandled={onPendingSnippetHandled}
        />
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
        aria-label={
          secondaryOpen
            ? "Hide skills, playbooks, snippets and tests"
            : "Show skills, playbooks, snippets and tests"
        }
        title="Skills, playbooks, snippets and tests"
      >
        <MoreHorizontal class="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  );
}
