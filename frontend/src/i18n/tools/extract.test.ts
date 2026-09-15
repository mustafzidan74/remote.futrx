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
  const keys = occurrences.map((o) => o.key).sort();
  assert.deepEqual(keys, [
    "Explicit text",
    "Opens the terminal",
    "Saved",
    "Saving",
    "Settings for this project",
    "Tokens and cost per project.",
    "Usage",
    "Usage panel",
    "Working…",
    "chats & projects",
  ]);
  assert.equal(occurrences.find((o) => o.key === "Usage panel")?.line, 6);
  // A template literal and a browser dialog never reach the shim as one key.
  assert.deepEqual(unkeyable.map((o) => o.key), ["`Search ${count} items`", 'confirm("Delete it?")']);
});

test("code-shaped strings are left out of the catalog", () => {
  for (const key of ["--env KEY=VALUE", "src/app.tsx", "(?i)refactor", '"status":"ok"', "projectId", "snake_case_name", "-----BEGIN"]) {
    assert.equal(looksLikeCode(key), true, key);
  }
  for (const key of ["Sign in", "Settings", "Don't show again", "Uptime (30 days)"]) {
    assert.equal(looksLikeCode(key), false, key);
  }
});

test("entities decode the way the JSX compiler decodes them", () => {
  assert.equal(decodeEntities("a &lt;skill&gt; &amp; &#39;b&#x27;"), "a <skill> & 'b'");
});
