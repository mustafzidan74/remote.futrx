import assert from "node:assert/strict";
import test from "node:test";
import { messageSearchState } from "./messageSearchState.ts";
import {
  SNIPPET_HIGHLIGHT_END,
  SNIPPET_HIGHLIGHT_START,
  type SearchResult,
} from "../../models/search.ts";

function result(overrides: Partial<SearchResult> = {}): SearchResult {
  return {
    chatId: "chat-1",
    chatTitle: "Deploy notes",
    projectId: "project-1",
    projectName: "Platform",
    role: "user",
    at: 100,
    snippet: "plain",
    ...overrides,
  };
}

test("shouldSearch requires at least two characters", () => {
  assert.equal(messageSearchState.shouldSearch(""), false);
  assert.equal(messageSearchState.shouldSearch(" a "), false);
  assert.equal(messageSearchState.shouldSearch("ab"), true);
  assert.equal(messageSearchState.shouldSearch("  caddy  "), true);
});

test("group keeps the server order and buckets hits by chat", () => {
  const groups = messageSearchState.group([
    result({ chatId: "a", at: 1 }),
    result({ chatId: "b", chatTitle: "Billing", at: 2 }),
    result({ chatId: "a", at: 3 }),
  ]);

  assert.deepEqual(
    groups.map((group) => group.chatId),
    ["a", "b"],
  );
  assert.equal(groups[0].results.length, 2);
  assert.deepEqual(
    groups[0].results.map((entry) => entry.at),
    [1, 3],
  );
  assert.equal(groups[1].chatTitle, "Billing");
});

test("group of an empty result set is empty", () => {
  assert.deepEqual(messageSearchState.group([]), []);
});

test("flatten walks the groups in display order", () => {
  const groups = messageSearchState.group([
    result({ chatId: "a", at: 1 }),
    result({ chatId: "b", at: 2 }),
    result({ chatId: "a", at: 3 }),
  ]);

  assert.deepEqual(
    messageSearchState.flatten(groups).map((entry) => entry.at),
    [1, 3, 2],
  );
});

test("move wraps in both directions and copes with an empty list", () => {
  assert.equal(messageSearchState.move(-1, 3, 1), 0);
  assert.equal(messageSearchState.move(0, 3, 1), 1);
  assert.equal(messageSearchState.move(2, 3, 1), 0, "down wraps to the top");
  assert.equal(messageSearchState.move(-1, 3, -1), 2, "up from nothing selects the last");
  assert.equal(messageSearchState.move(0, 3, -1), 2, "up wraps to the bottom");
  assert.equal(messageSearchState.move(1, 0, 1), -1, "an empty list has nothing active");
});

test("activeResult resolves only in-range indices", () => {
  const results = [result({ at: 1 }), result({ at: 2 })];
  assert.equal(messageSearchState.activeResult(results, 1)?.at, 2);
  assert.equal(messageSearchState.activeResult(results, -1), null);
  assert.equal(messageSearchState.activeResult(results, 2), null);
  assert.equal(messageSearchState.activeResult([], 0), null);
});

test("segments splits a snippet on the highlight sentinels", () => {
  const snippet = `restart ${SNIPPET_HIGHLIGHT_START}caddy${SNIPPET_HIGHLIGHT_END} on the box`;
  assert.deepEqual(messageSearchState.segments(snippet), [
    { text: "restart ", match: false },
    { text: "caddy", match: true },
    { text: " on the box", match: false },
  ]);
});

test("segments handles a match at the very start and end", () => {
  assert.deepEqual(
    messageSearchState.segments(`${SNIPPET_HIGHLIGHT_START}caddy${SNIPPET_HIGHLIGHT_END}`),
    [{ text: "caddy", match: true }],
  );
});

test("segments degrades gracefully on an unpaired sentinel", () => {
  const snippet = `broken ${SNIPPET_HIGHLIGHT_START}tail`;
  assert.deepEqual(messageSearchState.segments(snippet), [
    { text: "broken tail", match: false },
  ]);
});

test("segments of plain text is one unmatched run", () => {
  assert.deepEqual(messageSearchState.segments("nothing special"), [
    { text: "nothing special", match: false },
  ]);
});
