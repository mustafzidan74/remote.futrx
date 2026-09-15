// The app's JSX runtime: Preact's, with visible text translated on the way in.
// Wired through `jsxImportSource` (tsconfig.json and vite.config.ts), so no
// component imports it and none of them has to change.

import {
  Fragment,
  jsx as preactJsx,
  jsxAttr,
  jsxEscape,
  jsxs as preactJsxs,
  jsxTemplate,
} from "preact/jsx-runtime";
import { localizeProps } from "./translate.ts";

export { Fragment, jsxAttr, jsxEscape, jsxTemplate };
export type { JSX } from "preact/jsx-runtime";

type RuntimeArgs = [type: unknown, props: Record<string, unknown>, ...rest: unknown[]];

export const jsx = ((type: unknown, props: Record<string, unknown>, ...rest: unknown[]) =>
  (preactJsx as unknown as (...args: RuntimeArgs) => unknown)(
    type,
    localizeProps(type, props) as Record<string, unknown>,
    ...rest,
  )) as typeof preactJsx;

export const jsxs = ((type: unknown, props: Record<string, unknown>, ...rest: unknown[]) =>
  (preactJsxs as unknown as (...args: RuntimeArgs) => unknown)(
    type,
    localizeProps(type, props) as Record<string, unknown>,
    ...rest,
  )) as typeof preactJsxs;
