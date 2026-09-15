// Catalog maintenance.
//
//   npm run i18n:extract   rewrite src/i18n/catalog/en.keys.json from source
//   npm run i18n:check     report keys the Arabic catalog lacks or no longer needs
//   npm run i18n:check -- --strict   ...and fail when there are any
//
// Run the check first thing after every upstream sync: it lists exactly the
// strings upstream added or reworded.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { extractFromSource, type Occurrence } from "../src/i18n/tools/extract.ts";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const srcDir = path.join(root, "src");
const catalogDir = path.join(srcDir, "i18n", "catalog");
const keysFile = path.join(catalogDir, "en.keys.json");
const arabicFile = path.join(catalogDir, "ar.json");
// Keys that stay English on purpose: product names, code, example values.
const untranslatedFile = path.join(catalogDir, "ar.untranslated.json");

// The i18n machinery itself, dev-only previews and tests are not interface.
const SKIPPED_DIRS = new Set([path.join(srcDir, "i18n"), path.join(srcDir, "dev")]);

function sourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIPPED_DIRS.has(full)) files.push(...sourceFiles(full));
    } else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) && !entry.name.endsWith(".d.ts")) {
      files.push(full);
    }
  }
  return files;
}

function extractAll() {
  const occurrences: Occurrence[] = [];
  const unkeyable: Occurrence[] = [];
  for (const file of sourceFiles(srcDir)) {
    const relative = path.relative(root, file).split(path.sep).join("/");
    const result = extractFromSource(relative, fs.readFileSync(file, "utf8"));
    occurrences.push(...result.occurrences);
    unkeyable.push(...result.unkeyable);
  }
  const byKey = new Map<string, Occurrence[]>();
  for (const occurrence of occurrences) {
    const list = byKey.get(occurrence.key) ?? [];
    list.push(occurrence);
    byKey.set(occurrence.key, list);
  }
  return { byKey, unkeyable };
}

function readJson<T>(file: string, fallback: T): T {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8")) as T;
  } catch {
    return fallback;
  }
}

const where = (list: Occurrence[] | undefined) =>
  (list ?? []).slice(0, 2).map((o) => `${o.file}:${o.line}`).join(", ");

const [command = "check", ...flags] = process.argv.slice(2);
const { byKey, unkeyable } = extractAll();
const keys = [...byKey.keys()].sort((a, b) => a.localeCompare(b, "en"));

if (command === "extract") {
  fs.writeFileSync(keysFile, JSON.stringify(keys, null, 2) + "\n");
  console.log(`wrote ${keys.length} keys to ${path.relative(root, keysFile)}`);
  if (flags.includes("--unkeyable")) {
    for (const item of unkeyable) console.log(`  unkeyable ${item.file}:${item.line}  ${item.key}`);
  }
  process.exit(0);
}

if (command !== "check") {
  console.error(`unknown command "${command}" (use extract or check)`);
  process.exit(2);
}

const strict = flags.includes("--strict");
const committed = readJson<string[]>(keysFile, []);
const arabic = readJson<Record<string, unknown>>(arabicFile, {});
const untranslated = new Set(readJson<string[]>(untranslatedFile, []));
const keySet = new Set(keys);

const stale = committed.length !== keys.length || committed.some((key, index) => key !== keys[index]);
const missing = keys.filter((key) => !(key in arabic) && !untranslated.has(key));
const orphaned = [...Object.keys(arabic), ...untranslated].filter((key) => !keySet.has(key));
const doubled = [...untranslated].filter((key) => key in arabic);

console.log(`interface strings: ${keys.length}`);
console.log(
  `arabic catalog:    ${Object.keys(arabic).length} entries, ${untranslated.size} kept English, ` +
    `${missing.length} missing, ${orphaned.length} orphaned`,
);
console.log(`unkeyable sites:   ${unkeyable.length} (npm run i18n:extract -- --unkeyable)`);
if (stale) console.log("en.keys.json is out of date: run npm run i18n:extract");

const limit = flags.includes("--all") ? Infinity : 40;
if (missing.length) {
  console.log("\nmissing from ar.json:");
  for (const key of missing.slice(0, limit)) console.log(`  ${JSON.stringify(key)}  ${where(byKey.get(key))}`);
  if (missing.length > limit) console.log(`  ... ${missing.length - limit} more (--all)`);
}
if (doubled.length) {
  console.log("\nboth translated and listed in ar.untranslated.json:");
  for (const key of doubled.slice(0, limit)) console.log(`  ${JSON.stringify(key)}`);
}
if (orphaned.length) {
  console.log("\nin ar.json or ar.untranslated.json but no longer in the source:");
  for (const key of orphaned.slice(0, limit)) console.log(`  ${JSON.stringify(key)}`);
}

process.exit(strict && (stale || missing.length > 0 || orphaned.length > 0 || doubled.length > 0) ? 1 : 0);
