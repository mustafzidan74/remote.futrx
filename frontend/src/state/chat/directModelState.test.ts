import assert from "node:assert/strict";
import test from "node:test";
import {
  directBadge,
  directModelLabel,
  isDirect,
  NO_DIRECT_MODEL,
  sameDirectModel,
  type DirectModelChoice,
} from "../../models/directModels.ts";

const GEMINI: DirectModelChoice = {
  source: "pool",
  providerId: "gemini",
  providerLabel: "Google Gemini",
  model: "gemini-flash-latest",
  modelLabel: "Gemini 3.7 Flash",
};

const LOCAL: DirectModelChoice = {
  source: "local",
  providerLabel: "On this server",
  model: "qwen3:1.7b",
  modelLabel: "qwen3:1.7b",
};

test("a chat with no direct model runs an agent", () => {
  assert.equal(isDirect(undefined), false);
  assert.equal(isDirect(NO_DIRECT_MODEL), false);
  assert.equal(directBadge(NO_DIRECT_MODEL, [GEMINI]), null);
});

test("a stored choice matches the offer it came from", () => {
  const stored = { source: "pool" as const, providerId: "gemini", model: "gemini-flash-latest" };
  assert.equal(sameDirectModel(stored, GEMINI), true);
  assert.equal(sameDirectModel(stored, LOCAL), false);
});

test("two models from the same provider are told apart", () => {
  // The picker lists every model a provider offers, so matching on the
  // provider alone would light up the wrong row.
  const flash = { source: "pool" as const, providerId: "gemini", model: "gemini-flash-latest" };
  const lite: DirectModelChoice = { ...GEMINI, model: "gemini-flash-lite-latest" };
  assert.equal(sameDirectModel(flash, lite), false);
});

test("the local model carries no provider id", () => {
  const stored = { source: "local" as const, providerId: "", model: "qwen3:1.7b" };
  assert.equal(sameDirectModel(stored, LOCAL), true);
});

test("the badge names the model and says what it cannot do", () => {
  const badge = directBadge({ source: "pool", providerId: "gemini", model: "gemini-flash-latest" }, [
    GEMINI,
  ]);
  assert.ok(badge);
  assert.equal(badge.short, "Gemini 3.7 Flash · Google Gemini");
  // The whole point of the badge: an operator who asked for a file edit and
  // got an explanation needs to see why.
  assert.match(badge.title, /cannot read or write files/);
  assert.match(badge.title, /switch to an agent/);
});

test("the badge still works for a model no longer on offer", () => {
  // A provider switched off after a chat was pointed at it. The chat keeps its
  // choice, and the header should still name it rather than going blank.
  const badge = directBadge({ source: "pool", providerId: "groq", model: "openai/gpt-oss-120b" }, []);
  assert.ok(badge);
  assert.equal(badge.short, "openai/gpt-oss-120b");
});

test("the label falls back to something readable", () => {
  assert.equal(directModelLabel({ source: "local" }), "local model");
  assert.equal(directModelLabel({ source: "pool", model: "llama" }), "llama");
});
