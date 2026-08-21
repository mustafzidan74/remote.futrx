import assert from "node:assert/strict";
import test from "node:test";
import type { AgentEndpoint, AgentEndpointChoice } from "../../models/agentEndpoints.ts";
import {
  draftFrom,
  emptyDraft,
  endpointBadge,
  endpointOptions,
  formatModels,
  lastTestLabel,
  modelSummary,
  parseHeaders,
  parseModels,
  statusLabel,
  statusTone,
  toPayload,
  upsert,
  validate,
} from "./agentEndpointsState.ts";

function endpoint(overrides: Partial<AgentEndpoint> = {}): AgentEndpoint {
  return {
    id: "zhipu-glm",
    label: "Zhipu GLM",
    cli: "claude",
    baseUrl: "https://open.bigmodel.cn/api/anthropic",
    apiKeyRef: "ZHIPU_API_KEY",
    models: [{ id: "glm-4.6", label: "GLM-4.6" }, { id: "glm-4.5-air" }],
    enabled: true,
    keyResolved: true,
    ...overrides,
  };
}

function validDraft() {
  return {
    ...emptyDraft(),
    id: "zhipu-glm",
    label: "Zhipu GLM",
    cli: "claude" as const,
    baseUrl: "https://open.bigmodel.cn/api/anthropic",
    apiKeyRef: "ZHIPU_API_KEY",
    modelLines: "glm-4.6 = GLM-4.6\nglm-4.5-air",
    enabled: true,
  };
}

test("a well-formed draft validates", () => {
  assert.equal(validate(validDraft()), null);
});

// The id becomes a `model_providers.<id>` config key on the codex command
// line, so the character set is not a matter of taste.
test("validate rejects ids that are not legal provider keys", () => {
  const cases: [string, string][] = [
    ["", "empty"],
    ["zhipu.glm", "a dot"],
    ["-zhipu", "a leading hyphen"],
    ["Zhipu GLM", "a space and capitals"],
    ["zhipu/glm", "a slash"],
  ];
  for (const [id, why] of cases) {
    const problem = validate({ ...validDraft(), id });
    assert.ok(problem, `expected ${why} to be rejected`);
  }
  assert.equal(validate({ ...validDraft(), id: "zhipu-glm_2" }), null);
});

// An edit may not restate the id, because the id is the handle every chat
// pointed at this endpoint already stores.
test("validate skips the id rule when editing", () => {
  assert.equal(validate({ ...validDraft(), id: "" }, { creating: false }), null);
});

test("validate insists on an absolute, plain base URL", () => {
  const cases = [
    "",
    "/api/anthropic",
    "open.bigmodel.cn/api/anthropic",
    "ftp://open.bigmodel.cn/api",
    "https://user:pass@open.bigmodel.cn/api",
    "https://open.bigmodel.cn/api?key=abc",
  ];
  for (const baseUrl of cases) {
    assert.ok(validate({ ...validDraft(), baseUrl }), `expected ${baseUrl || "(empty)"} to be rejected`);
  }
});

// The whole security model rests on the key living in the vault, so an
// enabled profile that names none is refused before the round trip.
test("validate refuses to enable a profile with no vault key", () => {
  const problem = validate({ ...validDraft(), apiKeyRef: "" });
  assert.match(String(problem), /Secrets vault key/);
});

test("validate allows a disabled template to name no vault key", () => {
  assert.equal(validate({ ...validDraft(), apiKeyRef: "", enabled: false }), null);
});

test("validate rejects a key reference that is not a vault key", () => {
  assert.ok(validate({ ...validDraft(), apiKeyRef: "zhipu_api_key" }));
  assert.ok(validate({ ...validDraft(), apiKeyRef: "ZHIPU-API-KEY" }));
});

test("validate rejects header names that could not become a config key", () => {
  assert.ok(validate({ ...validDraft(), headerLines: "bad name: value" }));
  assert.equal(validate({ ...validDraft(), headerLines: "HTTP-Referer: https://x.test" }), null);
});

test("parseModels reads both forms and drops duplicates", () => {
  const models = parseModels("glm-4.6 = GLM-4.6\n\nglm-4.5-air\nglm-4.6 = ignored\n  ");
  assert.deepEqual(models, [{ id: "glm-4.6", label: "GLM-4.6" }, { id: "glm-4.5-air" }]);
});

test("formatModels round-trips through parseModels", () => {
  const models = [{ id: "glm-4.6", label: "GLM-4.6" }, { id: "glm-4.5-air" }];
  assert.deepEqual(parseModels(formatModels(models)), models);
});

test("parseHeaders reads Name: Value and keeps colons in the value", () => {
  assert.deepEqual(parseHeaders("HTTP-Referer: https://example.test\nX-Title: remote.futrx"), {
    "HTTP-Referer": "https://example.test",
    "X-Title": "remote.futrx",
  });
  assert.deepEqual(parseHeaders("no-colon-here"), {});
});

