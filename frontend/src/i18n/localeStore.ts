// The active locale for this page: resolved once at boot, changed when the
// user's server-side setting arrives or changes, and applied to <html> and to
// the translation shim in one place.

import arCatalog from "./catalog/ar.json";
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
import { setActiveTranslations, type Locale } from "./translate.ts";

type Listener = (locale: Locale) => void;

const CATALOGS: Record<Exclude<Locale, "en">, Catalog> = {
  ar: arCatalog as Catalog,
  qps: {},
};

class LocaleStore {
  #locale: Locale = "en";
  #override: Locale | null = null;
  #listeners = new Set<Listener>();

  /** Resolve and apply the locale before the first render. */
  boot(): void {
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

export const localeStore = new LocaleStore();
