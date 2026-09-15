// Which locale to render, decided from the user's choice, the browser, and a
// URL override. Pure functions only; the store applies the result.

import type { Locale } from "./translate.ts";

/** What a user can pick in Settings → Appearance. */
export type LanguageChoice = "auto" | "en" | "ar";

export const LANGUAGE_CHOICES: readonly LanguageChoice[] = ["auto", "en", "ar"];

/** The query parameter that forces a locale for one page load (screenshots, QA). */
export const LOCALE_QUERY_PARAM = "lang";

const OVERRIDES: readonly Locale[] = ["en", "ar", "qps"];

export function isLanguageChoice(value: unknown): value is LanguageChoice {
  return typeof value === "string" && (LANGUAGE_CHOICES as readonly string[]).includes(value);
}

/**
 * The locale a choice resolves to. "auto" follows the first browser language
 * the app has a translation for, in the browser's own order of preference.
 */
export function resolveLocale(choice: LanguageChoice, browserLanguages: readonly string[]): "en" | "ar" {
  if (choice === "en" || choice === "ar") return choice;
  for (const language of browserLanguages) {
    const primary = language.toLowerCase().split("-")[0];
    if (primary === "ar") return "ar";
    if (primary === "en") return "en";
  }
  return "en";
}

/** A `?lang=` override, or null when the URL does not carry a supported one. */
export function localeOverride(search: string): Locale | null {
  const value = new URLSearchParams(search).get(LOCALE_QUERY_PARAM);
  return value && (OVERRIDES as readonly string[]).includes(value) ? (value as Locale) : null;
}

export function directionOf(locale: Locale): "rtl" | "ltr" {
  return locale === "ar" ? "rtl" : "ltr";
}

/** The `lang` attribute. The pseudo-locale is English text in brackets. */
export function documentLanguageOf(locale: Locale): string {
  return locale === "qps" ? "en" : locale;
}
