import type {
  ChatMeta,
  ChatMode,
  ChatProvider,
  ReasoningEffort,
  SelectedSkill,
  ServiceTier,
} from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import { useUserSettingsContext } from "../../context/UserSettingsContext";
import { chatPreferenceState } from "../../chat/chatPreferenceState";
import { useChatMetaActions } from "./useChatMetaActions";

export function useChatPreferences({
  chat,
  loadedMeta,
  refreshMeta,
}: {
  chat: ChatMeta;
  loadedMeta: ChatMeta | null;
  refreshMeta: () => Promise<void>;
}) {
  const { settings, setChatSettings } = useUserSettingsContext();
  const displayMeta = chatPreferenceState.resolveMeta(chat, loadedMeta, settings.chat);
  const displayProvider = displayMeta.provider;
  const displayMode = displayMeta.mode;
  const selectedSkills = displayMeta.selectedSkills || [];
  const metaActions = useChatMetaActions({ chatId: chat.id, refreshMeta });

  function changeProvider(provider: ChatProvider) {
    if (provider === displayProvider) return;
    metaActions.applyMeta({ provider, model: "", reasoningEffort: "", serviceTier: "", selectedSkills: [] });
    void setChatSettings({ provider, model: "", reasoningEffort: "", serviceTier: "" });
  }

  function selectSkill(skill: RegisteredSkill) {
    const next = chatPreferenceState.selectedSkill(skill, displayProvider);
    if (chatPreferenceState.includesSkill(selectedSkills, next, displayProvider)) return;
    metaActions.applyMeta({ selectedSkills: [...selectedSkills, next] });
  }

  function removeSelectedSkill(skill: SelectedSkill) {
    metaActions.applyMeta({
      selectedSkills: chatPreferenceState.withoutSkill(
        selectedSkills,
        skill,
        displayProvider
      ),
    });
  }

  function changeModel(model: string) {
    metaActions.applyMeta({ model });
    void setChatSettings({ model });
  }

  function changeMode(mode: ChatMode) {
    metaActions.applyMeta({ mode });
    void setChatSettings({ mode });
  }

  function changeReasoningEffort(reasoningEffort: ReasoningEffort) {
    metaActions.applyMeta({ reasoningEffort });
    void setChatSettings({ reasoningEffort });
  }

  function changeServiceTier(serviceTier: ServiceTier) {
    metaActions.applyMeta({ serviceTier });
    void setChatSettings({ serviceTier });
  }

  return {
    displayMeta,
    displayMode,
    selectedSkills,
    // Exposed for prompt sources that change several settings at once — the
    // Playbooks menu applies skills, mode, and provider in a single patch.
    applyMeta: metaActions.applyMeta,
    changeProvider,
    changeModel,
    changeMode,
    changeReasoningEffort,
    changeServiceTier,
    selectSkill,
    removeSelectedSkill,
  };
}
