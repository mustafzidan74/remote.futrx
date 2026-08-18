/**
 * Platform-wide agent reply preferences.
 *
 * The admin document decides what language every agent answers in, how verbose
 * it is, and any house rules every project inherits. A user may override the
 * language for their own runs through their personal settings; everything else
 * is platform policy.
 */

export type AgentReplyTone = "default" | "concise" | "detailed";
export type AgentPreferencesScope = "all" | "newProjectsOnly";

export interface AgentPreferences {
  /** A built-in id ("auto", "en", "ar", "ar-EG") or a custom language label. */
  replyLanguage: string;
  tone: AgentReplyTone;
  extraInstructions: string;
  applyTo: AgentPreferencesScope;
  updatedAt?: number;
  updatedBy?: string;
}

export type UpdateAgentPreferencesInput = Partial<
  Pick<AgentPreferences, "replyLanguage" | "tone" | "extraInstructions" | "applyTo">
>;

/** Mirrors the backend caps in service/agentprefs, so the form fails locally. */
export const MAX_EXTRA_INSTRUCTIONS_LENGTH = 4000;
export const MAX_REPLY_LANGUAGE_LENGTH = 64;

export const DEFAULT_AGENT_PREFERENCES: AgentPreferences = {
  replyLanguage: "auto",
  tone: "default",
  extraInstructions: "",
  applyTo: "all",
};
