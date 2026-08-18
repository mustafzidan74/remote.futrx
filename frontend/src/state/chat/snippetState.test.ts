import { deepStrictEqual, ok, strictEqual, throws } from "node:assert/strict";
import { test } from "node:test";
import type { Snippet } from "../../models/snippet.ts";
import {
  exportSnippets,
  filterSnippets,
  findSnippetByShortcut,
  newSnippetInput,
  normalizeShortcut,
  parseSnippetImport,
  parseTags,
  resolveSnippetText,
  snippetInputFrom,
  snippetPreview,
  snippetText,
  snippetsFor,
  sortSnippets,
  suggestTitle,
  usesSelection,
  validateSnippetInput,
} from "./snippetState.ts";

function snippet(overrides: Partial<Snippet> = {}): Snippet {
  return {
    id: "one",
    title: "One",
    body: "body",
    audience: "agent",
    variants: {},
    tags: [],
    shortcut: "",
    createdAt: 1,
    updatedAt: 1,
    uses: 0,
    ...overrides,
  };
}

const context = {
  projectName: "Acme Shop",
  slug: "acme-shop",
  previewUrl: "https://acme-shop--3000.dev.mz-ss.tech",
  portalUrl: "https://remote.example.com/portal/9f2a?t=secret",
  date: "18 August 2026",
};

/* ------------------------------------------------------------------ *
 * Placeholders
 * ------------------------------------------------------------------ */

test("every snippet placeholder resolves from the project", () => {
  const resolved = resolveSnippetText(
    "{{project}} / {{projectName}} / {{clientName}} / {{slug}} / {{previewUrl}} / {{portalUrl}} / {{date}}",
    context,
  );

  strictEqual(
    resolved.text,
    "Acme Shop / Acme Shop / Acme Shop / acme-shop / " +
      "https://acme-shop--3000.dev.mz-ss.tech / https://remote.example.com/portal/9f2a?t=secret / " +
      "18 August 2026",
  );
  strictEqual(resolved.ready, true);
  deepStrictEqual(resolved.unresolved, []);
});

test("the client name defaults to the project and can be overridden", () => {
  strictEqual(resolveSnippetText("Hello {{clientName}}", context).text, "Hello Acme Shop");
  strictEqual(
    resolveSnippetText("Hello {{clientName}}", { ...context, clientName: "Mr Aziz" }).text,
    "Hello Mr Aziz",
  );
});

test("{{selection}} is replaced by the current draft", () => {
  const resolved = resolveSnippetText("Rewrite this in Arabic:\n\n{{selection}}", {
    ...context,
    selection: "The build is broken.",
  });

  strictEqual(resolved.text, "Rewrite this in Arabic:\n\nThe build is broken.");
  strictEqual(resolved.ready, true);
});

test("an empty draft leaves {{selection}} visible rather than deleting it", () => {
  const resolved = resolveSnippetText("Summarize: {{selection}}", { ...context, selection: "   " });

  strictEqual(resolved.text, "Summarize: {{selection}}");
  strictEqual(resolved.ready, false);
  strictEqual(resolved.unresolved[0]?.name, "selection");
  strictEqual(
    resolved.text.slice(resolved.unresolved[0].start, resolved.unresolved[0].end),
    "{{selection}}",
  );
});

test("values nobody can supply stay visible, positioned against the result", () => {
  const resolved = resolveSnippetText("Open {{previewUrl}} for {{project}} at {{portalUrl}}", {
    projectName: "Acme Shop",
  });

  strictEqual(resolved.text, "Open {{previewUrl}} for Acme Shop at {{portalUrl}}");
  strictEqual(resolved.ready, false);
  deepStrictEqual(
    resolved.unresolved.map((item) => item.name),
    ["previewUrl", "portalUrl"],
  );
  for (const item of resolved.unresolved) {
    strictEqual(resolved.text.slice(item.start, item.end), item.token);
  }
});

test("an unknown placeholder is left alone rather than emptied", () => {
  const resolved = resolveSnippetText("Invoice {{invoiceNumber}} for {{project}}", context);

  strictEqual(resolved.text, "Invoice {{invoiceNumber}} for Acme Shop");
  strictEqual(resolved.unresolved.length, 1);
});

