// Dates and numbers in the active locale.
//
// Components format with `toLocaleString()` and friends and pass no locale,
// which means "the browser's language". Under the Arabic interface that must
// be Arabic whatever the browser says, and with Latin digits: ports, sizes and
// token counts are read next to terminal output. Rather than edit every call
// site upstream owns, the three Date methods and Number's are wrapped once: a
// call that names its own locale is left exactly as it was.

import type { Locale } from "./translate.ts";

/** The Intl locale each interface locale formats with; undefined = browser default. */
export function formattingLocaleOf(locale: Locale): string | undefined {
  return locale === "ar" ? "ar-EG-u-nu-latn" : undefined;
}

type Formatter = (this: unknown, locales?: string | string[], options?: object) => string;

let current: string | undefined;
let installed = false;

/** A call that leaves the choice to the browser: no locale, or an empty list. */
function unspecified(locales: unknown): boolean {
  return locales === undefined || (Array.isArray(locales) && locales.length === 0);
}

function wrap(prototype: object, name: string): void {
  const original = (prototype as Record<string, Formatter>)[name];
  Object.defineProperty(prototype, name, {
    configurable: true,
    writable: true,
    value: function (this: unknown, locales?: string | string[], options?: object): string {
      return original.call(this, unspecified(locales) && current ? current : locales, options);
    },
  });
}

/** Point default-locale formatting at the interface locale. */
export function applyFormattingLocale(locale: Locale): void {
  current = formattingLocaleOf(locale);
  if (installed || current === undefined) return;
  installed = true;
  for (const name of ["toLocaleString", "toLocaleDateString", "toLocaleTimeString"]) wrap(Date.prototype, name);
  wrap(Number.prototype, "toLocaleString");
}
