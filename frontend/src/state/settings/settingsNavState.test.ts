import assert from "node:assert/strict";
import test from "node:test";
import {
  filterSettingsTabs,
  firstSettingsMatch,
  matchingSettingsTabIds,
} from "./settingsNavState.ts";

const tabs = [
  { id: "appearance", label: "Appearance", description: "Choose how Remote looks." },
  { id: "users", label: "Users", description: "Control who can access this server." },
  { id: "secrets", label: "Secrets vault", description: "Store tokens and licence keys." },
  { id: "trash", label: "Trash", description: "Restore a deleted project." },
];

test("keeps every tab when nothing is typed", () => {
  assert.deepEqual(
    filterSettingsTabs(tabs, "  ").map((tab) => tab.id),
    ["appearance", "users", "secrets", "trash"],
  );
});

test("ranks a label hit above a description-only hit", () => {
  const matches = filterSettingsTabs(tabs, "trash").map((tab) => tab.id);
  assert.equal(matches[0], "trash");

  // Nobody types "users" to find Users here — they type what they want to do.
  assert.equal(firstSettingsMatch(tabs, "who can access")?.id, "users");
});

test("reports the matching ids so a grouped nav can grey the rest out", () => {
  assert.deepEqual([...matchingSettingsTabIds(tabs, "vault")], ["secrets"]);
  assert.equal(matchingSettingsTabIds(tabs, "zzz").size, 0);
  assert.equal(firstSettingsMatch(tabs, "zzz"), null);
});
