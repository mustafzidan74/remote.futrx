import assert from "node:assert/strict";
import test from "node:test";
import {
  CUSTOM_LANGUAGE_VALUE,
  isCustomLanguage,
  languageSelectValue,
  preferencesDirty,
  preferencesProblem,
  preferencesRequest,
  preferencesSummary,
} from "./replyPreferencesState.ts";
import {
  DEFAULT_AGENT_PREFERENCES,
  MAX_EXTRA_INSTRUCTIONS_LENGTH,
  MAX_REPLY_LANGUAGE_LENGTH,
  type AgentPreferences,
} from "../../models/agentPreferences.ts";

function draft(overrides: Partial<AgentPreferences> = {}): AgentPreferences {
  return { ...DEFAULT_AGENT_PREFERENCES, ...overrides };
}

test("languageSelectValue maps built-ins to themselves and anything else to custom", () => {
  assert.equal(languageSelectValue(""), "auto");
  assert.equal(languageSelectValue("auto"), "auto");
  assert.equal(languageSelectValue("ar-EG"), "ar-EG");
  assert.equal(languageSelectValue("Levantine Arabic"), CUSTOM_LANGUAGE_VALUE);
});

test("isCustomLanguage recognises a free-text label", () => {
  assert.equal(isCustomLanguage(""), false);
  assert.equal(isCustomLanguage("en"), false);
  assert.equal(isCustomLanguage("Levantine Arabic"), true);
});

test("preferencesRequest trims and defaults an empty language to auto", () => {
  assert.deepEqual(preferencesRequest(draft({ replyLanguage: "  ", extraInstructions: " x " })), {
    replyLanguage: "auto",
    tone: "default",
    extraInstructions: "x",
    applyTo: "all",
  });
});

test("preferencesProblem enforces the same caps as the backend", () => {
  assert.equal(preferencesProblem(draft()), null);
  assert.match(
    preferencesProblem(draft({ replyLanguage: "a".repeat(MAX_REPLY_LANGUAGE_LENGTH + 1) })) ?? "",
    /characters or fewer/,
  );
  assert.match(preferencesProblem(draft({ replyLanguage: "a\nb" })) ?? "", /single line/);
  assert.match(
    preferencesProblem(
      draft({ extraInstructions: "a".repeat(MAX_EXTRA_INSTRUCTIONS_LENGTH + 1) }),
    ) ?? "",
    /Extra instructions/,
  );
});

test("preferencesDirty ignores whitespace-only edits", () => {
  const stored = draft({ replyLanguage: "ar-EG", extraInstructions: "Never force-push." });
  assert.equal(preferencesDirty(stored, stored), false);
  assert.equal(
    preferencesDirty(draft({ ...stored, extraInstructions: " Never force-push. " }), stored),
    false,
  );
  assert.equal(preferencesDirty(draft({ ...stored, tone: "concise" }), stored), true);
  assert.equal(preferencesDirty(draft({ ...stored, replyLanguage: "en" }), stored), true);
});

test("preferencesSummary mirrors what the agent is told", () => {
  assert.match(preferencesSummary(draft()), /Nothing is injected/);
  assert.match(
    preferencesSummary(draft({ extraInstructions: "Never force-push." })),
    /Only your extra instructions/,
  );

  const egyptian = preferencesSummary(draft({ replyLanguage: "ar-EG", tone: "concise" }));
  assert.match(egyptian, /Egyptian Arabic/);
  assert.match(egyptian, /keep code, identifiers, commands and file paths in English/);
  assert.match(egyptian, /be concise/);

  assert.match(
    preferencesSummary(draft({ replyLanguage: "Levantine Arabic" })),
    /Reply in Levantine Arabic/,
  );
  assert.match(preferencesSummary(draft({ replyLanguage: "en", tone: "detailed" })), /be thorough/);
});
