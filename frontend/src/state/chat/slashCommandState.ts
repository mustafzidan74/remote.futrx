import type { Playbook } from "../../models/playbook.ts";
import type { RegisteredSkill } from "../../models/skill.ts";

/**
 * Slash commands: the registry, the filter, and the parser behind the
 * composer's `/` menu.
 *
 * Everything here is pure. The composer owns the side effects — sending a
 * prompt, switching a mode, calling an API — and this module only answers
 * three questions: what commands exist right now, which of them matches what
 * the user is typing, and what a typed line means.
 *
 * Three sources are merged into one list because a user does not care which
 * of them a command came from: the built-ins are the platform's own verbs,
 * playbooks are the operator's saved prompts, and skills are whatever the
 * agent can load. They are labelled by group so the menu can still say.
 */

export type SlashGroup = "builtin" | "playbook" | "skill";

export const SLASH_GROUP_LABEL: Record<SlashGroup, string> = {
  builtin: "Commands",
  playbook: "Playbooks",
  skill: "Skills",
};

/** Identifies what a built-in does, so the composer can switch on it. */
export type SlashAction =
  | "test"
  | "deploy"
  | "snapshot"
  | "preview"
  | "screenshot"
  | "autopilot"
  | "autotest"
  | "review"
  | "pr"
  | "skills"
  | "browser"
  | "help";

export interface SlashCommand {
  /** Stable key, unique across the merged registry. */
  id: string;
  /** The word typed after the slash. */
  command: string;
  /** Extra spellings the filter accepts, never shown as the primary name. */
  aliases?: string[];
  group: SlashGroup;
  title: string;
  hint?: string;
  /** Rendered after the command in the menu, e.g. "on|off [rounds]". */
  argHint?: string;
  /** True when picking the entry should leave the caret ready for an argument. */
  takesArg?: boolean;
  /** Extra text the filter searches: a skill's description, a playbook's hint. */
  keywords?: string;
  action?: SlashAction;
  playbook?: Playbook;
  skill?: RegisteredSkill;
}

/**
 * The platform's own verbs. Each one maps to something the composer can
 * already do through a button; the command is a keyboard path to it, not a
 * second implementation.
 */
export const BUILTIN_SLASH_COMMANDS: SlashCommand[] = [
  {
    id: "builtin:test",
    command: "test",
    group: "builtin",
    action: "test",
    title: "/test",
    argHint: "[url or what to check]",
    takesArg: true,
    hint: "Run a Playwright check. No argument tests the last change.",
    keywords: "playwright e2e check verify",
  },
  {
    id: "builtin:deploy",
    command: "deploy",
    group: "builtin",
    action: "deploy",
    title: "/deploy",
    hint: "Load the deploy-to-Hestia playbook into the composer.",
    keywords: "hestia ship release publish",
  },
  {
    id: "builtin:snapshot",
    command: "snapshot",
    group: "builtin",
    action: "snapshot",
    title: "/snapshot",
    argHint: "[label]",
    takesArg: true,
    hint: "Take a project snapshot now. No agent run.",
    keywords: "backup archive restore point",
  },
  {
    id: "builtin:preview",
    command: "preview",
    group: "builtin",
    action: "preview",
    title: "/preview",
    hint: "Open this project's running app in a new tab.",
    keywords: "open app port browser",
  },
  {
    id: "builtin:screenshot",
    command: "screenshot",
    group: "builtin",
    action: "screenshot",
    title: "/screenshot",
    argHint: "[port]",
    takesArg: true,
    hint: "Photograph the preview and share it.",
    keywords: "capture picture image share",
  },
  {
    id: "builtin:autopilot",
    command: "autopilot",
    group: "builtin",
    action: "autopilot",
    title: "/autopilot",
    argHint: "on|off [rounds]",
    takesArg: true,
    hint: "Arm or stop the unattended follow-up loop.",
    keywords: "loop rounds unattended",
  },
  {
    id: "builtin:autotest",
    command: "autotest",
    group: "builtin",
    action: "autotest",
    title: "/autotest",
    argHint: "on|off",
    takesArg: true,
    hint: "Check every change automatically after a run.",
    keywords: "playwright policy verify",
  },
  {
    id: "builtin:review",
    command: "review",
    group: "builtin",
    action: "review",
    title: "/review",
    hint: "Switch to review mode and review the last change.",
    keywords: "audit inspect critique",
  },
  {
    id: "builtin:pr",
    command: "pr",
    group: "builtin",
    action: "pr",
    title: "/pr",
    argHint: "[title]",
    takesArg: true,
    hint: "Open a GitHub pull request from this project's workspace.",
    keywords: "github pull request push branch merge",
  },
  {
    id: "builtin:skills",
    command: "skills",
    group: "builtin",
    action: "skills",
    title: "/skills",
    hint: "Browse every skill this chat can load.",
    keywords: "library capabilities",
  },
  {
    id: "builtin:browser",
    command: "browser",
    group: "builtin",
    action: "browser",
    title: "/browser",
    argHint: "<url or port>",
    takesArg: true,
    hint: "Load an address in the project's Agent Browser.",
    keywords: "chromium novnc navigate open",
  },
  {
    id: "builtin:help",
    command: "help",
    group: "builtin",
    action: "help",
    title: "/help",
    hint: "List every command available here.",
    keywords: "commands usage",
  },
];

