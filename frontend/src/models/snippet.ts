/** Who a snippet is written for. */
export type SnippetAudience = "agent" | "client";

/** The two languages a client template is written in. */
export type SnippetLanguage = "en" | "ar";

export interface SnippetVariants {
  ar?: string;
  en?: string;
}

/**
 * One entry of the signed-in user's private library.
 *
 * An agent snippet is a prompt and lives in `body`. A client template is a
 * message for a human and lives in `variants`, one per language; `body` is
 * then only the fallback for a template that was never translated.
 */
export interface Snippet {
  id: string;
  title: string;
  body: string;
  audience: SnippetAudience;
  variants?: SnippetVariants;
  tags?: string[];
  /** The word `/s-<shortcut>` types. */
  shortcut?: string;
  createdAt: number;
  updatedAt: number;
  /** Insertions so far. The picker sorts on it. */
  uses: number;
}

/** The editable half of a snippet: what the editor submits. */
export interface SnippetInput {
  title: string;
  body: string;
  audience: SnippetAudience;
  variants: SnippetVariants;
  tags: string[];
  shortcut: string;
}
