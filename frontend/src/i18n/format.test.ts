import assert from "node:assert/strict";
import test from "node:test";
import { applyFormattingLocale, formattingLocaleOf } from "./format.ts";

const at = new Date(Date.UTC(2026, 8, 15, 12, 0));

test("Arabic formats default-locale calls in Arabic with Latin digits", () => {
  const english = at.toLocaleDateString("en-US", { month: "long", day: "numeric", timeZone: "UTC" });
  applyFormattingLocale("ar");
  assert.equal(at.toLocaleDateString(undefined, { month: "long", day: "numeric", timeZone: "UTC" }), "15 سبتمبر");
  assert.equal(at.toLocaleDateString([], { month: "long", day: "numeric", timeZone: "UTC" }), "15 سبتمبر");
  assert.equal((1234567).toLocaleString(), "1,234,567");
  // A call that names its locale keeps it.
  assert.equal(at.toLocaleDateString("en-US", { month: "long", day: "numeric", timeZone: "UTC" }), english);
  assert.equal((1234.5).toLocaleString("en-US"), "1,234.5");
  applyFormattingLocale("en");
  assert.equal(
    at.toLocaleDateString(undefined, { month: "long", timeZone: "UTC" }),
    new Intl.DateTimeFormat(undefined, { month: "long", timeZone: "UTC" }).format(at),
  );
});

test("only Arabic changes the formatting locale", () => {
  assert.equal(formattingLocaleOf("ar"), "ar-EG-u-nu-latn");
  assert.equal(formattingLocaleOf("en"), undefined);
  assert.equal(formattingLocaleOf("qps"), undefined);
});