test("toPayload normalizes the id and trims every field", () => {
  const payload = toPayload({
    ...validDraft(),
    id: "  Zhipu-GLM  ",
    label: "  Zhipu GLM  ",
    baseUrl: "  https://open.bigmodel.cn/api/anthropic  ",
    apiKeyRef: "  ZHIPU_API_KEY  ",
    notes: "  memo  ",
  });
  assert.equal(payload.id, "zhipu-glm");
  assert.equal(payload.label, "Zhipu GLM");
  assert.equal(payload.baseUrl, "https://open.bigmodel.cn/api/anthropic");
  assert.equal(payload.apiKeyRef, "ZHIPU_API_KEY");
  assert.equal(payload.notes, "memo");
});

// The payload is the whole write. Nothing resembling a key value may be in
// it: the register stores a reference and the server resolves it.
test("toPayload carries only a key reference, never a value", () => {
  const payload = toPayload(validDraft());
  assert.ok(!("apiKey" in payload));
  assert.equal(payload.apiKeyRef, "ZHIPU_API_KEY");
});

test("draftFrom round-trips a stored profile", () => {
  const draft = draftFrom(endpoint({ headers: { "X-Title": "remote.futrx" }, notes: "memo" }));
  assert.equal(draft.id, "zhipu-glm");
  assert.equal(draft.modelLines, "glm-4.6 = GLM-4.6\nglm-4.5-air");
  assert.equal(draft.headerLines, "X-Title: remote.futrx");
  assert.equal(validate(draft, { creating: false }), null);
});

test("upsert replaces by id and keeps label order", () => {
  const list = [endpoint(), endpoint({ id: "openrouter", label: "OpenRouter", cli: "codex" })];
  const next = upsert(list, endpoint({ label: "Aardvark AI" }));
  assert.deepEqual(next.map((item) => item.label), ["Aardvark AI", "OpenRouter"]);
  assert.equal(next.length, 2);
});

test("status reports disabled before it reports a missing key", () => {
  assert.equal(statusLabel(endpoint({ enabled: false, keyResolved: false })), "Disabled");
  assert.equal(statusTone(endpoint({ enabled: false, keyResolved: false })), "off");
  assert.equal(statusLabel(endpoint({ enabled: true, keyResolved: false })), "Key missing");
  assert.equal(statusTone(endpoint({ enabled: true, keyResolved: false })), "warn");
  assert.equal(statusLabel(endpoint()), "Enabled");
  assert.equal(statusTone(endpoint()), "on");
});

test("modelSummary abbreviates a long list and names an empty one", () => {
  assert.equal(modelSummary(endpoint({ models: [] })), "endpoint default");
  assert.equal(modelSummary(endpoint()), "GLM-4.6, glm-4.5-air");
  assert.equal(
    modelSummary(endpoint({ models: [{ id: "a" }, { id: "b" }, { id: "c" }, { id: "d" }] })),
    "a, b +2",
  );
});

test("lastTestLabel names an untested profile", () => {
  assert.equal(lastTestLabel(endpoint()), "never tested");
  assert.match(lastTestLabel(endpoint({ lastTest: { at: 1_700_000_000_000, ok: true } })), /^passed /);
  assert.match(lastTestLabel(endpoint({ lastTest: { at: 1_700_000_000_000, ok: false } })), /^failed /);
});

function choice(overrides: Partial<AgentEndpointChoice> = {}): AgentEndpointChoice {
  return {
    id: "zhipu-glm",
    label: "Zhipu GLM",
    cli: "claude",
    models: [{ id: "glm-4.6", label: "GLM-4.6" }],
    ...overrides,
  };
}

test("endpointOptions renders one composer row per model", () => {
  const options = endpointOptions([
    choice(),
    choice({ id: "openrouter", label: "OpenRouter", cli: "codex", models: [{ id: "z-ai/glm-4.6" }] }),
  ]);
  assert.deepEqual(options.map((option) => option.label), [
    "Claude Code · GLM-4.6",
    "Codex · z-ai/glm-4.6",
  ]);
  assert.deepEqual(options.map((option) => option.model), ["glm-4.6", "z-ai/glm-4.6"]);
});

test("endpointOptions still offers an endpoint that lists no model", () => {
  const options = endpointOptions([choice({ models: [] })]);
  assert.equal(options.length, 1);
  assert.equal(options[0].model, "");
  assert.equal(options[0].label, "Claude Code · Zhipu GLM");
});

// The badge is what stops a GLM-written page being handed to a client as
// Claude's work, so its wording is a behavioural requirement, not decoration.
test("endpointBadge names the model, the vendor, and the negative", () => {
  const badge = endpointBadge([choice()], "zhipu-glm", "glm-4.6");
  assert.equal(badge?.short, "GLM-4.6 via Zhipu GLM");
  assert.equal(badge?.title, "running on GLM-4.6 via Zhipu GLM — not Anthropic");
});

test("endpointBadge is absent for a chat on its vendor's own endpoint", () => {
  assert.equal(endpointBadge([choice()], "", "claude-sonnet-4-5"), null);
  assert.equal(endpointBadge([choice()], undefined, undefined), null);
});

// A chat may outlive the profile it names — an admin can delete one. The
// badge must still warn rather than disappearing, because the run itself
// still says "not Anthropic" until somebody repoints the chat.
test("endpointBadge falls back to the id when the profile is gone", () => {
  const badge = endpointBadge([], "zhipu-glm", "glm-4.6");
  assert.equal(badge?.short, "glm-4.6 via zhipu-glm");
  assert.match(String(badge?.title), /not Anthropic$/);
});
