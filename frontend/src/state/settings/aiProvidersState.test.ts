import assert from "node:assert/strict";
import test from "node:test";
import {
  PROVIDER_STATUS_TONE,
  describeTestResult,
  emptyProviderForm,
  formProviderFrom,
  formatLatency,
  formatModels,
  limitsFromForm,
  limitsToForm,
  meterLabel,
  meterPercent,
  meterSourceLabel,
  meterTone,
  modelsSummary,
  moveProvider,
  parseModels,
  providerFormProblem,
  providerInputFromForm,
  quotaSubtitle,
  topQuotaRows,
  type ProviderForm,
} from "./aiProvidersState.ts";
import type { ProviderView, QuotaRow, QuotaView, UsageMeter } from "../../models/aiProviders.ts";

function form(overrides: Partial<ProviderForm> = {}): ProviderForm {
  return {
    ...emptyProviderForm(),
    id: "groq",
    label: "Groq",
    baseUrl: "https://api.groq.com/openai/v1",
    modelsText: "llama-3.3-70b-versatile",
    apiKey: "gsk_live",
    ...overrides,
  };
}

function providerView(overrides: Partial<ProviderView> = {}): ProviderView {
  return {
    id: "groq",
    label: "Groq",
    kind: "openai",
    baseUrl: "https://api.groq.com/openai/v1",
    keyConfigured: true,
    apiKeyMasked: "••••1234",
    models: [{ id: "llama-3.3-70b-versatile" }],
    limits: { rpm: 30, rpd: 1000, tpm: null, tpd: 100000, monthlyTokens: null },
    priority: 0,
    enabled: true,
    status: "ready",
    usage: {
      requestsToday: { used: 12, limit: 1000, percent: 1, source: "counted" },
      tokensToday: { used: 400, source: "counted" },
      tokensMonth: { used: 400, source: "counted" },
      requestsMinute: { used: 0, limit: 30, source: "counted" },
      tokensMinute: { used: 0, source: "counted" },
      errors: 0,
    },
    ...overrides,
  };
}

function meter(overrides: Partial<UsageMeter> = {}): UsageMeter {
  return { used: 0, source: "counted", ...overrides };
}

test("an id the URL router could not carry is refused before the round trip", () => {
  const cases: Array<[string, string]> = [
    ["", "a blank id"],
    ["g", "a single character, which the pattern's 2-40 range excludes"],
    ["-groq", "a leading hyphen"],
    ["groq-", "a trailing hyphen"],
    ["groq cloud", "a space"],
    ["groq_cloud", "an underscore"],
    ["a".repeat(41), "41 characters"],
  ];
  for (const [id, why] of cases) {
    assert.notEqual(providerFormProblem(form({ id })), null, why);
  }
  assert.equal(providerFormProblem(form({ id: "gh" })), null, "two characters is the floor");
  assert.equal(providerFormProblem(form({ id: "a".repeat(40) })), null, "40 characters is the ceiling");
  assert.equal(
    providerFormProblem(form({ id: "  Groq  " })),
    null,
    "case and padding are normalized, exactly as the server normalizes them",
  );
});

test("the two ids that would shadow an admin route are reserved", () => {
  for (const id of ["reorder", "settings"]) {
    const problem = providerFormProblem(form({ id }));
    assert.notEqual(problem, null, `${id} must not become a provider id`);
    assert.match(problem ?? "", /reserved/);
  }
});

test("the base URL must be one an endpoint could actually be reached at", () => {
  assert.match(providerFormProblem(form({ baseUrl: "   " })) ?? "", /required/);
  for (const baseUrl of ["api.groq.com/openai/v1", "ftp://api.groq.com", "not a url", "/v1"]) {
    assert.notEqual(providerFormProblem(form({ baseUrl })), null, baseUrl);
  }
  assert.equal(providerFormProblem(form({ baseUrl: "http://127.0.0.1:8080/v1" })), null);
});

test("an entry with no model has nothing to call", () => {
  assert.match(providerFormProblem(form({ modelsText: "   \n \n" })) ?? "", /at least one model/);
});

test("a vault key name is checked here, not at 3am when a quota runs out", () => {
  for (const apiKeyRef of ["9GROQ", "groq key", "groq-key"]) {
    assert.notEqual(providerFormProblem(form({ apiKeyRef })), null, apiKeyRef);
  }
  assert.equal(providerFormProblem(form({ apiKeyRef: "GROQ_API_KEY", apiKey: "" })), null);
  assert.equal(providerFormProblem(form({ apiKeyRef: "_private2", apiKey: "" })), null);
});

