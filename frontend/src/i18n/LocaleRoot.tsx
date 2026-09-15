import { useEffect, useState } from "preact/hooks";
import { App } from "../app/App";
import { localeStore } from "./localeStore.ts";

/**
 * Remounts the app when the locale changes. Translated text is fixed at the
 * moment an element is created, so a language switch needs every component to
 * render again; a remount is the one way to guarantee that without threading a
 * locale dependency through components that must stay identical to upstream.
 * It happens only on an actual switch, which is rare.
 */
export function LocaleRoot() {
  const [locale, setLocale] = useState(localeStore.locale);
  useEffect(() => localeStore.subscribe(setLocale), []);
  return <App key={locale} />;
}
