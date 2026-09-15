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
  ],
};
