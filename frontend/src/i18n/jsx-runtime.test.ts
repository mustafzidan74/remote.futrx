import assert from "node:assert/strict";
import test from "node:test";
import { jsxDEV } from "./jsx-dev-runtime.ts";
import { jsx, jsxs } from "./jsx-runtime.ts";
import { setActiveTranslations } from "./translate.ts";

type VNodeLike = { type: unknown; props: Record<string, unknown>; key: unknown };

// The runtime is what the compiled components call, so this is the whole
// feature end to end short of a browser.
test("the runtimes hand Preact translated props and keep type and key", () => {
  setActiveTranslations("ar", { "Sign in": "تسجيل الدخول", Close: "إغلاق" });

  const button = jsx("button", { title: "Close", children: "Sign in" }, "login") as unknown as VNodeLike;
  assert.equal(button.type, "button");
  assert.equal(button.key, "login");
  assert.deepEqual(button.props, { title: "إغلاق", children: "تسجيل الدخول" });

  const list = jsxs("div", { children: ["Sign in", " ", "Close"] }) as unknown as VNodeLike;
  assert.deepEqual(list.props.children, ["تسجيل الدخول", " ", "إغلاق"]);

  const dev = jsxDEV("span", { children: "Close" }, undefined) as unknown as VNodeLike;
  assert.equal(dev.props.children, "إغلاق");

  setActiveTranslations("en", {});
  const english = jsx("button", { children: "Sign in" }) as unknown as VNodeLike;
  assert.equal(english.props.children, "Sign in");
});
