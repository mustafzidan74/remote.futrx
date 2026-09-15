// Translation at the JSX boundary.
//
// Components keep their English literals exactly as upstream wrote them; the
// JSX runtime shim passes every element through `localizeProps` on its way to
// Preact. That is what lets the fork re-sync upstream without hand-merging a
// `t()` call into every string in every component.

import { fill, keyOf, selectPlural, type Catalog } from "./normalize.ts";

/**
 * The locale the shim renders. `qps` is a development pseudo-locale: it
 * brackets every string the shim reaches, so text that shows up without
 * brackets is text the shim never sees and needs a `t()` call instead.
 */
export type Locale = "en" | "ar" | "qps";

interface ActiveTranslations {
  locale: Exclude<Locale, "en">;
  catalog: Catalog;
  plurals: Intl.PluralRules;
}

let active: ActiveTranslations | null = null;

/** Switch what the shim renders. English deactivates it entirely. */
export function setActiveTranslations(locale: Locale, catalog: Catalog): void {
  active =
    locale === "en"
      ? null
      : { locale, catalog, plurals: new Intl.PluralRules(locale === "qps" ? "en" : locale) };
}

export function activeLocale(): Locale {
  return active?.locale ?? "en";
}

/** Translate one string, or return it unchanged when there is nothing to do. */
export function t(text: string): string {
  if (!active) return text;
  const keyed = keyOf(text);
  if (!keyed) return text;
  if (active.locale === "qps") {
    // A component can pass a prop it received on to a host element, so the
    // same string may reach the shim twice; bracket it once.
    const core = text.trim();
    return core.startsWith("⟦") ? text : `${keyed.leading}⟦${core}⟧${keyed.trailing}`;
  }
  const entry = active.catalog[keyed.key];
  if (entry === undefined) return text;
  const template =
    typeof entry === "string" ? entry : selectPlural(entry, keyed.values[0], active.plurals);
  return keyed.leading + fill(template, keyed.values) + keyed.trailing;
}

/** Attributes a browser shows to people on a native element. */
const HOST_TEXT_PROPS = ["title", "placeholder", "aria-label", "aria-description", "aria-placeholder", "alt"];

/** Props components conventionally render as visible text. */
const COMPONENT_TEXT_PROPS = ["title", "label", "placeholder", "description", "hint", "message"];

/**
 * Elements whose text is content rather than interface: code, terminals and
 * anything the page marks as not-for-translation. Their children are left
 * exactly as they are.
 */
const VERBATIM_ELEMENTS = new Set(["pre", "code", "kbd", "samp", "textarea", "script", "style"]);

type Props = Record<string, unknown> | null | undefined;

/**
 * The props with their visible text translated. Returns the same object when
 * nothing changed, so an English render allocates nothing.
 */
export function localizeProps(type: unknown, props: Props): Props {
  if (!active || !props) return props;
  const host = typeof type === "string";
  let next: Record<string, unknown> | null = null;

  const names = host ? HOST_TEXT_PROPS : COMPONENT_TEXT_PROPS;
  for (const name of names) {
    const value = props[name];
    if (typeof value !== "string") continue;
    const translated = t(value);
    if (translated !== value) (next ??= { ...props })[name] = translated;
  }

  if (!(host && isVerbatim(type as string, props)) && "children" in props) {
    const children = props.children;
    const translated = localizeChildren(children);
    if (translated !== children) (next ??= { ...props }).children = translated;
  }

  return next ?? props;
}

/**
 * Content an element carries on its own behalf. A `dir` attribute is the
 * signal the chat and markdown renderers already set on user and agent text,
 * so it doubles as "this is someone's words, not a label".
 */
function isVerbatim(type: string, props: Record<string, unknown>): boolean {
  return (
    VERBATIM_ELEMENTS.has(type) ||
    props.translate === false ||
    props.translate === "no" ||
    props.dir !== undefined
  );
}

function localizeChildren(children: unknown): unknown {
  if (typeof children === "string") return t(children);
  if (!Array.isArray(children)) return children;
  let next: unknown[] | null = null;
  for (let index = 0; index < children.length; index++) {
    const child = children[index];
    const translated = localizeChildren(child);
    if (translated !== child) (next ??= children.slice())[index] = translated;
  }
  return next ?? children;
}