/** Playbook ids `/deploy` looks for, newest naming first. */
export const DEPLOY_PLAYBOOK_IDS = ["deploy-hestia", "deploy-to-hestia"];

/** The prompt `/review` puts in the composer. */
export const REVIEW_PROMPT = "Review the last change.";

/**
 * The prompt `/deploy` falls back to when neither a deploy playbook nor a
 * deploy skill is installed. It is deliberately vague about the mechanism:
 * with nothing registered, the agent has to read the project to find out how
 * it ships, and inventing steps here would be worse than asking.
 */
export const DEPLOY_FALLBACK_PROMPT =
  "Deploy this project to its Hestia host: check the deployment configuration in the repo, " +
  "run the project's own deploy procedure, and report what you did and what the live URL is. " +
  "Ask before doing anything destructive to the live site.";

export interface SlashRegistryInput {
  playbooks?: Playbook[];
  skills?: RegisteredSkill[];
}

/**
 * Merges the three sources into one ordered registry.
 *
 * Collisions resolve by group priority — a built-in always wins the bare
 * word — and the loser keeps a prefixed spelling rather than disappearing:
 * a playbook whose id is `test` is still reachable as `/pb-test`.
 */
export function buildSlashRegistry(input: SlashRegistryInput = {}): SlashCommand[] {
  const registry: SlashCommand[] = [...BUILTIN_SLASH_COMMANDS];
  const taken = new Set(registry.map((entry) => entry.command));

  for (const playbook of input.playbooks ?? []) {
    const id = normalizeCommandWord(playbook.id);
    if (!id) continue;
    const prefixed = `pb-${id}`;
    const command = taken.has(id) ? prefixed : id;
    if (taken.has(command)) continue;
    taken.add(command);
    if (command !== prefixed) taken.add(prefixed);
    registry.push({
      id: `playbook:${playbook.id}`,
      command,
      // The prefixed spelling always works, so a user who learned one form
      // never has to find out whether their playbook's id was unique.
      aliases: command === prefixed ? [] : [prefixed],
      group: "playbook",
      title: `/${command}`,
      hint: playbook.hint || playbook.title,
      keywords: `${playbook.title} ${playbook.hint || ""}`,
      playbook,
    });
  }

  for (const skill of input.skills ?? []) {
    const command = normalizeCommandWord(skill.command || skill.name);
    if (!command || taken.has(command)) continue;
    taken.add(command);
    registry.push({
      id: `skill:${skill.source || "skill"}:${command}`,
      command,
      group: "skill",
      title: `/${command}`,
      hint: skill.description || skill.name,
      keywords: `${skill.name} ${skill.description || ""}`,
      skill,
    });
  }

  return registry;
}

