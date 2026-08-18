import { deepStrictEqual, strictEqual } from "node:assert/strict";
import { test } from "node:test";
import type { Playbook } from "../../models/playbook.ts";
import type { RegisteredSkill } from "../../models/skill.ts";
import {
  BUILTIN_SLASH_COMMANDS,
  applySlashCommand,
  buildSlashRegistry,
  filterSlashCommands,
  findSlashCommand,
  isEscapedSlash,
  parseBrowserArg,
  parseOnOffArg,
  parsePortArg,
  parseSlashInput,
  pickDeployPlaybook,
  pickReviewSkill,
  skillInvocation,
  slashHelpText,
  slashTokenAt,
  unescapeSlash,
} from "./slashCommandState.ts";

function playbook(id: string, title = id): Playbook {
  return { id, title, prompt: "do the thing", order: 0 };
}

function skill(command: string, name = command): RegisteredSkill {
  return { name, command, provider: "claude", description: `${name} description` };
}

const commandsOf = (entries: { command: string }[]) => entries.map((entry) => entry.command);

test("the registry merges built-ins, playbooks, and skills", () => {
  const registry = buildSlashRegistry({
    playbooks: [playbook("security-review"), playbook("deploy-to-hestia")],
    skills: [skill("playwright-e2e"), skill("wordpress-guard")],
  });

  strictEqual(registry.length, BUILTIN_SLASH_COMMANDS.length + 4);
  strictEqual(findSlashCommand(registry, "security-review")?.group, "playbook");
  strictEqual(findSlashCommand(registry, "playwright-e2e")?.group, "skill");
  strictEqual(findSlashCommand(registry, "test")?.group, "builtin");
});

test("a playbook whose id collides with a built-in keeps the prefixed spelling", () => {
  const registry = buildSlashRegistry({ playbooks: [playbook("review"), playbook("ship")] });

  strictEqual(findSlashCommand(registry, "review")?.group, "builtin");
  const shadowed = findSlashCommand(registry, "pb-review");
  strictEqual(shadowed?.group, "playbook");
  strictEqual(shadowed?.command, "pb-review");
  // A unique id stays typeable as itself, and the prefixed form still works.
  strictEqual(findSlashCommand(registry, "ship")?.command, "ship");
  strictEqual(findSlashCommand(registry, "pb-ship")?.command, "ship");
});

test("a skill that collides with a built-in or playbook is not registered twice", () => {
  const registry = buildSlashRegistry({
    playbooks: [playbook("audit-live-site")],
    skills: [skill("test"), skill("audit-live-site"), skill("browser")],
  });

  strictEqual(findSlashCommand(registry, "test")?.group, "builtin");
  strictEqual(findSlashCommand(registry, "audit-live-site")?.group, "playbook");
  strictEqual(findSlashCommand(registry, "browser")?.group, "builtin");
  strictEqual(registry.filter((entry) => entry.command === "test").length, 1);
});

test("skill commands are normalized into typeable words", () => {
  const registry = buildSlashRegistry({ skills: [{ name: "Deploy To Hestia", provider: "claude" }] });
  strictEqual(findSlashCommand(registry, "deploy-to-hestia")?.group, "skill");
});

test("the filter is prefix-first, then subsequence, then keywords", () => {
  const registry = buildSlashRegistry({ skills: [skill("playwright-e2e")] });

  deepStrictEqual(commandsOf(filterSlashCommands(registry, "auto")), ["autopilot", "autotest"]);
  strictEqual(filterSlashCommands(registry, "apt")[0].command, "autopilot");
  strictEqual(filterSlashCommands(registry, "screenshot")[0].command, "screenshot");
  // "backup" appears only in /snapshot's keywords.
  strictEqual(filterSlashCommands(registry, "backup")[0].command, "snapshot");
  deepStrictEqual(filterSlashCommands(registry, "zzzz"), []);
  strictEqual(filterSlashCommands(registry, "").length, registry.length);
});

test("an exact match sorts above a longer command that merely starts with it", () => {
  const registry = buildSlashRegistry({ skills: [skill("test-runner")] });
  strictEqual(filterSlashCommands(registry, "test")[0].command, "test");
});

test("the token is detected at the start of the text and after whitespace", () => {
  deepStrictEqual(slashTokenAt("/aut", 4), { start: 0, end: 4, query: "aut" });
  deepStrictEqual(slashTokenAt("fix this /sn", 12), { start: 9, end: 12, query: "sn" });
  deepStrictEqual(slashTokenAt("line one\n/te", 12), { start: 9, end: 12, query: "te" });
  // The caret sits before the token's end: only what precedes it filters.
  deepStrictEqual(slashTokenAt("/autopilot", 3), { start: 0, end: 3, query: "au" });
});

test("the token is not detected inside a path, a url, or after an escape", () => {
  strictEqual(slashTokenAt("see src/state/chat", 18), null);
  strictEqual(slashTokenAt("https://example.com/a", 21), null);
  strictEqual(slashTokenAt("//literal", 9), null);
  strictEqual(slashTokenAt("\\/literal", 9), null);
  // A completed word with an argument closes the menu.
  strictEqual(slashTokenAt("/test the cart", 14), null);
  strictEqual(slashTokenAt("plain text", 10), null);
});

