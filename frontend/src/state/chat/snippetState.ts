import type {
  Snippet,
  SnippetAudience,
  SnippetInput,
  SnippetLanguage,
} from "../../models/snippet.ts";
import { resolvePlaceholders, type ResolvedPrompt } from "./playbookState.ts";

/**
 * Snippets: the state behind the personal prompt library and the client
 * message templates that share its store.
 *
 * Everything here is pure. The hooks own the side effects — calling the API,
 * writing the composer — and this module answers the questions a saved piece
 * of text raises: what does it say once its placeholders are filled in, which
 * entries match what the user is searching for, and what does a shortcut
 * refer to.
 */

/** Placeholders a snippet can be resolved against. */
export interface SnippetContext {
  projectName?: string;
  slug?: string;
  previewUrl?: string;
  portalUrl?: string;
  /** Defaults to the project name, which is what a solo freelancer means. */
  clientName?: string;
  /** The composer's current draft, substituted for `{{selection}}`. */
  selection?: string;
  /** Overrides today's date; the picker never sets it, tests do. */
  date?: string;
}

/** Every placeholder a snippet may carry, for the editor's help text. */
export const SNIPPET_PLACEHOLDERS = [
  "{{project}}",
  "{{projectName}}",
  "{{clientName}}",
  "{{slug}}",
  "{{previewUrl}}",
  "{{portalUrl}}",
  "{{date}}",
  "{{selection}}",
] as const;

/** The placeholder that stands for whatever is already in the composer. */
export const SELECTION_PLACEHOLDER = "selection";

/**
 * Resolves a snippet's text against the chat or project it is being used in.
 *
 * A name nobody can fill in right now — no project, no portal link, an empty
 * draft — is left in place verbatim, exactly as playbooks do, so the user sees
 * what the snippet still needs instead of sending a sentence with a hole in it.
 */
export function resolveSnippetText(text: string, context: SnippetContext = {}): ResolvedPrompt {
  const projectName = context.projectName;
  return resolvePlaceholders(text, {
    project: projectName,
    projectName,
    clientName: context.clientName || projectName,
    slug: context.slug,
    previewUrl: context.previewUrl,
    portalUrl: context.portalUrl,
    selection: context.selection,
    date: context.date || today(),
  });
}

/** The wording a client template uses for a language, with its fallbacks. */
export function snippetText(snippet: Snippet, language: SnippetLanguage = "en"): string {
  if (snippet.audience !== "client") return snippet.body;
  const variants = snippet.variants ?? {};
  const primary = language === "ar" ? variants.ar : variants.en;
  const secondary = language === "ar" ? variants.en : variants.ar;
  for (const candidate of [primary, secondary, snippet.body]) {
    if ((candidate ?? "").trim()) return candidate as string;
  }
  return "";
}

/** True when a snippet embeds the current draft rather than adding to it. */
export function usesSelection(text: string): boolean {
  return new RegExp(`\\{\\{\\s*${SELECTION_PLACEHOLDER}\\s*\\}\\}`).test(text);
}