/** Reduces a playbook id or skill command to the word a user can type. */
export function normalizeCommandWord(value: string): string {
  return (value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/* ------------------------------------------------------------------ *
 * Detecting the token being typed
 * ------------------------------------------------------------------ */

export interface SlashToken {
  /** Index of the leading slash in the full text. */
  start: number;
  /** Index just past the caret; the token is replaced up to here. */
  end: number;
  /** What has been typed after the slash, up to the caret. */
  query: string;
}

/**
 * Finds the slash token the caret sits in, or null when the menu should be
 * closed.
 *
 * A token qualifies when its slash begins the text or follows whitespace, so
 * a URL's slashes and a path typed mid-sentence never open the menu. `//` and
 * `\/` are the deliberate escapes: both mean "I want a literal slash", and
 * both keep the menu shut.
 */
export function slashTokenAt(text: string, caret: number): SlashToken | null {
  const position = Math.max(0, Math.min(caret, text.length));
  let start = position;
  while (start > 0 && !/\s/.test(text[start - 1])) start -= 1;
  if (text[start] !== "/") return null;
  // `//` and `\/` both mean "I am writing a literal slash".
  if (text[start + 1] === "/") return null;
  const previous = start > 0 ? text[start - 1] : "";
  if (previous === "/" || previous === "\\") return null;
  const query = text.slice(start + 1, position);
  if (/\s/.test(query)) return null;
  return { start, end: position, query };
}

/* ------------------------------------------------------------------ *
 * Filtering
 * ------------------------------------------------------------------ */

/**
 * Ranks the registry against a query. An empty query keeps the registry's own
 * order, which puts the platform's verbs above a long skill library.
 *
 * The match is prefix-first and subsequence-second: typing `apt` finds
 * `/autopilot` without also dragging in every command that merely contains
 * those letters somewhere in its description.
 */
export function filterSlashCommands(registry: SlashCommand[], query: string): SlashCommand[] {
  const term = query.trim().toLowerCase();
  if (!term) return [...registry];

  const scored: { entry: SlashCommand; rank: number; index: number }[] = [];
  registry.forEach((entry, index) => {
    const rank = rankSlashCommand(entry, term);
    if (rank !== null) scored.push({ entry, rank, index });
  });
  scored.sort((left, right) => left.rank - right.rank || left.index - right.index);
  return scored.map((item) => item.entry);
}

function rankSlashCommand(entry: SlashCommand, term: string): number | null {
  const command = entry.command.toLowerCase();
  if (command === term) return 0;
  if (command.startsWith(term)) return 1;
  for (const alias of entry.aliases ?? []) {
    const lowered = alias.toLowerCase();
    if (lowered === term) return 0;
    if (lowered.startsWith(term)) return 2;
  }
  // An anchored subsequence ("apt" in "autopilot") beats a floating one
  // ("apt" in "snapshot"): the first letter is what the user reached for.
  if (isSubsequence(term, command)) return command[0] === term[0] ? 3 : 4;
  const keywords = `${entry.title} ${entry.hint || ""} ${entry.keywords || ""}`.toLowerCase();
  if (keywords.includes(term)) return 5;
  return null;
}

/** True when every character of `needle` appears in `haystack`, in order. */
function isSubsequence(needle: string, haystack: string): boolean {
  let index = 0;
  for (const character of haystack) {
    if (character === needle[index]) index += 1;
    if (index === needle.length) return true;
  }
  return needle.length === 0;
}

/** Finds one entry by the exact word typed, aliases included. */
export function findSlashCommand(
  registry: SlashCommand[],
  command: string,
): SlashCommand | null {
  const word = command.trim().toLowerCase();
  if (!word) return null;
  return (
    registry.find(
      (entry) =>
        entry.command.toLowerCase() === word ||
        (entry.aliases ?? []).some((alias) => alias.toLowerCase() === word),
    ) ?? null
  );
}

/* ------------------------------------------------------------------ *
 * Parsing a submitted line
 * ------------------------------------------------------------------ */

export interface ParsedSlashInput {
  command: string;
  arg: string;
}

/**
 * Reads a composed message as a command, or answers null when it is an
 * ordinary prompt. Only the whole message counts: a slash on line three is
 * part of what the user is saying, not an instruction to the composer.
 */
export function parseSlashInput(text: string): ParsedSlashInput | null {
  const trimmed = text.trim();
  if (!trimmed.startsWith("/") || isEscapedSlash(text)) return null;
  const body = trimmed.slice(1);
  const boundary = body.search(/\s/);
  const command = (boundary === -1 ? body : body.slice(0, boundary)).toLowerCase();
  if (!command || !/^[a-z0-9_-]+$/.test(command)) return null;
  const arg = boundary === -1 ? "" : body.slice(boundary).trim();
  return { command, arg };
}

/** True when the message opens with an escaped slash rather than a command. */
export function isEscapedSlash(text: string): boolean {
  const trimmed = text.trimStart();
  return trimmed.startsWith("//") || trimmed.startsWith("\\/");
}

/**
 * Removes the escape so the agent receives what the user meant to write.
 * Only the leading escape is touched: a `//` inside a URL further along the
 * message is not an escape and must survive untouched.
 */
export function unescapeSlash(text: string): string {
  const leading = text.length - text.trimStart().length;
  const prefix = text.slice(0, leading);
  const body = text.slice(leading);
  if (body.startsWith("//")) return prefix + body.slice(1);
  if (body.startsWith("\\/")) return prefix + body.slice(1);
  return text;
}

/** Replaces the token under the caret with a chosen command. */
export function applySlashCommand(
  text: string,
  token: SlashToken,
  entry: SlashCommand,
): { text: string; caret: number } {
  const insertion = `/${entry.command}${entry.takesArg ? " " : ""}`;
  const next = text.slice(0, token.start) + insertion + text.slice(token.end);
  return { text: next, caret: token.start + insertion.length };
}

/* ------------------------------------------------------------------ *
 * Argument parsing
 * ------------------------------------------------------------------ */

export interface OnOffArg {
  /** null when the argument was missing or unreadable. */
  on: boolean | null;
  /** The optional number after on/off, when one was given and is sane. */
  count: number | null;
}

/** Reads `on`, `off`, and an optional round count: "on 12", "off". */
export function parseOnOffArg(arg: string): OnOffArg {
  const parts = arg.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const [word, ...rest] = parts;
  let on: boolean | null = null;
  if (word === "on" || word === "true" || word === "yes" || word === "1") on = true;
  if (word === "off" || word === "false" || word === "no" || word === "0") on = false;
  const parsed = Number.parseInt(rest[0] ?? "", 10);
  return { on, count: Number.isFinite(parsed) && parsed > 0 ? parsed : null };
}

/** Reads a bare port number, the only argument `/screenshot` takes. */
export function parsePortArg(arg: string): number | null {
  const parsed = Number.parseInt(arg.trim(), 10);
  if (!Number.isFinite(parsed) || parsed < 1024 || parsed > 65535) return null;
  return parsed;
}

/**
 * Reads `/browser`'s argument, which may be a full URL or just a port. A bare
 * port becomes container loopback, because that is the only address the
 * in-container browser can reach without meeting the platform's sign-in page.
 */
export function parseBrowserArg(arg: string): string | null {
  const value = arg.trim();
  if (!value) return null;
  const port = parsePortArg(value);
  if (port !== null) return `http://127.0.0.1:${port}/`;
  if (/^https?:\/\//i.test(value)) return value;
  if (/^[a-z0-9.-]+(:\d+)?(\/|$)/i.test(value)) return `http://${value}`;
  return null;
}

/* ------------------------------------------------------------------ *
 * Prompt fragments the commands insert
 * ------------------------------------------------------------------ */

/**
 * The explicit invocation a picked skill inserts. Naming the skill in the
 * prompt is what makes the agent load it deliberately, the same way typing
 * `/skill` does in a terminal agent — selecting it in the chat only makes it
 * available.
 */
export function skillInvocation(skill: Pick<RegisteredSkill, "name" | "command">): string {
  const name = (skill.command || skill.name || "").trim();
  return name ? `Use the ${name} skill: ` : "";
}

/** The deploy playbook `/deploy` prefers, or null when none is installed. */
export function pickDeployPlaybook(playbooks: Playbook[]): Playbook | null {
  for (const id of DEPLOY_PLAYBOOK_IDS) {
    const found = playbooks.find((playbook) => playbook.id === id);
    if (found) return found;
  }
  return playbooks.find((playbook) => playbook.id.includes("deploy")) ?? null;
}

/** The deploy skill `/deploy` falls back to, or null when none is registered. */
export function pickDeploySkill(skills: RegisteredSkill[]): RegisteredSkill | null {
  return pickSkillMatching(skills, ["deploy-to-hestia", "deploy-hestia"], "deploy");
}

/**
 * The review skill `/review` selects. `review-protocol` is the name the
 * command documents; a library that calls its reviewer something else still
 * gets picked up rather than leaving the command half-working.
 */
export function pickReviewSkill(skills: RegisteredSkill[]): RegisteredSkill | null {
  return pickSkillMatching(skills, ["review-protocol", "code-review-guard"], "review");
}

function pickSkillMatching(
  skills: RegisteredSkill[],
  preferred: string[],
  fallbackSubstring: string,
): RegisteredSkill | null {
  const identity = (skill: RegisteredSkill) =>
    normalizeCommandWord(skill.command || skill.name);
  for (const name of preferred) {
    const found = skills.find((skill) => identity(skill) === name);
    if (found) return found;
  }
  return skills.find((skill) => identity(skill).includes(fallbackSubstring)) ?? null;
}

/** The `/help` answer: one line per command, grouped. */
export function slashHelpText(registry: SlashCommand[]): string {
  const groups: SlashGroup[] = ["builtin", "playbook", "skill"];
  const lines: string[] = [];
  for (const group of groups) {
    const entries = registry.filter((entry) => entry.group === group);
    if (entries.length === 0) continue;
    const names = entries.map((entry) => `/${entry.command}`).join(", ");
    lines.push(`${SLASH_GROUP_LABEL[group]}: ${names}`);
  }
  lines.push("Type // to send a message that starts with a literal slash.");
  return lines.join("\n");
}
