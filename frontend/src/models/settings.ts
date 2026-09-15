import type { LanguageChoice } from "../i18n/locale.ts";
import type {
  ApprovalPolicy,
  ChatMode,
  ChatProvider,
  ReasoningEffort,
  SandboxPolicy,
  ServiceTier,
} from "./chat";

export type AppearanceTheme = "system" | "dark" | "light";

/** The interface language; "auto" follows the browser. See src/i18n/locale.ts. */
export type { LanguageChoice };

export interface AppearanceSettings {
  theme: AppearanceTheme;
  language: LanguageChoice;
}

export interface ChatSettings {
  provider: ChatProvider;
  model: string;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
  approvalPolicy: ApprovalPolicy;
  sandboxPolicy: SandboxPolicy;
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
  /** Preferences for loose chats running on the host. */
  chat: ChatSettings;
  agent: AgentUserSettings;
  /** Preferences for chats running inside a project container. */
  projectChat: ChatSettings;
  updatedAt?: number;
}

export interface UpdateUserSettingsInput {
  appearance?: Partial<AppearanceSettings>;
  chat?: Partial<ChatSettings>;
  agent?: Partial<AgentUserSettings>;
  projectChat?: Partial<ChatSettings>;
}
