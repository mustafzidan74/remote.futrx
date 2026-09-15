const rtlcss = require("postcss-rtlcss");
const { Mode } = require("postcss-rtlcss/options");

// A rule that states `direction` outright is marking an island of one
// direction — code, paths, a left-to-right label inside Arabic — and flipping
// it would invert the very thing it exists to pin. rtlcss honours a directive
// comment placed immediately before a rule. `Once`, not a `Rule` visitor:
// rtlcss does its work in `Once` too, and `Once` hooks run in plugin order.
const keepExplicitDirection = {
  postcssPlugin: "keep-explicit-direction",
  Once(root) {
    root.walkRules((rule) => {
      const pinsDirection = rule.nodes?.some((node) => node.type === "decl" && node.prop === "direction");
      const previous = rule.prev();
      const marked = previous?.type === "comment" && previous.text.startsWith("rtl:");
      if (pinsDirection && !marked) rule.before({ text: "rtl:ignore", raws: { left: "", right: "" } });
    });
  },
};

// Tailwind moves elements through `--tw-translate-x`, a custom property rtlcss
// cannot know is horizontal. A drawer anchored at `left-0` is re-anchored at
// the right by rtlcss, but its `-translate-x-full` would still hide it towards
// the left and leave a strip on screen. Every class that sets the property
// gets a `[dir="rtl"]` twin with the value negated, right after it (so inside
// the same media query, and later than the base rule it overrides) — zero
// values included, or `md:translate-x-0` would lose to the twin of
// `-translate-x-full` on the same element.
const negate = (value) => {
  const trimmed = value.trim();
  if (trimmed.startsWith("-")) return trimmed.slice(1);
  if (/^[\d.]/.test(trimmed)) return /^0(px|%)?$/.test(trimmed) ? trimmed : `-${trimmed}`;
  return `calc(${trimmed} * -1)`;
};

const mirrorHorizontalTranslate = {
  postcssPlugin: "mirror-horizontal-translate",
  Once(root) {
    root.walkRules((rule) => {
      if (rule.selector.includes("[dir")) return;
      if (!rule.selectors.every((selector) => selector.startsWith("."))) return;
      const declaration = rule.nodes?.find((node) => node.type === "decl" && node.prop === "--tw-translate-x");
      if (!declaration) return;
      const twin = rule.clone({ selectors: rule.selectors.map((selector) => `[dir="rtl"] ${selector}`) });
      twin.removeAll();
      twin.append({ prop: "--tw-translate-x", value: negate(declaration.value) });
      rule.after(twin);
    });
  },
};

module.exports = {
  plugins: [
    require("tailwindcss"),
    require("autoprefixer"),
    keepExplicitDirection,
    // Right-to-left counterparts for every direction-bearing rule, emitted as
    // `[dir="rtl"]` overrides. Override mode leaves each original rule exactly
    // as it was, so the left-to-right interface renders as before; only the
    // Arabic interface picks up the added rules.
    rtlcss({ mode: Mode.override }),
    mirrorHorizontalTranslate,
  ],
};
