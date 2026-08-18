import type { ChatMode, ChatProvider, ReasoningEffort, ServiceTier } from "./chat";

export type AppearanceTheme = "system" | "dark" | "light";

export interface AppearanceSettings {
  theme: AppearanceTheme;
}

export interface ChatSettings {
  provider: ChatProvider;
  model: string;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
}

/**
 * This user's personal overrides of the platform agent preferences. Only the
 * reply language is personal: tone and house rules are platform policy.
 */
export interface AgentUserSettings {
  /**
   * Empty means "follow the platform setting". "auto" is a real choice meaning
   * "mirror whatever I write in", and it overrides the platform value.
   */
  replyLanguage: string;
}

export interface UserSettings {
  appearance: AppearanceSettings;
  chat: ChatSettings;
  agent: AgentUserSettings;
  updatedAt?: number;
}

export interface UpdateUserSettingsInput {
  appearance?: Partial<AppearanceSettings>;
  chat?: Partial<ChatSettings>;
  agent?: Partial<AgentUserSettings>;
}
