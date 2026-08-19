import { useAuxModel } from "../../state/hooks/settings/useAuxModel";
import { refreshAuxModelJobs } from "../../state/hooks/settings/useAuxModelJobs";
import {
  AUX_MODEL_PROVIDER_LABELS,
  formatLatency,
  isLoopback,
  jobEnabled,
  latencyVerdict,
} from "../../state/settings/auxModelState";
import type { AuxModelProvider } from "../../models/auxModel";
import { AlertCircle, Check, Loader, MemoryStick, X, Zap } from "../primitives/icons";

const inputClass =
  "w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 " +
  "placeholder:text-ink-400 focus:outline-none focus:border-accent-blue";

/**
 * The auxiliary model panel.
 *
 * Its whole job is to make one claim honestly: this is a *small* model for the
 * platform's own chores, and switching it off costs nothing. The note at the
 * bottom says so in as many words, because the natural reading of "local
 * model" on a page full of agent settings is "this replaces Claude", and it
 * emphatically does not.
 */
export function AuxModelSettings() {
  const editor = useAuxModel(true);
  const { form, settings, defaults, testResult } = editor;
  const remoteEndpoint = form.provider === "openai-compatible" && !isLoopback(form.baseUrl);

  async function save(event: Event) {
    await editor.save(event);
    // The buttons this model powers ask a cached endpoint whether they should
    // exist; a save is exactly when that answer changes.
    refreshAuxModelJobs();
  }

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <MemoryStick class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14.5px] font-semibold text-ink-50">Auxiliary model</div>
            {editor.loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : settings?.enabled ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> on
              </span>
            ) : (
              <span class="text-[11px] text-ink-400">off</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            A small, cheap model for the platform's own text chores: naming chats, writing the one
            sentence a phone notification carries, drafting a commit subject, translating a client
            message. Run it on this host with Ollama, or point it at any OpenAI-compatible
            endpoint.
          </div>
        </div>
      </header>

      <form onSubmit={save} class="p-3 space-y-3">
        <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) =>
              editor.patch({ enabled: (event.currentTarget as HTMLInputElement).checked })
            }
            class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
          />
          <span class="min-w-0">
            <span class="block text-[13px] text-ink-100">Use an auxiliary model</span>
            <span class="block text-[12px] text-ink-300 leading-relaxed">
              Every job below keeps working without it. If the endpoint is off, unreachable, or
              slow, the platform silently does what it did before — a truncated title, the raw tail
              of the agent's reply, a dated commit message.
            </span>
          </span>
        </label>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Endpoint</legend>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Provider</span>
            <select
              value={form.provider}
              onChange={(event) =>
                editor.setProvider(
                  (event.currentTarget as HTMLSelectElement).value as AuxModelProvider,
                )
              }
              class={inputClass}
            >
              {(settings?.providers ?? ["ollama", "openai-compatible"]).map((provider) => (
                <option key={provider} value={provider}>
                  {AUX_MODEL_PROVIDER_LABELS[provider] ?? provider}
                </option>
              ))}
            </select>
            <span class="block text-[11.5px] text-ink-400 leading-relaxed">
              {form.provider === "ollama"
                ? "Posts to {base URL}/api/chat. Install Ollama on this host and bind it to 127.0.0.1 — see the Auxiliary model page in the docs."
                : "Posts to {base URL}/v1/chat/completions, with a bearer token when a key is stored. Covers OpenAI, Groq, Together, llama.cpp, vLLM, and LM Studio."}
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Base URL</span>
            <input
              type="text"
              value={form.baseUrl}
              onInput={(event) =>
                editor.patch({ baseUrl: (event.currentTarget as HTMLInputElement).value })
              }
              placeholder={defaults.ollamaBaseUrl}
              autocomplete="off"
              spellcheck={false}
              dir="ltr"
              class={`${inputClass} bidi-ltr`}
            />
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Model</span>
            <input
              type="text"
              value={form.model}
              onInput={(event) =>
                editor.patch({ model: (event.currentTarget as HTMLInputElement).value })
              }
              placeholder={defaults.model}
              autocomplete="off"
              spellcheck={false}
              dir="ltr"
              class={`${inputClass} bidi-ltr`}
            />
            <span class="block text-[11.5px] text-ink-400 leading-relaxed">
              <code class="text-ink-300">{defaults.model}</code> is the recommended local build: it
              needs roughly 2–3 GB of RAM and answers a six-word title in well under a second.
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">
              API key {form.provider === "ollama" ? "(not needed for local Ollama)" : ""}
            </span>
            <input
              type="password"
              value={form.apiKey}
              onInput={(event) =>
                editor.patch({ apiKey: (event.currentTarget as HTMLInputElement).value })
              }
              placeholder={settings?.keyConfigured ? settings.apiKeyMasked : "sk-…"}
              autocomplete="off"
              spellcheck={false}
              class={inputClass}
            />
            <span class="flex flex-wrap items-center gap-2 text-[11.5px] text-ink-400">
              {settings?.keyConfigured ? (
                <>
                  <span>A key is stored. Leave this blank to keep it.</span>
                  <button
                    type="button"
                    onClick={editor.clearApiKey}
                    disabled={editor.saving}
                    class="inline-flex items-center gap-1 rounded border border-white/10 px-1.5 py-0.5
                           text-ink-200 hover:bg-white/[0.07] disabled:opacity-50"
                  >
                    <X class="w-3 h-3" /> Remove stored key
                  </button>
                </>
              ) : (
                <span>
                  {remoteEndpoint
                    ? "No key stored yet — a remote endpoint needs one."
                    : "No key stored. A loopback endpoint does not need one."}
                </span>
              )}
            </span>
          </label>

          <div class="grid gap-3 sm:grid-cols-2">
            <label class="block space-y-1.5">
              <span class="text-xs text-ink-300">Timeout (seconds)</span>
              <input
                type="number"
                min={defaults.minTimeoutSeconds}
                max={defaults.maxTimeoutSeconds}
                value={form.timeoutSeconds}
                onInput={(event) =>
                  editor.patch({
                    timeoutSeconds: Number((event.currentTarget as HTMLInputElement).value),
                  })
                }
                class={`${inputClass} tabular-nums`}
              />
              <span class="block text-[11.5px] text-ink-400 leading-relaxed">
                The hard ceiling on one call. Past it the job gives up and the fallback is used.
              </span>
            </label>
            <label class="block space-y-1.5">
              <span class="text-xs text-ink-300">Answer cap (tokens)</span>
              <input
                type="number"
                min={defaults.minMaxTokens}
                max={defaults.maxMaxTokens}
                value={form.maxTokens}
                onInput={(event) =>
                  editor.patch({
                    maxTokens: Number((event.currentTarget as HTMLInputElement).value),
                  })
                }
                class={`${inputClass} tabular-nums`}
              />
              <span class="block text-[11.5px] text-ink-400 leading-relaxed">
                An upper bound. Each job has its own, much smaller cap — a title gets 40 tokens
                whatever this says.
              </span>
            </label>
          </div>
        </fieldset>

        <fieldset class="space-y-2">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Jobs</legend>
          <p class="text-[11.5px] text-ink-400 leading-relaxed">
            All on by default. Switching one off restores that feature's original behaviour exactly.
          </p>
          <div class="space-y-1.5">
            {(settings?.jobLabels ?? []).map((job) => (
              <label
                key={job.id}
                class="flex items-center gap-2.5 rounded-md border border-white/10 bg-white/[0.03] px-2.5 py-2 cursor-pointer"
              >
                <input
                  type="checkbox"
                  checked={jobEnabled(form.jobs, job.id)}
                  onChange={(event) =>
                    editor.setJob(job.id, (event.currentTarget as HTMLInputElement).checked)
                  }
                  class="h-4 w-4 flex-none accent-accent-blue"
                />
                <span class="min-w-0 flex-1">
                  <span class="block text-[13px] text-ink-100">{job.label}</span>
                  <span class="block text-[11.5px] text-ink-400 leading-snug">
                    {JOB_NOTES[job.id] ?? ""}
                  </span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        {editor.error && (
          <div
            role="alert"
            class="flex items-start gap-2 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-2.5 py-2"
          >
            <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none text-accent-red" aria-hidden="true" />
            <span class="min-w-0 flex-1 break-words text-[12px] leading-relaxed text-ink-100">
              {editor.error}
            </span>
          </div>
        )}

        {testResult && (
          <div
            role="status"
            class={`rounded-md border px-2.5 py-2 text-[12px] leading-relaxed ${
              testResult.ok
                ? "border-accent-green/30 bg-accent-green/[0.08] text-ink-100"
                : "border-accent-red/30 bg-accent-red/[0.08] text-ink-100"
            }`}
          >
            {testResult.ok ? (
              <>
                <div>
                  <span class="text-ink-50">{testResult.model}</span> answered in{" "}
                  <span class="text-ink-50 tabular-nums">
                    {formatLatency(testResult.durationMs)}
                  </span>
                  {" — "}
                  {LATENCY_NOTES[latencyVerdict(testResult.durationMs)]}
                </div>
                {testResult.answer && (
                  <div dir="auto" class="bidi-auto mt-1.5 text-ink-200 italic">
                    “{testResult.answer}”
                  </div>
                )}
              </>
            ) : (
              <>
                Test failed after {formatLatency(testResult.durationMs)}:{" "}
                {testResult.error || "no answer"}
              </>
            )}
          </div>
        )}

        <div class="flex flex-wrap items-center gap-2 pt-1">
          <button
            type="submit"
            disabled={editor.saving || editor.loading}
            class="h-9 rounded-md bg-accent-blue px-3 text-[13px] font-semibold text-ink-900
                   hover:bg-accent-blue/85 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {editor.saving ? "Saving…" : "Save"}
          </button>
          <button
            type="button"
            onClick={editor.runTest}
            disabled={editor.testing || editor.loading}
            title="Ask the model for one short sentence and report how long it took"
            class="inline-flex h-9 items-center gap-1.5 rounded-md border border-white/10 px-3 text-[13px]
                   font-medium text-ink-100 hover:bg-white/[0.07] disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {editor.testing ? <Loader class="h-3.5 w-3.5 animate-spin" /> : <Zap class="h-3.5 w-3.5" />}
            {editor.testing ? "Testing…" : "Test"}
          </button>
          {editor.saved && (
            <span class="inline-flex items-center gap-1 text-[12px] text-accent-green">
              <Check class="h-3.5 w-3.5" /> Saved
            </span>
          )}
        </div>

        <p class="rounded-md border border-white/10 bg-white/[0.03] px-2.5 py-2 text-[12px] leading-relaxed text-ink-300">
          <span class="text-ink-100">This never replaces the coding agents.</span> Claude, Codex,
          Kimi, and Antigravity still run every prompt you send, with their own credentials and
          their own models. The auxiliary model only writes the short strings the platform itself
          needs — it never sees a workspace, never runs a tool, and never touches a chat's answer.
          Commit-message drafting sends the diff <em>shape</em> only: per-file line counts and the
          changed paths, never a line of your code.
        </p>
      </form>
    </section>
  );
}

/** One line per job, so the toggle says what it actually changes. */
const JOB_NOTES: Record<string, string> = {
  chatTitle:
    "A 3–6 word name in your own language, instead of the first 60 characters of the prompt.",
  runSummary:
    "One useful sentence in a Telegram or WhatsApp alert, instead of the raw tail of the reply.",
  commitMessage:
    "A conventional-commit subject in the pull-request dialog, instead of “Changes from Remote — <date>”.",
  translate: "The Translate buttons on a client message template.",
  chatSummary: "The one-line subtitle under a chat in the sidebar and on the dashboard.",
};

const LATENCY_NOTES: Record<"fast" | "usable" | "slow", string> = {
  fast: "fast enough for every job.",
  usable: "usable, though titles will land a moment after the run does.",
  slow: "slower than the jobs expect; consider a smaller model or a closer endpoint.",
};
