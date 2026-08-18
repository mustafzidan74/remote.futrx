import { fuzzyScore } from "../../shared/fuzzy.ts";

/**
 * The command palette: one keystroke to reach anything the workspace can
 * already do — open a chat, open a project, jump to a settings page, start
 * something new.
 *
 * Everything here is a pure function over plain records so the palette can be
 * tested without a DOM: the container builds the item list from the workspace
 * context, this module decides what is shown, in what order, and where the
 * highlight sits.
 */

export type CommandKind = "action" | "chat" | "project" | "settings";

export interface CommandItem {
  id: string;
  title: string;
  subtitle?: string;
  /** Words the filter matches but the row never prints (slugs, aliases). */
  keywords?: string;
  /** Section heading. Sections render in the order they first appear. */
  group: string;
  kind: CommandKind;
  /** Unix-ms activity, used to break score ties. Timeless items leave it at 0. */
  recency?: number;
  run: () => void;
}

export interface CommandPaletteState {
  open: boolean;
  query: string;
  highlight: number;
}

export type CommandPaletteAction =
  | { type: "open" }
  | { type: "close" }
  | { type: "toggle" }
  | { type: "set-query"; query: string }
  | { type: "move"; delta: number; count: number }
  | { type: "highlight"; index: number };

/** How many rows the list renders at once. Beyond this, refine the query. */
export const COMMAND_RESULT_LIMIT = 40;

const TITLE_MATCH_BONUS = 25;

class CommandPaletteStateTransitions {
  createInitial(): CommandPaletteState {
    return { open: false, query: "", highlight: 0 };
  }

  readonly reduce = (
    state: CommandPaletteState,
    action: CommandPaletteAction,
  ): CommandPaletteState => {
    switch (action.type) {
      case "open":
        // Reopening starts clean: the palette is a launcher, not a form.
        return { open: true, query: "", highlight: 0 };
      case "close":
        return { ...state, open: false };
      case "toggle":
        return state.open ? { ...state, open: false } : { open: true, query: "", highlight: 0 };
      case "set-query":
        return { ...state, query: action.query, highlight: 0 };
      case "move":
        return { ...state, highlight: wrapHighlight(state.highlight + action.delta, action.count) };
      case "highlight":
        return { ...state, highlight: Math.max(0, action.index) };
    }
  };
}

export const commandPaletteState = new CommandPaletteStateTransitions();

/** Wraps at both ends, so Down on the last row lands on the first. */
export function wrapHighlight(index: number, count: number): number {
  if (count <= 0) return 0;
  return ((index % count) + count) % count;
}

/**
 * Ranked matches for `query`.
 *
 * A hit in the title outranks a hit only the subtitle or the keywords explain:
 * "the chat called deploy" and "a chat in the project called deploy" are
 * different questions, and the first is nearly always the one being asked.
 */
export function filterCommands(
  items: CommandItem[],
  query: string,
  limit = COMMAND_RESULT_LIMIT,
): CommandItem[] {
  const trimmed = query.trim();
  if (!trimmed) return items.slice(0, limit);

  const scored: Array<{ item: CommandItem; score: number; order: number }> = [];
  items.forEach((item, order) => {
    const titleScore = fuzzyScore(item.title, trimmed);
    const haystackScore = fuzzyScore(
      `${item.title} ${item.subtitle ?? ""} ${item.keywords ?? ""}`,
      trimmed,
    );
    if (titleScore === null && haystackScore === null) return;
    const score = titleScore === null ? (haystackScore as number) : titleScore + TITLE_MATCH_BONUS;
    scored.push({ item, score, order });
  });

  scored.sort((left, right) => {
    if (right.score !== left.score) return right.score - left.score;
    const recency = (right.item.recency ?? 0) - (left.item.recency ?? 0);
    if (recency !== 0) return recency;
    return left.order - right.order;
  });

  return scored.slice(0, limit).map((entry) => entry.item);
}

export interface CommandSection {
  group: string;
  items: CommandItem[];
}

/** Groups a result list for rendering, keeping the ranked order intact. */
export function sectionsOf(items: CommandItem[]): CommandSection[] {
  const sections: CommandSection[] = [];
  for (const item of items) {
    const existing = sections.find((section) => section.group === item.group);
    if (existing) existing.items.push(item);
    else sections.push({ group: item.group, items: [item] });
  }
  return sections;
}

