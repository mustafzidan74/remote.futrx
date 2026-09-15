// Translation at the JSX boundary.
//
// Components keep their English literals exactly as upstream wrote them; the
// JSX runtime shim passes every element through `localizeProps` on its way to
// Preact. That is what lets the fork re-sync upstream without hand-merging a
// `t()` call into every string in every component.

import {
  NUMBER_PLACEHOLDER,
  TEXT_PLACEHOLDER,
  fill,
  keyOf,
  selectPlural,
  type Catalog,
  type CatalogEntry,
  type KeyedText,
} from "./normalize.ts";

/**
 * The locale the shim renders. `qps` is a development pseudo-locale: it
 * brackets every string the shim reaches, so text that shows up without
 * brackets is text the shim never sees and needs a `t()` call instead.
 */
export type Locale = "en" | "ar" | "qps";

/** A catalog key with `{s}` slots, compiled for matching. */
interface Pattern {
  key: string;
  /** Literal text around the slots, in order; one more than there are slots. */
  literals: string[];
  regex: RegExp;
  /** The longest literal, checked with `includes` before the regex runs. */
  hint: string;
}

interface PatternMatch {
  pattern: Pattern;
  captures: string[];
}

interface ActiveTranslations {
  locale: Exclude<Locale, "en">;
  catalog: Catalog;
  plurals: Intl.PluralRules;
  patterns: Pattern[];
  /** Normalised key → pattern match (null for none), so a miss is paid once. */
  matches: Map<string, PatternMatch | null>;
}

/** Enough for every distinct composed string one session renders. */
const MATCH_CACHE_LIMIT = 4000;

let active: ActiveTranslations | null = null;

/** Switch what the shim renders. English deactivates it entirely. */
export function setActiveTranslations(locale: Locale, catalog: Catalog): void {
  active =
    locale === "en"
      ? null
      : {
          locale,
          catalog,
          plurals: new Intl.PluralRules(locale === "qps" ? "en" : locale),
          patterns: compilePatterns(catalog),
          matches: new Map(),
        };
}

export function activeLocale(): Locale {
  return active?.locale ?? "en";
}

/** Translate one string, or return it unchanged when there is nothing to do. */
export function t(text: string): string {
  return lookup(text) ?? text;
}

/** The translation of a string, or null when the catalog has none. */
function lookup(text: string): string | null {
  if (!active) return null;
  const keyed = keyOf(text);
  if (!keyed) return null;
  if (active.locale === "qps") {
    // A component can pass a prop it received on to a host element, so the
    // same string may reach the shim twice; bracket it once.
    const core = text.trim();
    return core.startsWith("⟦") ? null : `${keyed.leading}⟦${core}⟧${keyed.trailing}`;
  }
  const entry = active.catalog[keyed.key];
  if (entry !== undefined) {
    return keyed.leading + fill(pick(entry, keyed.values[0]), keyed.values) + keyed.trailing;
  }
  const match = matchPattern(keyed.key);
  if (!match) return null;
  return keyed.leading + renderPattern(match, keyed) + keyed.trailing;
}

function pick(entry: CatalogEntry, count: string | undefined): string {
  return typeof entry === "string" ? entry : selectPlural(entry, count, (active as ActiveTranslations).plurals);
}

