import {
  MAX_EXTRA_INSTRUCTIONS_LENGTH,
  MAX_REPLY_LANGUAGE_LENGTH,
  type AgentPreferences,
  type UpdateAgentPreferencesInput,
} from "../../models/agentPreferences.ts";

/**
 * Editing state for the admin "Reply preferences" panel.
 *
 * The page edits a draft and submits the whole document, so everything that
 * decides what the draft looks like — and whether it can be saved — lives here
 * and the panel stays a form.
 */

/**
 * The languages the picker offers. `custom` is not a language: it is the
 * escape hatch that reveals a free-text field, because an operator may want a
 * dialect nobody enumerated.
 */
export const REPLY_LANGUAGE_OPTIONS: Array<{ value: string; label: string; hint?: string }> = [
  { value: "auto", label: "Match the user", hint: "Reply in whatever language the prompt used." },
  { value: "en", label: "English" },
  { value: "ar", label: "Arabic (Modern Standard)", hint: "العربية الفصحى المبسطة" },
  { value: "ar-EG", label: "Egyptian Arabic", hint: "عامية مصرية مبسطة" },
];

export const CUSTOM_LANGUAGE_VALUE = "custom";

export const REPLY_TONE_OPTIONS: Array<{
  value: AgentPreferences["tone"];
  label: string;
  hint: string;
}> = [
  { value: "default", label: "Default", hint: "Leave the agent's own verbosity alone." },
  { value: "concise", label: "Concise", hint: "Ask for short, direct answers." },
  { value: "detailed", label: "Detailed", hint: "Ask for reasoning and trade-offs." },
];

export const APPLY_TO_OPTIONS: Array<{
  value: AgentPreferences["applyTo"];
  label: string;
  hint: string;
}> = [
  { value: "all", label: "Every project", hint: "Existing projects and loose chats included." },
  {
    value: "newProjectsOnly",
    label: "New projects only",
    hint: "Projects created after this was last saved. Loose chats are excluded.",
  },
];

const BUILT_IN_LANGUAGES = new Set(REPLY_LANGUAGE_OPTIONS.map((option) => option.value));

/** Reports whether a stored language needs the custom free-text field. */
export function isCustomLanguage(language: string): boolean {
  return language.trim() !== "" && !BUILT_IN_LANGUAGES.has(language.trim());
}

/** The value the select should show for a stored language. */
export function languageSelectValue(language: string): string {
  const trimmed = language.trim();
  if (trimmed === "") return "auto";
  return BUILT_IN_LANGUAGES.has(trimmed) ? trimmed : CUSTOM_LANGUAGE_VALUE;
}

/** The request body for a draft: the whole document, trimmed. */
export function preferencesRequest(draft: AgentPreferences): UpdateAgentPreferencesInput {
  return {
    replyLanguage: draft.replyLanguage.trim() || "auto",
    tone: draft.tone,
    extraInstructions: draft.extraInstructions.trim(),
    applyTo: draft.applyTo,
  };
}

/**
 * Why a draft cannot be submitted as it stands, or null. The caps mirror the
 * backend's, so a rejection is shown before a round trip rather than after.
 */
export function preferencesProblem(draft: AgentPreferences): string | null {
  const language = draft.replyLanguage.trim();
  if (language.length > MAX_REPLY_LANGUAGE_LENGTH) {
    return `Language must be ${MAX_REPLY_LANGUAGE_LENGTH} characters or fewer.`;
  }
  if (/[\n\r]/.test(language)) {
    return "Language must be a single line.";
  }
  if (draft.extraInstructions.length > MAX_EXTRA_INSTRUCTIONS_LENGTH) {
    return `Extra instructions must be ${MAX_EXTRA_INSTRUCTIONS_LENGTH} characters or fewer.`;
  }
  return null;
}

/** Whether the draft differs from what the server last returned. */
export function preferencesDirty(draft: AgentPreferences, stored: AgentPreferences): boolean {
  return (
    JSON.stringify(preferencesRequest(draft)) !== JSON.stringify(preferencesRequest(stored))
  );
}

/**
 * A one-line summary of what the agents will be told, so an admin can see the
 * effect of the form without opening a container. It intentionally mirrors the
 * backend's own phrasing in service/agentprefs.
 */
export function preferencesSummary(draft: AgentPreferences): string {
  const clauses: string[] = [];
  const language = languageClause(draft.replyLanguage);
  if (language) {
    clauses.push(language);
    clauses.push("keep code, identifiers, commands and file paths in English");
  }
  if (draft.tone === "concise") clauses.push("be concise");
  if (draft.tone === "detailed") {
    clauses.push("be thorough — explain your reasoning and call out trade-offs");
  }
  if (clauses.length === 0) {
    return draft.extraInstructions.trim()
      ? "Only your extra instructions are injected."
      : "Nothing is injected — agents keep their own defaults.";
  }
  return `${clauses.join("; ")}.`;
}

function languageClause(language: string): string {
  const trimmed = language.trim();
  switch (trimmed) {
    case "":
    case "auto":
      return "";
    case "en":
      return "Reply in English unless the user writes in another language";
    case "ar":
      return "Reply in Modern Standard Arabic (العربية الفصحى المبسطة) unless the user writes in another language";
    case "ar-EG":
      return "Reply in Egyptian Arabic (عامية مصرية مبسطة) unless the user writes in another language";
    default:
      return `Reply in ${trimmed} unless the user writes in another language`;
  }
}