export interface CommandPaletteProject {
  id: string;
  name: string;
  slug: string;
}

export interface CommandPaletteChat {
  id: string;
  title: string;
  projectId?: string;
  lastMessageAt: number;
}

export interface CommandPaletteSettingsTab {
  id: string;
  label: string;
  description: string;
}

export interface CommandPaletteSources {
  projects: CommandPaletteProject[];
  chats: CommandPaletteChat[];
  settingsTabs: CommandPaletteSettingsTab[];
  /** Name of the theme the toggle switches to, e.g. "light". */
  nextThemeName: string;
}

export interface CommandPaletteHandlers {
  openChat: (chatId: string) => void;
  newProject: () => void;
  newChat: (projectId: string) => void;
  openProject: (projectId: string) => void;
  openProjectPreview: (projectId: string) => void;
  snapshotProject: (projectId: string) => void;
  openSettings: (tabId: string) => void;
  toggleTheme: () => void;
}

export const CHATS_GROUP = "Chats";
export const ACTIONS_GROUP = "Actions";
export const PROJECTS_GROUP = "Projects";
export const SETTINGS_GROUP = "Settings";

/**
 * The full catalog, in the order an empty query shows it.
 *
 * Chats come first because "take me back to what I was doing" is why the
 * palette gets opened. A workspace with nothing in it yet has no chats, so the
 * first-run reading order starts at "New project" on its own.
 */
export function buildCommandItems(
  sources: CommandPaletteSources,
  handlers: CommandPaletteHandlers,
): CommandItem[] {
  const projectsById = new Map(sources.projects.map((project) => [project.id, project]));
  const items: CommandItem[] = [];

  const recentChats = [...sources.chats].sort(
    (left, right) => (right.lastMessageAt || 0) - (left.lastMessageAt || 0),
  );
  for (const chat of recentChats) {
    const project = chat.projectId ? projectsById.get(chat.projectId) : undefined;
    items.push({
      id: `chat:${chat.id}`,
      title: chat.title || "Untitled chat",
      subtitle: project ? project.name : "No project",
      keywords: project ? project.slug : "loose unassigned",
      group: CHATS_GROUP,
      kind: "chat",
      recency: chat.lastMessageAt || 0,
      run: () => handlers.openChat(chat.id),
    });
  }

  items.push({
    id: "action:new-project",
    title: "New project",
    subtitle: "Create a sandboxed container",
    keywords: "create add container",
    group: ACTIONS_GROUP,
    kind: "action",
    run: handlers.newProject,
  });

  for (const project of sources.projects) {
    items.push({
      id: `action:new-chat:${project.id}`,
      title: `New chat in ${project.name}`,
      subtitle: project.slug,
      keywords: "create start conversation",
      group: ACTIONS_GROUP,
      kind: "action",
      run: () => handlers.newChat(project.id),
    });
  }

  items.push({
    id: "action:toggle-theme",
    title: `Switch to ${sources.nextThemeName} theme`,
    subtitle: "Appearance",
    keywords: "dark light colour color",
    group: ACTIONS_GROUP,
    kind: "action",
    run: handlers.toggleTheme,
  });

  for (const project of sources.projects) {
    items.push({
      id: `project:${project.id}`,
      title: `Open ${project.name}`,
      subtitle: project.slug,
      keywords: "project container settings info",
      group: PROJECTS_GROUP,
      kind: "project",
      run: () => handlers.openProject(project.id),
    });
    items.push({
      id: `project-preview:${project.id}`,
      title: `Open preview of ${project.name}`,
      subtitle: "Preview links and public shares",
      keywords: `${project.slug} url port share`,
      group: PROJECTS_GROUP,
      kind: "project",
      run: () => handlers.openProjectPreview(project.id),
    });
    items.push({
      id: `project-snapshot:${project.id}`,
      title: `Snapshot ${project.name}`,
      subtitle: "Archive the files and database",
      keywords: `${project.slug} backup restore`,
      group: PROJECTS_GROUP,
      kind: "project",
      run: () => handlers.snapshotProject(project.id),
    });
  }

  for (const tab of sources.settingsTabs) {
    items.push({
      id: `settings:${tab.id}`,
      title: tab.label,
      subtitle: tab.description,
      keywords: `settings preferences ${tab.id}`,
      group: SETTINGS_GROUP,
      kind: "settings",
      run: () => handlers.openSettings(tab.id),
    });
  }

  return items;
}