function compilePatterns(catalog: Catalog): Pattern[] {
  const patterns: Pattern[] = [];
  for (const key of Object.keys(catalog)) {
    if (!key.includes(TEXT_PLACEHOLDER)) continue;
    const literals = key.split(TEXT_PLACEHOLDER);
    // Two slots with nothing between them cannot be told apart.
    if (literals.slice(1, -1).some((literal) => literal === "")) continue;
    const hint = literals.reduce((longest, literal) => (literal.length > longest.length ? literal : longest), "");
    const source = literals.map((literal) => literal.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("(.+?)");
    patterns.push({ key, literals, regex: new RegExp(`^${source}$`), hint });
  }
  // The most specific pattern wins: more literal text first.
  return patterns.sort((a, b) => b.literals.join("").length - a.literals.join("").length);
}

function matchPattern(key: string): PatternMatch | null {
  const state = active as ActiveTranslations;
  const cached = state.matches.get(key);
  if (cached !== undefined) return cached;
  let found: PatternMatch | null = null;
  for (const pattern of state.patterns) {
    if (pattern.hint && !key.includes(pattern.hint)) continue;
    const result = pattern.regex.exec(key);
    if (result) {
      found = { pattern, captures: result.slice(1) };
      break;
    }
  }
  if (state.matches.size >= MATCH_CACHE_LIMIT) state.matches.clear();
  state.matches.set(key, found);
  return found;
}

/**
 * Numbers in the key belong either to the pattern's own text (they fill its
 * `{n}`) or to a slot's capture (they go back into that capture before it is
 * translated in turn). Walking the key in order tells which is which.
 */
function renderPattern(match: PatternMatch, keyed: KeyedText): string {
  const { pattern, captures } = match;
  const ownValues: string[] = [];
  const texts: string[] = [];
  let valueIndex = 0;
  const takeNumbers = (segment: string, into: string[]) => {
    let at = segment.indexOf(NUMBER_PLACEHOLDER);
    while (at !== -1) {
      into.push(keyed.values[valueIndex++] ?? NUMBER_PLACEHOLDER);
      at = segment.indexOf(NUMBER_PLACEHOLDER, at + NUMBER_PLACEHOLDER.length);
    }
  };
  pattern.literals.forEach((literal, index) => {
    takeNumbers(literal, ownValues);
    if (index >= captures.length) return;
    const captureValues: string[] = [];
    takeNumbers(captures[index], captureValues);
    texts.push(t(fill(captures[index], captureValues)));
  });
  const entry = (active as ActiveTranslations).catalog[pattern.key] as CatalogEntry;
  return fill(pick(entry, ownValues[0]), ownValues, texts);
}

const FIRST_STRONG_ISOLATE = "\u2068";
const POP_DIRECTIONAL_ISOLATE = "\u2069";
const LATIN = /[A-Za-z]/;
const RIGHT_TO_LEFT = /[\u0590-\u08FF\uFB1D-\uFDFF\uFE70-\uFEFF]/;

/**
 * Text as the page shows it. English left untranslated inside the Arabic
 * interface (a product name, an error from a CLI, a string upstream added
 * since the last catalog update) is wrapped in a Unicode first-strong isolate,
 * so it keeps its own left-to-right order: "exit: killed" instead of
 * ":exit: killed" with the colon thrown to the far end.
 */
function display(text: string): string {
  const translated = t(text);
  if (translated !== text || !active || active.locale !== "ar") return translated;
  if (!LATIN.test(text) || RIGHT_TO_LEFT.test(text) || text.startsWith(FIRST_STRONG_ISOLATE)) return text;
  return FIRST_STRONG_ISOLATE + text + POP_DIRECTIONAL_ISOLATE;
}

/** Attributes a browser shows to people on a native element. */
const HOST_TEXT_PROPS = ["title", "placeholder", "aria-label", "aria-description", "aria-placeholder", "alt"];

/** Props components conventionally render as visible text. */
const COMPONENT_TEXT_PROPS = [
  "title",
  "label",
  "placeholder",
  "description",
  "hint",
  "message",
  "text",
  "subtitle",
  "tooltip",
  "note",
  "capNote",
  "sub",
  "confirmLabel",
  "cancelLabel",
  "pendingLabel",
  "aria-label",
];

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
    // Isolation marks only where text meets the DOM: a component's props may
    // be compared or used as ids before they are rendered.
    const translated = host ? display(value) : t(value);
    if (translated !== value) (next ??= { ...props })[name] = translated;
  }

  if (!(host && isVerbatim(type as string, props)) && "children" in props) {
    const children = props.children;
    const translated = localizeChildren(children, host);
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

function localizeChildren(children: unknown, host: boolean): unknown {
  if (typeof children === "string") return host ? display(children) : t(children);
  if (!Array.isArray(children)) return children;
  const merged = new Set<number>();
  let next: unknown[] | null = mergeTextRuns(children, merged);
  for (let index = 0; index < children.length; index++) {
    if (merged.has(index)) continue;
    const child = children[index];
    const translated = localizeChildren(child, host);
    if (translated !== child) (next ??= children.slice())[index] = translated;
  }
  return next ?? children;
}

/** Children that render as text. */
function isText(child: unknown): child is string | number {
  return typeof child === "string" || typeof child === "number";
}

/** `false`, `null` and `undefined` render nothing, so they do not break a run of text. */
function isInvisible(child: unknown): boolean {
  return child === null || child === undefined || typeof child === "boolean";
}

/**
 * `{count} link{count === 1 ? "" : "s"}` reaches the runtime as three
 * children. Read together they are one catalog key ("{n} links"), so a run of
 * adjacent text children is looked up as a whole first and only translated
 * piece by piece when the whole has no entry. The translation takes the run's
 * first slot and the rest become empty strings, so the array keeps its shape.
 */
function mergeTextRuns(children: unknown[], merged: Set<number>): unknown[] | null {
  let next: unknown[] | null = null;
  let index = 0;
  while (index < children.length) {
    if (!isText(children[index])) {
      index++;
      continue;
    }
    const members: number[] = [];
    let end = index;
    while (end < children.length && (isText(children[end]) || isInvisible(children[end]))) {
      if (isText(children[end])) members.push(end);
      end++;
    }
    if (members.length > 1 && members.some((member) => typeof children[member] === "string")) {
      const translated = lookup(members.map((member) => String(children[member])).join(""));
      if (translated !== null) {
        const copy: unknown[] = (next ??= children.slice());
        members.forEach((member, position) => {
          copy[member] = position === 0 ? translated : "";
          merged.add(member);
        });
      }
    }
    index = end;
  }
  return next;
}