test("a provider may not be enabled without a credential of some kind", () => {
  const keyless = form({ apiKey: "", apiKeyRef: "", enabled: true });
  assert.match(providerFormProblem(keyless) ?? "", /before enabling Groq/);

  assert.equal(
    providerFormProblem({ ...keyless, enabled: false }),
    null,
    "an honestly disabled entry needs nothing",
  );
  assert.equal(
    providerFormProblem({ ...keyless, keyConfigured: true }),
    null,
    "a key already stored on the server counts",
  );
  assert.notEqual(
    providerFormProblem({ ...keyless, keyConfigured: true, clearApiKey: true }),
    null,
    "removing the stored key leaves an enabled entry with nothing to send",
  );
  assert.equal(providerFormProblem({ ...keyless, apiKeyRef: "GROQ_API_KEY" }), null);
});

test("formProviderFrom never carries a key into the form", () => {
  const loaded = formProviderFrom(
    providerView({ apiKeyMasked: "••••1234", keyConfigured: true, apiKeyRef: "GROQ_API_KEY" }),
  );
  assert.equal(loaded.apiKey, "", "the masked key must not become the input's value");
  assert.equal(loaded.clearApiKey, false);
  assert.equal(loaded.keyConfigured, true, "the form still knows a key exists");
  assert.equal(loaded.apiKeyMasked, "••••1234", "the mask is a placeholder, not a value");
  assert.equal(loaded.apiKeyRef, "GROQ_API_KEY", "a vault key *name* was never secret");

  const submitted = providerInputFromForm(loaded);
  assert.equal(submitted.apiKey, "", "a blank field is the 'keep the stored key' signal");
  assert.equal(submitted.clearApiKey, false);
});

test("providerInputFromForm trims and lower-cases what the server would anyway", () => {
  const input = providerInputFromForm(
    form({ id: "  GROQ  ", label: "  Groq  ", baseUrl: " https://api.groq.com/openai/v1 ", notes: " free " }),
  );
  assert.equal(input.id, "groq");
  assert.equal(input.label, "Groq");
  assert.equal(input.baseUrl, "https://api.groq.com/openai/v1");
  assert.equal(input.notes, "free");
  assert.deepEqual(input.models, [{ id: "llama-3.3-70b-versatile" }]);
});

test("moveProvider clamps at both ends and hands back the same array on a no-op", () => {
  const ids = ["a", "b", "c"];

  assert.deepEqual(moveProvider(ids, "c", -1), ["a", "c", "b"]);
  assert.deepEqual(moveProvider(ids, "a", 1), ["b", "a", "c"]);
  assert.deepEqual(moveProvider(ids, "a", 5), ["b", "c", "a"], "an overshoot lands at the end");
  assert.deepEqual(moveProvider(ids, "c", -9), ["c", "a", "b"], "and at the start");

  assert.equal(moveProvider(ids, "a", -1), ids, "the top entry cannot go higher");
  assert.equal(moveProvider(ids, "c", 1), ids, "the last entry cannot go lower");
  assert.equal(moveProvider(ids, "b", 0), ids, "moving nowhere is not a change");
  assert.equal(moveProvider(ids, "missing", 1), ids, "an id that is not in the list moves nothing");
  assert.deepEqual(ids, ["a", "b", "c"], "the input is never mutated");
});

test("the models textarea round-trips whatever an operator pasted into it", () => {
  const text = [
    "llama-3.3-70b-versatile",
    "gemini-2.0-flash | Gemini 2.0 Flash",
    "qwen-3-coder | Qwen3 Coder | 128000",
    "glm-4-flash | GLM 4 Flash | 128000 | text,code,bulk",
    "mistral-small |  | 32000",
    "kimi-k2 |  |  | bulk",
  ].join("\n");

  assert.equal(formatModels(parseModels(text)), text, "text in, same text out");

  assert.deepEqual(parseModels(text), [
    { id: "llama-3.3-70b-versatile" },
    { id: "gemini-2.0-flash", label: "Gemini 2.0 Flash" },
    { id: "qwen-3-coder", label: "Qwen3 Coder", contextTokens: 128000 },
    {
      id: "glm-4-flash",
      label: "GLM 4 Flash",
      contextTokens: 128000,
      good_for: ["text", "code", "bulk"],
    },
    { id: "mistral-small", contextTokens: 32000 },
    { id: "kimi-k2", good_for: ["bulk"] },
  ]);
});

test("the models textarea forgives the mess a paste actually arrives as", () => {
  const models = parseModels(
    ["", "  gemini-2.0-flash  ", "gemini-2.0-flash", "  ", "glm-4-flash | | -5 | text, CODE, nonsense"].join("\n"),
  );
  assert.deepEqual(models, [
    { id: "gemini-2.0-flash" },
    { id: "glm-4-flash", good_for: ["text", "code"] },
  ]);
  assert.equal(
    parseModels("gemini-2.0-flash | gemini-2.0-flash")[0].label,
    undefined,
    "a label equal to the id carries nothing and would break the round trip",
  );
  assert.equal(modelsSummary(models), "gemini-2.0-flash, glm-4-flash");
  assert.equal(modelsSummary(models, 1), "gemini-2.0-flash +1");
  assert.equal(modelsSummary([]), "no models");
});

