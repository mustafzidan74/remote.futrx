import assert from "node:assert/strict";
import test from "node:test";
import { fuzzyMatches, fuzzyScore } from "./fuzzy.ts";

test("matches subsequences and rejects the rest", () => {
  assert.equal(fuzzyMatches("New project", "npj"), true);
  assert.equal(fuzzyMatches("New project", "new proj"), true);
  assert.equal(fuzzyMatches("New project", "NWP"), true);
  assert.equal(fuzzyMatches("New project", "zz"), false);
  // An empty query is not a filter, so everything survives it.
  assert.equal(fuzzyScore("New project", "   "), 0);
});

test("ranks word-boundary hits above hits inside a word", () => {
  const boundary = fuzzyScore("Secrets vault", "sv") as number;
  const inside = fuzzyScore("Resources", "sc") as number;
  assert.ok(boundary > inside, `${boundary} should beat ${inside}`);
});

test("ranks adjacent hits above scattered ones", () => {
  const adjacent = fuzzyScore("Code", "co") as number;
  const scattered = fuzzyScore("Chrome overview", "co") as number;
  assert.ok(adjacent > scattered, `${adjacent} should beat ${scattered}`);
});

test("prefers the candidate that starts matching earlier", () => {
  const early = fuzzyScore("Trash", "tr") as number;
  const late = fuzzyScore("Audit trail", "tr") as number;
  assert.ok(early > late, `${early} should beat ${late}`);
});
