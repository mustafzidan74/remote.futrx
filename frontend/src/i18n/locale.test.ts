import assert from "node:assert/strict";
import test from "node:test";
import {
  directionOf,
  documentLanguageOf,
  isLanguageChoice,
  localeOverride,
  resolveLocale,
} from "./locale.ts";

test("an explicit choice wins over the browser", () => {
  assert.equal(resolveLocale("ar", ["en-US"]), "ar");
  assert.equal(resolveLocale("en", ["ar-EG"]), "en");
});

test("auto follows the first browser language the app can render", () => {
  assert.equal(resolveLocale("auto", ["ar-EG", "en-US"]), "ar");
  assert.equal(resolveLocale("auto", ["fr-FR", "ar"]), "ar");
  assert.equal(resolveLocale("auto", ["en-GB", "ar-SA"]), "en");
  assert.equal(resolveLocale("auto", ["de-DE"]), "en");
  assert.equal(resolveLocale("auto", []), "en");
});

test("the URL override accepts only supported locales", () => {
  assert.equal(localeOverride("?lang=ar"), "ar");
  assert.equal(localeOverride("?chat=abc&lang=qps"), "qps");
  assert.equal(localeOverride("?lang=fr"), null);
  assert.equal(localeOverride(""), null);
});

test("direction and document language per locale", () => {
  assert.equal(directionOf("ar"), "rtl");
  assert.equal(directionOf("en"), "ltr");
  assert.equal(directionOf("qps"), "ltr");
  assert.equal(documentLanguageOf("qps"), "en");
  assert.equal(documentLanguageOf("ar"), "ar");
});

test("stored values are validated before use", () => {
  assert.equal(isLanguageChoice("ar"), true);
  assert.equal(isLanguageChoice("auto"), true);
  assert.equal(isLanguageChoice("fr"), false);
  assert.equal(isLanguageChoice(null), false);
});