/** Today, in the visitor's locale, for `{{date}}`. */
function today(): string {
  return new Date().toLocaleDateString(undefined, {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

/* ------------------------------------------------------------------ *
 * Listing
 * ------------------------------------------------------------------ */

/** Keeps only one audience; the composer and the client panel never mix. */
export function snippetsFor(snippets: Snippet[], audience: SnippetAudience): Snippet[] {
  return snippets.filter((snippet) => (snippet.audience || "agent") === audience);
}

/**
 * The order the server already returns — most used first — recomputed locally
 * so an insertion re-sorts the open menu without a round trip.
 */
export function sortSnippets(snippets: Snippet[]): Snippet[] {
  return [...snippets].sort((left, right) => {
    if ((right.uses ?? 0) !== (left.uses ?? 0)) return (right.uses ?? 0) - (left.uses ?? 0);
    if ((right.updatedAt ?? 0) !== (left.updatedAt ?? 0)) {
      return (right.updatedAt ?? 0) - (left.updatedAt ?? 0);
    }
    return left.title.localeCompare(right.title);
  });
}

/**
 * Filters by title, tag, shortcut, and body. The body is searched last and
 * only as a substring: a user who remembers a phrase but not the title should
 * still find their snippet, without that phrase outranking a title match.
 */
export function filterSnippets(snippets: Snippet[], query: string): Snippet[] {
  const term = query.trim().toLowerCase();
  if (!term) return [...snippets];
  const scored: { snippet: Snippet; rank: number; index: number }[] = [];
  snippets.forEach((snippet, index) => {
    const rank = rankSnippet(snippet, term);
    if (rank !== null) scored.push({ snippet, rank, index });
  });
  scored.sort((left, right) => left.rank - right.rank || left.index - right.index);
  return scored.map((item) => item.snippet);
}

function rankSnippet(snippet: Snippet, term: string): number | null {
  const title = snippet.title.toLowerCase();
  if (title.startsWith(term)) return 0;
  const shortcut = (snippet.shortcut || "").toLowerCase();
  if (shortcut && shortcut.startsWith(term)) return 1;
  if ((snippet.tags ?? []).some((tag) => tag.toLowerCase().startsWith(term))) return 2;
  if (title.includes(term)) return 3;
  const bodies = [snippet.body, snippet.variants?.ar ?? "", snippet.variants?.en ?? ""];
  if (bodies.some((body) => body.toLowerCase().includes(term))) return 4;
  return null;
}

/** Finds the snippet a `/s-<shortcut>` or `/snippet <word>` line refers to. */
export function findSnippetByShortcut(snippets: Snippet[], word: string): Snippet | null {
  const wanted = normalizeShortcut(word);
  if (!wanted) return null;
  return (
    snippets.find((snippet) => normalizeShortcut(snippet.shortcut || "") === wanted) ??
    snippets.find((snippet) => slugOf(snippet.title) === wanted) ??
    snippets.find((snippet) => snippet.id.toLowerCase() === wanted) ??
    null
  );
}

/** Accepts the spellings a user types: `wpfix`, `s-wpfix`, `/s-wpfix`. */
export function normalizeShortcut(value: string): string {
  return (value || "")
    .trim()
    .toLowerCase()
    .replace(/^\//, "")
    .replace(/^s-/, "")
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function slugOf(value: string): string {
  return (value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** A one-line preview of what a snippet will insert. */
export function snippetPreview(snippet: Snippet, language: SnippetLanguage = "en"): string {
  const text = snippetText(snippet, language).replace(/\s+/g, " ").trim();
  return text.length > 120 ? `${text.slice(0, 119)}…` : text;
}

/* ------------------------------------------------------------------ *
 * Editing
 * ------------------------------------------------------------------ */

/** A blank draft for the editor, optionally pre-filled from a message. */
export function newSnippetInput(body = "", audience: SnippetAudience = "agent"): SnippetInput {
  return {
    title: suggestTitle(body),
    body: audience === "client" ? "" : body,
    audience,
    variants: audience === "client" ? { en: body, ar: "" } : {},
    tags: [],
    shortcut: "",
  };
}

/** The editable half of an existing snippet, in the editor's shape. */
export function snippetInputFrom(snippet: Snippet): SnippetInput {
  return {
    title: snippet.title,
    body: snippet.body,
    audience: snippet.audience || "agent",
    variants: { ar: snippet.variants?.ar ?? "", en: snippet.variants?.en ?? "" },
    tags: [...(snippet.tags ?? [])],
    shortcut: snippet.shortcut ?? "",
  };
}

/**
 * A title proposed from the text being saved. It is the first line, clipped —
 * enough to recognise the snippet in a list, and always editable before the
 * save goes through.
 */
export function suggestTitle(body: string): string {
  const firstLine = (body || "").split("\n").map((line) => line.trim()).find(Boolean) ?? "";
  if (firstLine.length <= 60) return firstLine;
  return `${firstLine.slice(0, 59)}…`;
}

/** Reads the editor's comma- or space-separated tag field. */
export function parseTags(value: string): string[] {
  const seen = new Set<string>();
  for (const raw of (value || "").split(/[,\n]+/)) {
    const tag = raw.trim().toLowerCase();
    if (tag) seen.add(tag);
  }
  return [...seen].slice(0, 10);
}

/** Reports why the editor cannot save yet, or null when it can. */
export function validateSnippetInput(input: SnippetInput): string | null {
  if (!input.title.trim()) return "Give the snippet a title.";
  const hasText =
    input.body.trim() ||
    (input.variants.ar ?? "").trim() ||
    (input.variants.en ?? "").trim();
  if (!hasText) return "A snippet needs some text.";
  if (input.shortcut.trim() && !normalizeShortcut(input.shortcut)) {
    return "A shortcut needs at least one letter or digit.";
  }
  return null;
}

/* ------------------------------------------------------------------ *
 * Import and export
 * ------------------------------------------------------------------ */

/** The document the export button downloads. */
export function exportSnippets(snippets: Snippet[]): string {
  return `${JSON.stringify({ version: 1, snippets }, null, 2)}\n`;
}

/**
 * Reads an exported document back. It accepts both shapes a user is likely to
 * hand it — the wrapper this module writes, and a bare array — because a file
 * that was obviously meant as a snippet library should not be refused over its
 * outermost brace.
 */
export function parseSnippetImport(source: string): Snippet[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(source);
  } catch {
    throw new Error("That file is not valid JSON.");
  }
  const list = Array.isArray(parsed)
    ? parsed
    : Array.isArray((parsed as { snippets?: unknown })?.snippets)
      ? ((parsed as { snippets: unknown[] }).snippets)
      : null;
  if (!list) throw new Error("That file carries no snippets.");
  const snippets = list.filter(isSnippetLike).map(toSnippet);
  if (snippets.length === 0) throw new Error("That file carries no snippets.");
  return snippets;
}

function isSnippetLike(value: unknown): value is Record<string, unknown> {
  if (!value || typeof value !== "object") return false;
  const record = value as Record<string, unknown>;
  const variants = (record.variants ?? {}) as Record<string, unknown>;
  return [record.title, record.body, variants.ar, variants.en].some(
    (field) => typeof field === "string" && field.trim() !== ""
  );
}

function toSnippet(record: Record<string, unknown>): Snippet {
  const variants = (record.variants ?? {}) as Record<string, unknown>;
  return {
    id: typeof record.id === "string" ? record.id : "",
    title: typeof record.title === "string" ? record.title : "",
    body: typeof record.body === "string" ? record.body : "",
    audience: record.audience === "client" ? "client" : "agent",
    variants: {
      ar: typeof variants.ar === "string" ? variants.ar : "",
      en: typeof variants.en === "string" ? variants.en : "",
    },
    tags: Array.isArray(record.tags)
      ? record.tags.filter((tag): tag is string => typeof tag === "string")
      : [],
    shortcut: typeof record.shortcut === "string" ? record.shortcut : "",
    createdAt: typeof record.createdAt === "number" ? record.createdAt : 0,
    updatedAt: typeof record.updatedAt === "number" ? record.updatedAt : 0,
    uses: typeof record.uses === "number" ? record.uses : 0,
  };
}
