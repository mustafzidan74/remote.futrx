import assert from "node:assert/strict";
import test from "node:test";
import { journalState } from "./journalState.ts";
import type { JournalEntry } from "../../models/journal.ts";

const entry = (chatId: string, at: number, request = "x"): JournalEntry => ({
  at,
  chatId,
  projectId: "p1",
  request,
  status: "done",
});

test("a query carries only the filters that are set", () => {
  assert.deepEqual(journalState.query({ text: "  ", from: "", to: "" }), {});
  assert.deepEqual(journalState.query({ text: " barcode ", from: "", to: "" }, { limit: 25 }), {
    q: "barcode",
    limit: 25,
  });
});

test("a day filter covers that whole local day", () => {
  const query = journalState.query({ text: "", from: "2026-09-10", to: "2026-09-10" });
  const from = new Date(query.from as string);
  const to = new Date(query.to as string);
  assert.equal(from.getFullYear(), 2026);
  assert.equal(from.getHours(), 0);
  assert.equal(to.getHours(), 23);
  assert.ok(to.getTime() - from.getTime() > 23 * 60 * 60 * 1000);
});

// A half-typed date must not blank the list, so it is simply not a bound.
test("an incomplete date is not a bound", () => {
  const query = journalState.query({ text: "", from: "2026-09", to: "not a date" });
  assert.equal(query.from, undefined);
  assert.equal(query.to, undefined);
});

test("paging drops entries the previous page already showed", () => {
  const first = [entry("c1", 300), entry("c2", 200)];
  const next = [entry("c2", 200), entry("c3", 100)];
  const merged = journalState.appendPage(first, next);
  assert.deepEqual(
    merged.map((item) => item.chatId),
    ["c1", "c2", "c3"],
  );
});

test("filtered() tells the empty state which message to show", () => {
  assert.equal(journalState.filtered({ text: "", from: "", to: "" }), false);
  assert.equal(journalState.filtered({ text: " ", from: "", to: "2026-09-01" }), true);
});