test("a blank limit means 'not documented', never a cap of zero", () => {
  const limits = limitsFromForm({
    rpm: "30",
    rpd: "  ",
    tpm: "0",
    tpd: "-5",
    monthlyTokens: "1000000",
  });
  assert.deepEqual(limits, {
    rpm: 30,
    rpd: null,
    tpm: null,
    tpd: null,
    monthlyTokens: 1000000,
  });

  assert.deepEqual(limitsToForm(limits), {
    rpm: "30",
    rpd: "",
    tpm: "",
    tpd: "",
    monthlyTokens: "1000000",
  });
  assert.deepEqual(
    limitsFromForm(limitsToForm(limits)),
    limits,
    "a limit survives a trip through the form",
  );
  assert.deepEqual(limitsToForm(null), {
    rpm: "",
    rpd: "",
    tpm: "",
    tpd: "",
    monthlyTokens: "",
  });
});

test("a meter with no documented cap prints a count, never a percentage", () => {
  const uncapped = meter({ used: 128 });
  assert.equal(meterPercent(uncapped), null, "there is nothing to be a percentage of");
  assert.equal(meterLabel(uncapped), "128");
  assert.equal(meterTone(null), "grey", "an empty track is not an alarm");

  const capped = meter({ used: 128, limit: 250 });
  assert.equal(meterPercent(capped), 51);
  assert.equal(meterLabel(capped), "128 / 250");

  assert.equal(meterPercent(meter({ used: 5, limit: 0 })), null, "a cap of zero is not a cap");
  assert.equal(meterLabel(meter({ used: 5, limit: 0 })), "5");
  assert.equal(meterPercent(meter({ used: 900, limit: 100 })), 100, "over the cap still fills once");
  assert.equal(meterLabel(meter({ used: 1250000, limit: 4000000 })), "1,250,000 / 4,000,000");
  assert.equal(meterPercent(undefined), null);
});

test("a percentage the provider reported wins over the one we could compute", () => {
  const reported = meter({ used: 10, limit: 1000, percent: 82, source: "reported" });
  assert.equal(meterPercent(reported), 82, "the vendor's own headers are the truth");
  assert.equal(meterSourceLabel(reported), "reported by provider");
  assert.equal(meterSourceLabel(meter()), "counted locally");
});

test("meter tone only raises its voice near the end of a free tier", () => {
  assert.equal(meterTone(0), "green");
  assert.equal(meterTone(69), "green");
  assert.equal(meterTone(70), "amber");
  assert.equal(meterTone(89), "amber");
  assert.equal(meterTone(90), "red");
  assert.equal(meterTone(100), "red");
});

test("a status that is not a fault is not coloured like one", () => {
  assert.equal(PROVIDER_STATUS_TONE.ready, "green");
  assert.equal(PROVIDER_STATUS_TONE.cooling, "amber");
  assert.equal(PROVIDER_STATUS_TONE.exhausted, "amber");
  assert.equal(PROVIDER_STATUS_TONE["no-key"], "grey");
  assert.equal(PROVIDER_STATUS_TONE.disabled, "grey");
});

test("the Test result reads as a sentence an operator can act on", () => {
  assert.equal(
    describeTestResult({
      ok: true,
      providerId: "groq",
      label: "Groq",
      model: "llama-3.3-70b-versatile",
      durationMs: 640,
      answer: "Working.",
    }),
    "llama-3.3-70b-versatile answered in 640 ms.",
  );
  assert.equal(
    describeTestResult({
      ok: false,
      providerId: "groq",
      label: "Groq",
      model: "llama-3.3-70b-versatile",
      durationMs: 12,
      error: "401 invalid api key",
    }),
    "Test failed after 12 ms: 401 invalid api key",
  );
  assert.equal(formatLatency(640), "640 ms");
  assert.equal(formatLatency(1500), "1.5 s");
  assert.equal(formatLatency(-1), "—");
});

test("the dashboard card trims the server's order rather than inventing one", () => {
  const rows: QuotaRow[] = ["a", "b", "c", "d", "e"].map((id) => ({
    id,
    label: id.toUpperCase(),
    status: "ready",
    requestsToday: meter(),
    tokensToday: meter(),
    tokensMonth: meter(),
  }));
  const view: QuotaView = { available: true, providers: rows, month: "2026-08" };

  assert.deepEqual(
    topQuotaRows(view).map((row) => row.id),
    ["a", "b", "c", "d"],
    "the card shows four, and the server already put the busiest first",
  );
  assert.equal(quotaSubtitle(view), "5 providers · 2026-08");
  assert.equal(quotaSubtitle({ available: true, providers: [], month: "2026-08" }), "No providers connected");
  assert.equal(quotaSubtitle({ available: false, providers: [], month: "2026-08" }), "Not set up");
  assert.equal(quotaSubtitle(null), "Not set up");
  assert.deepEqual(topQuotaRows(null), []);
});
