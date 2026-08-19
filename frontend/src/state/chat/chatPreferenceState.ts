import type {
  ChatMeta,
  ChatMode,
  ChatModelPolicy,
  ChatProvider,
  ReasoningEffort,
  SelectedSkill,
  ServiceTier,
} from "../../models/chat";
import type { ChatSettings } from "../../models/settings";
import type { RegisteredSkill } from "../../models/skill";

export interface ResolvedChatMeta extends ChatMeta {
  provider: ChatProvider;
  model: string;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
  modelPolicy: ChatModelPolicy;
}

class ChatPreferenceState {
  resolveMeta(
    chat: ChatMeta,
    loadedMeta: ChatMeta | null,
    defaults: ChatSettings
  ): ResolvedChatMeta {
    const baseMeta = loadedMeta ?? chat;
    return {
      ...baseMeta,
      // The policies the server drives on its own — an autopilot round it
      // spent, a team hop it advanced — are read from the workspace copy,
      // which the workspace socket upserts on every metadata write. The
      // loaded copy is fetched once per chat and would otherwise freeze the
      // header pill at whatever the state was when the chat was opened.
      // They are spread in only when the workspace copy actually carries
      // them, so a chat resolved before the list has loaded keeps the shape
      // it had rather than gaining three undefined keys.
      ...(chat.autopilot ? { autopilot: chat.autopilot } : {}),
      ...(chat.autoTest ? { autoTest: chat.autoTest } : {}),
      ...(chat.team ? { team: chat.team } : {}),
      provider: baseMeta.provider || defaults.provider,
      model: baseMeta.model ?? defaults.model,
      mode: baseMeta.mode || defaults.mode,
      reasoningEffort: baseMeta.reasoningEffort ?? defaults.reasoningEffort,
      serviceTier: baseMeta.serviceTier ?? defaults.serviceTier,
      // Pinned is the answer for a chat created before routing existed, and
      // is the default for a new one: the model the user picked is the model
      // that runs until they ask for Auto.
      modelPolicy: baseMeta.modelPolicy === "auto" ? "auto" : "pinned",
    };
  }

  selectedSkill(skill: RegisteredSkill, defaultProvider: ChatProvider): SelectedSkill {
    return {
      name: skill.name,
      command: skill.command || skill.name,
      provider: skill.provider || defaultProvider,
      source: skill.source,
    };
  }

  includesSkill(
    selectedSkills: SelectedSkill[],
    skill: SelectedSkill,
    defaultProvider: ChatProvider
  ): boolean {
    const key = this.skillKey(skill, defaultProvider);
    return selectedSkills.some((selected) => this.skillKey(selected, defaultProvider) === key);
  }

  withoutSkill(
    selectedSkills: SelectedSkill[],
    skill: SelectedSkill,
    defaultProvider: ChatProvider
  ): SelectedSkill[] {
    const key = this.skillKey(skill, defaultProvider);
    return selectedSkills.filter((selected) => this.skillKey(selected, defaultProvider) !== key);
  }

  private skillKey(
    skill: SelectedSkill | RegisteredSkill,
    defaultProvider: ChatProvider
  ): string {
    const provider = skill.provider || defaultProvider;
    const command = (skill.command || skill.name).trim().toLowerCase();
    // Remote initially advertises this reserved skill from its built-in
    // catalog, then provisions the same skill into the project workspace.
    // Keep its identity stable across that source transition.
    const source = command === "scheduled-tasks" ? "remote" : skill.source || "";
    return `${provider}:${source.toLowerCase()}:${command}`;
  }
}

export const chatPreferenceState = new ChatPreferenceState();
