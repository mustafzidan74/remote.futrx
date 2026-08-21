import assert from "node:assert/strict";
import test from "node:test";
import {
  AUX_MODEL_FALLBACK_DEFAULTS,
  applyProviderChange,
  describeTestResult,
  formFromSettings,
  formatLatency,
  isLoopback,
  jobSource,
  latencyVerdict,
  setJobSource,
  updateInputFromForm,
  validateAuxModelForm,
  type AuxModelForm,
} from "./auxModelState.ts";
import type { AuxModelSettings } from "../../models/auxModel.ts";

const defaults = AUX_MODEL_FALLBACK_DEFAULTS;

function form(overrides: Partial<AuxModelForm> = {}): AuxModelForm {
  return {
    enabled: false,
    provider: "ollama",
    baseUrl: defaults.ollamaBaseUrl,
    model: defaults.model,
    apiKey: "",
    timeoutSeconds: defaults.timeoutSeconds,
    maxTokens: defaults.maxTokens,
    jobs: {
      chatTitle: "local",
      runSummary: "local",
      commitMessage: "local",
      translate: "local",
      chatSummary: "local",
    },
    ...overrides,
  };
}

function settings(overrides: Partial<AuxModelSettings> = {}): AuxModelSettings {
  return {
    enabled: false,
    configured: true,
    provider: "ollama",
    baseUrl: defaults.ollamaBaseUrl,
    model: defaults.model,
    keyConfigured: false,
    timeoutSeconds: defaults.timeoutSeconds,
    maxTokens: defaults.maxTokens,
    jobs: {
      chatTitle: "local",
      runSummary: "pool",
      commitMessage: "off",
      translate: "local",
      chatSummary: "local",
    },
    jobLabels: [],
    providers: ["ollama", "openai-compatible"],
    sources: ["local", "pool", "off"],
    poolAvailable: false,
    defaults,
    ...overrides,
  };
}

test("formFromSettings never carries a key into the form", () => {
  const loaded = formFromSettings(
    settings({ apiKeyMasked: "••••1234", keyConfigured: true, provider: "openai-compatible" }),
  );
  assert.equal(loaded.apiKey, "", "the masked key must not become the input's value");
  assert.equal(loaded.provider, "openai-compatible");
  assert.equal(loaded.jobs.commitMessage, "off", "a stored choice survives the load");
  assert.equal(loaded.jobs.runSummary, "pool");
});

test("switching to Ollama fills the endpoint in, but never overwrites a typed one", () => {
  const typed = form({ provider: "openai-compatible", baseUrl: "https://api.groq.com/openai/v1" });
  const kept = applyProviderChange(typed, "ollama", defaults);
  assert.equal(
    kept.baseUrl,
    "https://api.groq.com/openai/v1",
    "an endpoint the operator typed must survive a provider glance",
  );

  const blank = form({ provider: "openai-compatible", baseUrl: "  " });
  assert.equal(applyProviderChange(blank, "ollama", defaults).baseUrl, defaults.ollamaBaseUrl);

  const suggestion = form({ provider: "openai-compatible", baseUrl: defaults.ollamaBaseUrl });
  assert.equal(applyProviderChange(suggestion, "ollama", defaults).baseUrl, defaults.ollamaBaseUrl);

  const unchanged = form();
  assert.equal(
    applyProviderChange(unchanged, "ollama", defaults),
    unchanged,
    "re-selecting the current provider is a no-op",
  );
});

test("validateAuxModelForm refuses what the endpoint would refuse", () => {
  const cases: Array<[string, AuxModelForm, boolean]> = [
    ["the local default is valid", form(), false],
    ["a blank base URL", form({ baseUrl: "   " }), true],
    ["a host with no scheme", form({ baseUrl: "127.0.0.1:11434" }), true],
    ["a non-http scheme", form({ baseUrl: "ftp://example.com" }), true],
    ["a blank model", form({ model: "  " }), true],
    ["a timeout under the floor", form({ timeoutSeconds: 1 }), true],
    ["a timeout over the ceiling", form({ timeoutSeconds: 600 }), true],
    ["a token cap over the ceiling", form({ maxTokens: 99999 }), true],
  ];

  for (const [name, candidate, expectProblem] of cases) {
    const problem = validateAuxModelForm(candidate, null, defaults);
    assert.equal(problem !== null, expectProblem, `${name}: got ${problem}`);
  }
});

