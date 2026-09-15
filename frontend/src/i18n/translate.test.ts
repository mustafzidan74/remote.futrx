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
  assert.deepEqual(localizeProps(Component, { label: "Settings", title: "Close", id: "Settings" }), {
    label: "الإعدادات",
    title: "إغلاق",
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
  assert.equal(t("42"), "42");
  setActiveTranslations("en", {});
});
