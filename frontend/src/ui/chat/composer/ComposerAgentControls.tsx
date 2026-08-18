import type { ChatProvider, SelectedSkill } from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { modelOptionsForProvider } from "../../../config/chat";
import { ComposerModelPicker } from "./ComposerModelPicker";
import { PlaybookPicker } from "./PlaybookPicker";
import { ProviderToggle } from "./ProviderToggle";
import { SkillPicker } from "./SkillPicker";

export function ComposerAgentControls({
  projectId,
  model,
  provider,
  streaming,
  selectedSkills,
  playbooks,
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
  onSelectSkill: (skill: RegisteredSkill) => void;
  onProviderChange: (provider: ChatProvider) => void;
  onModelChange: (model: string) => void;
}) {
  const modelOptions = modelOptionsForProvider(provider);
  const selectedCount = selectedSkills.length;
  return (
    <div class="codex-composer-agent-controls flex min-w-0 flex-wrap items-center gap-1">
      <ProviderToggle
        provider={provider}
        streaming={streaming}
        onChange={onProviderChange}
      />

      <ComposerModelPicker
        provider={provider}
        model={model}
        streaming={streaming}
        options={modelOptions}
        onChange={onModelChange}
      />

      <SkillPicker
        provider={provider}
        projectId={projectId}
        selectedCount={selectedCount}
        onSelect={(skill) => onSelectSkill(skill)}
      />

      <PlaybookPicker library={playbooks} disabled={playbooks.running !== null} />
    </div>
  );
}
