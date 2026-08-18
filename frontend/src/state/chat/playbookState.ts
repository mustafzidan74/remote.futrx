import type { ChatProvider, SelectedSkill, UpdateChatInput } from "../../models/chat.ts";
import type { Playbook, PlaybookSkillRef } from "../../models/playbook.ts";
import { chatPreferenceState } from "./chatPreferenceState.ts";

/**
 * Playbooks: the state behind the composer's one-click prompt templates.
 *
 * Two things happen when a playbook is picked, and both live here so the
 * button stays a button:
 *
 * 1. The prompt's `{{placeholders}}` are resolved against the chat's project.
 *    A placeholder nobody can fill in — `{{askUrl}}` is the deliberate one —
 *    survives into the composer so the user replaces it themselves; a prompt
 *    in that state is never sent automatically.
 * 2. The playbook's skills, mode, and provider become a chat-meta patch for
 *    the existing chat update API. Skills merge into the current selection
 *    rather than replacing it, except across a provider switch, which is the
 *    one case where the old provider's skills stop being meaningful.
 */

/** Placeholders the client can fill from the chat's project. */
export interface PlaybookContext {
  projectName?: string;
  slug?: string;
  previewUrl?: string;
}

export interface UnresolvedPlaceholder {
  token: string;
  name: string;
  start: number;
  end: number;
}

export interface ResolvedPrompt {
  text: string;
  /** Placeholders left in the text, positioned against `text`. */
  unresolved: UnresolvedPlaceholder[];
  /** True when nothing is left for the user to fill in. */
  ready: boolean;
}

const placeholderPattern = /\{\{\s*([A-Za-z][A-Za-z0-9_]*)\s*\}\}/g;

/**
 * Resolves the placeholders a playbook prompt may carry.
 *
 * Unknown names and names whose value is not available right now (a chat with
 * no project, a project with no running preview) are left in place verbatim,
 * so the user can see exactly what the playbook still needs.
 */
export function resolvePlaybookPrompt(
  prompt: string,
  context: PlaybookContext = {}
): ResolvedPrompt {
  const values: Record<string, string | undefined> = {
    project: context.projectName,
    slug: context.slug,
    previewUrl: context.previewUrl,
  };

  let text = "";
  let cursor = 0;
  const unresolved: UnresolvedPlaceholder[] = [];
  placeholderPattern.lastIndex = 0;

  for (let match = placeholderPattern.exec(prompt); match; match = placeholderPattern.exec(prompt)) {
    const [token, name] = match;
    text += prompt.slice(cursor, match.index);
    cursor = match.index + token.length;
    const value = (values[name] ?? "").trim();
    if (value) {
      text += value;
      continue;
    }
    unresolved.push({ token, name, start: text.length, end: text.length + token.length });
    text += token;
  }
  text += prompt.slice(cursor);

  return { text, unresolved, ready: unresolved.length === 0 };
}

/** The range to select so the user lands on the first thing to fill in. */
export function firstUnresolvedRange(resolved: ResolvedPrompt): { start: number; end: number } | null {
  const first = resolved.unresolved[0];
  return first ? { start: first.start, end: first.end } : null;
}

/** Human-readable list of what a playbook still needs, for the composer hint. */
export function unresolvedSummary(resolved: ResolvedPrompt): string {
  if (resolved.ready) return "";
  const names = [...new Set(resolved.unresolved.map((item) => item.token))];
  return names.join(", ");
}

export interface PlaybookChatState {
  provider: ChatProvider;
  selectedSkills: SelectedSkill[];
  mode?: string;
}

export interface PlaybookApplication {
  /** Empty when the chat already matches the playbook. */
  patch: UpdateChatInput;
  changed: boolean;
  provider: ChatProvider;
}

/**
 * Turns a playbook into a chat-meta patch. Returning the patch instead of
 * calling the API keeps this testable and lets the caller decide whether a
 * no-op round trip is worth making.
 */
export function playbookChatPatch(
  playbook: Playbook,
  current: PlaybookChatState
): PlaybookApplication {
  const targetProvider = (playbook.provider || current.provider) as ChatProvider;
  const providerChanged = targetProvider !== current.provider;
  // A provider switch is exactly when the previous provider's skills stop
  // being loadable, which is why the composer's own provider toggle clears
  // them. Applying a playbook across that boundary behaves the same way.
  const base = providerChanged ? [] : current.selectedSkills;
  const skills = mergeSkills(base, playbook.skills ?? [], targetProvider);

  const patch: UpdateChatInput = {};
  if (providerChanged) {
    patch.provider = targetProvider;
    patch.model = "";
    patch.reasoningEffort = "";
    patch.serviceTier = "";
  }
  if (skills.length !== current.selectedSkills.length || providerChanged) {
    patch.selectedSkills = skills;
  }
  if (playbook.mode && playbook.mode !== current.mode) {
    patch.mode = playbook.mode;
  }

  return { patch, changed: Object.keys(patch).length > 0, provider: targetProvider };
}

function mergeSkills(
  selected: SelectedSkill[],
  additions: PlaybookSkillRef[],
  provider: ChatProvider
): SelectedSkill[] {
  const merged = [...selected];
  for (const addition of additions) {
    const command = (addition.command || addition.name || "").trim();
    if (!command) continue;
    const skill: SelectedSkill = {
      name: (addition.name || command).trim(),
      command,
      provider: (addition.provider || provider) as ChatProvider,
      source: addition.source,
    };
    if (chatPreferenceState.includesSkill(merged, skill, provider)) continue;
    merged.push(skill);
  }
  return merged;
}

/** Playbooks are stored ordered; guard against a hand-edited document. */
export function sortPlaybooks(playbooks: Playbook[]): Playbook[] {
  return [...playbooks].sort((left, right) => (left.order ?? 0) - (right.order ?? 0));
}

/** The label the composer menu shows for one entry. */
export function playbookLabel(playbook: Playbook): string {
  const title = playbook.title.trim();
  const icon = (playbook.icon || "").trim();
  // Seeded titles already start with their emoji; do not print it twice.
  if (icon && title.startsWith(icon)) return title;
  return icon ? `${icon} ${title}` : title;
}
