import assert from "node:assert/strict";
import test from "node:test";
import { fill, jsxTextValue, keyOf, selectPlural } from "./normalize.ts";

test("a key is the trimmed, single-spaced text with numbers as placeholders", () => {
  assert.deepEqual(keyOf("  3 of 12   chats selected "), {
    key: "{n} of {n} chats selected",
    values: ["3", "12"],
    leading: "  ",
    trailing: " ",
  });
});

test("decimal and grouped numbers are one placeholder each", () => {
  assert.deepEqual(keyOf("Used 1,024.5 MB")?.values, ["1,024.5"]);
});

// Numbers, punctuation and text already in another script have nothing to
// look up, and must not cost a catalog lookup on every render.
test("text without two Latin letters in a row is not a key", () => {
  for (const text of ["", "  ", "42", "·", "—", "3 h x", "لغة الواجهة", "a"]) {
    assert.equal(keyOf(text), null, JSON.stringify(text));
  }
});

test("fill restores numbers in order and tolerates a template with fewer slots", () => {
  assert.equal(fill("{n} من {n}", ["3", "12"]), "3 من 12");
  assert.equal(fill("كل المحادثات", ["3"]), "كل المحادثات");
});

test("Arabic plural forms are chosen by count, falling back to other", () => {
  const rules = new Intl.PluralRules("ar");
  const entry = {
    zero: "لا محادثات",
    one: "محادثة واحدة",
    two: "محادثتان",
    few: "{n} محادثات",
    many: "{n} محادثة",
    other: "{n} محادثة",
  };
  assert.equal(selectPlural(entry, "0", rules), "لا محادثات");
  assert.equal(selectPlural(entry, "1", rules), "محادثة واحدة");
  assert.equal(selectPlural(entry, "2", rules), "محادثتان");
  assert.equal(selectPlural(entry, "5", rules), "{n} محادثات");
  assert.equal(selectPlural(entry, "11", rules), "{n} محادثة");
  assert.equal(selectPlural({ other: "{n} عنصر" }, "2", rules), "{n} عنصر");
  assert.equal(selectPlural(entry, undefined, rules), "{n} محادثة");
});

// The extractor reads raw source; the runtime receives what the JSX compiler
// produced. Both must land on the same key.
test("multi-line JSX text collapses the way the JSX compiler collapses it", () => {
  const raw = "\n            Pick a project on the left,\n            then create a chat.\n          ";
  assert.equal(jsxTextValue(raw), "Pick a project on the left, then create a chat.");
  assert.equal(jsxTextValue(" · "), " · ");
});

test("fill places text captures where the translation puts {s}", () => {
  assert.equal(fill("{s} · {n} من {n}", ["3", "12"], ["Acme"]), "Acme · 3 من 12");
});

test("a number with a one-letter unit is a key; a lone number or letter is not", () => {
  assert.equal(keyOf("3w")?.key, "{n}w");
  assert.equal(keyOf("22 d")?.key, "{n} d");
  assert.equal(keyOf("42"), null);
  assert.equal(keyOf("x"), null);
});