test("{{date}} falls back to today when the caller supplies none", () => {
  const resolved = resolveSnippetText("Sent {{date}}", { projectName: "Acme" });

  strictEqual(resolved.ready, true);
  ok(!resolved.text.includes("{{date}}"));
});

test("usesSelection sees the placeholder however it is spaced", () => {
  strictEqual(usesSelection("a {{selection}} b"), true);
  strictEqual(usesSelection("a {{ selection }} b"), true);
  strictEqual(usesSelection("a {{selected}} b"), false);
});

/* ------------------------------------------------------------------ *
 * Client templates
 * ------------------------------------------------------------------ */

test("a client template picks its language and falls back to the other", () => {
  const bilingual = snippet({
    audience: "client",
    body: "",
    variants: { en: "Hello", ar: "مرحبا" },
  });
  strictEqual(snippetText(bilingual, "en"), "Hello");
  strictEqual(snippetText(bilingual, "ar"), "مرحبا");

  const halfTranslated = snippet({ audience: "client", body: "", variants: { en: "Hello" } });
  strictEqual(snippetText(halfTranslated, "ar"), "Hello");

  const bare = snippet({ audience: "client", body: "Plain", variants: {} });
  strictEqual(snippetText(bare, "ar"), "Plain");
});

test("an agent snippet ignores the language entirely", () => {
  const prompt = snippet({ body: "Prompt", variants: { ar: "مرحبا" } });
  strictEqual(snippetText(prompt, "ar"), "Prompt");
});

test("the two audiences never mix", () => {
  const list = [
    snippet({ id: "a", audience: "agent" }),
    snippet({ id: "b", audience: "client", variants: { en: "Hi" } }),
  ];
  deepStrictEqual(
    snippetsFor(list, "client").map((item) => item.id),
    ["b"],
  );
  deepStrictEqual(
    snippetsFor(list, "agent").map((item) => item.id),
    ["a"],
  );
});

/* ------------------------------------------------------------------ *
 * Listing
 * ------------------------------------------------------------------ */

test("the library is ordered by what gets used most", () => {
  const ordered = sortSnippets([
    snippet({ id: "quiet", title: "Quiet", uses: 0, updatedAt: 5 }),
    snippet({ id: "busy", title: "Busy", uses: 9, updatedAt: 1 }),
    snippet({ id: "newer", title: "Newer", uses: 0, updatedAt: 9 }),
  ]);

  deepStrictEqual(
    ordered.map((item) => item.id),
    ["busy", "newer", "quiet"],
  );
});

test("search prefers the title, then the shortcut, then a tag, then the body", () => {
  const list = [
    snippet({ id: "body", title: "Unrelated", body: "mentions deploy somewhere" }),
    snippet({ id: "tag", title: "Also unrelated", tags: ["deploy"] }),
    snippet({ id: "shortcut", title: "Still unrelated", shortcut: "deploy" }),
    snippet({ id: "title", title: "Deploy checklist" }),
  ];

  deepStrictEqual(
    filterSnippets(list, "deploy").map((item) => item.id),
    ["title", "shortcut", "tag", "body"],
  );
  strictEqual(filterSnippets(list, "nothing-matches").length, 0);
  strictEqual(filterSnippets(list, "  ").length, list.length);
});

test("a shortcut is found however the user spells it", () => {
  const list = [snippet({ id: "wp", title: "WP fix", shortcut: "wpfix" })];

  for (const spelling of ["wpfix", "WPFIX", "s-wpfix", "/s-wpfix"]) {
    strictEqual(findSnippetByShortcut(list, spelling)?.id, "wp", spelling);
  }
  strictEqual(findSnippetByShortcut(list, "nope"), null);
  strictEqual(findSnippetByShortcut(list, ""), null);
});

test("a snippet with no shortcut is still reachable by its title or id", () => {
  const list = [snippet({ id: "delivery-note", title: "Delivery note" })];

  strictEqual(findSnippetByShortcut(list, "delivery-note")?.id, "delivery-note");
  strictEqual(normalizeShortcut(" /s-Delivery Note "), "delivery-note");
});

