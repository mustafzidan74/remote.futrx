import { requestJson } from "./apiRequest";
import {
  type UpdateUserSettingsInput,
  type UserSettings,
} from "../models/settings";
import { API_ROUTES } from "../config/routes";
import {
  DEFAULT_USER_SETTINGS,
  VALID_APPEARANCE_THEMES,
  VALID_CHAT_MODES,
  VALID_CHAT_PROVIDERS,
  VALID_REASONING_EFFORTS,
  VALID_SERVICE_TIERS,
} from "../config/settings";

export const settingsApi = {
  fetch: async () =>
    normalizeUserSettings(
      await requestJson<UserSettings>("GET", API_ROUTES.settings)
    ),
  update: async (body: UpdateUserSettingsInput) =>
    normalizeUserSettings(
      await requestJson<UserSettings>("PATCH", API_ROUTES.settings, body)
    ),
};

function normalizeUserSettings(settings: UserSettings): UserSettings {
  const theme = settings?.appearance?.theme;
  const provider = settings?.chat?.provider;
  const mode = settings?.chat?.mode;
  const reasoningEffort = settings?.chat?.reasoningEffort;
  const serviceTier = settings?.chat?.serviceTier;
  return {
    ...DEFAULT_USER_SETTINGS,
    ...settings,
    appearance: {
      ...DEFAULT_USER_SETTINGS.appearance,
      ...settings?.appearance,
      theme: VALID_APPEARANCE_THEMES.has(theme)
        ? theme
        : DEFAULT_USER_SETTINGS.appearance.theme,
    },
    chat: {
      ...DEFAULT_USER_SETTINGS.chat,
      ...settings?.chat,
      provider: VALID_CHAT_PROVIDERS.has(provider)
        ? provider
        : DEFAULT_USER_SETTINGS.chat.provider,
      model:
        typeof settings?.chat?.model === "string"
          ? settings.chat.model
          : DEFAULT_USER_SETTINGS.chat.model,
      mode: VALID_CHAT_MODES.has(mode) ? mode : DEFAULT_USER_SETTINGS.chat.mode,
      reasoningEffort: VALID_REASONING_EFFORTS.has(reasoningEffort)
        ? reasoningEffort
        : DEFAULT_USER_SETTINGS.chat.reasoningEffort,
      serviceTier: VALID_SERVICE_TIERS.has(serviceTier)
        ? serviceTier
        : DEFAULT_USER_SETTINGS.chat.serviceTier,
    },
    agent: {
      ...DEFAULT_USER_SETTINGS.agent,
      ...settings?.agent,
      // A custom language label is free text, so the only shape check possible
      // here is "is it a string at all".
      replyLanguage:
        typeof settings?.agent?.replyLanguage === "string"
          ? settings.agent.replyLanguage
          : DEFAULT_USER_SETTINGS.agent.replyLanguage,
    },
  };
}