test("a remote endpoint may not be switched on without a key, a local one may", () => {
  const remote = form({
    enabled: true,
    provider: "openai-compatible",
    baseUrl: "https://api.groq.com/openai/v1",
  });
  assert.notEqual(
    validateAuxModelForm(remote, null, defaults),
    null,
    "enabling a remote endpoint with no credential claims a feature that will not work",
  );

  assert.equal(
    validateAuxModelForm(remote, settings({ keyConfigured: true }), defaults),
    null,
    "a key already stored on the server counts",
  );
  assert.equal(
    validateAuxModelForm({ ...remote, apiKey: "sk-live" }, null, defaults),
    null,
    "a key typed into the form counts",
  );

  const localOpenAI = form({
    enabled: true,
    provider: "openai-compatible",
    baseUrl: "http://127.0.0.1:8080/v1",
  });
  assert.equal(
    validateAuxModelForm(localOpenAI, null, defaults),
    null,
    "llama.cpp on loopback needs no credential",
  );

  const localOllama = form({ enabled: true });
  assert.equal(validateAuxModelForm(localOllama, null, defaults), null);
});

test("isLoopback recognizes the endpoints that need no credential", () => {
  const cases: Array<[string, boolean]> = [
    ["http://127.0.0.1:11434", true],
    ["http://localhost:8080/v1", true],
    ["https://api.openai.com/v1", false],
    ["not a url", false],
  ];
  for (const [url, expected] of cases) {
    assert.equal(isLoopback(url), expected, url);
  }
});

test("updateInputFromForm trims, and an untouched key field means 'keep it'", () => {
  const input = updateInputFromForm(
    form({ baseUrl: "  http://127.0.0.1:11434  ", model: " qwen2.5:3b ", apiKey: "  " }),
  );
  assert.equal(input.baseUrl, "http://127.0.0.1:11434");
  assert.equal(input.model, "qwen2.5:3b");
  assert.equal(input.apiKey, "", "a blank field is the 'keep the stored key' signal");
  assert.equal(input.clearApiKey, false);

  const cleared = updateInputFromForm(form({ enabled: true, apiKey: "typed" }), {
    clearApiKey: true,
    enabledOverride: false,
  });
  assert.equal(cleared.apiKey, "");
  assert.equal(cleared.clearApiKey, true);
  assert.equal(
    cleared.enabled,
    false,
    "removing the credential must switch the feature off in the same request",
  );
});

test("pointing one job at another source leaves the other four alone", () => {
  const next = setJobSource(setJobSource(form(), "commitMessage", "off"), "runSummary", "pool");
  assert.equal(jobSource(next.jobs, "commitMessage"), "off");
  assert.equal(jobSource(next.jobs, "runSummary"), "pool");
  assert.equal(jobSource(next.jobs, "chatTitle"), "local");
  assert.equal(jobSource(next.jobs, "translate"), "local");
});

test("an unknown source reads as the local endpoint, never as the pool", () => {
  assert.equal(
    jobSource({}, "chatTitle"),
    "local",
    "a job the server has not published yet runs locally, like the server's own rule",
  );
  assert.equal(
    jobSource({ chatTitle: "POOL" }, "chatTitle"),
    "local",
    "a typo must never silently start spending somebody's free tier",
  );
  assert.equal(jobSource({ chatTitle: "pool" }, "chatTitle"), "pool");
  assert.equal(jobSource({ chatTitle: "off" }, "chatTitle"), "off");
});

test("the Test result reads as a sentence an operator can act on", () => {
  assert.equal(
    describeTestResult({
      ok: true,
      provider: "ollama",
      baseUrl: "http://127.0.0.1:11434",
      model: "qwen2.5:3b",
      durationMs: 640,
      answer: "I am working, I am qwen2.5:3b.",
    }),
    "qwen2.5:3b answered in 640 ms.",
  );

  assert.equal(
    describeTestResult({
      ok: false,
      provider: "ollama",
      baseUrl: "http://127.0.0.1:11434",
      model: "qwen2.5:3b",
      durationMs: 12,
      error: "connection refused",
    }),
    "Test failed after 12 ms: connection refused",
  );
});

test("latency is formatted and judged the way an operator compares it", () => {
  assert.equal(formatLatency(640), "640 ms");
  assert.equal(formatLatency(1500), "1.5 s");
  assert.equal(formatLatency(-1), "—");

  assert.equal(latencyVerdict(400), "fast");
  assert.equal(latencyVerdict(2000), "fast");
  assert.equal(latencyVerdict(3500), "usable");
  assert.equal(latencyVerdict(9000), "slow");
});
