import assert from "node:assert/strict";
import test from "node:test";
import { activeLocale, localizeProps, setActiveTranslations, t } from "./translate.ts";

const catalog = {
  "Sign in": "تسجيل الدخول",
  Settings: "الإعدادات",
  "Search chats and projects": "ابحث في المحادثات والمشاريع",
  "{n} chats": { one: "محادثة واحدة", two: "محادثتان", other: "{n} محادثة" },
  Close: "إغلاق",
};

test("English renders exactly what the component wrote, allocating nothing", () => {
  setActiveTranslations("en", catalog);
  const props = { title: "Settings", children: "Settings" };
  assert.equal(activeLocale(), "en");
  assert.equal(t("Settings"), "Settings");
  assert.equal(localizeProps("button", props), props);
});

test("Arabic translates known text, keeps surrounding whitespace, and leaves the rest", () => {
  setActiveTranslations("ar", catalog);
  assert.equal(t(" Settings "), " الإعدادات ");
  assert.equal(t("Something upstream added yesterday"), "Something upstream added yesterday");
  assert.equal(t("12 chats"), "12 محادثة");
  assert.equal(t("2 chats"), "محادثتان");
});

test("host elements translate their visible attributes and text children", () => {
  setActiveTranslations("ar", catalog);
  const localized = localizeProps("input", {
    placeholder: "Search chats and projects",
    "aria-label": "Search chats and projects",
    name: "Settings",
  });
  assert.deepEqual(localized, {
    placeholder: "ابحث في المحادثات والمشاريع",
    "aria-label": "ابحث في المحادثات والمشاريع",
    // Not a visible attribute: an identifier that happens to read like text.
    name: "Settings",
  });

  const nested = localizeProps("span", { children: ["Sign in", ["  Settings"], 3, null] });
  assert.deepEqual(nested, { children: ["تسجيل الدخول", ["  الإعدادات"], 3, null] });
});

test("components translate the props they conventionally render", () => {
  setActiveTranslations("ar", catalog);
  const Component = () => null;
  assert.deepEqual(localizeProps(Component, { label: "Settings", subtitle: "Close", id: "Settings" }), {
    label: "الإعدادات",
    subtitle: "إغلاق",
    id: "Settings",
  });
});

// A chat message that happens to say "Settings" is the user's words. The
// renderers mark such text with `dir`; code and terminals are verbatim too.
test("content marked as someone's words or as code is never translated", () => {
  setActiveTranslations("ar", catalog);
  for (const [type, props] of [
    ["div", { dir: "auto", children: "Settings" }],
    ["p", { dir: "ltr", children: ["Sign in"] }],
    ["span", { translate: false, children: "Settings" }],
    ["pre", { children: "Settings" }],
    ["code", { children: "Close" }],
  ] as const) {
    const input = { ...props };
    assert.equal(localizeProps(type, input), input, `${type} ${JSON.stringify(props)}`);
  }
  // The element's own visible attributes are still interface.
  assert.deepEqual(localizeProps("div", { dir: "auto", title: "Close", children: "Close" }), {
    dir: "auto",
    title: "إغلاق",
    children: "Close",
  });
});

test("the pseudo-locale brackets every string the shim reaches", () => {
  setActiveTranslations("qps", {});
  assert.equal(t("  Anything at all "), "  ⟦Anything at all⟧ ");
  assert.equal(t(t("Twice through the shim")), "⟦Twice through the shim⟧");
  assert.equal(t("42"), "42");
  setActiveTranslations("en", {});
});

test("patterns translate composed sentences and the text in their slots", () => {
  setActiveTranslations("ar", {
    "Open {s}": "فتح {s}",
    "{s} has never been snapshotted": "لم تؤخذ أي لقطة لـ {s}",
    "{n} d ago": "منذ {n} يوم",
    "active {s}": "نشط {s}",
    "Imported {n} comments from #{s}": { one: "استيراد تعليق واحد من #{s}", other: "استيراد {n} تعليق من #{s}" },
    Settings: "الإعدادات",
  });
  assert.equal(t("Open shop-42"), "فتح shop-42");
  assert.equal(t("Open Settings"), "فتح الإعدادات");
  assert.equal(t(" Acme has never been snapshotted"), " لم تؤخذ أي لقطة لـ Acme");
  // The capture is itself a key: numbers travel with it.
  assert.equal(t("active 22 d ago"), "نشط منذ 22 يوم");
  // Numbers in the pattern's own text fill its {n}; the slot keeps its own.
  assert.equal(t("Imported 1 comments from #18"), "استيراد تعليق واحد من #18");
  assert.equal(t("Imported 7 comments from #18"), "استيراد 7 تعليق من #18");
  assert.equal(t("Nothing matches this"), "Nothing matches this");
  setActiveTranslations("en", {});
});

test("adjacent text children are looked up as one sentence first", () => {
  setActiveTranslations("ar", { "{n} links": "{n} روابط", link: "رابط", Close: "إغلاق" });
  assert.deepEqual(localizeProps("span", { children: [3, " link", "s"] }), { children: ["3 روابط", "", ""] });
  // No whole-run entry: each piece is translated alone.
  assert.deepEqual(localizeProps("span", { children: [1, " link", ""] }), { children: [1, " رابط", ""] });
  // Invisible children do not break a run; elements do.
  assert.deepEqual(localizeProps("span", { children: [3, false, " link", "s"] }), { children: ["3 روابط", false, "", ""] });
  const element = { type: "b" };
  assert.deepEqual(localizeProps("span", { children: ["Close", element, "Close"] }), {
    children: ["إغلاق", element, "إغلاق"],
  });
  setActiveTranslations("en", {});
});

test("English left in the Arabic interface keeps its own direction", () => {
  setActiveTranslations("ar", { Settings: "الإعدادات" });
  assert.deepEqual(localizeProps("span", { children: ["exit: killed", "Settings", "12", "اهلا"] }), {
    children: ["\u2068exit: killed\u2069", "الإعدادات", "12", "اهلا"],
  });
  assert.deepEqual(localizeProps("button", { title: "Claude · Opus" }), { title: "\u2068Claude · Opus\u2069" });
  // Only for display: t() itself returns untranslated text unchanged.
  assert.equal(t("exit: killed"), "exit: killed");
  setActiveTranslations("qps", {});
  assert.deepEqual(localizeProps("span", { children: "12" }), { children: "12" });
  setActiveTranslations("en", {});
});
