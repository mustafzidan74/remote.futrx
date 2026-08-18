import type { Playbook, PlaybookSkillRef } from "../../models/playbook.ts";
import type { RegisteredSkill } from "../../models/skill.ts";

/**
 * Editing state for the admin Playbooks page.
 *
 * The API takes the whole library in one PUT, so the page edits a draft array
 * and submits it. Everything that decides what the draft looks like lives
 * here, which keeps the form a form.
 */

/** Ids are what the server keys entries on, so they are generated, not typed. */
export function playbookIdFromTitle(title: string, taken: string[]): string {
  const base =
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48) || "playbook";
  if (!taken.includes(base)) return base;
  for (let suffix = 2; ; suffix++) {
    const candidate = `${base}-${suffix}`;
    if (!taken.includes(candidate)) return candidate;
  }
}

export function newPlaybook(existing: Playbook[]): Playbook {
  return {
    id: playbookIdFromTitle("New playbook", existing.map((entry) => entry.id)),
    title: "New playbook",
    icon: "⚡",
    hint: "",
    prompt: "",
    skills: [],
    mode: "",
    provider: "",
    order: existing.length,
  };
}

export function updatePlaybook(
  library: Playbook[],
  id: string,
  patch: Partial<Playbook>,
): Playbook[] {
  return library.map((entry) => (entry.id === id ? { ...entry, ...patch } : entry));
}

export function removePlaybook(library: Playbook[], id: string): Playbook[] {
  return reindex(library.filter((entry) => entry.id !== id));
}

/** Moves an entry one slot in `direction`; out-of-range moves are no-ops. */
export function movePlaybook(library: Playbook[], id: string, direction: -1 | 1): Playbook[] {
  const index = library.findIndex((entry) => entry.id === id);
  const target = index + direction;
  if (index < 0 || target < 0 || target >= library.length) return library;
  const next = [...library];
  [next[index], next[target]] = [next[target], next[index]];
  return reindex(next);
}

export function togglePlaybookSkill(
  library: Playbook[],
  id: string,
  skill: RegisteredSkill,
): Playbook[] {
  return library.map((entry) => {
    if (entry.id !== id) return entry;
    const skills = entry.skills ?? [];
    const command = skill.command || skill.name;
    const existing = skills.findIndex((selected) => skillKey(selected) === `${skill.source ?? ""}:${command}`);
    if (existing >= 0) {
      return { ...entry, skills: skills.filter((_, index) => index !== existing) };
    }
    const added: PlaybookSkillRef = {
      name: skill.name,
      command,
      source: skill.source,
    };
    return { ...entry, skills: [...skills, added] };
  });
}

export function hasPlaybookSkill(playbook: Playbook, skill: RegisteredSkill): boolean {
  const command = skill.command || skill.name;
  return (playbook.skills ?? []).some(
    (selected) => skillKey(selected) === `${skill.source ?? ""}:${command}`,
  );
}

/**
 * Skill refs the server does not currently publish. A playbook may name a
 * skill an operator has not installed yet, so this is a warning on the page
 * rather than a validation error in the API.
 */
export function unknownPlaybookSkills(
  playbook: Playbook,
  available: RegisteredSkill[],
): string[] {
  const known = new Set(available.map((skill) => (skill.command || skill.name).toLowerCase()));
  return (playbook.skills ?? [])
    .map((skill) => (skill.command || skill.name || "").trim())
    .filter((command) => command.length > 0 && !known.has(command.toLowerCase()));
}

/** What the page submits: trimmed, renumbered, and free of empty entries. */
export function playbookLibraryRequest(library: Playbook[]): Playbook[] {
  return reindex(
    library
      .map((entry) => ({
        ...entry,
        title: entry.title.trim(),
        icon: (entry.icon ?? "").trim(),
        hint: (entry.hint ?? "").trim(),
        prompt: entry.prompt.trim(),
      }))
      .filter((entry) => entry.title !== "" || entry.prompt !== ""),
  );
}

/** Why the library cannot be saved yet, in the order an admin would fix it. */
export function playbookLibraryProblem(library: Playbook[]): string | null {
  for (const entry of library) {
    if (!entry.title.trim()) return "Every playbook needs a title.";
    if (!entry.prompt.trim()) return `"${entry.title.trim()}" needs a prompt.`;
  }
  return null;
}

function reindex(library: Playbook[]): Playbook[] {
  return library.map((entry, index) => ({ ...entry, order: index }));
}

function skillKey(skill: PlaybookSkillRef): string {
  return `${skill.source ?? ""}:${skill.command || skill.name || ""}`;
}
