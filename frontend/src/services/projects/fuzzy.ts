/**
 * Subsequence scoring, shared by the command palette and the settings search.
 *
 * The rule is "every character of the query appears in order", which is what
 * lets `npj` find "New project" — but ordering matters more than matching, so
 * a hit is scored rather than just accepted: runs of adjacent characters and
 * hits on a word boundary are what a human means by "this is the one", and an
 * early first hit breaks the remaining ties.
 */

const CONSECUTIVE_BONUS = 8;
const BOUNDARY_BONUS = 6;
const CHARACTER_SCORE = 1;
const LATE_START_PENALTY = 0.1;

/** Score for `needle` inside `haystack`, or null when it does not match. */
export function fuzzyScore(haystack: string, needle: string): number | null {
  const text = haystack.toLowerCase();
  const query = needle.trim().toLowerCase();
  if (!query) return 0;

  let score = 0;
  let cursor = 0;
  let previousIndex = -1;
  let firstIndex = -1;

  for (const character of query) {
    // A space in the query is a deliberate "and then, anywhere later": it lets
    // "new proj" match "New project" without paying the gap penalty twice.
    if (character === " ") {
      previousIndex = -2;
      continue;
    }
    const index = text.indexOf(character, cursor);
    if (index < 0) return null;
    if (firstIndex < 0) firstIndex = index;
    score += CHARACTER_SCORE;
    if (index === previousIndex + 1) score += CONSECUTIVE_BONUS;
    else if (index === 0 || !isWordCharacter(text[index - 1])) score += BOUNDARY_BONUS;
    previousIndex = index;
    cursor = index + 1;
  }

  return score - firstIndex * LATE_START_PENALTY;
}

export function fuzzyMatches(haystack: string, needle: string): boolean {
  return fuzzyScore(haystack, needle) !== null;
}

function isWordCharacter(character: string): boolean {
  return /[a-z0-9]/.test(character);
}
