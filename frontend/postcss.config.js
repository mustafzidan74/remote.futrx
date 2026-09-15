import autoprefixer from "autoprefixer";
import rtlcss from "postcss-rtlcss";
import { Mode } from "postcss-rtlcss/options";
import tailwindcss from "tailwindcss";

// A rule that states `direction` outright is marking an island of one
// direction — code, paths, a left-to-right label inside Arabic — and flipping
// it would invert the very thing it exists to pin. rtlcss honours this comment.
const keepExplicitDirection = {
  postcssPlugin: "keep-explicit-direction",
  // `Once`, not a `Rule` visitor: rtlcss does its work in `Once` too, and
  // `Once` hooks run in plugin order before any visitor walk.
  Once(root) {
    root.walkRules((rule) => {
      const pinsDirection = rule.nodes?.some((node) => node.type === "decl" && node.prop === "direction");
      const previous = rule.prev();
      const marked = previous?.type === "comment" && previous.text.startsWith("rtl:");
      // A directive comment immediately before a rule applies to the whole rule.
      if (pinsDirection && !marked) rule.before({ text: "rtl:ignore", raws: { left: "", right: "" } });
    });
  },
};

export default {
  plugins: [
    tailwindcss,
    autoprefixer,
    keepExplicitDirection,
    // Right-to-left counterparts for every direction-bearing rule, emitted as
    // `[dir="rtl"]` overrides. Override mode leaves each original rule exactly
    // as it was, so the left-to-right interface renders as before; only the
    // Arabic interface picks up the added rules. Runs after Tailwind so it sees
    // the generated utilities.
    rtlcss({ mode: Mode.override }),
  ],
};