test("picking a command replaces the token and leaves room for an argument", () => {
  const registry = buildSlashRegistry({});
  const text = "fix this /sn";
  const token = slashTokenAt(text, text.length)!;

  const withArg = applySlashCommand(text, token, findSlashCommand(registry, "snapshot")!);
  strictEqual(withArg.text, "fix this /snapshot ");
  strictEqual(withArg.caret, withArg.text.length);

  const withoutArg = applySlashCommand(text, token, findSlashCommand(registry, "review")!);
  strictEqual(withoutArg.text, "fix this /review");
});

test("picking a command keeps whatever follows the caret", () => {
  const text = "/sn and then ship";
  const token = slashTokenAt(text, 3)!;
  const registry = buildSlashRegistry({});
  const applied = applySlashCommand(text, token, findSlashCommand(registry, "snapshot")!);
  strictEqual(applied.text, "/snapshot  and then ship");
  strictEqual(applied.caret, "/snapshot ".length);
});

test("parseSlashInput reads the command and its argument", () => {
  deepStrictEqual(parseSlashInput("/test"), { command: "test", arg: "" });
  deepStrictEqual(parseSlashInput("  /test the cart total  "), {
    command: "test",
    arg: "the cart total",
  });
  deepStrictEqual(parseSlashInput("/autopilot on 12"), { command: "autopilot", arg: "on 12" });
  deepStrictEqual(parseSlashInput("/TEST"), { command: "test", arg: "" });
});

test("parseSlashInput ignores ordinary prompts and escaped slashes", () => {
  strictEqual(parseSlashInput("fix the /test route"), null);
  strictEqual(parseSlashInput("//test"), null);
  strictEqual(parseSlashInput("\\/test"), null);
  strictEqual(parseSlashInput("/"), null);
  strictEqual(parseSlashInput("/ test"), null);
  strictEqual(parseSlashInput("https://example.com"), null);
});

test("an escaped slash is unescaped once, and only at the start", () => {
  strictEqual(isEscapedSlash("//test"), true);
  strictEqual(isEscapedSlash("\\/test"), true);
  strictEqual(isEscapedSlash("/test"), false);
  strictEqual(unescapeSlash("//test"), "/test");
  strictEqual(unescapeSlash("\\/test"), "/test");
  strictEqual(unescapeSlash("//a and http://x//y"), "/a and http://x//y");
  strictEqual(unescapeSlash("plain"), "plain");
});

test("parseOnOffArg reads the switch and the optional count", () => {
  deepStrictEqual(parseOnOffArg("on"), { on: true, count: null });
  deepStrictEqual(parseOnOffArg("on 12"), { on: true, count: 12 });
  deepStrictEqual(parseOnOffArg("OFF"), { on: false, count: null });
  deepStrictEqual(parseOnOffArg(""), { on: null, count: null });
  deepStrictEqual(parseOnOffArg("maybe"), { on: null, count: null });
  deepStrictEqual(parseOnOffArg("on -4"), { on: true, count: null });
});

test("parsePortArg accepts only preview ports", () => {
  strictEqual(parsePortArg("3000"), 3000);
  strictEqual(parsePortArg(" 5173 "), 5173);
  strictEqual(parsePortArg("80"), null);
  strictEqual(parsePortArg("70000"), null);
  strictEqual(parsePortArg("nope"), null);
});

test("parseBrowserArg turns a bare port into container loopback", () => {
  strictEqual(parseBrowserArg("3000"), "http://127.0.0.1:3000/");
  strictEqual(parseBrowserArg("https://example.com/a"), "https://example.com/a");
  strictEqual(parseBrowserArg("example.com/a"), "http://example.com/a");
  strictEqual(parseBrowserArg(""), null);
  strictEqual(parseBrowserArg("not a url"), null);
});

test("a picked skill inserts an explicit invocation", () => {
  strictEqual(skillInvocation({ name: "Playwright", command: "playwright-e2e" }),
    "Use the playwright-e2e skill: ");
  strictEqual(skillInvocation({ name: "Playwright" }), "Use the Playwright skill: ");
  strictEqual(skillInvocation({ name: "", command: "" }), "");
});

test("/deploy and /review find their playbook and skill by preference order", () => {
  strictEqual(pickDeployPlaybook([playbook("deploy-to-hestia")])?.id, "deploy-to-hestia");
  strictEqual(
    pickDeployPlaybook([playbook("deploy-to-hestia"), playbook("deploy-hestia")])?.id,
    "deploy-hestia",
  );
  strictEqual(pickDeployPlaybook([playbook("security-review")]), null);

  strictEqual(pickReviewSkill([skill("code-review-guard")])?.command, "code-review-guard");
  strictEqual(
    pickReviewSkill([skill("code-review-guard"), skill("review-protocol")])?.command,
    "review-protocol",
  );
  strictEqual(pickReviewSkill([skill("wordpress-guard")]), null);
});

test("/help lists every group and the escape", () => {
  const help = slashHelpText(buildSlashRegistry({
    playbooks: [playbook("security-review")],
    skills: [skill("playwright-e2e")],
  }));
  strictEqual(help.includes("/test"), true);
  strictEqual(help.includes("Playbooks: /security-review"), true);
  strictEqual(help.includes("Skills: /playwright-e2e"), true);
  strictEqual(help.includes("//"), true);
});
