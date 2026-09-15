// The active locale for this page: resolved once at boot, changed when the
// user's server-side setting arrives or changes, and applied to <html> and to
// the translation shim in one place.

import arCatalog from "./catalog/ar.json";
// Backend text the UI shows verbatim: error messages and dashboard alerts. Kept apart because the source
// check (npm run i18n:check) can only see frontend strings.
import arErrors from "./catalog/ar.errors.json";
// Text the app computes at runtime, which no source scan can list: relative
// ages and durations ("3w", "5m ago").
import arFormats from "./catalog/ar.formats.json";
import { applyFormattingLocale } from "./format.ts";
import { STORAGE_KEYS } from "../config/storageKeys.ts";
import { browserStorageService } from "../services/platform/browserStorageService.ts";
import {
  directionOf,
  documentLanguageOf,
  isLanguageChoice,
  localeOverride,
  resolveLocale,
  type LanguageChoice,
} from "./locale.ts";
import type { Catalog } from "./normalize.ts";
import { setActiveTranslations, t, type Locale } from "./translate.ts";

type Listener = (locale: Locale) => void;

const CATALOGS: Record<Exclude<Locale, "en">, Catalog> = {
  ar: { ...(arFormats as Catalog), ...(arErrors as Catalog), ...(arCatalog as Catalog) },
  qps: {},
};

class LocaleStore {
  #locale: Locale = "en";
  #override: Locale | null = null;
  #listeners = new Set<Listener>();

  /** Resolve and apply the locale before the first render. */
  boot(): void {
    translateNativeDialogs();
    this.#override = typeof location === "undefined" ? null : localeOverride(location.search);
    this.#apply(this.#resolve(this.remembered()));
  }

  get locale(): Locale {
    return this.#locale;
  }

  /** The choice this browser last applied, "auto" when nothing is cached. */
  remembered(): LanguageChoice {
    const stored = browserStorageService.readString(STORAGE_KEYS.languageChoice);
    return isLanguageChoice(stored) ? stored : "auto";
  }

  /**
   * Apply the user's choice. Cached locally so the next load paints in the
   * right language and direction before the server answers — the same
   * reasoning as the theme cache.
   */
  setChoice(choice: LanguageChoice): void {
    browserStorageService.writeString(STORAGE_KEYS.languageChoice, choice);
    this.#apply(this.#resolve(choice));
  }

  subscribe(listener: Listener): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  #resolve(choice: LanguageChoice): Locale {
    if (this.#override) return this.#override;
    const languages = typeof navigator === "undefined" ? [] : navigator.languages ?? [navigator.language];
    return resolveLocale(choice, languages);
  }

  #apply(locale: Locale): void {
    setActiveTranslations(locale, locale === "en" ? {} : CATALOGS[locale]);
    applyFormattingLocale(locale);
    if (typeof document !== "undefined") {
      const root = document.documentElement;
      root.lang = documentLanguageOf(locale);
      root.dir = directionOf(locale);
    }
    if (locale === this.#locale) return;
    this.#locale = locale;
    for (const listener of this.#listeners) listener(locale);
  }
}

/**
 * `alert("...")`, `confirm("...")` and `prompt("...")` never pass through JSX.
 * Their message goes through the catalog on the way to the browser instead;
 * in English `t` returns it untouched.
 */
function translateNativeDialogs(): void {
  if (typeof window === "undefined" || (window as { __remoteDialogsLocalized?: boolean }).__remoteDialogsLocalized) return;
  (window as { __remoteDialogsLocalized?: boolean }).__remoteDialogsLocalized = true;
  const localize = (message: unknown) => (typeof message === "string" ? t(message) : message);
  const { alert, confirm, prompt } = window;
  window.alert = (message?: unknown) => alert.call(window, localize(message));
  window.confirm = (message?: string) => confirm.call(window, localize(message) as string | undefined);
  window.prompt = (message?: string, fallback?: string) => prompt.call(window, localize(message) as string | undefined, fallback);
}

export const localeStore = new LocaleStore();
