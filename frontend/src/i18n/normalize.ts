// The catalog is keyed by the English text itself, so the runtime shim and the
// extractor have to agree exactly on what "the text" is. Both go through here.

/** One plural-bearing catalog entry, by CLDR plural category. */
export interface PluralEntry {
  zero?: string;
  one?: string;
  two?: string;
  few?: string;
  many?: string;
  other: string;
}

export type CatalogEntry = string | PluralEntry;
export type Catalog = Readonly<Record<string, CatalogEntry>>;

export interface KeyedText {
  /** The lookup key: trimmed, single-spaced, every number replaced by `{n}`. */
  key: string;
  /** The numbers the key replaced, in order, as they appeared. */
  values: string[];
  /** Whitespace around the text, restored around the translation. */
  leading: string;
  trailing: string;
}

/** The placeholder a number becomes in a key and in a translation. */
export const NUMBER_PLACEHOLDER = "{n}";

const NUMBER = /\d+(?:[.,]\d+)*/g;
const EDGES = /^(\s*)([\s\S]*?)(\s*)$/;
// Two Latin letters in a row: anything without them is a number, a symbol, a
// separator, or text already in another script, and has nothing to look up.
const TRANSLATABLE = /[A-Za-z]{2}/;

export function keyOf(text: string): KeyedText | null {
  const [, leading, core, trailing] = EDGES.exec(text) as RegExpExecArray;
  if (!TRANSLATABLE.test(core)) return null;
  const values: string[] = [];
  const key = core
    .replace(/\s+/g, " ")
    .replace(NUMBER, (value) => {
      values.push(value);
      return NUMBER_PLACEHOLDER;
    });
  return { key, values, leading, trailing };
}

/** Put the captured numbers back into a translated template, in order. */
export function fill(template: string, values: readonly string[]): string {
  let index = 0;
  return template.replace(/\{n\}/g, () => values[index++] ?? NUMBER_PLACEHOLDER);
}

/**
 * The form of a plural entry for a count. Arabic has six categories; an entry
 * may leave any but `other` out, and a missing one falls back to `other`.
 */
export function selectPlural(entry: PluralEntry, count: string | undefined, rules: Intl.PluralRules): string {
  const value = count === undefined ? NaN : Number(count.replace(/,/g, ""));
  if (Number.isNaN(value)) return entry.other;
  const category = rules.select(value) as keyof PluralEntry;
  return entry[category] ?? entry.other;
}

/**
 * JSX text as the compiler hands it to the runtime: lines trimmed where they
 * meet a line break, blank lines dropped, the rest joined by one space. The
 * extractor reads source text, so it needs this to arrive at the same string.
 */
export function jsxTextValue(raw: string): string {
  const lines = raw.split(/\r\n|\n|\r/);
  if (lines.length === 1) return raw;
  const kept: string[] = [];
  lines.forEach((line, index) => {
    let value = line;
    if (index > 0) value = value.replace(/^\s+/, "");
    if (index < lines.length - 1) value = value.replace(/\s+$/, "");
    if (value) kept.push(value);
  });
  return kept.join(" ");
}
