// Development counterpart of ./jsx-runtime.ts, used by `vite dev`.

import { Fragment, jsxDEV as preactJsxDEV } from "preact/jsx-dev-runtime";
import { localizeProps } from "./translate.ts";

export { Fragment };
export type { JSX } from "preact/jsx-dev-runtime";

type RuntimeArgs = [type: unknown, props: Record<string, unknown>, ...rest: unknown[]];

export const jsxDEV = ((type: unknown, props: Record<string, unknown>, ...rest: unknown[]) =>
  (preactJsxDEV as unknown as (...args: RuntimeArgs) => unknown)(
    type,
    localizeProps(type, props) as Record<string, unknown>,
    ...rest,
  )) as typeof preactJsxDEV;
