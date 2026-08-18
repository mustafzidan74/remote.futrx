/**
 * Voice-input catalogue. Dictation language is a BCP-47 tag because that is
 * what the Web Speech API takes; the server fallback reduces it to the
 * ISO-639-1 subtag its provider wants.
 */
export type VoiceLanguage = "auto" | "ar-EG" | "ar-SA" | "en-US" | "en-GB";

export interface VoiceLanguageOption {
  value: VoiceLanguage;
  label: string;
}

/**
 * Arabic first: this platform's operators dictate in Arabic far more often
 * than in English, and "auto" is a fallback rather than a good default —
 * Chrome's auto mode resolves to the browser UI language, which is frequently
 * English on a machine whose owner speaks Arabic.
 */
export const VOICE_LANGUAGE_OPTIONS: readonly VoiceLanguageOption[] = [
  { value: "ar-EG", label: "العربية (مصر)" },
  { value: "ar-SA", label: "العربية (السعودية)" },
  { value: "en-US", label: "English (US)" },
  { value: "en-GB", label: "English (UK)" },
  { value: "auto", label: "Auto (browser language)" },
];

export const VALID_VOICE_LANGUAGES = new Set<string>(
  VOICE_LANGUAGE_OPTIONS.map((option) => option.value),
);

/** Where the per-device language choice is remembered. */
export const VOICE_LANGUAGE_STORAGE_KEY = "remote.futrx.voiceLanguage.v1";

/** Where the "prefer the server transcriber" choice is remembered. */
export const VOICE_ENGINE_STORAGE_KEY = "remote.futrx.voiceEngine.v1";

/** Which transcriber the mic button drives. */
export type VoiceEngine = "browser" | "server";

export const VALID_VOICE_ENGINES = new Set<string>(["browser", "server"]);

/**
 * The language a user who has never chosen one starts with. An Arabic browser
 * gets Egyptian Arabic; everyone else gets their own browser language, which
 * is the least surprising thing to do for a locale we do not list.
 */
export function defaultVoiceLanguage(browserLocale: string | undefined): VoiceLanguage {
  return (browserLocale ?? "").trim().toLowerCase().startsWith("ar") ? "ar-EG" : "auto";
}

/**
 * The tag handed to SpeechRecognition and to the server. "auto" resolves to
 * the browser's own language, which is what the Web Speech API defaults to
 * when `lang` is left unset.
 */
export function resolveVoiceLanguage(
  language: VoiceLanguage,
  browserLocale: string | undefined,
): string {
  return language === "auto" ? (browserLocale ?? "").trim() : language;
}

export function voiceLanguageLabel(language: VoiceLanguage): string {
  return VOICE_LANGUAGE_OPTIONS.find((option) => option.value === language)?.label ?? language;
}

/** The browser's own language tag, or undefined outside a browser. */
export function currentBrowserLocale(): string | undefined {
  return typeof navigator === "undefined" ? undefined : navigator.language;
}
