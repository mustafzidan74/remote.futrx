import type {
  ChatMeta,
  ChatProvider,
  ResolvedChatMeta,
  SelectedSkill,
} from "../../../models/chat";
import type { ChatSettings } from "../../../models/settings";
import type { RegisteredSkill } from "../../../models/skill";

class ChatPreferenceState {
  resolveMeta(
    chat: ChatMeta,
    loadedMeta: ChatMeta | null,
    defaults: ChatSettings
  ): ResolvedChatMeta {
    const baseMeta = loadedMeta ?? chat;
    const selectedSkills = chat.selectedSkills ?? [];
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
      // Workspace chat upserts are the live cross-client source. Prefer their
      // preference fields over the one-time detail fetch so a selection made
      // on another browser is reflected while this chat remains open.
      provider: chat.provider || baseMeta.provider || defaults.provider,
      model: chat.model ?? baseMeta.model ?? defaults.model,
      mode: chat.mode || baseMeta.mode || defaults.mode,
      reasoningEffort:
        chat.reasoningEffort ?? baseMeta.reasoningEffort ?? defaults.reasoningEffort,
      serviceTier: chat.serviceTier ?? baseMeta.serviceTier ?? defaults.serviceTier,
      approvalPolicy:
        chat.approvalPolicy ?? baseMeta.approvalPolicy ?? defaults.approvalPolicy,
      sandboxPolicy:
        chat.sandboxPolicy ?? baseMeta.sandboxPolicy ?? defaults.sandboxPolicy,
      selectedSkills,
      // Pinned is the answer for a chat created before routing existed, and
      // is the default for a new one: the model the user picked is the model
      // that runs until they ask for Auto.
      modelPolicy: (chat.modelPolicy ?? baseMeta.modelPolicy) === "auto" ? "auto" : "pinned",
      // Deliberately not defaulted from the user's chat settings: pointing a
      // chat at a third party is a decision about one piece of work, never
      // something a new chat should inherit silently.
      endpointId: chat.endpointId ?? baseMeta.endpointId ?? "",
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
