import type { ChatMode, ChatProvider, ReasoningEffort, ServiceTier } from "../models/chat";
import type { AppearanceTheme, UserSettings } from "../models/settings";
import {
  CHAT_MODE_OPTIONS,
  CHAT_PROVIDER_OPTIONS,
  REASONING_EFFORT_OPTIONS,
  SERVICE_TIER_OPTIONS,
} from "./chatCatalog";

export const DEFAULT_USER_SETTINGS: UserSettings = {
  appearance: { theme: "system" },
  chat: { provider: "codex", model: "", mode: "code", reasoningEffort: "", serviceTier: "" },
  agent: { replyLanguage: "" },
};

export const VALID_APPEARANCE_THEMES = new Set<AppearanceTheme>([
  "system",
  "dark",
  "light",
]);
export const VALID_CHAT_PROVIDERS = new Set<ChatProvider>(
  CHAT_PROVIDER_OPTIONS
    .filter((option) => option.validInUserSettings)
    .map((option) => option.value),
);
export const VALID_CHAT_MODES = new Set<ChatMode>(
  CHAT_MODE_OPTIONS.map((option) => option.value),
);
export const VALID_REASONING_EFFORTS = new Set<ReasoningEffort>(
  REASONING_EFFORT_OPTIONS.map((option) => option.value),
);
export const VALID_SERVICE_TIERS = new Set<ServiceTier>(
  SERVICE_TIER_OPTIONS.map((option) => option.value),
);

export const SYSTEM_LIGHT_MEDIA_QUERY = "(prefers-color-scheme: light)";
