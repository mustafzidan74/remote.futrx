import assert from "node:assert/strict";
import test from "node:test";
import {
  byMostChanged,
  changeLabel,
  changeTone,
  comparisonHeadline,
  pageChanged,
  type VisualComparison,
  type VisualPageDiff,
} from "../../models/visualDiff.ts";

function diff(over: Partial<VisualPageDiff> = {}): VisualPageDiff {
  return { path: "/", changedPercent: 0, changedPixels: 0, ...over };
}

test("a handful of stray pixels is not a change", () => {
  // The floor exists so an operator trusts the list. A page reported at 0.04%
  // when nobody touched it teaches them to skim past the 4% that matters.
  assert.equal(pageChanged(diff({ changedPercent: 0.04 })), false);
  assert.equal(pageChanged(diff({ changedPercent: 0.4 })), true);
});

test("a page whose height moved counts however small the percentage", () => {
  // Everything past the old last row is different; the percentage understates
  // it because most of the overlap still matches.
  const grew = diff({ changedPercent: 0.02, sizeChanged: true });
  assert.equal(pageChanged(grew), true);
  assert.equal(changeTone(grew), "major");
  assert.match(changeLabel(grew), /page height changed/);
});

test("a page that could not be compared is neither changed nor fine", () => {
  const broken = diff({ error: "net::ERR_CONNECTION_REFUSED" });
  assert.equal(pageChanged(broken), false);
  assert.equal(changeTone(broken), "failed");
  assert.equal(changeLabel(broken), "could not compare");
});

test("the tone bands are coarse on purpose", () => {
  assert.equal(changeTone(diff({ changedPercent: 0 })), "none");
  assert.equal(changeTone(diff({ changedPercent: 1 })), "slight");
  assert.equal(changeTone(diff({ changedPercent: 8 })), "notable");
  assert.equal(changeTone(diff({ changedPercent: 40 })), "major");
});

test("small changes keep two decimals, large ones do not", () => {
  assert.equal(changeLabel(diff({ changedPercent: 0.37 })), "0.37% changed");
  assert.equal(changeLabel(diff({ changedPercent: 42.19 })), "42.2% changed");
});

function comparison(over: Partial<VisualComparison> = {}): VisualComparison {
  return {
    id: "c1",
    baselineId: "b1",
    status: "ready",
    pages: [],
    changedPages: 0,
    maxChangedPercent: 0,
    createdAt: 0,
    ...over,
  };
}

test("nothing moved is stated plainly", () => {
  // Twelve unremarkable rows communicate this badly; a sentence does not.
  const clean = comparison({ pages: [diff(), diff({ path: "/about" })] });
  assert.equal(comparisonHeadline(clean), "nothing moved across 2 pages");
});

test("the headline counts pages, not pixels", () => {
  const moved = comparison({
    changedPages: 1,
    pages: [diff({ changedPercent: 12 }), diff({ path: "/about" })],
  });
  assert.equal(comparisonHeadline(moved), "1 of 2 pages changed");
});

test("a failed run says why instead of reporting zero changes", () => {
  const failed = comparison({ status: "failed", error: "the container stopped" });
  assert.equal(comparisonHeadline(failed), "the container stopped");
});

test("the worst offender sorts to the top and failures to the bottom", () => {
  const sorted = byMostChanged([
    diff({ path: "/a", changedPercent: 2 }),
    diff({ path: "/broken", error: "timed out" }),
    diff({ path: "/b", changedPercent: 30 }),
    diff({ path: "/grew", changedPercent: 0.1, sizeChanged: true }),
  ]);
  assert.deepEqual(
    sorted.map((page) => page.path),
    ["/grew", "/b", "/a", "/broken"],
  );
});
