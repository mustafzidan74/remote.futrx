import assert from "node:assert/strict";
import test from "node:test";
import {
  buildCommandItems,
  commandPaletteState,
  filterCommands,
  sectionsOf,
  wrapHighlight,
  type CommandPaletteHandlers,
  type CommandPaletteSources,
} from "./commandPaletteState.ts";

const sources: CommandPaletteSources = {
  projects: [
    { id: "shop", name: "Shop", slug: "shop" },
    { id: "blog", name: "Blog", slug: "blog-site" },
  ],
  chats: [
    { id: "old", title: "Fix the checkout", projectId: "shop", lastMessageAt: 10 },
    { id: "new", title: "Deploy notes", projectId: "blog", lastMessageAt: 30 },
    { id: "loose", title: "Scratch", lastMessageAt: 20 },
  ],
  settingsTabs: [
    { id: "trash", label: "Trash", description: "Restore a deleted project." },
    { id: "secrets", label: "Secrets vault", description: "Store tokens once." },
  ],
  nextThemeName: "light",
};

function recordingHandlers(log: string[]): CommandPaletteHandlers {
  return {
    openChat: (id) => log.push(`chat:${id}`),
    openHome: () => log.push("home"),
    newProject: () => log.push("new-project"),
    newChat: (id) => log.push(`new-chat:${id}`),
    openProject: (id) => log.push(`project:${id}`),
    openProjectPreview: (id) => log.push(`preview:${id}`),
    snapshotProject: (id) => log.push(`snapshot:${id}`),
    openSettings: (id) => log.push(`settings:${id}`),
    toggleTheme: () => log.push("theme"),
  };
}

test("lists chats newest first, then actions, projects, and settings", () => {
  const items = buildCommandItems(sources, recordingHandlers([]));
  assert.deepEqual(
    sectionsOf(items).map((section) => section.group),
    ["Chats", "Actions", "Projects", "Settings"],
  );

  const chats = items.filter((item) => item.kind === "chat");
  assert.deepEqual(chats.map((item) => item.title), ["Deploy notes", "Scratch", "Fix the checkout"]);
  // A chat outside every project says so rather than leaving the row bare.
  assert.equal(chats[1].subtitle, "No project");

  const actions = items.filter((item) => item.kind === "action").map((item) => item.title);
  assert.deepEqual(actions, [
    "Home",
    "New project",
    "New chat in Shop",
    "New chat in Blog",
    "Switch to light theme",
  ]);

  const projectTitles = items.filter((item) => item.kind === "project").map((item) => item.title);
  assert.deepEqual(projectTitles, [
    "Open Shop",
    "Open preview of Shop",
    "Snapshot Shop",
    "Open Blog",
    "Open preview of Blog",
    "Snapshot Blog",
  ]);
});

test("runs the handler the picked item stands for", () => {
  const log: string[] = [];
  const items = buildCommandItems(sources, recordingHandlers(log));
  for (const id of ["chat:new", "action:new-project", "project-snapshot:shop", "settings:trash"]) {
    items.find((item) => item.id === id)?.run();
  }
  assert.deepEqual(log, ["chat:new", "new-project", "snapshot:shop", "settings:trash"]);
});

test("filters fuzzily and ranks title matches first", () => {
  const items = buildCommandItems(sources, recordingHandlers([]));

  const deploy = filterCommands(items, "deploy");
  assert.equal(deploy[0].id, "chat:new");

  // "np" is a subsequence of "New project" and of nothing better.
  assert.equal(filterCommands(items, "np")[0].id, "action:new-project");

  // A slug lives in the keywords, so it finds the project without being shown.
  const bySlug = filterCommands(items, "blog-site").map((item) => item.id);
  assert.ok(bySlug.includes("project:blog"), bySlug.join(", "));

  assert.deepEqual(filterCommands(items, "zzzz"), []);
  // A blank query is the full catalog, capped at the render limit.
  assert.equal(filterCommands(items, "  ", 3).length, 3);
});

test("moves the highlight with wrap-around and resets it on a new query", () => {
  assert.equal(wrapHighlight(-1, 3), 2);
  assert.equal(wrapHighlight(3, 3), 0);
  assert.equal(wrapHighlight(1, 0), 0);

  const opened = commandPaletteState.reduce(commandPaletteState.createInitial(), { type: "open" });
  assert.deepEqual(opened, { open: true, query: "", highlight: 0 });

  const moved = commandPaletteState.reduce(opened, { type: "move", delta: -1, count: 4 });
  assert.equal(moved.highlight, 3);

  const typed = commandPaletteState.reduce(moved, { type: "set-query", query: "de" });
  assert.deepEqual(typed, { open: true, query: "de", highlight: 0 });

  // Toggling shut and open again clears the query rather than restoring it.
  const closed = commandPaletteState.reduce(typed, { type: "toggle" });
  assert.equal(closed.open, false);
  assert.deepEqual(commandPaletteState.reduce(closed, { type: "toggle" }), {
    open: true,
    query: "",
    highlight: 0,
  });
});