test("the preview collapses whitespace and clips long text", () => {
  const long = snippet({ body: `first line\n\n${"x".repeat(300)}` });
  const preview = snippetPreview(long);

  ok(!preview.includes("\n"));
  ok(preview.length <= 120);
  ok(preview.endsWith("…"));
});

/* ------------------------------------------------------------------ *
 * Editing
 * ------------------------------------------------------------------ */

test("a new snippet proposes a title from the first line", () => {
  strictEqual(suggestTitle("Fix the checkout\n\nMore detail"), "Fix the checkout");
  strictEqual(suggestTitle("   \n  Second line wins  "), "Second line wins");
  strictEqual(suggestTitle(""), "");
  strictEqual(suggestTitle("x".repeat(100)).length, 60);
});

test("saving a message as a client template seeds the English variant", () => {
  const forAgent = newSnippetInput("Run the tests");
  strictEqual(forAgent.body, "Run the tests");
  strictEqual(forAgent.audience, "agent");

  const forClient = newSnippetInput("The site is live", "client");
  strictEqual(forClient.body, "");
  strictEqual(forClient.variants.en, "The site is live");
});

test("the editor reads an existing snippet back without losing anything", () => {
  const stored = snippet({
    title: "T",
    audience: "client",
    variants: { en: "Hello", ar: "مرحبا" },
    tags: ["client"],
    shortcut: "hi",
  });

  deepStrictEqual(snippetInputFrom(stored), {
    title: "T",
    body: "body",
    audience: "client",
    variants: { en: "Hello", ar: "مرحبا" },
    tags: ["client"],
    shortcut: "hi",
  });
});

test("tags are split, lowercased, deduplicated, and capped", () => {
  deepStrictEqual(parseTags(" Deploy, deploy , wordpress\nWordPress "), ["deploy", "wordpress"]);
  strictEqual(parseTags(Array.from({ length: 20 }, (_, i) => `t${i}`).join(",")).length, 10);
  deepStrictEqual(parseTags(""), []);
});

test("the editor refuses what the server would refuse", () => {
  const valid = { ...newSnippetInput("text"), title: "Title" };
  strictEqual(validateSnippetInput(valid), null);
  ok(validateSnippetInput({ ...valid, title: "  " }));
  ok(validateSnippetInput({ ...valid, body: "" }));
  ok(validateSnippetInput({ ...valid, shortcut: "!!!" }));
  // Anything with a letter in it is accepted and stored slugified.
  strictEqual(validateSnippetInput({ ...valid, shortcut: "WP Fix" }), null);
  strictEqual(normalizeShortcut("WP Fix"), "wp-fix");
  strictEqual(
    validateSnippetInput({ ...valid, body: "", variants: { ar: "مرحبا" }, audience: "client" }),
    null,
  );
});

/* ------------------------------------------------------------------ *
 * Import and export
 * ------------------------------------------------------------------ */

test("an exported library reads back exactly", () => {
  const list = [snippet({ id: "a", title: "A" }), snippet({ id: "b", title: "B" })];
  const parsed = parseSnippetImport(exportSnippets(list));

  deepStrictEqual(
    parsed.map((item) => item.title),
    ["A", "B"],
  );
});

test("a bare array is accepted as well as the wrapper", () => {
  const parsed = parseSnippetImport(JSON.stringify([{ title: "A", body: "b" }]));
  strictEqual(parsed.length, 1);
  strictEqual(parsed[0].audience, "agent");
});

test("a file that carries no snippets is refused with a reason", () => {
  throws(() => parseSnippetImport("not json"), /valid JSON/);
  throws(() => parseSnippetImport("{}"), /no snippets/);
  throws(() => parseSnippetImport("[{}]"), /no snippets/);
});

test("imported entries are coerced into shape rather than trusted", () => {
  const parsed = parseSnippetImport(
    JSON.stringify([{ title: "A", body: 42, uses: "many", tags: ["x", 7], audience: "client" }]),
  );

  strictEqual(parsed[0].body, "");
  strictEqual(parsed[0].uses, 0);
  deepStrictEqual(parsed[0].tags, ["x"]);
  strictEqual(parsed[0].audience, "client");
});
