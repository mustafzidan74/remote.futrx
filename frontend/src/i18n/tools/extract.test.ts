import assert from "node:assert/strict";
import test from "node:test";
import { decodeEntities, extractFromSource, looksLikeCode } from "./extract.ts";

const source = `
const tabs = [{ id: "usage", label: "Usage", description: "Tokens and cost per project." }];

export function Panel({ saving, count }: { saving: boolean; count: number }) {
  return (
    <section aria-label="Usage panel">
      <h1 title={saving ? "Saving" : "Saved"}>
        Settings
        for this project
      </h1>
      {saving && "Working…"}
      <p>{count} chats &amp; projects</p>
      <input placeholder={\`Search \${count} items\`} name="query" />
      <code>npm run build</code>
      <p dir="auto">A user's message</p>
      <Tooltip hint="Opens the terminal" />
      {confirm("Delete it?")}
      {t("Explicit text")}
    </section>
  );
}
`;

test("finds interface text under the keys the runtime computes", () => {
  const { occurrences, unkeyable } = extractFromSource("src/ui/Panel.tsx", source);
  const keys = [...new Set(occurrences.map((o) => o.key))].sort();
  assert.deepEqual(keys, [
    "Delete it?",
    "Explicit text",
    "Opens the terminal",
    "Saved",
    "Saving",
    "Search {n} items",
    "Settings for this project",
    "Tokens and cost per project.",
    "Usage",
    "Usage panel",
    "Working…",
    "chats & projects",
  ]);
  assert.equal(occurrences.find((o) => o.key === "Usage panel")?.line, 6);
  assert.deepEqual(unkeyable, []);
});

test("composed text becomes pattern keys", () => {
  const composed = `
export function Row({ name, count, error, on }: { name: string; count: number; error: Error; on: boolean }) {
  alert("update failed: " + error.message);
  confirm({ title: \`Delete \${name}?\`, message: "It cannot be undone." });
  return (
    <div title={\`Open \${name}\`} aria-label={\`Auto-test — \${on ? "on" : "off"}\`}>
      {\`\${count} result\${count === 1 ? "" : "s"}\`}
      <span>
        {count} link{count === 1 ? "" : "s"}
      </span>
      {\`\${name} · \${name}\`}
    </div>
  );
}
`;
  const { occurrences, unkeyable } = extractFromSource("src/ui/Row.tsx", composed);
  assert.deepEqual([...new Set(occurrences.map((o) => o.key))].sort(), [
    // Short alternatives expand into whole sentences rather than slots.
    "Auto-test — off",
    "Auto-test — on",
    "Delete {s}?",
    "It cannot be undone.",
    "Open {s}",
    "link",
    "update failed: {s}",
    "{n} link",
    "{n} links",
    "{n} result",
    "{n} results",
  ]);
  // Nothing but slots and punctuation: no words to translate.
  assert.deepEqual(unkeyable.map((o) => o.key), ["`${name} · ${name}`"]);
});

test("code-shaped strings are left out of the catalog", () => {
  for (const key of ["--env KEY=VALUE", "src/app.tsx", "(?i)refactor", '"status":"ok"', "projectId", "snake_case_name", "-----BEGIN"]) {
    assert.equal(looksLikeCode(key), true, key);
  }
  for (const key of ["-style secrets and links every site it finds", "Sign in", "Settings", "Don't show again", "Uptime (30 days)"]) {
    assert.equal(looksLikeCode(key), false, key);
  }
});

test("entities decode the way the JSX compiler decodes them", () => {
  assert.equal(decodeEntities("a &lt;skill&gt; &amp; &#39;b&#x27;"), "a <skill> & 'b'");
  assert.equal(decodeEntities("project&rsquo;s chat &mdash; done&hellip; &uarr;&darr; &crarr;"), "project’s chat — done… ↑↓ ↵");
});
