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
  "text",
  "subtitle",
  "tooltip",
  "note",
  "capNote",
  "sub",
  "confirmLabel",
  "cancelLabel",
  "pendingLabel",
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
  "cancelLabel",
  "emptyHint",
]);

/**
 * Strings that pass the runtime's "has letters" test but are code, not
 * interface: CLI flags, paths, regexes, JSON, key material. The runtime would
 * simply find no entry for them; leaving them out keeps the catalog readable.
 */
export function looksLikeCode(key: string): boolean {
  // A number glued inside a token is an id, a key or an example value, not a
  // quantity: "123456789:AA…", "ssh-ed25519 AAAAC3Nz…", "+201xxxxxxxx".
  if (/[A-Za-z]\{n\}[A-Za-z]/.test(key) && key.split(/\s+/).length < 5) return true;
  if (!/\s/.test(key) && /\{n\}/.test(key) && /[A-Za-z]/.test(key)) return true;
  key = key.replace(/\{[ns]\}/g, "0");
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

/**
 * A string that reads as a sentence or a label, wherever it sits: a variable
 * (`const title = ok ? "Saved" : "Failed"`), a return value, an error message.
 * Such text ends up on screen far more often than not, so it is listed even
 * outside the positions the rules above understand. Capitalised or ending in
 * sentence punctuation, so class lists, ids and enum values stay out.
 */
export function readsAsProse(text: string): boolean {
  const core = text.trim();
  if (!/[A-Za-z]{2}/.test(core)) return false;
  const words = core.split(/\s+/).filter((word) => /[A-Za-z]{2}/.test(word));
  if (words.length < 2 && !/^[A-Z][a-z]+$/.test(core)) return false;
  return /^[A-Z]/.test(core) || /[.?!…:]$/.test(core);
}

/** Attributes whose values are machine-read even when they look like words. */
const MACHINE_ATTRIBUTES = new Set([
  "class",
  "className",
  "id",
  "key",
  "type",
  "href",
  "src",
  "d",
  "viewBox",
  "role",
  "name",
  "value",
  "for",
  "rel",
  "target",
  "method",
  "action",
  "autocomplete",
  "inputMode",
  "pattern",
  "lang",
  "dir",
  "style",
  "accept",
  "download",
]);

/** Most keys a composed expression may expand to before it is reported instead. */
const MAX_ALTERNATIVES = 4;

/**
 * An expression inside composed text that yields a count: its value reaches
 * the runtime as digits, which the key rule turns into `{n}`. A guess from the
 * expression's spelling; a wrong guess only means the key never matches and
 * the text stays English, which the screenshot pass shows.
 */
const NUMERIC_EXPRESSION =
  /(\.length|\.size|\bMath\.|\.toFixed\(|\bn\b|\b(count|total|index|limit|runs|days|hours|minutes|seconds|rounded)\b|[a-z](Count|Total|Runs|Days|Hours|Minutes|Seconds|Length|Size|Number|Index|Limit)\b|_DAYS\b)/;

/** `x === 1 ? "" : "s"`: a plural ending glued to the word before it. */
function suffixAlternatives(expression: ts.Expression): string[] | null {
  let inner = expression;
  while (ts.isParenthesizedExpression(inner)) inner = inner.expression;
  if (!ts.isConditionalExpression(inner)) return null;
  const branches = [inner.whenTrue, inner.whenFalse];
  if (!branches.every((branch) => ts.isStringLiteral(branch) && /^[a-z]{0,3}$/.test(branch.text))) return null;
  return branches.map((branch) => (branch as ts.StringLiteral).text);
}

function cross(left: string[], right: string[]): string[] {
  const result: string[] = [];
  for (const a of left) for (const b of right) result.push(a + b);
  return result;
}

/** A composed key worth listing: it has words of its own, and is not code. */
function composedKey(text: string): string | null {
  const keyed = keyOf(text);
  if (!keyed) return null;
  const { key } = keyed;
  const literal = key.replace(/\{[ns]\}/g, " ");
  if (!/[A-Za-z]{2}/.test(literal)) return null;
  // Two slots need literal text between them for the runtime to split them.
  if (key.includes("{s}{s}")) return null;
  if (looksLikeCode(key)) return null;
  return key;
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

  // The text an expression composes, as keys with `{n}`/`{s}` slots:
  // `Open ${name}` → "Open {s}", `"failed: " + message` → "failed: {s}",
  // `${n} link${n === 1 ? "" : "s"}` → "{n} link" and "{n} links".
  const compose = (expression: ts.Expression): string[] => {
    if (ts.isStringLiteral(expression) || ts.isNoSubstitutionTemplateLiteral(expression)) return [expression.text];
    if (ts.isParenthesizedExpression(expression)) return compose(expression.expression);
    if (ts.isTemplateExpression(expression)) {
      let result = [expression.head.text];
      for (const span of expression.templateSpans) {
        result = cross(cross(result, slot(span.expression)), [span.literal.text]);
      }
      return result;
    }
    if (ts.isBinaryExpression(expression) && expression.operatorToken.kind === ts.SyntaxKind.PlusToken) {
      return cross(compose(expression.left), compose(expression.right));
    }
    return slot(expression);
  };

  const slot = (expression: ts.Expression): string[] => {
    const suffixes = suffixAlternatives(expression);
    if (suffixes) return suffixes;
    let inner = expression;
    while (ts.isParenthesizedExpression(inner)) inner = inner.expression;
    if (ts.isStringLiteral(inner) || ts.isNoSubstitutionTemplateLiteral(inner)) return [inner.text];
    // Literals inside the slot (`${on ? "on" : "off"}`) are keys of their own:
    // the runtime translates what a slot captures separately.
    recordExpression(expression);
    // A choice between two strings is text, whatever the condition counts.
    if (
      ts.isConditionalExpression(inner) &&
      [inner.whenTrue, inner.whenFalse].every((branch) => ts.isStringLiteral(branch) || ts.isNoSubstitutionTemplateLiteral(branch))
    ) {
      return ["{s}"];
    }
    // Judge the code, not the words inside its string literals (" · runs").
    const code = expression.getText(sourceFile).replace(/"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`/g, '""');
    return NUMERIC_EXPRESSION.test(code) ? ["{n}"] : ["{s}"];
  };

  const recordComposed = (expression: ts.Node, alternatives: string[]): boolean => {
    if (alternatives.length > MAX_ALTERNATIVES) {
      recordUnkeyable(expression);
      return false;
    }
    const keys = [...new Set(alternatives.map(composedKey).filter((key): key is string => key !== null))];
    for (const key of keys) occurrences.push({ key, file, line: lineOf(expression) });
    return keys.length > 0;
  };

  // String literals an expression can evaluate to: `"a"`, `cond ? "a" : "b"`,
  // `flag && "a"`, `value ?? "a"`, parentheses around any of those, and
  // composed text (templates and `+`).
  const recordExpression = (expression: ts.Expression | undefined): void => {
    if (!expression) return;
    if (ts.isStringLiteral(expression) || ts.isNoSubstitutionTemplateLiteral(expression)) {
      record(expression.text, expression);
    } else if (
      ts.isTemplateExpression(expression) ||
      (ts.isBinaryExpression(expression) && expression.operatorToken.kind === ts.SyntaxKind.PlusToken)
    ) {
      if (!recordComposed(expression, compose(expression)) && ts.isTemplateExpression(expression)) {
        recordUnkeyable(expression);
      }
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

  // Adjacent text and expressions inside one element render as a run of text
  // children, which the runtime also looks up as one sentence. Worth a key of
  // its own only when a glued plural ending makes the pieces meaningless alone.
  const recordTextRuns = (children: ts.NodeArray<ts.JsxChild>) => {
    let run: ts.JsxChild[] = [];
    const flush = () => {
      const hasSuffix = run.some((child) => ts.isJsxExpression(child) && child.expression && suffixAlternatives(child.expression));
      if (hasSuffix) {
        let alternatives = [""];
        for (const child of run) {
          if (ts.isJsxText(child)) alternatives = cross(alternatives, [decodeEntities(jsxTextValue(child.text))]);
          else if (ts.isJsxExpression(child) && child.expression) alternatives = cross(alternatives, slot(child.expression));
        }
        recordComposed(run[0], alternatives);
      }
      run = [];
    };
    for (const child of children) {
      if (ts.isJsxText(child) || (ts.isJsxExpression(child) && child.expression)) run.push(child);
      else if (!ts.isJsxExpression(child)) flush();
    }
    flush();
  };

  // Where a literal is an identifier, a comparison operand, a type or a lookup
  // rather than text for people.
  const isMachineContext = (node: ts.Node): boolean => {
    // Inside a machine-read attribute's value, up to any element nested in it
    // (`action={<button title="…">}` still holds interface text).
    for (let at = node.parent; at; at = at.parent) {
      if (ts.isJsxElement(at) || ts.isJsxSelfClosingElement(at) || ts.isJsxFragment(at)) break;
      if (ts.isJsxAttribute(at)) {
        if (MACHINE_ATTRIBUTES.has(at.name.getText(sourceFile))) return true;
        break;
      }
    }
    const parent = node.parent;
    if (!parent) return false;
    if (ts.isPropertyAssignment(parent) && parent.name === node) return true;
    if (ts.isElementAccessExpression(parent) || ts.isLiteralTypeNode(parent) || ts.isCaseClause(parent)) return true;
    if (
      ts.isBinaryExpression(parent) &&
      [
        ts.SyntaxKind.EqualsEqualsEqualsToken,
        ts.SyntaxKind.ExclamationEqualsEqualsToken,
        ts.SyntaxKind.EqualsEqualsToken,
        ts.SyntaxKind.ExclamationEqualsToken,
      ].includes(parent.operatorToken.kind)
    ) {
      return true;
    }
    return false;
  };

  const isConcatenation = (node: ts.Node): node is ts.BinaryExpression =>
    ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken;

  const recordProse = (node: ts.StringLiteral | ts.NoSubstitutionTemplateLiteral | ts.TemplateExpression | ts.BinaryExpression) => {
    if (isMachineContext(node)) return;
    // A piece of `"a " + b` is judged as the whole concatenation, once.
    let outer: ts.Node = node;
    for (let at = node.parent; at; at = at.parent) {
      if (isConcatenation(at)) outer = at;
      else if (!ts.isParenthesizedExpression(at)) break;
    }
    if (outer !== node) return;
    if (ts.isTemplateExpression(node) || isConcatenation(node)) {
      // A glued plural ending (`run${n === 1 ? "" : "s"}`) only ever builds
      // text for people, capitalised or not.
      const gluedPlural =
        ts.isTemplateExpression(node) && node.templateSpans.some((span) => suffixAlternatives(span.expression) !== null);
      const keys = compose(node).filter((text) => gluedPlural || readsAsProse(text.replace(/\{[ns]\}/g, "0")));
      if (keys.length) recordComposed(node, keys);
    } else if (readsAsProse(node.text)) {
      record(node.text, node);
    }
  };

  const visit = (node: ts.Node): void => {
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) return;
    if (ts.isCallExpression(node) && /^console\./.test(node.expression.getText(sourceFile))) return;
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateExpression(node)) {
      recordProse(node);
    } else if (isConcatenation(node) && !isConcatenation(node.parent)) {
      recordProse(node);
    }
    if (ts.isJsxElement(node) && isVerbatimElement(node.openingElement, sourceFile)) {
      // The runtime leaves these children alone (translate.ts), so their text
      // is content, not catalog material. The tag's own attributes still are.
      visit(node.openingElement);
      return;
    }
    if (ts.isJsxElement(node) || ts.isJsxFragment(node)) recordTextRuns(node.children);
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
        // The runtime translates native dialog messages (localeStore). An
        // options object (`confirm({ title, message })`) is covered by the
        // text-property rule as the visitor walks into it.
        if (!ts.isObjectLiteralExpression(first)) {
          const before = occurrences.length;
          recordExpression(first);
          if (occurrences.length === before) recordUnkeyable(node);
        }
      }
    }
    ts.forEachChild(node, visit);
  };

  visit(sourceFile);
  return { occurrences, unkeyable };
}
