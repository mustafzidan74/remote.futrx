import { fuzzyScore } from "../../shared/fuzzy.ts";

/**
 * Search over the settings navigation.
 *
 * Fourteen destinations in five groups is a wall to scan when you already know
 * the word you are looking for. Typing filters the same list rather than
 * replacing it, so the groups a match belongs to stay visible and the operator
 * keeps their bearings; Enter jumps to the best match.
 */

export interface SettingsNavTab {
  id: string;
  label: string;
  description: string;
}

/**
 * Matching tabs, best first. The description is searched too — "who can access
 * this server" should find Users even when the word "users" never gets typed.
 */
export function filterSettingsTabs<T extends SettingsNavTab>(tabs: T[], query: string): T[] {
  const trimmed = query.trim();
  if (!trimmed) return [...tabs];

  const scored: Array<{ tab: T; score: number; order: number }> = [];
  tabs.forEach((tab, order) => {
    const labelScore = fuzzyScore(tab.label, trimmed);
    const bodyScore = fuzzyScore(`${tab.label} ${tab.id} ${tab.description}`, trimmed);
    if (labelScore === null && bodyScore === null) return;
    const score = labelScore === null ? (bodyScore as number) : labelScore + LABEL_MATCH_BONUS;
    scored.push({ tab, score, order });
  });

  scored.sort((left, right) =>
    right.score === left.score ? left.order - right.order : right.score - left.score,
  );
  return scored.map((entry) => entry.tab);
}

const LABEL_MATCH_BONUS = 25;

/** The tab Enter opens, or null when nothing matched. */
export function firstSettingsMatch<T extends SettingsNavTab>(
  tabs: T[],
  query: string,
): T | null {
  return filterSettingsTabs(tabs, query)[0] ?? null;
}

/** Ids of the matching tabs, for a nav that renders the full grouped list. */
export function matchingSettingsTabIds(tabs: SettingsNavTab[], query: string): Set<string> {
  return new Set(filterSettingsTabs(tabs, query).map((tab) => tab.id));
}
