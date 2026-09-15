// Finds the interface text the translation shim will look up, so the catalog
// can be kept complete. It reads source with the TypeScript compiler and
// reports each string under the same key the runtime computes (normalize.ts).
//
// Not part of the app bundle: only scripts/i18n.ts imports it.

import ts from "typescript";
import { jsxTextValue, keyOf } from "../normalize.ts";

/** JSX attributes whose string values are shown to people. */
export const TEXT_ATTRIBUTES = new Set([
  "title",
  "placeholder",
  "aria-label",
  "aria-description",
  "aria-placeholder",
  "alt",
  "label",
  "description",
  "hint",
  "message",
]);

/** Object properties that conventionally end up rendered as text. */
export const TEXT_PROPERTIES = new Set([
  "label",
  "title",
  "description",
  "hint",
  "message",
  "placeholder",
  "confirmLabel",
  "pendingLabel",
  "emptyHint",
]);

/**
 * Strings that pass the runtime's "has letters" test but are code, not
 * interface: CLI flags, paths, regexes, JSON, key material. The runtime would
 * simply find no entry for them; leaving them out keeps the catalog readable.
 */
export function looksLikeCode(key: string): boolean {
  if (/^[-=_*#─]{3,}/.test(key)) return true;
  // A flag, unless it is the tail of a sentence split around code ("-style secrets and…").
  if (/^--?[a-z]/i.test(key) && key.split(/\s+/).length < 4) return true;
  if (/^[{[]/.test(key) || /":/.test(key)) return true;
  if (/\(\?[a-z]+\)/i.test(key)) return true;
  if (!/\s/.test(key)) {
    if (/[\/(){}[\]=<>"'`$@|]/.test(key)) return true;
    if (/^[\w.-]+\.[a-z]{1,5}$/i.test(key)) return true;
    if (/^[a-z]+[A-Z]\w*$/.test(key)) return true; // camelCase identifier
    if (/^[a-z0-9]+([_-][a-z0-9]+)+$/.test(key)) return true; // snake_case, kebab-case
  }
  return false;
}

const ENTITIES: Record<string, string> = {
  lt: "<", gt: ">", amp: "&", quot: '"', apos: "'", nbsp: " ",
  rsquo: "’", lsquo: "‘", rdquo: "”", ldquo: "“",
  mdash: "—", ndash: "–", hellip: "…", middot: "·",
  times: "×", rarr: "→", larr: "←", uarr: "↑", darr: "↓", crarr: "↵", bull: "•", copy: "©",
};

/** JSX text reaches the runtime with entities decoded; keys must match that. */
export function decodeEntities(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (match, body: string) => {
    if (body[0] === "#") {
      const code = body[1] === "x" || body[1] === "X" ? parseInt(body.slice(2), 16) : parseInt(body.slice(1), 10);
      return Number.isFinite(code) ? String.fromCodePoint(code) : match;
    }
    return ENTITIES[body.toLowerCase()] ?? match;
  });
}

/** Mirrors `isVerbatim` in translate.ts. */
const VERBATIM_TAGS = new Set(["pre", "code", "kbd", "samp", "textarea", "script", "style"]);

function isVerbatimElement(opening: ts.JsxOpeningElement, sourceFile: ts.SourceFile): boolean {
  if (VERBATIM_TAGS.has(opening.tagName.getText(sourceFile))) return true;
  return opening.attributes.properties.some((attribute) => {
    if (!ts.isJsxAttribute(attribute)) return false;
    const name = attribute.name.getText(sourceFile);
    if (name === "dir") return true;
    if (name !== "translate") return false;
    const value = attribute.initializer;
    return (
      (!!value && ts.isStringLiteral(value) && value.text === "no") ||
      (!!value && ts.isJsxExpression(value) && value.expression?.kind === ts.SyntaxKind.FalseKeyword)
    );
  });
}

export interface Occurrence {
  key: string;
  file: string;
  line: number;
}

export interface ExtractionResult {
  occurrences: Occurrence[];
  /**
   * Text the runtime cannot key reliably — template literals with
   * substitutions in text positions, and calls like `alert("...")` that never
   * pass through JSX. Listed so they can be given a catalog entry or a `t()`.
   */
  unkeyable: Occurrence[];
}

export function extractFromSource(file: string, source: string): ExtractionResult {
  const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const sourceFile = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, kind);
  const occurrences: Occurrence[] = [];
  const unkeyable: Occurrence[] = [];

  const lineOf = (node: ts.Node) =>
    sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;

  const record = (text: string, node: ts.Node) => {
    const keyed = keyOf(text);
    if (keyed && !looksLikeCode(keyed.key)) occurrences.push({ key: keyed.key, file, line: lineOf(node) });
  };

  const recordUnkeyable = (node: ts.Node) => {
    unkeyable.push({ key: node.getText(sourceFile).slice(0, 120), file, line: lineOf(node) });
  };

  // String literals an expression can evaluate to: `"a"`, `cond ? "a" : "b"`,
  // `flag && "a"`, `value ?? "a"`, and parentheses around any of those.
  const recordExpression = (expression: ts.Expression | undefined): void => {
    if (!expression) return;
    if (ts.isStringLiteral(expression) || ts.isNoSubstitutionTemplateLiteral(expression)) {
      record(expression.text, expression);
    } else if (ts.isTemplateExpression(expression)) {
      recordUnkeyable(expression);
    } else if (ts.isParenthesizedExpression(expression)) {
      recordExpression(expression.expression);
    } else if (ts.isConditionalExpression(expression)) {
      recordExpression(expression.whenTrue);
      recordExpression(expression.whenFalse);
    } else if (
      ts.isBinaryExpression(expression) &&
      [ts.SyntaxKind.AmpersandAmpersandToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(
        expression.operatorToken.kind,
      )
    ) {
      recordExpression(expression.left);
      recordExpression(expression.right);
    }
  };

  const visit = (node: ts.Node): void => {
    if (ts.isJsxElement(node) && isVerbatimElement(node.openingElement, sourceFile)) {
      // The runtime leaves these children alone (translate.ts), so their text
      // is content, not catalog material. The tag's own attributes still are.
      visit(node.openingElement);
      return;
    }
    if (ts.isJsxText(node)) {
      const text = decodeEntities(jsxTextValue(node.text));
      if (text.trim()) record(text, node);
    } else if (ts.isJsxExpression(node) && node.parent && (ts.isJsxElement(node.parent) || ts.isJsxFragment(node.parent))) {
      recordExpression(node.expression);
    } else if (ts.isJsxAttribute(node) && TEXT_ATTRIBUTES.has(node.name.getText(sourceFile))) {
      const initializer = node.initializer;
      if (initializer && ts.isStringLiteral(initializer)) record(initializer.text, initializer);
      else if (initializer && ts.isJsxExpression(initializer)) recordExpression(initializer.expression);
    } else if (
      ts.isPropertyAssignment(node) &&
      (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name)) &&
      TEXT_PROPERTIES.has(node.name.text)
    ) {
      recordExpression(node.initializer);
    } else if (ts.isCallExpression(node)) {
      const callee = node.expression.getText(sourceFile);
      const [first] = node.arguments;
      if (callee === "t") {
        recordExpression(first);
      } else if (["alert", "confirm", "prompt", "window.alert", "window.confirm", "window.prompt"].includes(callee) && first) {
        recordUnkeyable(node);
      }
    }
    ts.forEachChild(node, visit);
  };

  visit(sourceFile);
  return { occurrences, unkeyable };
}
