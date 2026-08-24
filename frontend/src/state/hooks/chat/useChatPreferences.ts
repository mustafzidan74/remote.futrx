import type {
  ChatMeta,
  ChatMode,
  ChatModelPolicy,
  ChatProvider,
  ReasoningEffort,
  SelectedSkill,
  ServiceTier,
} from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import { useUserSettingsContext } from "../../context/UserSettingsContext";
import { chatPreferenceState } from "../../chat/chatPreferenceState";
import { useChatMetaActions } from "./useChatMetaActions";
import { NO_DIRECT_MODEL, type DirectModelChoice } from "../../../models/directModels";

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
    // Naming an agent by hand pins the chat, the same way naming a model
    // does: the operator just answered the question routing was answering.
    metaActions.applyMeta({
      provider,
      model: "",
      reasoningEffort: "",
      serviceTier: "",
      selectedSkills: [],
      modelPolicy: "pinned",
    });
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
    // Picking a model by hand is how a chat leaves Auto: the operator just
    // said which model they want, so the routing policy stands down for this
    // chat until they ask for it back.
    metaActions.applyMeta({ model, modelPolicy: "pinned" });
    void setChatSettings({ model });
  }

  // Switching to Auto keeps the stored model untouched, so turning routing
  // back off returns the chat to the model it had rather than to Auto.
  function changeModelPolicy(modelPolicy: ChatModelPolicy) {
    if (modelPolicy === displayMeta.modelPolicy) return;
    metaActions.applyMeta({ modelPolicy });
  }

  /**
   * Points the chat at a third-party agent endpoint, or back at the vendor's
   * own with an empty id.
   *
   * All three fields move together because they are one choice: the endpoint
   * decides which CLI runs and which model ids that CLI may be asked for, and
   * a chat left on a first-party model id would ask the endpoint for a name
   * it has never heard of. The policy is pinned for the same reason it is
   * pinned when a model is chosen by hand — the operator just answered the
   * question routing was there to answer.
   *
   * Unlike changeProvider and changeModel this is deliberately *not* written
   * to the user's own defaults: a third-party endpoint is a per-chat choice
   * about one piece of work, not the setting every new chat should inherit.
   */
  function changeEndpoint(endpointId: string, cli: ChatProvider, model: string) {
    const nextProvider = endpointId ? cli : displayProvider;
    metaActions.applyMeta({
      endpointId,
      provider: nextProvider,
      model,
      modelPolicy: "pinned",
      // A skill list is provider-specific, and pointing a chat at another
      // CLI invalidates it exactly as changeProvider does.
      ...(nextProvider === displayProvider ? {} : { selectedSkills: [] }),
    });
  }

  /**
   * Points the chat at a completion-API model, or back at an agent with null.
   *
   * Like changeEndpoint this is per-chat and never written to the user's own
   * defaults: answering without tools is a choice about one conversation, not
   * the setting every new chat should start from. It also clears any endpoint,
   * because the two describe different run paths and a chat can only take one.
   */
  function changeDirectModel(choice: DirectModelChoice | null) {
    metaActions.applyMeta({
      directModel: choice
        ? { source: choice.source, providerId: choice.providerId || "", model: choice.model }
        : NO_DIRECT_MODEL,
      ...(choice ? { endpointId: "" } : {}),
    });
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
    changeModelPolicy,
    changeEndpoint,
    changeDirectModel,
    changeMode,
    changeReasoningEffort,
    changeServiceTier,
    selectSkill,
    removeSelectedSkill,
  };
}
